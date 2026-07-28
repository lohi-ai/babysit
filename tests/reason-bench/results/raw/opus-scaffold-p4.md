# P4 — Webhook delivery platform: reasoning + design

## Move 1 — Frame

**Task restated:** Design a webhook fan-out/delivery system that ingests events at 5,000/s sustained (bursting to 50,000/s for ~10-minute windows), delivers one HTTPS POST per event per subscribed endpoint across ~20,000 customer endpoints, guarantees at-least-once delivery, guarantees per-endpoint ordering, retries failures with exponential backoff for up to 24h before dead-lettering, tolerates ~5% instantaneous endpoint failure plus some endpoints being down for hours, and reuses the team's existing Kafka/Postgres/Redis-on-AWS stack cost-consciously.

**Success criteria (checkable):**
1. System absorbs a sustained 5,000 events/s and a 50,000/s, 10-minute burst without dropping events or building an unrecoverable backlog.
2. For any endpoint, deliveries are POSTed in the same order the source events occurred, even across retries.
3. No event is permanently lost due to a transient failure — every delivery either succeeds or is explicitly dead-lettered after 24h.
4. One endpoint being down for hours does not degrade delivery to other endpoints.
5. The design's steady-state cost is dominated by reused infra (Kafka/Postgres/Redis), not new systems, and new load is proportional to the *failure* rate, not the full traffic rate.

**Out of scope:** payload signing/HMAC auth, the subscription-management API/UI, multi-region disaster recovery, exact autoscaling policy numbers, and anything about *which* events are emitted (only their delivery).

**Two materially different readings worth naming, not silently picking:**
- **Reading A (strict):** "must arrive in order" means a later event for endpoint E must never be POSTed before an earlier event for E has succeeded — i.e., true head-of-line blocking per endpoint.
- **Reading B (loose):** "in order" just means the provider doesn't race multiple concurrent attempts for the same endpoint that could land out of sequence, but a bounded amount of reordering under failure would be tolerable if flagged.

I take **Reading A** — the task says "must arrive in order" without qualification, and it's the reading that actually has to shape the architecture (loose ordering barely constrains the design at all, so treating it as the binding constraint is also the more useful exercise).

## Move 2 — Gather

**Facts (from the task):**
- 5,000 events/s sustained; peaks to 50,000/s for ~10-minute bursts.
- ~20,000 subscribed customer HTTPS endpoints; delivery = one POST per event per subscribed endpoint.
- At-least-once delivery is required.
- Per-endpoint ordering is required.
- ~5% of requests fail/time out at any given moment (background failure rate); some endpoints are down for hours (extended outages).
- Retry with exponential backoff for up to 24h, then dead-letter.
- Existing operated infra: Kafka, Postgres, Redis, on AWS. Cost-consciousness is an explicit constraint.

**Assumptions (uncited — must be verified or carried into the output):**
- **Fanout factor** (endpoints subscribed per event) is not given. I assume it's low, roughly 1–2 endpoints per event, because the event rate (5k–50k/s) and endpoint count (20k) are the same order of magnitude — consistent with "an event belongs to a tenant and goes to that tenant's configured endpoint(s)," not a broadcast-to-all-endpoints model. If fanout were e.g. 20,000 (broadcast), delivery volume would be 10^8/s, which is implausible and clearly not intended. *Confidence: medium-high, but load-bearing — carried into Tradeoffs.*
- **Traffic distribution across endpoints is roughly uniform**, not dominated by a handful of "whale" endpoints. This matters for backlog sizing during multi-hour outages. *Carried into design as a hedge (Postgres spillover), not just assumed away.*
- Kafka is MSK, Postgres is RDS/Aurora, Redis is ElastiCache — standard AWS-managed counterparts of "the team already operates X."
- Dead-lettered events should be durably archived and queryable/replayable, even though the task doesn't explicitly ask for replay — a dead-letter with no way to inspect or replay it is not actionable, so I treat minimal visibility as implied.

