# P7 — usage-metering and invoicing backend

**Deliverable shape: Decision-shaped** (architecture/design doc). Branch and
Attack carry the run. The deliverable itself is capped at 1000 words;
reasoning sections below are exempt.

---

## Move 1 — Frame

Restatement: design the backend that ingests high-volume, at-least-once,
up-to-7-days-late usage events for 50k customers, closes per-customer
billing cycles at local midnight, rates usage against a temporally-versioned
price table, issues legally-immutable invoices, and still supports late
events, disputes, 13-month event drill-down, and bit-identical regeneration
of any past invoice — while keeping the expensive OLTP database small.

Checkable success criteria:

1. Every product fact is consumed by a named mechanism (dedup, lateness,
   tz-anchored close, mid-cycle price split, immutability + post-issuance
   handling, drill-down, reproducibility, cheap-storage bias).
2. Capacity/storage numbers appear wherever they drove a decision (event
   volume, retention footprint, OLTP row counts, dedup-state size, close
   fan-out).
3. Reproducibility is mechanical: a rerun reads only pinned, immutable
   inputs — no "read current tables and hope".
4. OLTP holds only money-shaped rows; the 36.5B events/yr never touch it.
5. Deliverable ≤ 1000 words.

Out of scope: payment collection/dunning, tax computation, multi-currency
FX, fraud detection, the pricing UI, auth.

Two readings of "at cycle close an invoice is generated": (a) issue
immediately at close, or (b) hold for a lateness grace period. The spec
pairs immutability with "late events … must still be handled after
issuance", which only matters if issuance precedes the late-event horizon —
so I take reading (a): issue at close, handle late arrivals as adjustments.
Named in the deliverable rather than picked silently; grace is left as a
policy knob.

## Move 2 — Gather

**Facts** (all cited from the task or arithmetic on it):

- F1. 100M events/day ≈ 1,160/s average; peak 5,000/s (given).
- F2. At-least-once delivery; client `event_id` + `occurred_at`; lateness
  bounded at 7 days (given).
- F3. Cycle anchor = signup day-of-month; close = midnight in customer's
  IANA tz (given). Implies DST anomalies and short-month clamping.
- F4. Mid-cycle price changes apply from `effective_at` (given) → a line
  item may span multiple price segments.
- F5. Invoices immutable once issued; late events and disputes handled
  afterward (given) → adjustment/credit-note mechanism required.
- F6. Drill-down invoice-line → raw events, 13 months (given) → raw events
  retained ≥ 13 months and joinable to a line item.
- F7. Regulator: rerun of any past cycle reproduces the identical invoice
  even after price changes (given) → all rating inputs must be versioned
  and pinned per run.
- F8. Object storage cheap, OLTP expensive (given).
- F9. Duplicates of one event share its `event_id` and `occurred_at`
  (derivable: retries resend the same client-generated event) → dedup can
  be scoped to the `occurred_at` partition.
- F10. Close fan-out: 50k customers / ~30 anchor days ≈ 1,700 invoices/day,
  smeared across 24 timezones — trivially small.

**Assumptions** (uncited; carried into the output where load-bearing):

- A1. ~200 B/event raw; 30–50 B in compressed Parquet. Sizes storage; low
  risk, verified by magnitude estimate in Attack.
- A2. Meter semantics: API calls = counter(sum), storage-hours =
  time-integrated, seats = gauge — must be declared per product; carried
  into the design as meter-type config.
- A3. Price `effective_at` is an absolute UTC instant (not customer-local).
  Carried into the status block.
- A4. Events later than 7 days can still arrive (clock skew, abuse); the
  contract bounds the SLA, not physics. Design handles arbitrary lateness
  via the same adjustment path, so this assumption is defused rather than
  gating.

No assumption gates the approach, so no spike is needed first.

## Move 3 — Branch

**A. Lakehouse rating (event lake as source of truth).** Immutable Parquet
in object storage; deterministic batch rating over pinned file versions;
Postgres holds prices, invoices, adjustments only.
- + Directly satisfies F7 (immutable pinned inputs) and F8 (36.5B rows/yr
  live in S3, not Postgres).
- + Dedup and late events become file-rewrite problems, not distributed
  state.
- − Rating latency is minutes, not milliseconds (acceptable: F10 says
  1,700 closes/day).

