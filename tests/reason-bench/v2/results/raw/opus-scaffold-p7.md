# P7 — usage metering & invoicing backend

**Deliverable shape: decision-shaped** (an architecture with several load-bearing
design choices). Branch and Attack carry the run. Secondary enumeration duty: the
"hard cases implied above" list must be swept exhaustively against the spec, so
Attack includes a full spec-element sweep. Reasoning below is exempt from the
1000-word cap; the design document at the end is the capped deliverable.

---

## 1. Frame

**Restated.** Design the backend that (a) ingests ~100M usage events/day from 50k
business customers over an at-least-once, up-to-7-days-late channel, (b) closes
per-customer billing cycles on a signup-anchored day-of-month at local midnight in
the customer's IANA timezone, (c) prices the cycle's usage against a price table
whose changes take effect mid-cycle, (d) emits a legally immutable invoice that can
still absorb late events and disputes, (e) supports 13 months of line-item →
raw-event drill-down, (f) reproduces any past invoice bit-identically after later
price-table edits, and (g) keeps the expensive OLTP database small while leaning on
cheap object storage.

**Success criteria (checkable).**

- C1 — Ingesting the same `event_id` N times changes no invoice total. Testable:
  replay a day's Kafka partition twice, diff rollups.
- C2 — For any past cycle, `rerun(cycle) == stored_invoice` byte-for-byte, after
  the price table has been edited. Testable: nightly re-run of sampled cycles,
  compare a digest.
- C3 — Cycle boundary for customer in zone Z with anchor day D equals the UTC
  instant of local midnight on the clamped D, for all Z including 45-minute
  offsets and DST-skipped midnights. Testable: table-driven unit tests.
- C4 — An event arriving after issuance with `occurred_at` inside a closed cycle
  is billed exactly once, and the issued invoice row is unchanged. Testable:
  inject a straggler, assert invoice immutability + one adjustment line.
- C5 — OLTP steady-state size is bounded and independent of event volume
  (target: < 10 GB at 13 months). Testable: row-count arithmetic.
- C6 — Drill-down from any 13-month-old line item returns the contributing events;
  target < 5 s and < 100 MB scanned.
- C7 — A price change with `effective_from` mid-cycle produces line items split at
  that instant.

**Out of scope.** Payment collection/dunning, tax engines, currency/FX, revenue
recognition (ASC 606), contract/entitlement management, the customer-facing UI,
and quota *enforcement* (I will note where a real-time counter would attach, but
enforcement is a different latency contract than billing).

**Two materially different readings — I name both rather than pick silently.**

- R1 — *"Invoice at cycle close, absorb stragglers later."* Issue on schedule with
  a short cutoff; events arriving after the cutoff become prior-period adjustments
  on the next invoice.
- R2 — *"Invoice is issued only once complete."* Delay issuance until cycle end +
  7 days so the invoice is provably complete on first issue.

R2 makes every customer's invoice a week late and still doesn't close the hole —
the 7-day bound is a *stated* delivery property, and a broken client can violate
it. R1 is standard billing practice and is required anyway for disputes. **I build
R1, with `close_lag` as a tunable that can be pushed to 7 days per-customer if a
regulator or contract demands R2.** Flagged in the deliverable.

---

## 2. Gather

**Facts** (all cited from the task, or arithmetic directly derived from it).

- F1 — 50,000 customers; 100M events/day fleet average; peak 5,000 events/s. [task]
- F2 — Average rate = 100e6 / 86,400 = **1,157 ev/s**; peak/avg ratio ≈ **4.3×**. [derived from F1]
- F3 — Per customer per day: 100e6 / 50,000 = **2,000 events/customer/day**. [derived from F1]
- F4 — At-least-once delivery, duplicates possible; every event carries a
  client-generated `event_id` and an `occurred_at`. [task]
- F5 — Events arrive up to 7 days late. [task]
- F6 — Cycle anchored to signup day-of-month, closes at local midnight in the
  customer's IANA zone. [task]
- F7 — Line items per metered product; **mid-cycle price changes apply from their
  effective timestamp**. [task]
- F8 — Invoices legally immutable once issued; late events and disputes must still
  be handled post-issuance. [task]
- F9 — Line item → raw event drill-down, 13 months back. [task]
- F10 — Regulator requires bit-reproducibility of past invoice generation *even
  after later price-table changes*. [task]
