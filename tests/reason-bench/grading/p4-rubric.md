# P4 rubric — webhook delivery (8 pts)

GRADER ONLY.

- **T1 — ordering × retry = head-of-line blocking, surfaced and resolved.**
  Per-endpoint ordering + at-least-once retry means one failing delivery
  blocks that endpoint's queue (or ordering is relaxed). Answer must name the
  conflict and choose: strict FIFO-with-pause per endpoint, or relaxed
  ordering with sequence numbers/timestamps for customer-side reordering —
  with the tradeoff stated.
- **T2 — retry backlog magnitude.** ~5% × 5k/s over hours ⇒ millions to tens
  of millions of pending deliveries (24h worst case ≈ 21M+). Answer does the
  estimate (or equivalent sizing) and designs storage for it — a
  next_attempt_at-indexed store, not "Kafka consumer retries in place".
- **T3 — dead endpoints.** Circuit breaker / auto-disable for endpoints down
  for hours, dead-letter after 24h, customer notification.
- **T4 — thundering herd.** Recovery of a big endpoint or platform restart:
  jittered backoff, per-endpoint concurrency/rate cap so one customer can't
  absorb the fleet.
- **T5 — idempotency for at-least-once.** Stable delivery/event ID in the
  payload/headers + HMAC signature so customers can verify and dedupe.
- **T6 — 20k endpoints vs Kafka partitions reconciled.** Concrete mechanism:
  hash endpoints across partitions (accepting cross-endpoint HOL within a
  partition, mitigated), or Kafka for ingest fan-out + per-endpoint delivery
  state in Postgres/Redis with a scheduler. Hand-waving "one topic per
  endpoint" (20k topics) fails.
- **T7 — delivery-state data model.** Table/keys with status, attempt count,
  next_attempt_at (indexed for the scheduler sweep), last error; partitioning
  or TTL for the 24h horizon.
- **T8 — customer-facing observability.** Delivery logs/portal, manual replay,
  failure alerting to the customer.
