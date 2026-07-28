# Webhook Delivery Platform — Architecture

## Architecture: components and flow

1. **Event source → `events.raw` (Kafka)**. Producers publish domain events to an existing topic, partitioned by tenant/aggregate id (as today). This is unchanged — the webhook system is a consumer of it, not a new producer path.

2. **Fan-out service (`dispatcher`)**. A consumer group reads `events.raw`, resolves subscriptions (which of the ~20k endpoints care about this event type/tenant) from a Redis cache (read-through from Postgres, the source of truth for subscriptions), and emits one **delivery task** per (event, endpoint) pair onto `deliveries.pending` (Kafka), **keyed by `endpoint_id`** (hashed into ~300 partitions — enough to parallelize, cheap enough to operate). Keying by endpoint is what gives us ordering: Kafka guarantees order within a partition, and consistent hashing keeps one endpoint's deliveries on one partition.

3. **Delivery workers**. A consumer group (autoscaled on AWS — ECS/Fargate or EKS HPA on consumer lag) processes `deliveries.pending`. Each worker maintains, per endpoint currently assigned to it, a single in-flight request — never two concurrent POSTs to the same endpoint — so ordering survives even if a partition happens to hold multiple endpoints. On success, commit offset and mark the row done. On failure, **do not block the partition for 24h**: write a retry record and let the consumer move to the next message, but a per-endpoint gate (Redis key `inflight:<endpoint_id>`) prevents the *next* event for that same endpoint from being attempted until the current one resolves (success or dead-letter) — this is what actually enforces ordering under retry, not partition order alone.

4. **Retry scheduler**. Failures go into a Redis sorted set (`retry:<bucket>`, score = `next_attempt_at`) for near-term retries (<~2h) and a Postgres-polled table for longer ones, to bound Redis memory during a mass-outage. A lightweight poller pops due items and re-enqueues them onto `deliveries.pending` for the owning worker, respecting the per-endpoint gate.

5. **Dead-letter path**. After the backoff schedule exhausts 24h, the delivery is marked `dead_letter`, written to a `dead_letters` table (and optionally an S3-archived Kafka topic for cheap long-term storage), and the endpoint gate is released so subsequent events proceed — a single bad event does not permanently wedge an endpoint's queue.

## Data model (Postgres)

```
deliveries(
  id UUID PK,
  event_id UUID,          -- reference into Kafka/event store, not full payload
  endpoint_id UUID,
  sequence BIGINT,         -- monotonic per endpoint_id, assigned at fan-out
  status ENUM(pending, inflight, success, retrying, dead_letter),
  attempt_count INT,
  next_attempt_at TIMESTAMPTZ,
  last_error TEXT,
  created_at, updated_at
)
-- index on (endpoint_id, sequence) and (status, next_attempt_at)

subscriptions(endpoint_id PK, tenant_id, event_types[], url, secret, active)
dead_letters(delivery_id PK, endpoint_id, event_id, failed_at, reason)
```

Payloads are not duplicated into Postgres — only metadata and a pointer, to keep row size and write volume manageable at 50k/s peaks.

## Failure handling

- **Backoff**: `next_attempt_at = now + min(24h, base * 2^attempt + jitter)`. Jitter avoids thundering herds when an endpoint comes back.
- **Idempotency**: every POST carries `X-Delivery-Id` (the `deliveries.id`) so customers can dedupe; at-least-once means a retry may follow a success whose ack was lost, and we push resolution to the receiver rather than building exactly-once ourselves.
- **Timeouts**: strict per-request timeout (~5s) so one slow endpoint can't stall a worker; failures are POSTs plus timeouts, treated identically.
- **Mass-outage endpoints** (down for hours): their events accumulate in Postgres/Redis rather than Kafka, since Kafka retention for 24h at fan-out volume is expensive to hold "in flight." Kafka is the fast path; Postgres is the durable backlog for anything not delivered promptly.

## Tradeoffs, explicit

1. **Ordering costs latency, not just for the failed message but everything behind it for that endpoint.** A dead endpoint means its whole queue waits up to 24h. This is inherent to the requirement (ordered delivery), not a design flaw — but it means per-endpoint backlogs need durable, cheap storage, not just Kafka retention.
2. **~300 partitions is a compromise, not perfect isolation.** True per-endpoint isolation would want ~20k partitions, which is operationally and cost-heavy on MSK/Kafka. We accept occasional partition sharing and cover the gap with the Redis in-flight gate, at the cost of an extra moving part.
3. **At-least-once + ordering means duplicates are possible**; we chose not to build exactly-once (dedup ledger with distributed transactions) because it's expensive and the customer-side idempotency key is cheap and standard for webhook consumers.
4. **Redis for near-term retries, Postgres for far-term** — a two-tier design instead of one. Simpler would be "everything in Postgres," but polling Postgres at sub-second granularity for near-term retries doesn't scale; simplest would be "everything in Redis," but that risks unbounded memory during a multi-hour outage across many endpoints.
5. **No new infrastructure** (no SQS/EventBridge/dedicated queue) — reuses Kafka/Postgres/Redis the team already operates, trading a slightly more manual delayed-retry mechanism for lower cost and no new operational surface.
