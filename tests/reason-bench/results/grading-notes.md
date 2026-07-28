# Grading notes (grader: Fable, per-trap binary vs rubric)

## P1 — feature planning (7 traps: T1 merge-model, T2 auth/permission drift,
T3 long-divergence UX, T4 phased cut, T5 device durability, T6 checkable
criteria, T7 protocol migration)

| run | score | misses |
|-----|-------|--------|
| haiku-base | 6/7 | T2 |
| sonnet-base | 6/7 | T2 |
| opus-base | 6/7 | T2 |
| haiku-scaffold | 7/7 | — |
| sonnet-scaffold | 6/7 | T2 (names it out-of-scope but doesn't say what happens to queued edits) |
| opus-scaffold | 6/7 | T2 (same) |

Notes: ceiling effect — all tiers strong at planning. Qualitative delta the
trap score under-captures: all three BASE runs assumed a CRDT adoption
directly; all three SCAFFOLD runs surfaced "current engine OT vs CRDT is
unknown and load-bearing" and made it a gating Phase-0 spike — textbook
Gather/Assumption discipline. haiku-scaffold was the only run to resolve
auth-token expiry offline (cached refresh tokens + visible sync block).

## P3 — UI design (7 traps: T1 select-all-matching vs page, T2 non-blocking
job progress, T3 per-row failure report/retry/export, T4 honest undo, T5
calibrated confirmation, T6 stale-selection snapshot-vs-revalidate, T7 a11y)

| run | score | misses |
|-----|-------|--------|
| haiku-base | 7/7 | — |
| sonnet-base | 7/7 | — (T6 weakest: filterSnapshot in model + concurrent-edit
  failures, but confirm-vs-execute resolution less explicit than others) |
| opus-base | 7/7 | — |
| haiku-scaffold | 7/7 | — |
| sonnet-scaffold | 7/7 | — |
| opus-scaffold | 7/7 | — |

Notes: complete ceiling — bulk-edit UX (Gmail select-all pattern, async jobs,
compensating undo) is deeply represented in training data at every tier; the
problem doesn't discriminate. Scaffold runs added explicit assumption
registers and concurrency attack scenarios (all three independently invented
the "second admin edits row mid-job" attack) but no trap-score delta.

## P5 — QA plan (8 traps: T1 month-end anchors, T2 DST, T3 tz-change
mid-sub, T4 double-charge idempotency + ledger invariant, T5 proration
edges, T6 time-control tooling, T7 falsifiable PASS, T8 webhook races)

| run | score | misses |
|-----|-------|--------|
| haiku-base | 7/8 | T4 (idempotency store + reconciliation present, but the
  charge-timeout→retry ambiguous-outcome double-charge case never named) |
| sonnet-base | 8/8 | — |
| opus-base | 8/8 | — |
| haiku-scaffold | 7/8 | T1 (Feb 29 covered; Jan-31→short-month anchor not) |
| sonnet-scaffold | 6/8 | T1 ("month-boundary" mentioned in a layer label,
  no named case), T3 (tz-change mid-sub absent from case list) |
| opus-scaffold | 7/8 | T1 |

Notes: the bench's first NEGATIVE scaffold result. All three base runs
enumerated the Jan-31/month-end anchor case; all three scaffold runs missed
it. Plausible mechanism: the five-move scaffold consumes deliverable word
budget on meta-reasoning (Frame/Branch/Attack), squeezing checklist breadth
in a ≤800-word cap — QA-plan quality here is enumeration-driven, and base
answers simply listed more edge cases. Scaffold runs were stronger on
assumption honesty (all flagged "idempotency keys unverified" as a question
for engineering) but that isn't what T1 measures.

## P4 — system architecture (8 traps: T1 ordering×retry HOL, T2 backlog
magnitude quantified + next_attempt_at store, T3 breaker/DLQ, T4 jitter,
T5 delivery-ID + HMAC, T6 20k endpoints vs partitions, T7 delivery-state
schema, T8 customer observability/replay)

| run | score | misses |
|-----|-------|--------|
| haiku-base | 6/8 | T2 (no quantitative backlog estimate), T5 |
| sonnet-base | 6/8 | T2, T5 |
| opus-base | 5/8 | T2, T5, T8 (no customer-facing surface) |
| haiku-scaffold | 6/8 | T5, T8 (ops alerts only) |
| sonnet-scaffold | 7/8 | T5 |
| opus-scaffold | 7/8 | T5 |

Notes: the scaffold's clearest positive delta, and it comes from exactly the
move designed to produce it — Attack's magnitude check. haiku-scaffold
computed a 216k-event per-endpoint backlog and the ~5/s serial drain ceiling;
sonnet-scaffold computed 100k deliveries/s peak and used it to move the hot
path off Postgres; opus-scaffold computed ~20k events per average-endpoint
24h outage vs whale-endpoint skew and added Postgres spillover because of it.
No base run quantified anything. T5 (HMAC signing) missed by all six — the
scaffold runs at least declared signing out-of-scope explicitly; base runs
silently carried a `secret` column they never used. T1 resolved by all six;
sonnet-scaffold's low-water-mark advance after DLQ was the cleanest answer
to the "dead-letter breaks strict ordering" tension.
