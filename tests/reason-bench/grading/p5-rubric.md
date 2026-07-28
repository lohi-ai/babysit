# P5 rubric — recurring billing QA (8 pts)

GRADER ONLY.

- **T1 — month-end anchors.** Jan 31 → Feb 28/29 → Mar 31 behavior specified
  and tested (incl. leap year, and whether anchor "snaps back" to 31st).
- **T2 — DST.** Anchor time falls in the nonexistent hour (spring forward) or
  repeated hour (fall back); billing day boundary shifts.
- **T3 — timezone change mid-subscription.** Customer moves timezones (or org
  setting changes): does the anchor follow, can a period shorten/lengthen,
  can a change cause a skipped or double bill?
- **T4 — double-charge idempotency.** PSP timeout after success, webhook
  replay/duplicate, dunning retry racing a slow success — evidence must
  include a ledger invariant (≤1 successful charge per subscription-period).
- **T5 — proration edges.** Upgrade on the anchor instant, multiple plan
  changes in one cycle, downgrade credit handling, annual↔monthly switch.
- **T6 — time-control tooling.** Clock injection / time-travel harness named
  as a required fixture — you cannot wait a month; frozen-clock tests for DST
  and month-end tables.
- **T7 — falsifiable PASS evidence.** For each area, states what evidence
  marks PASS and what would falsify it (e.g., duplicate-charge ledger query
  returning >0; invariant sweep over generated periods). "All tests green"
  without invariants fails this.
- **T8 — webhook race conditions.** PSP webhook vs concurrent user action
  (cancel/upgrade during pending charge), out-of-order webhooks, dunning
  state machine transitions under concurrency.