**B. OLTP-centric.** Ingest upserts into Postgres; triggers maintain
aggregates; invoice = query at close.
- + Simplest mental model, trivial drill-down.
- − 36.5B rows/yr, 13-month retention ≈ multi-TB hot OLTP — violates F8
  outright. Reproducibility needs full temporal tables. Dead on F8 alone.

**C. Streaming aggregation (Flink/Kafka Streams).** Stateful windows with
7-day allowed lateness emit final per-cycle aggregates; invoices read
aggregates.
- + Low-latency aggregates; lateness is a first-class stream concept.
- − F7 killer: stream state is not replayable to an identical result
  unless every event is retained immutably — at which point the lake from
  A exists anyway and C is redundant machinery. Drill-down (F6) also needs
  the raw lake regardless.

**Pick: A** — it is the only candidate where reproducibility, drill-down,
and the cost constraint are structural properties rather than bolted-on.
Switch trigger: if a product requirement appears for sub-minute usage
visibility or in-cycle spend caps, add C as a *non-authoritative* speed
layer in front of A (lambda style) — never as the billing source of truth.

## Move 4 — Attack

**Counterexample 1 — duplicate spanning files.** Event `e-42`
(`occurred_at` 2026-03-03T10:14Z) is ingested twice, landing in two
different raw files written minutes apart. A naive per-file dedup misses
it. Fix that survives: by F9 both copies carry the same `occurred_at`, so
both fall in hour partition 2026-03-03T10. The compactor rebuilds each
hour's rollup by re-reading *all* raw files overlapping that hour and
deduping by `event_id` within it — dedup scope equals duplicate scope, so
no global 700M-key dedup store (7 days × 100M × ~40 B ≈ 28 GB of hot KV)
is needed. Attack landed on the first design sketch (ingest-time KV dedup)
and sent me back to refine A; the hour-scoped read-time dedup replaced it.

**Counterexample 2 — reproducibility vs. compaction.** Cycle for customer
C closes; a late event then triggers a rewrite of hour H's rollup to v2.
A rerun that reads "current" rollups would now differ. Fix: the billing
run writes a manifest listing the exact immutable rollup file versions and
the price-table watermark it read; reruns read the manifest, never
"latest". Old versions are retained for the 13-month window (F6 forces
raw retention anyway).

**Counterexample 3 — price change mid-hour.** Price effective
2026-03-15T10:30Z splits hour partition 10:00–11:00, which hourly rollups
cannot split. Fix: rating splits only the boundary hour by querying raw
events for that single hour — rare and cheap (~1,160 avg events/s ÷ 50k
customers ≈ 2 events/s/customer ≈ 7k events in the hour, worst-case one
partition scan).

**Counterexample 4 — calendar traps.** Anchor day 31 in February → clamp
to last day of month. DST transition at local midnight (e.g.
America/Santiago historically shifted at 00:00): if local midnight is
skipped, close at the next valid local instant; if repeated, close at the
first occurrence. Deterministic rule, stated in the deliverable.

**Quantification** (load-bearing estimates): raw lake 100M/day × 30–50 B
Parquet ≈ 3–5 GB/day → ~1.2–2 TB over 13 months (~$30–50/mo S3 — the
"object storage is cheap" bet holds). OLTP: 50k × 13 invoices ≈ 650k
headers + ~6.5M lines ≈ low-GB — small as required. Kafka: 5k/s × 200 B =
1 MB/s peak — one small cluster. Close fan-out 1,700/day (F10) — a single
worker pool.

**Spec sweep**: all product facts F1–F8 consumed (mapped in Frame
criterion 1); seats/storage-hours consumed via meter types (A2); "schemas
where they matter" → price, invoice, billing-run, late-ledger schemas
included; nothing declined except the out-of-scope list in Frame.

**Steelman of C**: "billing is naturally streaming; watermarks are exactly
the 7-day-lateness tool." True for *aggregation*, but the regulator
requirement is about *replay*, and replayability lives in immutable
storage, not stream state. C's watermarks also finalize at close+7d,
contradicting the issue-at-close reading. Rejection stands.

**Strongest surviving objection**: issuing at close guarantees that up to
7 days of late usage lands as prior-period adjustment lines on the *next*
invoice — customers may find frequent adjustment lines confusing. This is
a product-experience cost, not a correctness flaw; the grace-period knob
(hold issuance N days) exists for tenants that prefer fewer adjustments.