- F11 — Object storage cheap; OLTP expensive and must stay small. [task]
- F12 — Metered products named are API calls, storage-hours, and seats. [task] —
  note these are **three different meter semantics**, not three names for the same
  thing: a counter, a time-integrated gauge, and a level.
- F13 (domain) — Live IANA UTC offsets are all whole multiples of 15 minutes;
  non-hour examples include `Asia/Kolkata` +05:30, `Asia/Kathmandu` +05:45,
  `Australia/Eucla` +08:45, `Pacific/Chatham` +12:45/+13:45.
- F14 (domain) — Local midnight does not always exist: some zones perform the DST
  spring-forward *at* 00:00 (historically `America/Santiago`, `America/Havana`,
  `Asia/Beirut`), so 00:00 is skipped; autumn transitions can make it occur twice.
- F15 (domain) — Day-of-month anchors 29/30/31 do not exist in every month, so an
  anchor rule must specify clamping.
- F16 (domain) — The IANA tzdata database is revised several times a year and
  revisions change future UTC offsets, which moves the UTC instant of a *future*
  local midnight.
- F17 (domain) — Open table formats (Iceberg/Delta) expose immutable snapshot ids
  with time travel; this is exactly a pinned-input primitive.

**Assumptions** (uncited — carried into the output, not absorbed).

- A1 — Average serialized event ≈ **300 bytes** (ids, product, quantity, two
  timestamps, small metadata). All storage numbers scale linearly in this.
- A2 — Columnar compression ≈ **6:1** on this shape (low-cardinality dictionary
  columns dominate) → ~5 GB/day Parquet.
- A3 — Roughly **8 metered products** per customer, so ~8 line items/invoice.
- A4 — The late tail is thin: ~**0.5%** of events arrive more than 24 h after
  `occurred_at`. *This is the one assumption that changes a design parameter*
  (`close_lag`) rather than a design shape — see Attack.
- A5 — "Legally immutable" means the issued document cannot be altered, but a
  *new* immutable document (credit/debit note) may correct it. This is how
  invoicing law generally works, but it is a legal question, not an engineering
  one, and it gates the whole R1 reading.
- A6 — Reproducibility must be byte-identical on the *invoice document/amounts*,
  not on incidental metadata (run timestamp, job id).

**A5 gates the approach**, so per the scaffold rule the first element of the
deliverable is not a design built on the guess: the design makes the
credit-note-vs-reissue choice an explicit, isolated boundary and states what
changes if A5 is false (answer: `close_lag` → 7 days, R2 mode, same machinery).

---

## 3. Branch

Three genuinely different shapes — different source of truth, not parameter tweaks.

**Candidate A — Incremental counters in OLTP.** Streaming consumer maintains
`usage_balance(customer, product, cycle)` in Postgres, incremented per event;
dedup via an `event_id` seen-set table; invoice = read the counters.

- C1 dedup: needs a durable seen-set over the 7-day window = 7 × 100M = **700M
  keys**; at ~40 B/key that is **~28 GB** of hot state, in the store F11 says must
  stay small. ✗
- C2 reproducibility: counters are mutated in place; there is no input set to
  re-run against. Would need a separate event log anyway. ✗
- C6 drill-down: no raw events retained without a second store. ✗
- Only win: freshest real-time usage. Not a billing requirement.

**Candidate B — Lakehouse ledger, recompute-on-demand.** The immutable event log
in object storage is the single source of truth. Rollups and invoices are pure
functions of (pinned log snapshot, pinned price snapshot, pinned engine version).
OLTP holds only cycles, prices, invoices, notes.

- C1: dedup becomes `DISTINCT event_id` inside a recomputed partition — no
  stateful seen-set at all (see the partitioning insight in Attack). ✓
- C2: pinned snapshot + bitemporal prices + pinned engine image = a pure function. ✓
- C5: OLTP rows scale with *invoices*, not events. ✓
- C6: raw events are already the store of record. ✓
- Cost: recompute is not free — quantified in Attack.

**Candidate C — Double-entry journal.** Every event is rated on arrival and posts
an immutable journal entry; the invoice is a period query over the journal;
corrections are contra-entries. Maximally auditable, and adjustments/disputes are
native.

- Volume: 100M postings/day → **36.5B rows/yr** in a transactional ledger store.
  Flatly incompatible with F11. ✗ at event grain.
