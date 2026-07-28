# QA Plan — Recurring Billing Engine

## Test strategy

**Layers**
- *Unit*: proration math, dunning schedule calculator, anchor-date/timezone resolution — pure functions, property-tested against hand-derived expected values.
- *Contract*: PSP client against a recorded/simulated PSP (success, soft-decline, hard-decline, timeout, malformed webhook) so tests don't depend on live Stripe sandbox flakiness.
- *Integration*: billing scheduler + ledger + PSP client + webhook handler wired together with a fake clock and a fake PSP, driven end-to-end for one subscription lifecycle.
- *Time-travel / chaos*: fast-forward the fake clock across billing cycles, DST boundaries, leap years, and inject delayed/duplicate/out-of-order webhooks and PSP outages.
- *Reconciliation*: batch job that cross-checks internal ledger totals against PSP-reported charges, run nightly in a shadow environment before go-live.

**Tooling/fixtures**: controllable fake clock (not `sleep`), a PSP simulator with scriptable outcomes and adjustable webhook latency/ordering, a matrix of customer fixtures spanning timezones (incl. half/quarter-hour offsets — India, Nepal), DST-observing vs non-observing regions, monthly/annual anchors on the 29th/30th/31st and Feb 29, and multi-currency accounts.

## Prioritized test cases (highest-miss first)

1. **DST transitions on the anchor date/time.** Anchor at 2:30am on a spring-forward night that doesn't exist, or during the fall-back repeated hour. Verify the engine picks one deterministic interpretation, not a crash or double-fire.
2. **Webhook non-guarantees**: out-of-order delivery (final outcome arrives before/without the initial "processing" event), duplicate delivery, and webhook that never arrives (timeout with no resolution). This is the single most commonly under-tested area — most teams test the happy webhook path only.
3. **Idempotency under ambiguous PSP responses**: charge request times out but the PSP actually succeeded; retry must not double-charge. Requires idempotency keys tested against actual duplicate submission, not just code inspection.
4. **Proration compounding**: upgrade then downgrade (or vice versa) within the same cycle, downgrade to a $0 plan, and a plan change that lands exactly on the anchor timestamp (is it "before" or "after" the new cycle starts?).
5. **Dunning interacting with mid-cycle plan changes**: customer upgrades while an old invoice is still in dunning retry — does the new charge conflict with the retrying old one, and does canceling during dunning stop the schedule.
6. **Customer changes timezone** (moves, or profile edit) between billing cycles — anchor must not silently shift or double-fire relative to the old zone.
7. **Concurrent webhook processing**: two webhook deliveries for the same subscription processed by different workers simultaneously — race on ledger state.
8. **Clock skew** between the billing scheduler's clock and the PSP's webhook timestamp — schedule must key off internal state, not trust PSP wall-clock blindly.
9. **Leap-year annual anchors** (Feb 29 subscriber) in non-leap years — must resolve to a fixed, documented day (Feb 28 vs Mar 1), consistently every year.
10. **Retry-exhaustion terminal state**: after the last dunning attempt, does the subscription actually deactivate access, or silently stay active with the invoice stuck open.
11. **Currency rounding drift** in proration across many small adjustments — verify no cumulative rounding leak in the ledger.

## Evidence and falsification, by area

**Scheduling/anchoring** — PASS: a fixed cohort of fixtures (all timezones, DST edges, leap-day anchors) billed correctly across a simulated 3-year fast-forward, verified against an independently hand-computed expected-charge table. FALSIFY: any subscription fires twice in one cycle, skips a cycle, or the DST-ambiguous hour resolves differently across two runs (non-determinism).

**Payment/dunning** — PASS: injected soft-decline sequences retry on the documented schedule and terminate (cancel or hard-fail) exactly at the configured attempt count, verified via ledger + subscription-state audit log. FALSIFY: a charge retried after a hard-decline, a subscription still "active" after dunning exhaustion, or two charge attempts recorded for one retry slot.

**Idempotency/webhooks** — PASS: replaying the same webhook 5x, out of order, and with an injected duplicate charge-attempt all leave the ledger in the single correct final state, confirmed by reconciliation diff = 0 against the PSP simulator's ground truth. FALSIFY: any replay changes the ledger balance, or a lost/delayed webhook leaves a subscription stuck in "processing" indefinitely with no reconciliation sweep to unstick it.

**Proration** — PASS: property-based test asserting prorated amount for upgrade+downgrade nets to the same total as if the final plan had been active the whole cycle (within one rounding unit), across 500+ randomized change sequences. FALSIFY: any sequence where total charged diverges from that invariant, or a downgrade-to-zero produces a negative invoice.

**Reconciliation (release gate)** — PASS: nightly shadow-mode reconciliation between internal ledger and PSP dashboard shows zero unexplained deltas for 7 consecutive days pre-launch. FALSIFY: any unexplained delta, however small — that is the leading indicator of a webhook or idempotency bug that unit tests won't catch.