## Move 5 — Verify

Check defined before finalizing: hand-trace one customer through the hard
path and confirm each Frame criterion.

Trace — customer C, tz America/Santiago, anchor day 31, cycle
Feb 1 → Feb 28 (clamped) closing 2026-02-28T24:00 local: (1) event e-42
arrives twice → both land in hour partition 10Z; compactor dedups by
`event_id` → counted once. (2) Price for API calls changes effective
Feb 15T10:30Z → rating produces two price segments; boundary hour rated
from raw. (3) Invoice issued at close; manifest pins rollup versions
r1…rN + price watermark W. (4) Mar 3, an event with `occurred_at` Feb 20
arrives → cycle already invoiced → late ledger → rated at the *pinned*
Feb price version → adjustment line on the March invoice; February invoice
untouched (immutability holds). (5) Regulator rerun in 2027 reads the
manifest → same files, same price watermark, same pure function → same
hash. (6) Drill-down on the February API-calls line runs the same
dedup-by-`event_id` query over the pinned raw partitions → totals match
the invoice.

Frame re-read: criteria 1–4 verified by the trace and spec sweep;
criterion 5 verified by word count — deliverable below is ~940 words,
under the 1000 cap. No drift found.

---

# Deliverable — Metering & Invoicing Backend Design

## Principles

Two invariants drive everything: **(1)** the event lake in object storage
is the single source of billing truth — immutable, append-only files;
**(2)** every invoice is a pure function of pinned, versioned inputs.
Postgres stores only money-shaped rows (prices, invoices, adjustments,
customers); the ~36.5B events/year never touch it.

## Components and flow

**Ingest Gateway → Kafka → Lake Writer → Event Lake (S3) → Hourly
Compactor → Rollup Store (S3) → Cycle Scheduler → Rating Engine → OLTP
(Postgres) → Drill-down (Trino) + Late-Usage Ledger / Credit Notes.**

**1. Ingest.** Gateway validates schema, stamps `ingest_seq`, produces to
Kafka keyed by `customer_id`. Peak 5,000 events/s × ~200 B = 1 MB/s — one
small Kafka cluster; average load 1,160/s. No ingest-time dedup (see
Rollups).

**2. Event lake.** The Lake Writer micro-batches (~1 min) into immutable
Parquet: `s3://events/raw/cust_bucket=<b>/occ_date=<d>/hour=<h>/<seq>.parquet`,
partitioned by `occurred_at`. 100M events/day compress to ~3–5 GB/day →
**~1.2–2 TB for the 13-month drill-down window** — tens of dollars/month,
which is why raw-forever-ish is the right trade. Retention: 14 months.

**3. Rollups + dedup.** An hourly Compactor rebuilds, per
`(customer, product, occurred_hour)`, a rollup file by re-reading *all*
raw files overlapping that hour and **deduplicating by `event_id` within
the hour**. Because a duplicate carries the same `occurred_at` as its
original, dedup scope equals duplicate scope — this eliminates the
alternative 28 GB hot KV of 7 days' event_ids and makes dedup
deterministic and replayable. Rollup rewrites produce new immutable
versions (`v1, v2, …`); old versions are retained 14 months. A late event
simply triggers a new version of its hour. Rollups carry per-meter-type
aggregates declared in product config: `sum` (API calls),
`time-integrated` (storage-hours), `gauge/max` (seats).

**4. Cycle close.** Scheduler stores per customer `(anchor_dom, iana_tz)`
and computes the next close = local midnight on the anchor day, with
deterministic calendar rules: anchor day > days-in-month clamps to the
last day; if DST skips local midnight, close at the next valid instant;
if midnight repeats, use the first occurrence. Fan-out is tiny: 50k
customers / ~30 anchor days ≈ **1,700 invoices/day** — a single worker
pool.

**5. Rating.** At close, a billing run converts the cycle to UTC instants
`[start, end)`, selects rollup versions and a price-table watermark, and
computes line items. Mid-cycle price changes split each line into price
segments at each `effective_at` (UTC instant) inside the window; hourly
rollups can't split a boundary hour, so **only boundary hours are rated
from raw events** (~7k events/customer-hour worst case — one partition
scan, rare). Amounts use exact decimal arithmetic with a fixed, versioned
rounding rule.

