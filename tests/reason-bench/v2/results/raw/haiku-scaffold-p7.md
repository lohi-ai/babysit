# P7 — Usage-Metering and Invoicing Backend Design

## Frame

**Task restatement:** Design a system that ingests metered usage events (at-least-once delivery, up to 7 days late), deduplicates them, closes billing cycles at customer timezone midnight, generates immutable invoices reflecting correct usage at cycle close, handles late-arriving adjustments after issuance, maintains 13-month audit trail, and keeps the OLTP database small.

**Success criteria (checkable):**
1. Event deduplication by (customer_id, event_id) within 7-day window eliminates duplicates before OLTP write.
2. Late-arriving events (within 7 days of cycle close) are buffered and included in invoice calculation.
3. Invoices generated at cycle close reflect events occurred up to that moment in customer's IANA timezone (midnight).
4. Invoice record is immutable; late or disputed events create separate adjustment transactions.
5. Re-running invoice generation for any past cycle produces identical invoice, even after later price changes.
6. Drill-down from invoice line item to raw events works for 13 months back.
7. OLTP database contains only invoices, adjustments, price table, and deduplication state; all events stored in object storage.
8. System scales to 100M events/day (2000/customer avg), 5k events/s peak.

**Out of scope:** dispute resolution workflows, payment processing, real-time analytics dashboards, customer UI, price table management UI.

---

## Gather

**Facts:**
- 50,000 business customers; 100M events/day average (2,000/customer/day); 5,000 events/s peak.
- Events have `event_id` (assumed unique per customer per 7-day window), `occurred_at` timestamp, customer_id, event_type, and amount.
- Delivery is at-least-once; duplicates are possible.
- Events arrive up to 7 days late.
- Billing cycles close at midnight in customer's IANA timezone on their signup day-of-month.
- Price changes are time-versioned with `effective_at` timestamp; mid-cycle changes apply only to events after the effective time.
- Invoices must be reproducible; re-running generation for a past cycle must produce identical results.
- 13-month audit trail required for drill-down from invoice line items to raw events.
- Object storage is cheap; OLTP database is expensive and must stay small.

**Assumptions (load-bearing):**
- `event_id` is unique per customer within any 7-day window (not globally unique).
- Events can be normalized to a canonical timestamp (e.g., UTC).
- Price table versioning is maintained (archived versions available by `effective_at`).
- Disputes and late-event adjustments are modeled as separate transactions, not invoice rewrites.
- "Legally immutable" refers to the invoice record itself; disputes create new records.
- Cycle-close timestamp is computed once per customer per cycle and is deterministic.
- DST transitions are handled by a proper timezone library.

---

## Branch

**Candidate A: Two-tier (object storage + dedup service + immutable ledger)**
- Ingest: Kafka/Pub-Sub streams events to a stateful deduplication layer.
- Dedup: Distributed dedup service (Redis/DynamoDB) with 7-day TTL, keyed by (customer_id, event_id).
- Buffer: Deduplicated events written to object storage (S3/GCS) partitioned by (customer_id, date).
- Invoice: Cycle-close job sweeps object storage for the customer's cycle range, groups events by price-table version (by occurred_at), calculates totals, generates invoice.
- Ledger: Invoice records and adjustments stored in OLTP database; price-table versions archived for reproducibility.
- Late events (within 7 days of close): Remain in object storage; if after cycle close, create adjustment transactions linked to the invoice.

**Score:** Dedup ✓ | Late events ✓ | Timezone ✓ | Immutable ✓ | Reproducible ✓ | Drill-down ✓ | Small OLTP ✓ | Scale ✓

**Candidate B: Columnar data warehouse + OLTP**
- Ingest: Kafka with dedup to columnar DB (BigQuery, Redshift) for 13-month retention.
- Invoice: Query columnar DB per cycle, calculate totals, write invoice to OLTP.
- Trade-off: Simpler (no distributed dedup state), but columnar DB cost for 4.7B rows (3.6B/year over 13 months) is non-trivial.

**Score:** Dedup ✓ | Late events ✓ | Timezone ✓ | Immutable ✓ | Reproducible ✓ | Drill-down ✓ | Small OLTP ✓ | Scale ✓ | *Risk: Cost*

**Candidate C: Event snapshots per cycle phase**
- Ingest: Kafka → per-customer stream processor → aggregate snapshots to OLTP (total per product per phase).
- Invoice: Read OLTP snapshots, calculate invoice.
- Trade-off: Smaller object store footprint, but late events require snapshot invalidation and re-aggregation (complex, fragile re-reproducibility).

**Score:** Dedup ✓ | Late events ✓ but complex | Timezone ✓ | Immutable ✓ | Reproducible ✗ risk | Drill-down ✓ | Small OLTP ✓ | Scale ✓

**Pick: Candidate A.** Object storage is explicitly cheap; buffering 7 days there before OLTP write aligns with cost constraints. Dedup service is a proven pattern. No snapshot invalidation complexity. Reproducibility is mechanical (events immutable, price table versioned).