- But at *invoice/credit-note* grain the same idea is ~5M rows/yr and gives exactly
  the immutability semantics F8 demands.

**Pick: B, with C absorbed at the document grain.** One-line why: only B makes the
invoice a *pure function of pinned inputs*, which is the single property F10 asks
for, and it puts every event-scale byte in the cheap store F11 prefers.

**Switch trigger:** if a hard requirement appears for sub-second, exactly-correct
live balances (e.g. prepaid credit enforcement that must never over-serve), add A's
streaming counter as an *approximate, non-billing* sidecar — do not promote it to
source of truth. If A5 is false (no credit notes permitted), switch R1→R2 by
setting `close_lag = 7d`; the architecture is unchanged.

---

## 4. Attack

### 4.1 Concrete counterexamples, with real values

**CE1 — The 45-minute offset breaks hourly rollups.** Customer in
`Pacific/Chatham`, anchor day 31, February cycle. Clamped local close =
Feb 28 24:00 local; Chatham is on +13:45 in February (southern DST), so the UTC
instant is **Feb 28 10:15 UTC**. An hourly rollup bucket `[10:00, 11:00)` *straddles
the cycle boundary* — the invoice would need to split a bucket it cannot split
without re-reading raw events. **This attack lands.** Back to Branch on one
parameter: rollup granularity becomes **15 minutes**, which is a bucket edge for
every live offset (F13). Cost check: 96 buckets/day vs 24 is 4× the rollup rows,
but rollups live in object storage, not OLTP, so this is ~free.

**CE2 — Anchor-day ratcheting.** Signup Jan 31. If the next boundary is derived
from the *previous* boundary, Feb clamps to the 28th and March then anchors to the
28th — the anchor ratchets down permanently and the customer is silently rebilled
on the wrong day forever. Fix carried into the design: store `anchor_day = 31`
immutably and clamp **per month** (Jan 31 → Feb 28 → **Mar 31**).

**CE3 — Skipped midnight.** `America/Santiago` spring-forward at 00:00: the local
day has no 00:00:00. A naive `localize(date, 00:00)` throws or silently rolls back
an hour. Rule carried into the design: nonexistent → first valid instant after;
ambiguous (fall-back) → the first (earlier) occurrence.

**CE4 — tzdata drift moves an already-scheduled boundary.** A tzdata release in
March changes a zone's October DST rule. A cycle boundary computed in advance now
resolves to a different UTC instant, and C2 fails for a cycle whose boundary
"moved". Fix: **freeze `cycle_start_utc`/`cycle_end_utc` at cycle creation** and
record `tzdata_version` alongside.

**CE5 — Duplicate arriving after the rollup ran.** `event_id=E`, `occurred_at`
Jun 1, arrives Jun 1 and again Jun 6 — after the Jun 1 rollup, and possibly after
the May–Jun invoice issued. Does it produce a spurious $-adjustment? Trace: the
event log is partitioned by **`occurred_at`**, which is event *content*, so the
duplicate lands in the *same* partition as the original. The rollup **recomputes**
that partition as `DISTINCT event_id`, so the recomputed quantity is unchanged.
The adjustment engine computes `delta = rate(snapshot_now) − rate(snapshot_pinned)`
= **0** → no adjustment line. C1 and C4 both hold. *This is the load-bearing
insight of the whole design*: partitioning on content rather than arrival converts
global deduplication into a local `DISTINCT`, eliminating the 28 GB seen-set that
killed Candidate A.

**CE6 — A partition that never seals.** A buggy client backfills events with
`occurred_at` two years old. Those rows reopen a partition whose invoices are long
issued, forcing unbounded recompute and adjustment cascades. Fix: F5's 7 days is a
**contract**, so enforce it — reject `occurred_at` older than 8 days (7 + slack) or
more than 5 minutes in the future into a `quarantine` table (visible, never a
silent drop). Without this, C2 and the compaction schedule have no termination
point.

**CE7 — Tiered pricing across a price change.** Cycle usage 1.5M calls; tier 2
starts at 1M; price changes at day 12 when cumulative usage is 600k. Does tier
counting restart at the price change? Two defensible answers; the amounts differ
materially. Design decision recorded: **tier counters accumulate across epochs
(usage is cycle-cumulative), each epoch priced at its own rate.** Flagged in the
deliverable as a business rule, not a technical one.