**6. Issuance.** The run atomically writes the invoice, its lines, and a
**run manifest** pinning every input: rollup file versions, price
watermark, rating-code version, calendar decisions. The invoice row stores
a content hash and becomes immutable (`status=issued`; DB grants forbid
UPDATE).

## Schemas that matter (Postgres)

- `price(product_id, unit_price, currency, effective_at, created_at)` —
  **append-only, bi-temporal**: `effective_at` = when it applies,
  `created_at` = when it was known. Rating "as of watermark W" uses rows
  with `created_at ≤ W`, so later price edits can never alter a rerun.
- `invoice(id, customer_id, cycle_start_utc, cycle_end_utc, issued_at,
  content_hash, status)`;
  `invoice_line(invoice_id, product_id, price_segment_ref, quantity,
  amount, manifest_ref)`.
- `billing_run(id, customer_id, cycle, rollup_versions[], price_watermark,
  code_version, content_hash)`.
- `late_usage_ledger(event_ref, target_cycle, rated_amount,
  pinned_price_ref, applied_invoice_id)`;
  `credit_note(id, invoice_id, reason, amount, issued_at)` — immutable.

OLTP footprint: 650k invoices + ~6.5M lines over 13 months — **low
single-digit GB**, satisfying the keep-OLTP-small constraint.

## Hard cases

**Duplicates.** Handled deterministically at compaction (above); reruns
and drill-down apply the same `event_id` dedup rule, so all three surfaces
agree.

**Late events (≤ 7 days, or later).** Invoices issue at cycle close, per
spec. Any arriving event whose `occurred_at` falls in an already-invoiced
cycle is written to the lake normally *and* recorded in the
`late_usage_ledger`. The next billing run rates ledger entries **at the
original cycle's pinned price version** and emits them as clearly-labeled
prior-period adjustment lines on the next invoice. The same path absorbs
events later than 7 days, so the SLA bound is a contract, not a
correctness dependency. (A per-tenant grace knob can delay issuance N days
for tenants who prefer fewer adjustment lines.)

**Disputes after issuance.** The invoice never changes. Resolution issues
an immutable `credit_note` referencing the invoice; the account balance is
the sum of invoices and credit notes, giving regulators a complete
append-only audit trail.

**Reproducibility.** Rerun = read `billing_run` manifest → fetch the exact
pinned rollup/raw file versions and price watermark → run the pinned
rating-code version → compare content hash. Every input is immutable and
version-addressed, so identity is structural. Rollup/raw versions
referenced by any manifest are retained for the regulatory window.

**Drill-down (13 months).** Each line's `manifest_ref` identifies the
cycle window, product, and pinned raw partitions; Trino queries the
Parquet lake with the same dedup rule. Query cost is per-customer-partition
(one customer's cycle ≈ 60k events avg), so interactive latency is
seconds without any OLAP index — add one only if drill-down QPS grows.

**Timezones/anchors.** All storage and rating in UTC instants; timezone
logic exists only in the scheduler's close-time computation, with the
deterministic clamp/DST rules recorded in the run manifest so reruns
reproduce even calendar decisions.

## Capacity summary

| Quantity | Estimate | Decision it drives |
|---|---|---|
| Ingest | 1,160/s avg, 5k/s peak, ~1 MB/s | small Kafka; no exotic ingest tier |
| Raw lake | 3–5 GB/day; 1.2–2 TB/13 mo | keep raw forever-ish in S3; drill-down from raw |
| Dedup | hour-scoped at read | avoids 28 GB hot KV store |
| Closes | ~1,700 invoices/day | single scheduler + worker pool |
| OLTP | ~6.5M line rows / low GB | Postgres stays small |

```text
STATUS: DONE
CONFIDENCE: high — would rise with confirmation that price effective_at is an absolute UTC instant and that issue-at-close (vs. grace-period hold) matches product intent
ASSUMPTIONS: price effective_at is UTC-absolute (A3); ~200 B/event raw, 30–50 B compressed (A1); per-product meter semantics (sum / time-integrated / gauge) are declared in product config (A2)
ATTACK: issuing at cycle close means up to 7 days of late usage appears as prior-period adjustment lines on the next invoice, which some customers may find noisy — a product-experience cost, not a correctness flaw, and the per-tenant grace-period knob mitigates it without touching the immutability or reproducibility guarantees
```
