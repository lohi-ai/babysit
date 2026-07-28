# P4 — Webhook delivery platform: architecture

## Frame

Restated task: design a webhook delivery system for a SaaS platform that emits
5,000 events/s sustained (bursting to 50,000/s for ~10 min), fans each event
out as one POST per subscribed endpoint across ~20,000 customer endpoints,
guarantees at-least-once delivery with strict per-endpoint ordering, retries
failed deliveries with exponential backoff for up to 24h before dead-lettering,
and must build on the team's existing Kafka/Postgres/Redis-on-AWS stack while
staying cost-conscious.

Success criteria (checkable):
1. Names concrete components and the event flow from ingestion to POST.
2. Gives an explicit data model (tables/keys) for delivery state.
3. Explains failure handling for ambient 5% failures, multi-hour endpoint
   outages, and the 24h→DLQ transition.
4. States explicit tradeoffs, not just a description.
5. Stays inside Kafka/Postgres/Redis (no new managed service without
   justifying why it's necessary).
6. Handles ordering at 50,000/s peak without one global serialization point.
7. Answer (excluding scaffold sections) fits in 900 words.

Out of scope: payload signing/HMAC auth, event schema, subscription-management
UI, multi-region DR.

Two readings worth flagging: "in order" could mean (a) events for an endpoint
must never be *acknowledged as delivered* out of order, even if retried, or
(b) just "don't reorder the queue." Reading (a) is the load-bearing one —
HTTP gives no cross-request ordering guarantee, so if two POSTs for the same
endpoint are ever in flight concurrently, the endpoint can *observe* them
out of order even if the queue itself was FIFO. I design for (a).

## Gather

**Facts** (from task):
- 5,000 events/s sustained; 50,000/s peaks for ~10 min.
- ~20,000 customer endpoints; delivery = one POST per event per subscribed
  endpoint.
- At-least-once + per-endpoint ordering required.
- ~5% ambient request failure rate; some endpoints down for hours.
- Retry with exponential backoff up to 24h, then dead-letter.
- Existing stack: Kafka, Postgres, Redis, on AWS; cost-conscious.

**Assumptions** (uncited, carried into the design):
- Average fan-out is roughly 1 endpoint per event (SaaS events are typically
  tenant-scoped to one registered endpoint). Even if the true fan-out factor
  is higher (e.g. 3–5x), per-endpoint rate stays low enough not to change the
  design — flagged, not verified.
- The 5% failure rate is roughly independent per request (no evidence of
  correlated platform-wide outages beyond "some endpoints down for hours").
- "Down for hours" stays under 24h; genuinely permanent death isn't
  explicitly bounded by the stated policy (see Attack).
- Kafka/Postgres/Redis have or can be given headroom for this workload —
  not verified against current cluster sizing, which I don't have.

## Branch

**A — Kafka partition-per-endpoint-hash, block-on-failure.** Hash endpoint_id
into ~500 partitions; a consumer per partition delivers in order and doesn't
advance its offset until each POST succeeds or DLQs.
*Score:* simplest to build; but partitions holding multiple endpoints mean one
endpoint down for hours blocks every other endpoint sharing that partition —
fails "some endpoints down for hours" directly.

**B — Kafka as durable fan-out log; Postgres per-endpoint delivery ledger;
Redis per-endpoint lock + retry timers.** Fan-out writes one ordered row per
(event, endpoint) into Postgres immediately (cheap, no HTTP wait). Separate
dispatcher workers hold a per-endpoint lock so exactly one POST is in flight
per endpoint at a time, preserving order independent of Kafka partitioning.
*Score:* isolates ordering enforcement from ingest sharding, so a dead
endpoint only stalls itself; stays inside the existing stack; matches the low
per-endpoint rate (well under 1/s average, a few/s at peak).

**C — SQS FIFO (MessageGroupId=endpoint_id) + Lambda dispatch.** Native
per-group ordering, no lock-management code needed.
*Score:* elegant, but introduces a new managed service the team doesn't run,
against the stated cost/ops preference; FIFO group throughput limits add
another constraint to manage. Rejected, but the strongest rejected idea —
steelmanned in Attack.

**Pick: B.** One-line why: it enforces ordering per endpoint without letting
Kafka's partition assignment create cross-endpoint head-of-line blocking, and
needs no new infrastructure. Switch trigger: if per-endpoint rate rose past
what serial dispatch can drain (~1/RTT), or endpoint count grew 100x straining
Postgres, switch to C (SQS FIFO) or endpoint-sharded Redis Streams.

## Attack

Concrete failing input: an endpoint goes down for 6h during a burst window
where it's receiving ~10 events/s (above its long-run average). That's
~216,000 queued events for one endpoint. Event 1 retries for up to 24h before
DLQ; events 2..N sit behind it under the per-endpoint lock and each only
starts *its own* 24h clock once event 1 resolves. If the endpoint never truly
recovers, the queue for that one endpoint can take N×(up to 24h) to fully
drain into the DLQ — the stated policy is per-event, not per-backlog, so nothing
bounds total time or storage for a permanently-dead endpoint. This is a real
gap, not a strawman: it survives even with correct locking.

Magnitude check: normal-load Postgres write rate at fan-out is bounded by
event rate (≤50,000/s peak, ×fan-out factor) — feasible for a partitioned,
indexed table with batched inserts, in line with known Aurora/RDS scale
points; not the bottleneck. The real ceiling is serial per-endpoint drain
rate (~1/RTT, e.g. 5/s at 200ms), which is inherent to ordering + HTTP, not a
flaw specific to candidate B — candidate A and C have the same physics.

Steelmanning C: SQS FIFO would have saved the lock-management code, but
doesn't remove the per-group serial-drain ceiling either, and still needs the
same circuit-breaker/backlog-cap fix below. Doesn't flip the pick.

Surviving objection, addressed with a refinement: add a circuit breaker per
endpoint (open after a configurable continuous-failure threshold, e.g. 2h)
that pauses wasted retry attempts, plus a queue-depth alert (not a hard drop —
DLQ is the only thing allowed to remove at-least-once obligations) so ops is
notified to pause/cancel a subscription before it grows unbounded. This
doesn't fully close the gap (nothing in the stated requirements allows
bulk-DLQing a backlog automatically) — it's called out as an open risk.