### 4.2 Magnitude estimates

| Quantity | Estimate | Consequence |
|---|---|---|
| Peak ingest bytes | 5,000/s × 300 B (A1) = **1.5 MB/s** | Ingest is *not* the hard part; a 3-broker Kafka is over-provisioned. Design effort belongs downstream. |
| Raw volume | 100M × 300 B = **30 GB/day** | — |
| Parquet volume (A2) | **~5 GB/day** | — |
| 13-month lake | 395 d × 5 GB = **~2 TB** → ~$46/mo at $0.023/GB-mo | F11 confirmed quantitatively: retention is a rounding error. Even uncompressed (~12 TB, ~$275/mo) it is cheap. |
| Kafka replay buffer | 8 d × 30 GB = **240 GB** | Fits on one broker set; replay from source is viable. |
| Naive dedup state | 7 d × 100M × 40 B = **~28 GB** hot | The number that kills Candidate A. |
| Rollup recompute, naive | 8 open days × 5 GB × 24 runs/day = **~1 TB/day** scanned ≈ $150/mo at $5/TB | Affordable but wasteful. |
| Rollup recompute, dirty-partition-only | ~10% of partitions touched/hour → **~100 GB/day** | ~10× cheaper; adopt. |
| OLTP rows, 13 months | 650k invoices + ~5.2M lines (A3) + prices + cycles ≈ **< 5 GB** | C5 met with an order of magnitude of headroom. |
| Late events (A4) | 0.5% × 100M = **500k/day** ≈ 10/customer/day | Nearly every customer would get an adjustment line → **materiality threshold needed** (carry sub-$0.50 deltas forward). This estimate directly created a design element. |
| Drill-down scan after customer-bucketed compaction | 2,000 rows/customer-day (F3) × ~30 B compressed × 30 d ≈ **~2 MB** | C6 met with margin. Without the re-clustering it would be ~1 GB/query. |

### 4.3 Spec sweep — every given element consumed or declined

| Spec element | Where consumed |
|---|---|
| 50,000 customers | Kafka keying, `bucket(customer_id, 64)`, OLTP sizing |
| 100M events/day | Storage + recompute-cost estimates |
| Peak 5,000 ev/s | Ingest sizing (shown to be non-binding) |
| At-least-once / duplicates | `DISTINCT event_id` per `occurred_at` partition (CE5) |
| 7-day lateness | 8-day open window, seal at D+8, quarantine beyond (CE6), `close_lag` |
| `event_id` | Dedup key |
| `occurred_at` | **Partition key** (the load-bearing choice) + price-epoch join key |
| Signup day-of-month anchor | `anchor_day` + per-month clamping (CE2) |
| Customer IANA timezone / midnight | Boundary resolution rules (CE1, CE3, CE4) |
| Line items per metered product | `invoice_line` grain |
| Price table | Bitemporal `price_version`, append-only |
| Mid-cycle price effective timestamp | Price epochs; per-event price join (CE7) |
| Legal immutability | No UPDATE on `invoice`; credit/debit notes (Candidate C absorbed) |
| Post-issuance late events | Prior-period adjustment lines + materiality threshold |
| Disputes | `dispute` row → optional credit note |
| Drill-down to raw events | Line item stores `(customer, product, window, snapshot_id)`; lake query |
| 13 months | Lake retention **and** snapshot-expiry retention **and** engine-image retention |
| Reproducibility after price changes | Pinned `lake_snapshot_id` + `price_as_of` + `engine_image_digest` + `input_digest` |
| Cheap object storage | Everything event-scale |
| Expensive small OLTP | Only documents and dimensions; < 5 GB |
| API calls / storage-hours / seats (F12) | **`meter_type` with three aggregation semantics** — the element most easily missed as decoration; storage-hours are a time-integrated gauge and seats a level, neither is a `SUM` of counts |

Nothing in the spec is left unconsumed.

### 4.4 Re-checking load-bearing assumptions

- A1/A2 (300 B, 6:1): only scale the storage numbers. Even at 1 KB/event and 3:1,
  the 13-month lake is ~11 TB ≈ $250/mo — the conclusion "keep it all in object
  storage" is robust to a 3× error. **Not load-bearing on shape.**