## Move 3 — Branch

**Candidate A — Kafka partition-per-endpoint, retry-in-partition.** Fan out each event into a `deliveries` topic keyed by `endpoint_id`, one partition per endpoint, so Kafka's native per-partition ordering *is* per-endpoint ordering; consumers don't advance past a failed record for that endpoint. *Score:* ordering is free (+), but 20,000 partitions on one topic is well past comfortable Kafka operating limits (partition count drives controller metadata size and failover time; guidance from operators of large clusters is usually a few thousand partitions per broker, not tens of thousands on one topic) (–). Also, Kafka has no native "retry in 6 hours" primitive — an endpoint down for hours would need its partition's consumption simply paused for hours, which stalls offset commits and retention housekeeping for that partition (–).

**Candidate B — Postgres/Redis as the sole engine, Kafka ingest-only.** Every (event, endpoint) pair becomes a row in a Postgres `deliveries` table the moment it's produced; workers poll for due rows (`SELECT ... FOR UPDATE SKIP LOCKED`), Redis locks enforce per-endpoint single-flight. *Score:* trivially supports arbitrary backoff durations and dead-letter as a status column (+), simple single source of truth (+), but every single delivery — not just failures — becomes a Postgres write, and at fanout-adjusted peak that's tens of thousands of row writes/sec, which is the classic "relational DB as a full-throughput queue" anti-pattern (–).

**Candidate C — Hybrid: Kafka for the cheap happy path, Postgres/Redis only for failures.** Kafka (small, fixed partition pool, keyed by `endpoint_id`) carries all first-attempt traffic; workers attempt delivery immediately off the Kafka read, and only failures get written to Postgres/Redis for backoff+retry+ordering-enforcement. *Score:* steady-state Postgres load is ~5% of traffic, not 100% (+), reuses all three systems for what each is good at (+), avoids the partition-count blowup of Candidate A (+), but requires care to keep the "cheap path" from silently breaking either at-least-once or ordering guarantees (–, addressed in Attack).

**Pick: Candidate C.** One-line why: it's the only option whose steady-state cost scales with the *failure* rate rather than the full traffic rate, while still reusing existing infra for its strengths (Kafka for durable ordered ingest, Postgres for durable retry state, Redis for ephemeral per-endpoint coordination).

**Switch trigger:** if the fanout-factor assumption turns out wrong and is actually high (many endpoints per event), or if per-endpoint traffic turns out to be extremely skewed toward a handful of hot endpoints such that Redis-tier buffering can't absorb bursts cheaply, switch to Candidate B and accept the higher steady-state DB cost in exchange for a single, simpler source of truth.

## Move 4 — Attack

**Concrete failure to construct:** Candidate C as first drafted implicitly claimed "one partition per endpoint" ordering/isolation, but Move 3 already rejected that as infeasible at 20,000 endpoints and proposed a *fixed, small* partition pool (e.g., 256–512 partitions) with `endpoint_id` hashed onto it. That means **multiple endpoints share a Kafka partition.** If the design "pauses the partition" when one endpoint on it fails — the naive way to get ordering from Kafka — then a single endpoint down for hours **blocks every other endpoint hashed to that same partition**, directly violating success criterion 4 (isolation). This is the strongest surviving objection, and it would have been missed by fluent narrative alone.

**Magnitude check on the fix:** the fix is to decouple "reading order off Kafka" from "per-endpoint delivery order enforcement" — always commit/advance the Kafka offset quickly (non-blocking), and enforce true per-endpoint FIFO ordering at the application layer via a Redis lease + per-endpoint pending buffer, independent of which Kafka partition the events came from. Sanity-checking backlog size: at 5,000 events/s sustained over ~20,000 endpoints with low fanout, that's ~0.25 events/s per endpoint on average — a 24h outage for one average endpoint accumulates on the order of ~20,000 events, trivial to hold. But this depends on the *uniform-traffic* assumption; if traffic is skewed and a "whale" endpoint carries a large share of the 5,000/s, a multi-hour outage on that one endpoint could accumulate hundreds of thousands of pending events — too large to safely hold only in Redis memory. This is why the design needs a bounded Redis FIFO with Postgres spillover, not Redis alone, specifically to hedge against the skew assumption being wrong.