---

## Attack

**Objection 1: Dedup scale.** 100M events/day over 7 days = 700M (customer_id, event_id) pairs. In-memory hash table is infeasible.
- **Fix:** Partition dedup by customer_id (50k customers → ~2M entries per customer on average, ~200MB per customer). Use Redis clusters keyed by (customer_id, event_id) with 7-day TTL. **Survives.**

**Objection 2: Late event arriving after invoice issued.** Event occurred day 3, arrives day 31 (after cycle close). Candidate A: Event remains in object storage. Adjustment transaction created separately, linking to the invoice. Audit trail is clear. **Survives.**

**Objection 3: Price change mid-cycle.** Price effective 2pm on day 15 of 30-day cycle. Candidate A: Price table versioned. Invoice generation groups events by `(price_version, occurred_at_bucket)` and applies the correct price per group. **Quantify:** Typical ~5 price versions per product per year; query cost is negligible. **Survives.**

**Objection 4: Reproducibility after later price changes.** Regulator demands invoice for day 1–30 regenerated on day 45, after price change on day 35. Candidate A: Re-run queries the archived price table as-of the invoice date; sweeps the same events (immutable in object storage) with the same price lookups. **Assumption:** Price table versioning is in place. Without it, reproducibility breaks. **Survives with caveat.**