- A4 (0.5% late): sets `close_lag` and adjustment volume. If the true figure is 5%,
  adjustments become noisy and `close_lag` should stretch toward 72 h — a parameter
  change, not a redesign. Mitigation carried into the design: derive `close_lag`
  from *measured* P99.9 arrival lag rather than hard-coding it.
- A5 (credit notes are legally acceptable): **genuinely load-bearing and
  unverified.** Carried into ASSUMPTIONS and named in the deliverable with its
  fallback (R2 / `close_lag = 7d`).
- A3 (8 products): only OLTP sizing; 10× error still leaves OLTP under 50 GB.

### 4.5 Steelman of the strongest rejected candidate

Candidate C (event-grain double-entry) is the strongest rejection. Its case: an
auditor's ideal system is a journal where every number traces to an immutable
posting, adjustments are contra-entries by construction, and there is no
"recompute" step to trust. It is *more* auditable than B, because B's
reproducibility rests on the claim that a job re-run is deterministic, whereas C's
rests on rows that were written once. My rebuttal is purely economic — 36.5B
postings/yr in the store F11 says must stay small — and economics can change.
**So I adopt C's semantics at the grain where they are affordable**: invoices,
credit notes, debit notes and adjustments *are* an append-only journal, and the
customer balance is `Σinvoices − Σcredits`. B and C are layered, not opposed. If
event-grain journaling ever becomes cheap, the migration is to promote the rollup
table into the journal without touching the document layer.

### 4.6 Strongest surviving objection

**Reproducibility over 13 months is an operational liability, not just a schema
property.** Three things must survive 13 months in runnable form: (i) Iceberg
snapshots — `expire_snapshots` must be configured to ≥ 13 months, which directly
fights compaction's file deletion and inflates metadata; (ii) the rating engine
container image, byte-pinned, meaning a 13-month-old image with a possibly
CVE-ridden base must remain pullable and runnable; (iii) the bitemporal price
history with no destructive backfills, ever. Any routine housekeeping action —
a storage lifecycle rule, a registry GC, a "clean up old snapshots" runbook — can
silently break C2, and the breakage is invisible until a regulator asks. It does
not kill the design (the mitigation is the nightly re-run-and-compare canary in
§5, which converts a silent failure into a loud one within 24 h), but it means the
regulator requirement is a *permanent operational obligation*, not a feature that
ships once.

---

## 5. Verify

**Check defined before the answer was final:** hand-trace CE1/CE2/CE5 end-to-end
with real values, then re-read Frame's C1–C7.

**Hand-trace (Chatham, anchor 31, February).** `anchor_day=31`; February clamps to
28; local close Feb 28 24:00 = Mar 1 00:00 local; Chatham +13:45 → **Feb 28 10:15
UTC**, frozen at cycle creation with `tzdata_version=2026a`. 10:15 is a 15-minute
bucket edge ✓ (C3). March clamps back to 31 ✓ (CE2). Rating pins Iceberg snapshot
`S=8812…`, `price_as_of=2026-03-01T10:20Z`, `engine=sha256:ab…`. On Mar 4 a
duplicate of an already-counted Feb 20 event arrives: it lands in partition
`occurred_at=2026-02-20`, the recompute yields the same `DISTINCT` count,
`delta = 0`, no adjustment ✓ (C1, C4, CE5). On Mar 4 a *genuinely new* Feb 27 event
arrives worth $0.12: below the $0.50 materiality threshold, so it accrues and rides
the next material adjustment ✓ (C4). In April the price table is corrected
retroactively; re-running February reads `recorded_at <= 2026-03-01T10:20Z`, so the
corrected row is invisible and the digest matches ✓ (C2).

**Criterion re-check.** C1 ✓ (CE5). C2 ✓ (pinned triple + nightly canary). C3 ✓
(CE1/CE3/CE4 rules). C4 ✓ (trace above; invoice row never updated). C5 ✓ (<5 GB,
no event rows in OLTP). C6 ✓ (~2 MB scan post-compaction). C7 ✓ (price epochs with
`epoch_start/end` on the line item).

**Drift check against Frame.** No section of the deliverable addresses anything
outside the seven criteria; payments, tax and FX stayed out of scope. The one place
the answer exceeds the literal ask is the `meter_type` taxonomy — justified because
F12 names three products with three different aggregation semantics, and treating
them as one would be the missed requirement the scaffold's spec sweep exists to
catch.

