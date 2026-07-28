# Webhook Delivery Platform — Architecture

## Architecture: components and flow

```
Event Producers → Kafka (topic: events, partitioned by endpoint_id)
                        │
                        ▼
                 Fanout Consumer Group
                 (matches event → subscribed endpoints,
                  writes delivery rows)
                        │
                        ▼
                 Postgres (deliveries table, per-endpoint queues)
                        │
                        ▼
              Dispatcher Workers (pull ready deliveries,
              rate-limited per endpoint, HTTP POST)
                        │
              ┌─────────┴─────────┐
              ▼                   ▼
        Success → mark done   Failure → reschedule via
                                Redis-backed delay queue
                                (sorted set, score = next_attempt_at)
                        │
                        ▼
              Scheduler polls Redis ZSET, requeues
              due retries back onto per-endpoint Kafka
              retry topics or directly to dispatcher pool
                        │
                        ▼
              24h exceeded → Dead Letter (Postgres table + S3 archive)
```

**Ingestion**: events land on a Kafka topic partitioned by `endpoint_id` (or a hash of it) so that all events for one endpoint are strictly ordered within a partition. This is the single most important structural decision: ordering-per-endpoint is a Kafka partitioning problem, not an application-locking problem.

**Fanout**: a consumer group reads the topic, resolves subscriptions (endpoint list per event type, cached in Redis, source of truth in Postgres), and writes one `delivery` row per (event, endpoint) pair. Fanout is idempotent — keyed on `(event_id, endpoint_id)` — so consumer re-delivery on rebalance doesn't double-create rows.

**Dispatch**: per-endpoint worker pools (or a work-stealing pool keyed by `endpoint_id` hash bucket, e.g. 256–1024 buckets mapped consistently to workers) pull the oldest pending delivery for that endpoint, POST it, and only pull the next one after the current attempt resolves (success or terminal failure/backoff-scheduled). This single-in-flight-per-endpoint rule is what gives ordering at delivery time, not just at ingestion time — Kafka ordering alone doesn't help once retries interleave with new events unless dispatch also serializes per endpoint.

**Retry scheduling**: failed deliveries get `next_attempt_at = now + backoff(attempt)` and are pushed into a Redis ZSET (score = timestamp) per endpoint or globally sharded. A scheduler process scans due entries and re-enqueues them to the dispatcher. Redis is a natural fit here since the team already runs it and ZSET-by-score is exactly a delay-queue primitive; no new infra needed.

**Dead-lettering**: at 24h since first attempt, the delivery is marked `dead_lettered`, written to a `dead_letters` table (and optionally archived to S3 for cheap long-term retention), and a webhook/notification fires to the customer's dashboard so they know their endpoint has been failing.

## Data model

```
subscriptions(endpoint_id, url, event_types[], secret, active, created_at)

deliveries(
  id, event_id, endpoint_id,
  status enum(pending, in_flight, delivered, retrying, dead_lettered),
  attempt_count, next_attempt_at, first_attempted_at,
  last_response_code, last_error, payload_ref, -- pointer to event body (not duplicated)
  created_at, updated_at
)
  -- unique index on (event_id, endpoint_id) for idempotent fanout
  -- index on (endpoint_id, status, next_attempt_at) for dispatcher pull

dead_letters(delivery_id, endpoint_id, event_id, reason, archived_at)
```

Payload bodies are stored once (S3 or a compact events table) and referenced by `payload_ref` to avoid 20,000x duplication of large event bodies across delivery rows — at 5,000 events/s this matters a lot for storage cost.

## Failure handling

- **Transient failures (5xx, timeout, connection reset)**: exponential backoff with jitter, e.g. 1m, 5m, 30m, 2h, 6h, then capped hourly retries until 24h.
- **4xx (client error)**: still retried by default (customer endpoints often flap), but a `4xx-streak` counter can short-circuit to dead-letter sooner (e.g. 10 consecutive 400/404) to avoid wasting capacity on a clearly misconfigured endpoint.
- **Endpoint down for hours**: because retry scheduling is per-endpoint (not global), one dead endpoint doesn't block delivery to other endpoints. Its own backlog simply grows in Redis/Postgres; a circuit breaker per endpoint can pause active dispatch attempts once failure rate crosses a threshold, only probing periodically, to avoid burning worker capacity on hopeless sends during the burst window.
- **Burst absorption**: the 50k/s peak is absorbed by Kafka's partition log (cheap, durable buffering) and by scaling dispatcher worker count horizontally (stateless, autoscaled on queue depth) — Postgres and Redis are shielded from the peak because fanout writes are batched and dispatch is rate-controlled per endpoint regardless of producer rate.

## Tradeoffs

1. **Ordering forces single-in-flight-per-endpoint dispatch**, capping per-endpoint throughput to whatever that one endpoint can sustain. This is correct given the requirement but means a slow endpoint's queue grows unboundedly during bursts — accepted, since dead-lettering after 24h bounds worst case.
2. **At-least-once, not exactly-once**: customers must dedupe on `event_id`; we don't attempt distributed transactions across Kafka/Postgres/HTTP to get exactly-once, since that's expensive and the requirement only asks for at-least-once.
3. **Postgres as durable delivery-state store**: adds write load (~5k-50k rows/s at peak) — mitigated by batched inserts and partitioning the `deliveries` table by time or endpoint-hash; an alternative (fully Kafka-native retry topics) was rejected because Postgres gives simpler ad-hoc querying for support/dashboards.
4. **Redis ZSET as delay queue**: simple and reuses existing infra, but is memory-bound; large dead-endpoint backlogs are capped by moving anything past a size/time threshold to a lower-priority Postgres-polled tier instead of keeping it hot in Redis.
