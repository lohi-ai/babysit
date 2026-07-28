# P7 — Usage metering & invoicing backend

## Reasoning notes (exempt from the word cap)

The problem is not throughput — 100M events/day is ~1,160/s average, 5,000/s
peak, which is a handful of nodes. The problem is that four requirements are in
tension:

- **Immutable invoices** vs **7-day-late events**: an invoice cannot wait 7 days
  to issue (billing latency) and cannot change after issuing. So late data must
  land somewhere that is *not* the invoice → adjustment documents.
- **Reproducibility** vs **mutable price table + restated usage**: replay must
  read the same inputs the original run read. That forces (a) a bitemporal price
  table pinned by knowledge-time, and (b) usage input pinned by a *content
  manifest*, not by "whatever is in the partition now."
- **13-month drill-down** vs **small OLTP**: raw events must live in object
  storage; OLTP holds only identity, prices, cycles, invoices, and the currently
  open cycle's rollups.
- **Drill-down must agree with the invoice**: if a late event restates a day's
  usage, the invoice's drill-down must still show what was billed. Solved by the
  same manifest pinning — the invoice pins a manifest *version*.

One non-obvious trap worth calling out in the doc: with tiered/graduated pricing
a late event cannot be priced in isolation (it may cross a tier boundary), so
adjustments must recompute the whole cycle and bill the delta.

Second trap: local midnight does not always exist (DST spring-forward), and
tzdata updates retroactively move offsets — so cycle boundaries must be resolved
to a UTC instant *once* and stored.

---

# Deliverable

## Invariants

1. **The raw event log in object storage is the only source of truth.** Rollups,
   OLTP rows, and invoices are derived and rebuildable from it.
2. **Nothing an invoice reads is ever mutated.** Every input is pinned by content
   hash or knowledge-time, so a replay reads the exact bytes of the original run.

## Components & flow

`Ingest API → Kafka → Dedup+Landing → raw log (S3) → Rollup job → rollups →
Invoice engine → invoice + snapshot`. Plus: cycle scheduler, late-event router,
drill-down query service, replay verifier.

**Ingest API** (stateless): validates, stamps `received_at`, writes to Kafka
partitioned by `customer_id`, 14-day retention. 5k/s × ~350 B = 1.8 MB/s — three
small nodes.

**Dedup**: sharded RocksDB keyed by a 128-bit hash of `(customer_id, event_id)`,
TTL 21 days (3× the late window). 7 d × 100M = 700M live keys × ~20 B ≈ **15 GB**
over 8 shards on local NVMe. 64-bit keys would collide (~1% fleet-wide by
birthday bound); 128-bit is negligible. Dedup runs *before* landing, so every
stage downstream is freely replayable.

**Landing writer**: Parquet+zstd, partitioned `dt=<occurred_at UTC date>/cb=<customer_id mod 64>`,
sorted by `(customer_id, occurred_at)` so row-group min/max stats prune a
drill-down to a few MB. ~40 B/event compressed → **4 GB/day**, 64 files of ~64 MB.
Partitions are **append-only file sets, never rewritten**: a late event lands a
*new* file in the old `dt`. A `partition_manifest(dt, cb, version, files[], sha256)`
row makes each partition state content-addressable.

**Rollup job**: reads new files → hourly buckets `(customer, product, dimension,
hour_utc, qty, event_count)` in Parquet, plus a customer-local-day copy in OLTP
for *open cycles only*, dropped at issue. Deterministic: same manifest version in,
same rollups out.

## Stores

| Store | Holds | Size |
|---|---|---|
| Object storage (raw) | events, 13 mo | ~1.6 TB (4 GB/d × 400 d) |
| Object storage (rollups, snapshots) | hourly rollups; invoice docs under Object-Lock, 7 yr | ~100 GB |
| OLTP (Postgres) | customers, prices, cycles, invoices, adjustments, open rollups | **< 50 GB** |
| RocksDB | dedup keys | 15 GB |

Lifecycle: Standard → IA at 30 d → Glacier IR at 90 d → expire at 13 mo + margin.

**Price table (bitemporal, insert-only):**
`price_version(price_book_id, product, dimension, tier_spec jsonb, effective_from,
effective_to, recorded_at, superseded_at)`. `effective_*` = business time;
`recorded_*` = knowledge time. Corrections insert a new row at a new
`recorded_at`; nothing is updated.

**Cycle:** `billing_cycle(customer_id, seq, tz, anchor_dom, starts_at_utc,
ends_at_utc, tzdata_version, state)`. Boundaries are resolved to UTC **once at
creation** and stored, so later tzdata releases cannot move a closed cycle.
Day-of-month 29–31 clamps to month end. If local midnight does not exist
(spring-forward gap) use the first instant after the gap; if ambiguous
(fall-back) use the earlier occurrence.

**Invoice snapshot** (object storage, WORM): cycle bounds, `price_book_id` +
knowledge timestamp, watermark (max `received_at` consumed), the list of
`partition_manifest` versions consumed, engine version, line items, and a
SHA-256 over the canonical JSON stored on the OLTP `invoice` row.

## Hard cases

**Duplicates** — dedup store; rollups additionally idempotent per manifest
version, so a replay after a crash converges rather than double-counting.

**Late events (≤ 7 d)** — routed by `occurred_at` vs cycle state. Open cycle:
normal path. Closed-and-issued cycle: land in the raw partition (new manifest
version), then recompute that cycle at its **pinned price book + knowledge time**
with the new quantities and bill the *delta* as a `credit_note`/`debit_note`
referencing the original invoice. Recomputing the whole cycle (not pricing the
event alone) is required because tiered pricing is path-dependent. Deltas below a
materiality threshold (e.g. $1) accrue onto the next invoice instead of
generating a document.

**Issue timing** — close at the local-midnight instant, issue after a 2 h grace
using the watermark. Waiting 7 days for completeness is rejected; adjustments are
the completeness mechanism.

**Mid-cycle price change** — the engine segments usage at `effective_from`
boundaries. Hourly buckets that straddle a boundary are re-read from the raw log
and split exactly; price changes are rare, so this scan is cheap.

**Immutability + disputes** — `invoice` and its snapshot are append-only and
Object-Locked. Every post-issue change is a *new immutable document* linked by
`invoice_adjustment(invoice_id, kind, reason, amount, source_ref)`.

**Reproducibility** — replay = snapshot's manifest versions + knowledge time +
engine version → recompute → compare hash. Deterministic because inputs are
pinned by content, never by "current state." A nightly verifier replays a random
sample of past cycles and alarms on hash mismatch, so drift is caught before a
regulator asks.

**Drill-down** — line items carry `(customer, product, dimension, window,
price_version, manifest_versions)`. Query service (Trino/DuckDB over S3) prunes on
`dt`/`cb` + row-group stats. Reading the invoice's *pinned* manifest versions
guarantees the drill-down always sums to the billed amount; a separate "current"
view shows late events and links to the adjustment that captured them.
