# P7 rubric (grader only — subjects must never see this)

8 traps, 1 pt each, binary: the answer must **resolve** the trap (concrete
mechanism/number), not merely mention the topic. Every trap composes ≥2 spec
constraints.

- **T1 — late events × immutable close.** Picks and specifies a close
  policy (grace period past cycle close, or issue-then-adjust) AND handles
  events arriving after issuance via a supplementary/credit document — not
  silent drop, not invoice mutation. Both halves required.
- **T2 — raw-event magnitude quantified.** Computes retention volume
  (~100M/day → ≈40B events / multi-TB over 13 months) and uses the number
  to place raw events in object storage with an index for drill-down,
  keeping OLTP small. Numbers required; "store in S3" without arithmetic
  doesn't count.
- **T3 — immutability mechanism.** Corrections/disputes produce a new
  referencing document (credit note / adjustment invoice); original is
  never edited. Must be stated as the mechanism, not implied.
- **T4 — reproducibility.** Versioned/bitemporal price table + invoice
  pins the price version (or snapshot) + aggregation is deterministic
  (stable ordering / order-independent reduce). Recompute = identical
  invoice. Needs both the price-versioning and the determinism half.
- **T5 — dedup sized.** At-least-once × 7-day lateness ⇒ dedup window
  ≥7 days; estimates key volume (~700M keys) or state size and places the
  store accordingly. A bare "dedupe on event_id" with no window/size
  reasoning doesn't count.
- **T6 — mid-cycle price change.** Line items split by price-version
  effective interval within one cycle (two sub-line-items or equivalent),
  not "use latest price".
- **T7 — timezone-anchored close.** Cycle close computed in the customer's
  IANA tz including day-of-month anchor clamping (day-31 signups) and the
  midnight-DST edge; a scheduler that handles 50k per-customer closes.
  Generic "cron at midnight UTC" fails.
- **T8 — aggregation path correctness at scale.** Explains how line items
  are computed given 5k/s peak + lateness + dupes: e.g. aggregate-on-close
  from the immutable raw log, or streaming increments reconciled against
  raw at close. Must address why late/duplicate events cannot corrupt the
  issued total.
