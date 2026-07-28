# Webhook Delivery Platform — Architecture

## Components and flow

**1. Ingestion topic (`events.raw`, Kafka).** Producers publish domain events here, partitioned by tenant/aggregate id (whatever already gives source-side ordering). This is unchanged from today — no new write path for producers.

**2. Fan-out service (Kafka consumer group).** Reads `events.raw`, looks up the subscription matrix (which endpoints subscribed to this event type) from a Postgres `subscriptions` table cached in Redis (endpoint list per event type, TTL'd, invalidated on subscription change). For each matching endpoint it emits one message to `events.delivery`, a second Kafka topic **partitioned by `hash(endpoint_id)`** across ~1,024 partitions. Hashing by endpoint guarantees every event for a given endpoint lands in the same partition in the same order it was produced — this is where per-endpoint ordering is first established, essentially for free, using Kafka's own guarantee.

**3. Delivery-state writer.** A small consumer group behind `events.delivery` does one job: insert a row per (event, endpoint) into Postgres `deliveries` with a strictly increasing per-endpoint `sequence_no`, status `pending`, then commit the Kafka offset immediately. This decoupling is the key design decision (see tradeoffs): **Kafka is only used to assign order and fan out; it is not the retry queue.** Once a delivery is durably recorded in Postgres, the partition is free to move on even if that endpoint is completely dead — a stuck endpoint never causes head-of-line blocking for the ~19 other endpoints sharing its partition.

**4. Scheduler (Redis sorted set).** Holds `delivery_id -> next_attempt_at` for everything currently pending/in backoff. A poller pops due items in small batches and hands them to delivery workers. Redis is a cache of "what's due now," not the source of truth — it can be rebuilt from Postgres (`WHERE status='pending' AND next_attempt_at <= now()`) if lost.

**5. Delivery workers (stateless, autoscaled Fargate/ECS tasks).** Pull due deliveries, but enforce **one in-flight delivery per endpoint at a time**, driven by the next un-acked `sequence_no` for that endpoint (a Redis lock/lease keyed by endpoint_id prevents two workers racing the same endpoint). On 2xx, mark `delivered` and advance the endpoint's cursor. On failure, bump `attempt_count`, compute next backoff, write back to Postgres + Redis sorted set. Worker pool scales on queue depth (Redis ready-set size / Postgres backlog), not on raw event rate — this absorbs the 50k/s bursts without provisioning permanent capacity for them.

## Data model (Postgres)

- `subscriptions(endpoint_id, event_type, url, secret, active)`
- `deliveries(id, endpoint_id, event_id, sequence_no, status, attempt_count, next_attempt_at, last_error, created_at, updated_at)` — indexed on `(endpoint_id, sequence_no)` and on `(status, next_attempt_at)`. Partitioned by `created_at` (e.g. daily) so delivered rows age out cheaply; a nightly job archives/drops partitions past a retention window, keeping the hot table small (~pending + recent history only).
- `dead_letters(delivery_id, endpoint_id, event_id, final_error, attempts, dead_lettered_at)` for the 24h timeout case, queryable by customers via a status API, with manual replay support.
- `endpoint_health(endpoint_id, consecutive_failures, state)` — small Redis-backed counter, not Postgres, since it's ephemeral and hot.

## Failure handling

- **Exponential backoff with jitter**: 1s, 2s, 4s, … capped at, say, 30 min, randomized ±20%, tracked per delivery via `attempt_count`/`next_attempt_at`. Total window bounded at 24h; on expiry, move the row to `dead_letters` and stop retrying.
- **Circuit breaker per endpoint**: once `consecutive_failures` crosses a threshold (e.g. 5), mark the endpoint "suspected down" in Redis and stretch its effective backoff floor (e.g. minimum 5 min between attempts) rather than hammering a dead host — this matters at 20k endpoints where a chunk will be down at any moment.
- **At-least-once + idempotency**: workers only mark `delivered` after a 2xx; every POST includes `event_id` and `delivery_attempt` in headers so customers can dedupe on their side, since retries after a timeout can double-deliver.
- **Ordering under retry**: because only one in-flight attempt per endpoint is allowed, a failing endpoint simply queues behind its own backoff — later events for that endpoint wait in Postgres, not blocking anyone else.

## Tradeoffs being made

1. **Kafka partition count (~1,024) is far below 20,000 endpoints**, so many endpoints share a partition. This would normally cause head-of-line blocking on a slow endpoint; we explicitly avoid it by treating Kafka as a fan-out/ordering-assignment mechanism only, committing offsets as soon as a delivery row exists, and moving all retry/backoff logic into Postgres+Redis where per-endpoint state is independent. Cost: extra write path (delivery-state writer) and eventual consistency between "recorded" and "attempted."
2. **Serializing delivery per endpoint** caps single-endpoint throughput, but this is acceptable — a single customer HTTP endpoint was never going to sustain high concurrent throughput anyway, and ordering is a hard requirement.
3. **Autoscaling on queue depth rather than provisioning for 50k/s peak** saves significant cost (bursts are ~10 minutes), at the price of slightly elevated latency during the first minute of a burst while workers scale up.
4. **Postgres as system of record** for delivery state is simple and reuses existing infra, but requires aggressive partition pruning; we accept added operational complexity (partition rotation, archival) over introducing a new datastore.
5. **Redis retry-timer loss is tolerable**: since Postgres is authoritative, a Redis flush only causes a brief delay (next full-table poll), not lost or misordered deliveries.