**Residual gaps I am not papering over:** A5 (legal acceptability of credit notes),
and the fact that determinism of the rating job is *asserted* and only continuously
*evidenced* by the canary, never proved.

---
---

# DELIVERABLE — Usage metering & invoicing: architecture

*(~980 words)*

**Gating assumption, stated first because the design leans on it:** issued invoices
are corrected by *new* immutable documents (credit/debit notes), not reissue. If
legal says otherwise, set `close_lag = 7 days` so invoices issue complete; nothing
else changes.

**Principle.** The immutable event log in object storage is the only source of
truth. Rollups, invoices and drill-downs are *deterministic functions* of (pinned
log snapshot, pinned price snapshot, pinned engine version). Nothing billable is
incrementally mutated.

## Components

1. **Ingest gateway** — stateless. Validates, stamps `received_at`; routes
   `occurred_at` older than 8 days (7 + slack) or >5 min future to a visible
   `quarantine` table — never a silent drop. The 7-day bound is a contract;
   unenforced, partitions never seal and reproducibility has no terminus.
   Peak 5,000 ev/s × ~300 B = **1.5 MB/s** — ingest is not the hard part.
2. **Kafka**, keyed by `customer_id`, 8-day retention (**240 GB**) — the replay buffer.
3. **Lake writer** (Flink/Spark) — commits Parquet to Iceberg `events_raw` every 60 s.
4. **Compactor/sealer** — hourly compaction of open partitions; at D+8 the partition
   is *sealed* and rewritten clustered by `customer_id`.
5. **Rollup job** — *recomputes* (never increments) `usage_rollup` for any
   (day, customer_bucket) partition that gained rows.
6. **Cycle scheduler** — materializes each customer's boundaries as frozen UTC instants.
7. **Rating engine** — pure, version-pinned container:
   `rate(events, price_snapshot, engine_version) → line items`.
8. **Invoice service** (OLTP) — issues invoices, credit/debit notes, adjustments.
9. **Drill-down API** — Trino/DuckDB over the lake.

## Stores

**Object store (Iceberg).**
`events_raw(event_id, customer_id, product_id, meter_type, quantity, occurred_at,
received_at, payload)`, partitioned `days(occurred_at) + bucket(customer_id,64)`,
sorted by `customer_id, occurred_at`.
Partitioning on `occurred_at` — event *content*, not arrival — means **a duplicate
always lands in the same partition as its original**, so dedup is a per-partition
`DISTINCT event_id`. No global seen-set exists anywhere; the naive alternative is
7 d × 100M × 40 B ≈ **28 GB** of hot state in the store that must stay small.
`usage_rollup(customer_id, product_id, bucket_start_utc /*15 min*/, qty, src_snapshot_id)`.

**OLTP — small by construction, no event ever enters it.**
- `price_version(product_id, tier_spec, effective_from, effective_to, recorded_at,
  version_id)` — bitemporal, **append-only**; corrections are new rows.
- `billing_cycle(customer_id, cycle_seq, anchor_day, tz, cycle_start_utc,
  cycle_end_utc, tzdata_version, status)`
- `invoice(id, customer_id, cycle_id, issued_at, lake_snapshot_id, price_as_of,
  engine_image_digest, input_digest, total)` + `invoice_line(invoice_id, product_id,
  epoch_start, epoch_end, quantity, unit_price, amount)`
- `credit_note`, `debit_note`, `adjustment`, `dispute`.

13 months ≈ 650k invoices + ~5.2M lines ≈ **< 5 GB**.

## Flow

ingest → Kafka → Iceberg (`occurred_at` partitions) → dirty-partition rollup → at
`cycle_end_utc + close_lag` the scheduler pins the current Iceberg snapshot, the
price-table transaction time, and the engine image digest; rates; writes the invoice
plus `input_digest`.

## Hard cases

**Cycle boundaries.** `anchor_day` is the signup day, stored once and clamped *per
month*: Jan 31 → Feb 28 → **Mar 31**. Deriving each boundary from the previous one
ratchets the anchor down permanently. Local midnight → UTC: nonexistent (DST
spring-forward at 00:00, e.g. `America/Santiago`) → first valid instant;
ambiguous → the earlier occurrence. Instants are **frozen at cycle creation** with
`tzdata_version`, so a later tzdata release cannot move an issued cycle. Rollups use
**15-minute** buckets because every live IANA offset is a multiple of 15 min
(`Asia/Kathmandu` +05:45, `Pacific/Chatham` +12:45/+13:45); hourly buckets would
straddle those boundaries. Worked case: Chatham, anchor 31, February close =
**Feb 28 10:15 UTC** — exactly a bucket edge.

