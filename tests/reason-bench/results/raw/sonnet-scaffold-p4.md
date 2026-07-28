# P4 — Webhook delivery platform: scaffolded answer

## 1. Frame

**Restated task:** Design a webhook delivery system that ingests platform events (5,000/s sustained, bursts to 50,000/s for ~10 min) and delivers one HTTP POST per event to each of ~20,000 subscribed customer endpoints, at-least-once, preserving per-endpoint order, tolerating endpoints that fail ~5% of the time transiently or go down for hours, retrying with exponential backoff for up to 24h before dead-lettering, built on the team's existing Kafka/Postgres/Redis-on-AWS stack, cost-consciously.

**Success criteria (checkable):**
- Names concrete components and the event→delivery flow end to end.
- Gives a data model (fields/keys) for delivery state that supports retry, backoff, ordering, and dead-lettering.
- States a failure-handling policy: backoff schedule, handling of long-down endpoints, dead-letter trigger.
- Explicitly addresses how per-endpoint order is preserved (or where it can break, and why that's acceptable).
- States tradeoffs explicitly, not just the "happy path" design.
- Deliverable ≤ 900 words.

**Out of scope:** payload signing/auth, endpoint registration/UI, per-customer rate limiting, autoscaling policy detail, monitoring/alerting stack, exact Kafka topic replication factor tuning.

**Two materially different readings — must not pick silently:**
1. *Does "5,000 events/s" mean raw platform events before fan-out, or already-expanded per-endpoint deliveries?* The task says "a delivery is one POST per event per subscribed endpoint," implying deliveries = events × fan-out, i.e., 5,000/s is pre-fan-out. This changes peak load by the fan-out factor — a genuine unknown (see Assumptions).
2. *Does "in order" mean the system must block later events for an endpoint until an earlier one succeeds (even across a 24h retry window), or is "eventually consistent, sequence-numbered" order (client reorders) acceptable?* The task states ordering as a hard requirement alongside at-least-once, not "best effort," so I read it as server-enforced blocking order, with an explicit carve-out for dead-lettered events (see Branch/Attack).

## 2. Gather

**Facts (from the task text):**
- Sustained emission: 5,000 events/s; peak 50,000/s for ~10-minute bursts.
- ~20,000 customer HTTPS endpoints; delivery = one POST per event per subscribed endpoint.
- At-least-once delivery required.
- Per-endpoint ordering required.
- ~5% of requests fail/time out at any given moment (transient); some endpoints down for hours (persistent).
- Retry with exponential backoff up to 24h, then dead-letter.
- Existing infra: Kafka, Postgres, Redis, on AWS. Cost-conscious.

**Assumptions (uncited — flagged, carried into the design, not silently absorbed):**
- **Fan-out factor** (load-bearing, unverified): average number of endpoints subscribed per event is small, assumed ~2 (most events target one customer's endpoint, some customers register multiple). This assumption directly sets peak throughput (100,000 deliveries/s vs. 50,000) and is the single biggest unknown in the design — verify against real subscription data before sizing infra.
- HTTP timeout per attempt ~5–10s.
- The "5% fail at any given moment" and "down for hours" are different populations: most endpoints are transiently flaky; a small subset (<1%) is the long-outage case.
- Backoff schedule fits ~6–8 attempts into 24h (e.g., 1m, 5m, 30m, 2h, 6h, 24h).
- Dead-letter storage is a Postgres table (+ optionally S3 for payload archive) — no new storage system.
- "Order" means: events for the same endpoint are POSTed in emission order; it does not cover ordering across different endpoints or event types.

## 3. Branch

**A — Kafka-partition-native ordering.** Partition the events topic by endpoint_id hash (~2–4k partitions, several endpoints sharing a partition); consumers process each partition and enforce in-memory per-endpoint sequencing/backoff.
**B — Kafka for ingestion, Postgres+Redis for delivery-state scheduling.** Kafka is a durable, fan-out-agnostic ingest log; a projector expands events into per-(event,endpoint) delivery records keyed by endpoint_id (not by Kafka partition), scheduled and retried via Redis/Postgres, independent of partition count.
**C — Best-effort ordering via sequence numbers.** No server-side blocking; attach a monotonic per-endpoint sequence number and let the customer reorder/buffer.

**Scoring against Frame's criteria:**
- **A:** Reuses Kafka fully; strict ordering falls out of partition assignment. But 20,000 endpoints sharing ~2–4k partitions means one endpoint's 24h-long retry can stall consumer progress for every other endpoint sharing its partition unless the projector/dispatch split is careful — repartitioning Kafka to avoid this is operationally heavy.
- **B:** Ordering enforcement scales with endpoint count, not partition count (a Redis key per endpoint, not a Kafka partition per endpoint) — directly fits 20,000 endpoints and isolates one endpoint's outage from all others. Reuses all three existing systems. Risk: naive Postgres-as-hot-scheduling-path may not sustain peak write rate (see Attack).
- **C:** Cheapest, highest throughput, no ordering machinery — but it does not meet the ordering success criterion as stated (it delegates correctness to customers, most of whom won't implement it reliably). Rejected against Frame, not against difficulty.

**Pick: B**, because it decouples per-endpoint order/backoff scheduling from Kafka's partition-count ceiling, which is the actual bottleneck at 20k endpoints, while reusing 100% of existing infra.
**Switch trigger:** if measured fan-out is very high (broadcast-style events to hundreds/thousands of endpoints), Postgres/Redis write volume could exceed B's budget — reconsider A (accept some cross-endpoint blocking) or relax to C for broadcast-only event types.

## 4. Attack

**Concrete numbers.** With fan-out ≈2: sustained 5,000×2 = 10,000 deliveries/s; peak 50,000×2 = 100,000 deliveries/s for 10 min. A naive version of B — Postgres row per delivery attempt, scheduler polling Postgres for `next_attempt_at <= now` — cannot sustain 100,000 inserts+updates/s on a single Postgres primary; this is a real failure, not a vibe. **This sends part of the design back to Branch:** move the *hot* scheduling path (ready queue, per-endpoint lock, retry-due set) to Redis, which comfortably handles 100k+ ops/s on a modest cluster; keep Postgres as an async, batched durable record (audit + dead-letter), off the hot path.

**Retry storm check:** 5% of 100,000/s = 5,000 failed deliveries/s needing rescheduling — fine for a Redis sorted-set (score = next_attempt_at), not fine for row-scanning Postgres at that rate. Confirms the Redis-hot-path revision.

**Steelman of A:** if fan-out turns out to be ~1 (mostly single-endpoint events), Kafka-native partitioning avoids standing up a second scheduling layer entirely and is simpler. This is a legitimate alternative if the team is willing to manage a few thousand Kafka partitions and do per-endpoint sequencing in-memory inside consumers — it survives as the fallback under the switch trigger above.

**Strongest surviving objection:** dead-lettering after 24h necessarily breaks strict ordering for that one event — later events for the same endpoint must eventually proceed or the endpoint's queue stalls forever on one bad event, defeating availability. Resolution (kept, not dismissed): after dead-letter, advance the endpoint's low-water-mark and continue with later events; the customer detects the resulting gap via the monotonic sequence number embedded in each payload. Ordering is therefore guaranteed *among successfully delivered events*, not across a dead-lettered one — an explicit, stated tradeoff, not a hidden gap.

## 5. Verify

**Check:** hand-trace one endpoint end-to-end. Event → Kafka (keyed by endpoint_id) → projector expands to per-endpoint delivery record, assigns sequence_no, writes to Redis `ready:{endpoint_id}` (fast) and batches an async insert to Postgres (durable). Dispatcher acquires `lock:{endpoint_id}`, pops the lowest pending sequence, POSTs; on success, advances low-water-mark, releases lock, Postgres row updated to `delivered`. On failure, computes backoff, pushes into `due_retries` ZSET scored by `next_attempt_at`, releases lock (so other endpoints aren't blocked) but does **not** advance the low-water-mark, so the *same* endpoint's next sequence number waits. A scheduler polls `due_retries` for due items and re-enqueues into `ready:{endpoint_id}`. After ~6–8 attempts spanning 24h, mark `dead_letter` in Postgres, advance low-water-mark, proceed. This trace satisfies: ordering (per-endpoint lock + sequence), at-least-once (retry until success or explicit DLQ), isolation of a down endpoint (keyed state, not shared partition/consumer), and throughput (hot path entirely in Redis).

**Re-reading Frame:** components/flow ✓, data model ✓ (below), failure handling ✓, explicit tradeoffs ✓, word budget — the design above is compressed to fit; the actual deliverable restates it within 900 words.

---

# Deliverable: Webhook Delivery Architecture

**Components & flow.** Events are published to a Kafka topic `events`, keyed by `endpoint_id` hash, giving cheap partition-level locality (not correctness — correctness comes from Redis, below). A **projector** service consumes each event, looks up subscribed endpoints, and for each (event, endpoint) pair assigns a per-endpoint monotonic `sequence_no`, writes the delivery's state into Redis, and asynchronously batch-inserts a durable row into Postgres. A pool of stateless **dispatcher workers** pulls, per endpoint, the lowest pending sequence number that isn't already in flight, POSTs it, and reports success/failure. A **retry scheduler** requeues due retries from a Redis sorted set. Dead-lettered deliveries are finalized in Postgres.

**Data model.**
- Postgres `deliveries` (durable, async-written, partitioned monthly): `id, event_id, endpoint_id, sequence_no, status[pending|in_flight|delivered|dead_letter], attempt_count, next_attempt_at, last_error, created_at, delivered_at`. Indexes on `(endpoint_id, sequence_no)` and `(status, next_attempt_at)` for audits/replays, not for hot scheduling.
- Redis (hot path): `ready:{endpoint_id}` — sorted set of pending sequence numbers; `lock:{endpoint_id}` — TTL'd mutex, exactly one in-flight POST per endpoint; `due_retries` — global sorted set scored by `next_attempt_at`; `low_watermark:{endpoint_id}` — last sequence resolved (delivered or dead-lettered), gating which sequence is eligible next.

**Failure handling.** Transient failures (the ~5% case) get exponential backoff with jitter (e.g., 1m, 5m, 30m, 2h, 6h, 24h — ~6–8 attempts). Long-down endpoints (hours) need no special code: their `ready` queue simply accumulates while retries reschedule further out; because state is keyed per endpoint in Redis, one dead endpoint never blocks Kafka consumer lag or any other endpoint's dispatch. An optional circuit-breaker widens backoff faster after N consecutive failures to stop wasting dispatcher capacity on a clearly-down endpoint (cost optimization, not correctness). After the 24h window elapses, the delivery is marked `dead_letter` in Postgres and the endpoint's low-water-mark advances so later events are not blocked forever.

**Ordering.** Guaranteed *among successfully delivered events* per endpoint via the Redis lock + low-water-mark: the next sequence number for an endpoint isn't dispatched until the previous one is resolved (delivered or dead-lettered). A dead-lettered event necessarily creates a visible gap — the customer's own sequence-number check surfaces it rather than hiding it. Cross-endpoint ordering is not attempted (not required).

**Tradeoffs (explicit).**
- We chose strict per-endpoint ordering over per-endpoint parallelism: a slow endpoint delays only its own queue, accepted as the cost of correctness.
- The hot scheduling path lives in Redis, not Postgres, to survive the estimated peak (~100,000 deliveries/s at an assumed ~2x fan-out); Postgres is async/durable-only. This trades a small window of potential inconsistency (e.g., Redis restart losing an in-flight lock) for throughput — mitigated with idempotency keys in the POST payload and Redis AOF persistence, but it is a real, accepted risk versus a fully synchronous Postgres design that couldn't hit peak throughput.
- Fixed backoff schedule and Redis-first design avoid new infrastructure (reuses Kafka/Postgres/Redis) but require dispatcher worker fleets sized for the 10-minute bursts specifically (autoscaled/spot, not provisioned 24/7 for peak) — the main new operating cost.
- The fan-out factor is an unverified assumption load-bearing enough to flip the whole scheduling-layer choice (Branch's switch trigger); it should be confirmed against real subscription data before capacity planning.

```text
STATUS: DONE_WITH_CONCERNS
CONFIDENCE: medium — would raise to high with real fan-out-per-event data instead of the assumed ~2x, since that number sizes the entire hot path and could flip the Branch pick toward Candidate A.
ASSUMPTIONS: (1) average fan-out per event ≈2, unverified — load-bearing for peak throughput sizing; (2) backoff schedule (~6–8 attempts over 24h) assumed rather than specified by the task; (3) "order" defined as per-endpoint delivery order, broken only by an explicit dead-letter, not covering cross-endpoint or cross-event-type ordering.
ATTACK: Strongest surviving objection is that dead-lettering after 24h necessarily creates a visible ordering gap for that one event — resolved by advancing the low-water-mark after DLQ and relying on the embedded sequence number for the customer to detect the gap, which is a stated tradeoff (order guaranteed only among delivered events), not a silent failure.
```
