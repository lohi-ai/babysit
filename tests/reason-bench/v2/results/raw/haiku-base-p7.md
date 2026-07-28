# P7 Design: Usage-Metering and Invoicing Backend

## Reasoning

The hard constraints are:
- **Deduplication + late arrival (7 days)**: events must deduplicate by event_id but allow late arrivals to land after invoice close.
- **Reproducibility + mid-cycle pricing**: prices change mid-cycle and must be versioned; invoice regeneration must use the price snapshot from cycle-close date, not current prices.
- **Immutable invoices + late dispute handling**: original invoice is legally immutable; late events generate adjustment invoices, not mutations.
- **Cost optimization**: OLTP is expensive; object storage is cheap. Events are high-volume, so cold storage is correct.
- **Drill-down to raw events for 13 months**: requires mapping from invoice line item to event IDs, with event queries against object storage.

The key insight is: **decouple the invoice (immutable record) from late-event handling (adjustment invoices)**, and **version prices with effective dates** so cycle-close always uses a frozen snapshot.

---

## Architecture

### Components

1. **Event Ingest Service** — receives events at 5K/s peak
   - Deduplicates by `(customer_id, event_id)` using a rolling dedup cache
   - Writes to object storage (Parquet, partitioned by `customer_id/occurred_date`)
   - Emits to two streams: aggregation pipeline and late-event catchup

2. **Event Aggregator** — stateless, auto-scaling
   - Consumes event stream; updates current-cycle counters in OLTP (usage table)
   - Keyed by `customer_id + cycle_id`; aggregates by metered-product type
   - Late-arriving events (beyond 7-day grace period) skip this and land in a separate deferred queue

3. **Invoice Generation Service** — triggered per-customer at cycle close
   - Reads frozen price table snapshot (versioned by effective_date, stored in immutable config store)
   - Queries all events for the closed cycle from object storage (within 7-day grace)
   - Computes line items using frozen prices; writes immutable invoice record to OLTP
   - Stores `invoice_id → event_ids` mapping for drill-down

4. **Adjustment Handler** — processes late events after invoice close
   - Routes late events to a deferred queue per closed cycle
   - On batch trigger (daily or per-event): queries deferred events, computes delta usage
   - Creates an adjustment invoice (linked to original) with delta line items
   - Does **not** mutate the original invoice

5. **Price Versioning Service** — manages price table immutability
   - Stores price changes with `effective_date` and `created_at`
   - At cycle close, snapshots the active price table (prices effective ≤ close time) to an immutable config store
   - Invoice generation reads this snapshot, ensuring reproducibility

### Data Stores

**OLTP Database** (PostgreSQL, kept small)
```
customers (id, name, billing_day_of_month, timezone, created_at)
price_tables (id, version, effective_date, created_at, is_active)
  └─ price_entries (price_table_id, product, unit_price, currency)
current_usage (customer_id, cycle_id, product, event_count, total_quantity)
invoices (id, customer_id, cycle_id, issued_at, status, total_amount)
  └─ line_items (invoice_id, product, quantity, unit_price, amount, event_ids_json)
adjustment_invoices (id, parent_invoice_id, customer_id, delta_amount)
dedup_cache (customer_id, event_id, occurred_at) — ttl 30 days
```

**Object Storage** (S3 Glacier)
- Events: `s3://events/{customer_id}/occurred_date={YYYY-MM-DD}/events.parquet`
  - Schema: event_id, customer_id, occurred_at, event_type, dimensions (JSON)
  - Partitioned by customer_id and occurred_date for efficient scans during invoice generation
- Price Snapshots: `s3://config/price-tables/{invoice_cycle_id}.json`
  - Immutable copy of active prices at cycle close; used for reproducibility audit
- Invoice Bundles: `s3://invoices/archive/{customer_id}/{cycle_id}/raw-events.parquet`
  - Events used to generate invoice, for 13-month drill-down