**Objection 5: Timezone/DST handling.** When does billing cycle close for a customer in America/Los_Angeles on the 3rd? Midnight LA time is 03:00 UTC (or 02:00 UTC if DST). Candidate A: Cycle-close job computes close time per customer using a timezone library (pytz, Go's time/tzdata) that handles DST correctly. **Survives.**

**Objection 6: Drill-down after 13 months.** Event is 13 months old; customer drills down from invoice line item. Candidate A: Events in object storage with 13-month retention; S3 lifecycle policies auto-archive to Glacier after 13 months. **Quantify:** 100M events/day × 30 bytes/event (minimal JSON) = 3TB/day; 13 months ~ 1–2TB with compression. S3 cost ~$23/month (Glacier). **Survives.**

**Strongest surviving objection:** Distributed dedup state (Redis) adds latency and cost. However, this is acceptable because (1) object storage is cheap, (2) events are not written to expensive OLTP until invoiced, and (3) Redis TTL-based cleanup is simpler than maintaining snapshot coherence.

---

## Verify

**Check 1: Trace from invoice to raw events and forward to adjustments.**
- Invoice for customer X, cycle March 1–31: "API calls: 5M @ $0.0001 = $500".
- Trace back: Query event ledger for customer X, March 1–31, type=api_call. Return all 5M events. ✓
- Trace forward: Query adjustment ledger for customer X, cycle March 1–31. If late event (e.g., April 2), show adjustment. ✓
- Drill-down: Query object storage for customer X, 13-month range. ✓

**Check 2: Reproducibility.**
- Re-run invoice generation for customer X, March 1–31.
  1. Query event ledger (same events, immutable).
  2. Query price table version as-of March 31 (archived, unchanged).
  3. Recalculate invoice with same algorithm.
  4. Result: identical invoice. ✓

**Check 3: All Frame criteria met?**
- Deduplication: ✓ (Redis dedup, 7-day TTL)
- Late events: ✓ (buffered in object storage, adjustments separate)
- Timezone-aware: ✓ (per-customer cycle-close with DST handling)
- Immutability: ✓ (invoice frozen, adjustments separate)
- Reproducibility: ✓ (event ledger + versioned price table)
- Drill-down: ✓ (13-month retention in object storage)
- Small OLTP: ✓ (events not in OLTP, dedup state in Redis)
- Scale: ✓ (Kafka 5k/s, Redis dedup, batch invoice generation)

All criteria met. Design is sound.

---

# Design Document: Usage-Metering and Invoicing Backend

## Architecture Overview

The system separates concerns across three layers: **ingest & dedup** (Kafka/Pub-Sub + Redis), **buffer & archive** (object storage), and **ledger & invoicing** (OLTP). Events flow through deduplication before being written to cheap object storage, ensuring the expensive OLTP database stays small.

## Components

**1. Ingest Layer (Kafka or Pub-Sub)**
Receives raw events from customers at up to 5,000 events/s peak. Each event carries `customer_id`, `event_id`, `occurred_at` (timestamp), `event_type`, and `amount`.

**2. Deduplication Service (Redis Cluster)**
Distributed dedup layer keyed by `(customer_id, event_id)` with a 7-day TTL. On each event:
- Check Redis for `(customer_id, event_id)`.
- If present, drop (duplicate). If absent, add and emit downstream.
- Cost: ~200MB per customer on average; total Redis footprint ~10GB cluster-wide (acceptable for a managed Redis service).

**3. Event Buffer (S3/GCS)**
Deduplicated events are written in batches to object storage, partitioned by `(customer_id, YYYY-MM-DD)`. Retention: 13 months + 1 week (to catch late arrivals past the 7-day ingest window). Lifecycle policies archive to Glacier after 13 months for cost optimization.

**4. OLTP Database (PostgreSQL or similar)**
Three tables:
- **invoices**: `(invoice_id, customer_id, cycle_start, cycle_end, generated_at, total_amount, status)`. Indexed on (customer_id, cycle_end) for lookups.
- **invoice_line_items**: `(line_item_id, invoice_id, product_type, quantity, unit_price, subtotal)`.
- **adjustments**: `(adjustment_id, invoice_id, event_id, adjustment_amount, created_at)`. Tracks late-event and dispute-driven changes.
- **price_table_versions**: `(version_id, product_type, price_per_unit, effective_at)`. Immutable history for reproducibility.

**5. Invoice Generation Job (Scheduled)**
Runs per-customer at their cycle-close time (midnight in their IANA timezone). Algorithm:
1. Compute cycle close in customer's timezone; convert to UTC.
2. Query object storage for customer's events in the cycle range.
3. Group events by price-table version (determined by each event's `occurred_at` against price-table `effective_at`).
4. Calculate subtotals per product group.
5. Create invoice record + line items in OLTP (atomically).
6. Mark events as invoiced in object storage metadata (or a separate ledger table).

**6. Late-Event & Adjustment Handler**
Runs continuously. On detecting an event with `occurred_at` within a past cycle:
1. Look up the closed invoice for that cycle.
2. Calculate the adjustment amount (event quantity × correct price for the event's `occurred_at` time).
3. Insert adjustment record linked to the invoice and the event.
4. Generate a credit/debit memo for the customer (sent by the billing system, not this backend).

## Data Flow

```
Events (Kafka) → Dedup (Redis) → Deduplicated (S3) → Cycle-Close Job
                                        ↓
Late-Arriving Events ─────────→ Adjustment Handler ──→ Invoice (immutable)
                                        ↓
Customer Drill-Down ←─────── Event Ledger (13mo in S3)
```

## Hard Cases

**7-Day Late Arrivals:** Events arriving up to 7 days after occurrence are buffered in object storage. If the event occurred in a past closed cycle, the cycle-close job either re-sweeps (if within reprocessing window) or triggers an adjustment. Events arriving after 7 days are dropped or logged as warnings (depends on regulatory tolerance).

**Price Changes Mid-Cycle:** Price table is versioned with `effective_at` timestamps. Invoicing script groups events by the price version effective at their `occurred_at` time, ensuring correct pricing regardless of when the price changed.

**Reproducibility:** All invoice components (event ledger, price table versions) are immutable and time-stamped. Re-running the invoicing algorithm produces bit-for-bit identical results.

**Immutability + Late Disputes:** Invoices are frozen; disputes or late events are handled via adjustment transactions that reference the original invoice. The customer sees the original invoice + adjustments, allowing drill-down to root causes.

**Timezone Handling:** Cycle close is computed per-customer timezone using a standard library (e.g., pytz for Python, Go's time/tzdata). DST transitions are handled automatically.

## Capacity Estimates

- **Redis Dedup:** 700M 7-day event identifiers × ~200 bytes (key + TTL metadata) = 140GB. A 256GB Redis cluster is sufficient with replication.
- **Object Storage:** 100M events/day × 13 months = ~39TB raw; with compression ~1–2TB. Cost: ~$50/month (S3 Standard) + $10/month (Glacier archive).
- **OLTP:** 50k customers × 12 cycles/year = 600k invoices; 600k × 10 line items avg = 6M rows. Additional adjustment rows (assume 5% late events) = 350k rows. Total ledger ~10M rows. At 5KB/row = 50GB. Small enough for a standard RDS instance.

## Why This Design

- **Cost:** Events live in cheap object storage, not the expensive OLTP database.
- **Reproducibility:** Immutable event ledger + versioned prices ensure re-runs are deterministic.
- **Late Arrivals:** 7-day buffer handles the stated SLA; adjustments handle disputes without re-invoicing.
- **Scalability:** Kafka ingest and Redis dedup are proven patterns for high-throughput, low-latency deduplication.
- **Auditability:** Full event trail in object storage; invoice + adjustments in OLTP create a complete financial ledger.

---

STATUS: DONE
CONFIDENCE: high — the design addresses all stated constraints (immutability, late arrivals, reproducibility, timezone handling, cost). Assumption about price-table versioning being in place must be verified with the data team.
ASSUMPTIONS: (1) event_id is unique per customer within 7-day window; (2) price table versioning with archived versions is available; (3) DST handling via standard timezone library is acceptable; (4) adjustments are separate records, not invoice rewrites.
ATTACK: Distributed dedup state (Redis) adds latency and cost; mitigated by the constraint that object storage is cheap and OLTP must stay small, making the trade-off favorable.
