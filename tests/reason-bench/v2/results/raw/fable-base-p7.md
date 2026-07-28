# P7 — Usage-metering and invoicing backend

## Reasoning (exempt from cap)

Key tensions to resolve before designing:

1. **Immutable invoices vs. 7-day-late events.** You cannot hold every invoice 7 days *and* also claim "generated at cycle close," and even a 7-day grace doesn't eliminate stragglers (dedup and disputes exist too). The only clean model is **bitemporal**: every event has `occurred_at` (business time) and `recorded_at` (ingest time). An invoice is defined as "usage with `occurred_at` in the cycle, as known at watermark W." Anything arriving after W becomes an adjustment line on a *later* invoice, rated at the original cycle's pinned prices. This one decision simultaneously solves late events, disputes (credit notes), and regulator reproducibility.

2. **Reproducibility.** Re-running must be a pure function: `invoice = rate(sealed_event_set, price_snapshot, customer_config_snapshot)`. So the price table must be append-only/versioned, and each invoice must persist its input pins (watermark + price snapshot id + manifest of event files).

3. **Cheap object storage, small OLTP.** Raw events → Parquet in object storage; OLTP holds only invoices, pins, prices, customers. Drill-down is served by a query engine over Parquet, not by the OLTP DB. Volume check: 100M ev/day is only ~1,200/s average (5k/s peak) and ~3–4 TB per 13 months compressed — trivially object-storage-shaped, ruinous as OLTP rows.

4. **Dedup.** At-least-once with a 7-day lateness horizon means a streaming dedup window must span ≥7 days — doable but fragile. Since rating reads *all* events for a cycle anyway, exact dedup by `event_id` at aggregation time is free and exact; a short streaming dedup window is just a cost optimization.

5. **Timezone-anchored cycles.** 50k customers each closing at local midnight on their anchor day is a scheduling problem, not a data problem: precompute each customer's next close instant in UTC.

Word budget: deliverable below is ~950 words.

---

## Deliverable

### Components

- **Ingest API / collector** — stateless HTTP+gRPC endpoints; validates schema, stamps `recorded_at`, writes to Kafka. Peak 5,000 ev/s at ~300 B/event ≈ 1.5 MB/s — two small brokers handle this with 10× headroom.
- **Event lake writer** — Kafka consumer batching events into Parquet on object storage. Best-effort dedup over a rolling window (RocksDB keyed by `event_id`) reduces duplicates early; **exactness is not required here** (see Rating).
- **Aggregator** — hourly job compacting raw events into per-`(customer, product, hour)` usage aggregates, also in object storage. Hourly granularity is what makes mid-cycle price changes cheap to apply (splice at the effective hour; sub-hour effective timestamps rate the boundary hour from raw events).
- **Cycle scheduler** — maintains `next_close_at_utc` per customer, computed from anchor day-of-month + IANA tz (DST-aware). Fires invoice runs; ~1,700 closes/day fleet-wide.
- **Rating & invoicing engine** — pure function producing an invoice from sealed inputs (below).
- **Drill-down/query service** — Trino/Athena/DuckDB over the Parquet lake for line-item → raw-event queries.
- **Adjustment processor** — turns post-watermark events and dispute resolutions into credit/debit memo lines.

### Data stores

**Object storage (the big, cheap one):**
- `events/ingest_date=YYYY-MM-DD/customer_bucket=NNN/*.parquet` — raw immutable events: `event_id, customer_id, product, quantity, occurred_at, recorded_at, payload`. ~30 GB/day raw ≈ 6–10 GB/day Parquet → **~3–4 TB for the 13-month retention window**, lifecycle-expired after 13 months. Partitioning by *ingest* date makes files append-only and the watermark trivial ("all files with ingest_date ≤ W").
- `aggregates/` — hourly rollups, rebuilt idempotently; ~50k × 3 products × 24 h ≈ 3.6 M rows/day, a few hundred MB/day.
- `invoice_manifests/<invoice_id>.json` — the exact list of Parquet files (+ content hashes) that fed each invoice.