**Re-checking the at-least-once guarantee:** if a worker commits the Kafka offset *before* durably recording the delivery attempt (in Redis/Postgres), a crash between those two steps silently drops the event — violating criterion 3. The design must record-then-commit (or be crash-idempotent), never commit-then-record. This is a load-bearing implementation-order requirement, not a nice-to-have, and is called out explicitly in the deliverable below.

**Steelmanning the rejected Candidate B:** its real strength is operational simplicity — one source of truth, no dual-path retry logic (Redis short-term + Postgres long-term) to reason about at 3am, easier to debug "what's the state of delivery X." If burst traffic is actually much lower in practice than the stated peak, or if engineering time to build the hybrid's coordination correctly would cost more than the AWS spend it saves, B is a legitimate, simpler alternative — captured as the switch trigger above rather than dismissed.

## Move 5 — Verify

**Check defined:** hand-trace a concrete scenario through the picked (and Attack-corrected) design and confirm ordering, isolation, and at-least-once all hold.

*Trace:* Endpoint E17 has three events e1(seq 1), e2(seq 2), e3(seq 3), 1s apart, all hashed to shared Kafka partition P42 (also carrying other endpoints' traffic). Worker reads e1 off P42, durably records "E17 attempt seq 1, inflight" (Redis/Postgres) **before** committing the offset, commits, POSTs — fails (timeout). Records "seq 1, retrying, next_attempt_at = now+30s." Worker reads e2, e3 off P42 (order preserved because Kafka preserves within-partition order and reads are still fast/non-blocking); since E17 has an outstanding attempt, e2 and e3 go into E17's Redis FIFO rather than being attempted — order preserved without stalling P42 for other endpoints. At now+30s the scheduler retries seq 1; it succeeds; the worker then pops e2, delivers, then e3. Result: 1, 2, 3 delivered in order despite a mid-stream failure — **criterion 2 holds**. Other endpoints sharing P42 were never blocked — **criterion 4 holds**. Because record happened before commit, a crash at any point replays rather than drops — **criterion 3 holds**, at the cost of possible duplicate delivery on replay, which is why "at-least-once" (not "exactly-once") is the stated guarantee, matching the requirement. *Confidence: medium* — the logic is sound on paper, but the record-before-commit ordering and the FIFO/lease mechanics are exactly the part that needs correctness tests (crash-mid-attempt, concurrent worker restarts) before trusting it in production.

**Re-reading Frame:** all five success criteria are addressed by the corrected design; nothing drifted — the deliverable below states the architecture, data model, failure handling, and tradeoffs as asked, and stays inside the 900-word budget.

---

## Deliverable

### Architecture

**Ingest.** Events land on the existing Kafka (MSK) topic `events.raw`, partitioned by tenant, as today. A fan-out service reads each event, looks up active subscriptions (Postgres, cached in Redis), and for each matching endpoint writes one record to a `deliveries.attempts` Kafka topic, keyed by `endpoint_id` and hashed onto a fixed pool of ~256–512 partitions — not one partition per endpoint, since 20,000 partitions on one topic strains Kafka's per-broker partition limits and controller failover time.

**Delivery workers.** A stateless, autoscaled worker fleet consumes `deliveries.attempts`. Consumption is always fast and non-blocking: a worker reads a record, durably records the attempt against the endpoint's delivery state (Postgres/Redis) **before** committing the Kafka offset, then commits — this ordering is what makes at-least-once hold across crashes. Per-endpoint ordering is enforced at this layer, not by Kafka: each endpoint has a Redis lease ("one in-flight attempt per endpoint") plus a small FIFO for events that arrive while an earlier one is still retrying. This decouples "reading off Kafka cheaply, in bulk" from "delivering to one endpoint in strict order," so a dead endpoint sharing a partition with healthy endpoints never blocks them. Each POST uses a short timeout (~5s) and a per-endpoint circuit breaker in Redis: after a few consecutive failures the breaker opens and further attempts jump straight to the backoff schedule instead of hammering a dead endpoint.

**Retry ledger.** Only failed/retrying deliveries are persisted to Postgres (existing RDS/Aurora) — the happy path (~95% of attempts) never touches Postgres beyond a lightweight audit write. A scheduler (every 10–30s) scans rows whose `next_attempt_at` is due and re-injects them into the endpoint's Redis FIFO. Steady-state write volume is roughly proportional to the failure rate, not total traffic.

**Dead-letter.** After 24h of exponential backoff (e.g., 30s, 1m, 5m, 30m, 2h, 6h, 24h, with jitter), a delivery is marked dead, archived, and surfaced for manual replay.

### Data model (Postgres)

- `subscriptions(endpoint_id, tenant_id, url, event_types[], active)`
- `deliveries(delivery_id PK, event_id, endpoint_id, seq_no, status, attempt_count, next_attempt_at, last_error, created_at)` — indexed on `(endpoint_id, next_attempt_at)` for the scheduler and `(endpoint_id, seq_no)` for ordering/audit.
- `dead_letters(delivery_id FK, endpoint_id, payload_ref, reason, dead_at)`

### Failure handling

- **Transient (~5%) failures:** short Redis-tier retries within minutes; no Postgres write until the second failure.
- **Extended outages (hours):** circuit breaker stops wasted attempts; backlog accumulates in the endpoint's Redis FIFO up to a bounded size, then spills to Postgres keyed by `(endpoint_id, seq_no)` — bounding memory and protecting against skewed ("whale endpoint") traffic.
- **Ordering:** guaranteed per endpoint by the lease + FIFO, independent of retry tier — a later event never overtakes an earlier one still retrying.
- **At-least-once:** guaranteed by recording delivery state before committing the Kafka offset; a crash between the two replays rather than drops the event (possible duplicate, never loss).

### Tradeoffs

- **Ordering is enforced at the application layer (Redis lease + FIFO), not by Kafka partitioning.** A pure "one partition per endpoint" model would be simpler but is infeasible at 20,000 endpoints; the tradeoff buys scalability at the cost of more application logic to get right and test.
- **Postgres only sees failures, not successes** — low steady-state cost, but DB load is proportional to the failure rate; if real-world failures run persistently above the assumed 5%, this needs re-evaluation.
- **Assumed low fanout (~1–2 endpoints/event)**, inferred from event-rate and endpoint-count being the same order of magnitude, not stated directly — if wrong (broadcast-style fanout), ingest and Kafka partition/throughput sizing must be redone.
- **Assumed roughly uniform traffic across endpoints** for buffer sizing; the Postgres spillover path exists specifically to hedge against this being wrong.
- **Out of scope:** payload signing/auth, subscription-management API, multi-region DR, exact autoscaling tuning.

```text
STATUS: DONE_WITH_CONCERNS
CONFIDENCE: medium — would raise to high with real fanout/traffic-skew numbers and a load test of the record-before-commit path under worker crash/restart.
ASSUMPTIONS: (1) fanout per event is low (~1–2 endpoints), inferred from event-rate/endpoint-count magnitude, not stated; (2) traffic is roughly uniform across endpoints, not whale-dominated — hedged with Postgres spillover but not verified.
ATTACK: shared Kafka partitions mean a naive "pause partition on failure" breaks endpoint isolation; survives because ordering is enforced by a per-endpoint Redis lease/FIFO decoupled from partition consumption, not by pausing the partition itself — but this decoupling is exactly the part needing crash/restart correctness tests before production trust.
```