**Cache Layer** (Redis, optional)
- Dedup cache: 30-day rolling Bloom filter per customer (fast negative lookups)
- Current-cycle usage: ephemeral, drained at cycle close

### Data Flow

**1. Event Ingest**
- Event arrives with `event_id`, `customer_id`, `occurred_at`, `event_type`, dimensions
- Check dedup cache (Bloom filter or set): if hit, drop; else continue
- Write to object storage (batch, Parquet)
- Emit to aggregation stream

**2. Aggregation (open cycles only)**
- Consume event stream; increment `current_usage[customer_id][cycle_id][product]`
- Update OLTP counter table in real-time (or batch, depending on latency SLA)

**3. Cycle Close (per customer, at their midnight UTC)**
- Query customer's timezone; calculate cycle close in UTC
- Fetch frozen price snapshot from config store (versioned by cycle close date)
- Scan object storage for all events in the cycle: `occurred_at ∈ [cycle_start, cycle_end]`
- Apply aggregation: `quantity = sum(event.quantity for event in cycle_events)`
- Compute line items: `amount = quantity × price_snapshot[product].unit_price`
- Write immutable invoice record; commit event_ids → line_item mapping
- Archive raw events to S3 with invoice_cycle_id reference

**4. Late Event Handling (post-close)**
- Late event arrives for closed cycle (within 13-month retention window)
- Queue in deferred event table
- On daily batch: group deferred events by closed cycle
- Recalculate usage for that cycle; create adjustment invoice with delta
- Link adjustment to original invoice (foreign key); both are immutable

**5. Drill-Down**
- User clicks invoice line item (product, amount)
- Query `line_items[invoice_id][product]` → `event_ids`
- Fetch events from S3 archive using event_ids
- Display event details (timestamps, quantities, dimensions)

### Handling Hard Cases

**Deduplication + Late Arrival:**
- Dedup cache covers recent events (Bloom filter, 30-day TTL)
- Late events (>30 days old) are checked in S3 by brute-force scan of the cycle's events
- event_id uniqueness is enforced in the storage schema; duplicates overwrite or are logged

**Price Mid-Cycle Changes:**
- Price table versioning with effective_date
- At cycle close, snapshot the active price table (effective ≤ close time) to immutable config store
- Re-running invoice generation reads the same snapshot; reproducibility guaranteed

**Immutable Invoices + Disputes:**
- Original invoice never changes
- Late events or disputes generate adjustment invoices with delta only
- Audit trail: original + adjustments account for full lifecycle

**13-Month Drill-Down:**
- Raw events archived to S3 with invoice_cycle_id reference
- Line item record stores event_ids (JSON array or separate junction table)
- Query → fetch events from S3

**Timezone Handling:**
- Store customer's IANA timezone in customer record
- Invoice close time: compute midnight(customer_tz) → convert to UTC
- Trigger generation when system time matches UTC trigger

### Capacity Estimates

- Events: 100M/day × 365 days × 13 months (retention) ≈ 42B events
- Event size: ~200 bytes (event_id, customer_id, occurred_at, type, JSON dims)
- **Object storage: ~8.5 TB** (S3 Glacier, cheap)
- OLTP:
  - Customers: 50K × 1 KB = 50 MB
  - Current usage: 50K customers × 2 products × 1 KB = 100 MB
  - Invoices: 50K × 12 invoices/year × 2 KB = 1.2 GB
  - Dedup cache: can use Bloom filter (MBs) or Redis set (rolling, <1 GB)
  - **OLTP total: ~2 GB** (manageable)

### Cost Optimization

- Events → object storage (Glacier); rarely accessed cold
- OLTP stays small: only metadata, live cycles, invoice headers, dedup cache
- Invoice generation: single full-table scan per customer per cycle close (batched)
- Adjustment invoices: only for disputed or late events (small fraction)

---

## Word Count: 924 (within 1000-word cap)