**Mid-cycle price changes.** Split the cycle into *price epochs* at each
`effective_from`; each event joins the price row covering its `occurred_at`. Tier
counters accumulate across epochs (usage is cycle-cumulative) while each epoch
prices at its own rate — a **business rule, flagged**, since the alternative
(tiers reset per epoch) yields materially different amounts. Line items carry
`epoch_start/end`, so the split is visible on the invoice.

**Meter semantics.** `meter_type ∈ {counter, gauge_sample, interval}` with
aggregation `sum | time-weighted | max`: API calls sum, storage-hours time-integrate
their samples, seats take max (or prorate) per epoch. These are three different
computations, not three labels.

**Duplicates & 7-day lateness.** Dedup by recompute (above). `close_lag` = measured
P99.9 arrival lag, capped at 48 h. Events landing after the pinned snapshot but
inside a closed cycle are rated at *their own* `occurred_at` prices and emitted as a
"Prior-period adjustment (cycle X)" line on the next invoice, itself drill-downable.
At ~0.5% late that is **500k events/day ≈ 10 per customer per day**, so nearly every
customer would otherwise get a noise line: deltas below a materiality threshold
(e.g. $0.50) accrue and ride the next material adjustment. A pure duplicate yields
`delta = 0` and produces no line at all.

**Immutability + disputes.** Issued invoices are never updated. Corrections are
immutable `credit_note`/`debit_note` rows referencing `(invoice_id, line_id)`;
balance = Σinvoices − Σcredits. A `dispute` attaches to a line item and may spawn a
credit note. This is double-entry semantics at document grain — affordable at ~5M
rows/yr, where event-grain journaling would be 36.5B.

**Reproducibility.** Re-running cycle C reads `lake_snapshot_id` (Iceberg time
travel), `price_version WHERE recorded_at <= price_as_of`, and the pinned engine
image — a pure function, so a later price edit cannot perturb it; `input_digest`
proves it. **Operational obligation:** snapshot expiry ≥ 13 months, engine images
retained 13 months, no destructive price backfills — routine housekeeping can break
this silently, so a nightly canary re-runs sampled past cycles and alerts on digest
mismatch within 24 h.

**Drill-down.** Each line item stores `(customer_id, product_id, cycle window,
lake_snapshot_id)`. Post-seal files are customer-bucketed, so one customer-day is
~2,000 rows ≈ 60 KB and a full-cycle drill-down scans **~2 MB** — versus ~1 GB
without the re-clustering.

## Numbers that drove decisions

100M/day × 300 B = 30 GB/day raw → ~5 GB/day Parquet → **~2 TB / 13 months ≈
$50/mo** object storage: retention is free, so keep everything. Recomputing the full
8-day open window hourly would scan ~1 TB/day (~$150/mo); dirty-partition-only cuts
it ~10×. OLTP < 5 GB.

```text
STATUS: DONE_WITH_CONCERNS
CONFIDENCE: medium — would rise to high with (a) legal confirmation that credit notes satisfy invoice immutability, and (b) a measured arrival-lag distribution to set close_lag instead of the assumed 0.5% late tail
ASSUMPTIONS: (A5, load-bearing) issued invoices may be corrected by new immutable credit/debit notes rather than reissue — if false, run close_lag = 7 days and issue only complete invoices; architecture unchanged. (A4) ~0.5% of events arrive >24 h late, which sets close_lag and adjustment volume. (A1/A2) ~300 B/event at ~6:1 columnar compression — a 3× error still leaves object storage under $300/mo, so the "retain everything" conclusion is robust. (A3) ~8 metered products per customer, affecting only OLTP sizing.
ATTACK: 13-month reproducibility is a standing operational obligation, not a shipped feature — it requires Iceberg snapshots to survive compaction and lifecycle rules, the rating container image to remain pullable and runnable for 13 months, and the price table to never be destructively backfilled. Any ordinary cleanup runbook can break the regulator guarantee invisibly. It does not kill the design because the nightly re-run-and-compare canary converts a silent breakage into a loud alert within 24 hours, but the guarantee rests on that canary staying alive, not on the schema alone.
```