**OLTP (Postgres — deliberately tiny, low tens of GB):**
- `customers(id, tz, anchor_dom, ...)` — 50k rows.
- `price_versions(id, product, unit_price, effective_from, created_at)` — **append-only**; a "price change" is a new row, never an update. `created_at` (when the row entered the table) is what reproducibility pins on; `effective_from` is what rating applies.
- `invoices(id, customer_id, period_start, period_end, watermark, price_snapshot_at, manifest_ref, total, status, content_hash, issued_at)` — 50k/mo → ~8 M rows over 13 months.
- `invoice_lines(invoice_id, product, price_version_id, quantity, amount, kind)` where `kind ∈ {usage, late_usage_adjustment, dispute_credit}`; ~10 lines/invoice.
- `credit_notes(id, invoice_id, line_ref, amount, reason, issued_at)` — immutable, like invoices.
- `dedup_window` lives in the stream processor, not here.

### Ingest → invoice flow

1. Client sends event (`event_id`, `occurred_at`) → collector → Kafka → lake writer → Parquet (ingest-date partitioned). Duplicates may survive; late events land in *today's* ingest partition regardless of `occurred_at`.
2. Aggregator maintains hourly rollups keyed by `occurred_at` hour, **deduplicating by `event_id` within each (customer, cycle) scope** — exact, because it sees the full set.
3. At a customer's local-midnight close, the scheduler fires the rating engine with a **watermark W = close instant** (policy-tunable to close+N h if the business prefers fewer adjustments).
4. Rating seals its inputs: manifest of event files with `ingest_date ≤ W`; `price_snapshot_at = W` (i.e., only `price_versions.created_at ≤ W` are visible). It computes, per product: usage split into sub-ranges at each price version's `effective_from` within the cycle, quantity × unit price per range → one line per (product, price version).
5. Invoice is written with watermark, snapshot pin, manifest ref, and a `content_hash` of the rendered invoice; status → `issued`. **No row of an issued invoice is ever updated.**

### Hard cases

- **Duplicates:** exact dedup by `event_id` at rating/aggregation time; streaming dedup is only a cost optimization. Idempotent by construction.
- **Late events (≤7 days, or later):** an event with `occurred_at` in a closed cycle but `recorded_at > W` is picked up by the adjustment processor, rated **using the closed cycle's pinned price snapshot**, and emitted as a `late_usage_adjustment` line on the customer's *next* invoice, referencing the original cycle. The original invoice never changes.
- **Mid-cycle price changes:** append-only `price_versions` + rating's effective-time splicing produce one line per price segment, so the invoice itself shows the split.
- **Immutability + disputes:** disputes resolve into immutable `credit_notes` (or debit memos) linked to the disputed line — the accounting-standard pattern; net position changes, documents don't.
- **Reproducibility:** re-run = `rate(manifest, price_snapshot_at, customer snapshot)`. The manifest freezes the event set (late arrivals invisible), `created_at ≤ price_snapshot_at` freezes prices (later corrections invisible), and the stored `content_hash` lets the regulator verify bit-identity. Event files are immutable and retained ≥13 months, matching the drill-down obligation.
- **Timezone/anchor cycles:** per-customer `next_close_at_utc` recomputed after each close via the IANA tz database; anchor days 29–31 clamp to month-end.
- **Drill-down (13 months):** an invoice line carries its manifest + (customer, product, cycle occurred_at-range, W); the query service scans only that customer-bucket's partitions — seconds on Parquet, zero OLTP load.

### Capacity summary (decision drivers)

| Concern | Number | Consequence |
|---|---|---|
| Ingest | 5k ev/s peak, ~1.2k avg | small Kafka cluster; stateless collectors |
| Event lake | ~3–4 TB / 13 mo | object storage, not OLTP |
| Aggregates | ~0.3 GB/day | hourly granularity affordable |
| OLTP | ~8 M invoices, ~80 M lines / 13 mo | tens of GB — stays small as required |
| Invoice runs | ~1,700/day | single rating worker pool suffices |