## Verify

Check: hand-trace one endpoint at steady state (1 event/s, 200ms RTT, 5%
failure) — service capacity (~5/s) exceeds arrival rate, so no backlog forms;
matches expectation of "5% ambient failure doesn't need special-casing."
Trace an outage: arrival continues, POSTs fail every attempt, backlog grows
at the arrival rate for the outage duration, then drains at ~5/s after
recovery — matches the Attack finding that drain time can exceed outage time
if backlog is large, confirming that finding rather than contradicting it.
Re-reading Frame: components/flow, data model, failure handling, and
tradeoffs are all present; ordering holds under peak via per-endpoint locking
rather than a global gate; design stays inside Kafka/Postgres/Redis. Criterion
7 (900-word deliverable) — checked below.

---

## Deliverable

**Architecture.** *Ingest & fan-out:* producers publish to Kafka topic
`events.raw`, partitioned by tenant_id for producer throughput (ordering here
is only for fairness, not the correctness guarantee). A stateless fan-out
consumer group reads it, resolves subscriptions (cached in Redis from
Postgres), and for each (event, endpoint) pair writes one row into a Postgres
`delivery_queue`, stamped with a per-endpoint monotonic `sequence_no`. This
single write path is where canonical per-endpoint order is established,
decoupled from HTTP success/failure. *Dispatch:* a pool of stateless workers
(ECS/Fargate, autoscaled on queue depth) hold a Redis lock per endpoint
(`SETNX endpoint:{id}:lock`) so at most one POST per endpoint is ever in
flight; they fetch the oldest pending row for an idle endpoint, POST it, and
release/reschedule the lock on outcome. Because ordering is enforced by the
lock, not by Kafka partition assignment, a dead endpoint only stalls its own
queue. *Retry:* on failure, workers compute a jittered exponential
`next_retry_at`, store it in a Redis ZSET (score = due time) and update the
Postgres row; a scheduler polls the ZSET and re-queues due retries. Past 24h
since first attempt, the row moves to `dead_letter` and the lock releases for
the next event. A circuit breaker flags endpoints failing continuously past a
threshold (e.g. 2h) to stop wasted retries and alert ops on excessive queue
depth. *Storage:* Postgres is the durable ledger (source of truth,
partitioned by day for pruning); Redis is purely ephemeral coordination —
losing it costs a rebuild pass from Postgres, not correctness.

**Data model.**
- `endpoints(endpoint_id PK, tenant_id, url, secret, status)`
- `subscriptions(endpoint_id FK, event_type)`
- `delivery_queue(id PK, endpoint_id FK, event_id, sequence_no, payload_ref,
  status[pending|inflight|retrying|delivered|dead_letter], attempt_count,
  first_attempt_at, next_retry_at, last_error)` — indexed on
  `(endpoint_id, sequence_no)`, partial index on active statuses for
  dispatcher scans.
- `endpoint_cursor(endpoint_id PK, next_sequence_no)` for sequence
  allocation at fan-out time.
- Payload bodies stay in Kafka/S3, referenced by `payload_ref`, to keep
  Postgres rows small and cheap.

**Failure handling.** Ambient 5% failures use ordinary backoff with no
special-casing. Multi-hour outages are contained to the affected endpoint via
the per-endpoint lock; the circuit breaker prevents retry storms; DLQ fires
per-event at 24h. Bursts are absorbed by Kafka's durable buffer and by
horizontally scaling the fan-out and dispatch pools; Postgres write rate at
peak is the main capacity risk, mitigated with batched inserts and
PgBouncer-style pooling, with Aurora as headroom if needed.

**Tradeoffs.** Single-flight-per-endpoint ordering is chosen over higher
concurrency because HTTP can't order concurrent requests — this caps
post-outage drain rate at roughly one RTT per event, accepted because normal
per-endpoint rate is far below that ceiling. Postgres row-based state is
chosen over a pure Kafka-offset model for flexible retry/circuit-breaker
logic and queryable DLQs, at the cost of one write per (event, endpoint) pair
and a second system of record. New managed infra (SQS FIFO, Step Functions)
is explicitly rejected to stay inside the team's operated stack, revisit if
endpoint count or per-endpoint rate grows an order of magnitude. Dead-lettering
is bound per event, not per endpoint-outage, so a permanently-dead endpoint
can still accumulate an unbounded backlog before ops intervenes — mitigated
with depth alerting, not fully solved.

```text
STATUS: DONE_WITH_CONCERNS
CONFIDENCE: medium — would raise to high with real fan-out ratio and current Kafka/Postgres cluster headroom numbers from the team.
ASSUMPTIONS: (1) average fan-out ≈1 endpoint/event, not stated in the task; (2) 5% failures roughly independent, not correlated platform-wide; (3) existing Kafka/Postgres/Redis clusters have headroom for this workload, unverified.
ATTACK: a permanently-dead endpoint's backlog is bounded per-event (24h retry→DLQ) but not per-backlog, so total drain time/storage for that one endpoint is technically unbounded until ops manually intervenes — mitigated with circuit-breaker + depth alerting, but not fully closed by the stated retry policy.
```
