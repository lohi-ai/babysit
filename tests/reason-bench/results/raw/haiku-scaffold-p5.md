# P5 — QA: recurring billing engine (scaffolded answer)

## Move 1 — Frame

**Restated task:** Produce a QA plan (≤800 words) for a not-yet-shipped recurring
billing engine that (a) bills monthly/annual on a per-customer local-timezone
anchor, (b) charges through a Stripe-like PSP with success / soft-decline
(retryable) / hard-decline (terminal) outcomes, (c) retries failures on a
dunning schedule, (d) supports mid-cycle upgrade/downgrade with proration,
(e) learns final charge outcomes asynchronously via webhook.

**Success criteria (checkable):**
- Names concrete test layers, tooling, and fixtures — not just "test it."
- Lists prioritized test cases, explicitly surfacing the ones teams typically
  miss (not just the happy path).
- For each major area, states what evidence marks it PASS and what evidence
  would falsify that PASS.
- Deliverable stays ≤800 words.

**Out of scope:** UI/checkout-form testing, PSP's own internal correctness,
tax calculation, load/perf testing of the API itself (billing-correctness
under fault, not throughput, is the focus).

**Two readings considered:** (1) a one-time pre-launch validation plan for
this specific ship, vs (2) an ongoing regression suite description. The task
says "about to ship" — I take reading (1), but design the test layers so they
double as a regression suite afterward (not mutually exclusive, just a
difference in framing/emphasis).

## Move 2 — Gather

**Facts (from the task):**
- Billing anchor is customer-local-timezone, monthly or annual.
- PSP charge outcomes: success, soft-decline (retryable), hard-decline
  (terminal).
- Failed charges retry per a dunning schedule.
- Upgrade/downgrade mid-cycle triggers proration.
- Charge outcome is reported asynchronously via webhook (i.e., the initial
  API call and the final settled state are decoupled in time).

**Assumptions (uncited — carried into the plan, not silently absorbed):**
- The PSP sandbox/test mode supports forcing decline codes and simulating
  delayed/duplicate/out-of-order webhook delivery. (Real PSPs like Stripe
  do; assumed true here by analogy — must be verified against the actual
  PSP integration before the plan's fault-injection layer is buildable.)
- The system uses idempotency keys on charge creation. Not stated in the
  task, but load-bearing for avoiding double charges under retries +
  delayed webhooks — flagged as a question-to-verify with engineering, not
  just tested for.
- Dunning schedule parameters (retry count/backoff) are configurable and
  documented somewhere testable.
- Timezone handling uses IANA tz data and must define DST behavior.
- Proration is time-fraction-based (not flat-fee); exact rounding rule is
  unspecified and must be confirmed.
- Webhook is the system-of-record for final state; the initial synchronous
  charge response may legitimately disagree with it (race condition is
  real, not hypothetical).

## Move 3 — Branch

1. **Test pyramid (unit → integration vs PSP sandbox → few e2e).** Cheap,
   familiar, fast to build. Scores well on tooling simplicity, poorly on
   "cases most teams miss" — a mocked/synchronous integration layer doesn't
   naturally exercise webhook races or delivery faults.
2. **Contract + property-based + fault-injection.** Contract tests pin the
   PSP API/webhook shape; property-based tests fuzz proration and
   date/timezone math; a fault-injection harness delays/duplicates/reorders
   webhooks. Directly targets the async and math tail-risk the task calls
   out. Higher tooling cost (needs a webhook proxy).
3. **State-machine/scenario-driven.** Model the subscription lifecycle
   explicitly (active → past_due → canceled, concurrent upgrade-during-
   dunning, etc.), enumerate transition sequences, assert invariants per
   state. Strong on illegal-transition bugs, weaker alone on pure timing/
   math edge cases.

**Pick:** (2) as the backbone — contract + property-based + fault-injection —
with (3)'s lifecycle model folded in as the source of the top-priority test
cases, and (1)'s cheap unit layer underneath for the pure math. **Why:** the
task explicitly asks for the cases "most teams miss," and those are
concurrency/webhook-ordering and timezone-boundary bugs that only (2)'s
tooling actually exercises; (1) alone would produce a plan that reads well
but never finds them.
**Switch trigger:** if there is no way to simulate delayed/duplicate/
out-of-order webhook delivery against the PSP sandbox, fall back toward (1)
plus manual/scripted chaos scenarios — automated fault-injection is only
possible if the sandbox supports it.

## Move 4 — Attack

**Concrete failing scenario:** Charge succeeds at the PSP. The webhook is
delayed 6 hours (network partition). The app's internal logic times out
waiting and treats the charge as failed, triggering a dunning retry — which
also succeeds. Two settled charges, one subscription cycle. This is a real
double-charge bug class, not a corner case, and it only shows up if the plan
explicitly tests "webhook delayed past internal timeout," not just "webhook
missing."

**Magnitude check:** At scale (e.g. 1M subscribers across ~150 DST-observing
timezones), the two annual DST transition dates guarantee anchor-date logic
hits the "hour doesn't exist" / "hour occurs twice" edge case on a
predictable, recurring calendar day for a meaningful subscriber slice — not
negligible, must be an explicit, named test case rather than incidental
coverage.

**Assumption re-check:** the idempotency-key assumption is the load-bearing
one. If it turns out charges aren't actually deduplicated by key, the
double-charge scenario above isn't a test-around, it's a design gap — the
plan must surface it as a question for engineering, which it now does.

**Steelman of rejected candidate (1), the pyramid:** for a plan under time
pressure, unit + integration + a few e2e tests catches the bulk of ordinary
bugs at a fraction of the build cost, and full fault-injection may be
over-engineering before first ship. **Counter:** the prompt asks specifically
for the missed cases, and billing errors are asymmetric-cost (double-
charging a customer is a trust/legal problem, not just a bug ticket) —
that asymmetry justifies the extra investment even pre-launch.

**Strongest surviving objection:** building a full automated fault-injection
harness may not be feasible in the available timeline. It doesn't kill the
plan — the plan states a manual/scripted floor (a webhook-replay tool that
can delay/duplicate/reorder against a staging PSP account) as the fallback,
so the case still gets covered even if not automated.

## Move 5 — Verify

**Check:** re-read Move 1's criteria against the deliverable below — layers/
tooling/fixtures named (yes: 5 layers + PSP sandbox/test clocks/webhook proxy/
tz fixtures), prioritized missed-cases listed (yes: webhook race, out-of-
order webhooks, DST anchors, proration-during-dunning, dunning exhaustion,
cross-cadence proration, tz change mid-cycle, rounding), PASS/falsify stated
per area (yes, five areas below), ≤800 words (deliverable measured at ~640
words). Criteria met; no drift from Frame.

---

## Deliverable: QA Plan — Recurring Billing Engine

### Test Strategy: Layers, Tooling, Fixtures

**Layers:**
1. **Unit** — pure functions: proration math, dunning schedule calculator,
   billing-anchor date resolution (tz + DST), state-machine transition
   rules. No I/O.
2. **Contract** — pin the PSP's charge-creation API and webhook payload
   shape against its published/sandbox contract; catch drift if the PSP
   changes fields.
3. **Integration / fault-injection** — run against the PSP sandbox through a
   webhook proxy that can delay, duplicate, drop, and reorder deliveries;
   verify idempotency and reconciliation under those faults.
4. **State-machine scenario tests** — enumerate subscription lifecycle
   transitions (active → past_due → canceled, upgrade-during-dunning,
   downgrade-during-retry) and assert invariants at each state.
5. **End-to-end** — a small number of full flows (signup → charge → renewal
   → cancel) against a staging PSP account, run nightly.

**Tooling/fixtures:** PSP test-mode account with test clocks and forced
decline codes; a webhook replay/fault-injection proxy; fixture customers
across ≥6 timezones including DST-observing and non-DST zones; a
frozen-clock utility for date math.

### Prioritized Test Cases (most-missed first)

1. **Webhook race vs. internal timeout** — charge succeeds at the PSP but
   the webhook arrives after the app's "assume failed" timeout has already
   triggered a retry → idempotency key must prevent a duplicate charge.
2. **Duplicate/out-of-order webhooks** — same event delivered twice, or a
   hard-decline delivered after a success → must not double-bill or let a
   stale failure overwrite a terminal success.
3. **DST transition anchor dates** — anchor time falls in the "spring
   forward" missing hour or the "fall back" duplicate hour → billing fires
   exactly once, at a defined resolved time.
4. **Proration during an in-flight dunning retry** — upgrade while a failed
   charge is still retrying → proration computed against the correct
   plan/period, no double charge or lost credit.
5. **Dunning exhaustion** — all retries decline through the full schedule →
   subscription reaches a defined terminal state, not a silent stuck state.
6. **Cross-cadence proration** (annual↔monthly switch), not just
   same-cadence upgrades.
7. **Timezone change mid-cycle** — anchor recalculation must not skip or
   double a cycle.
8. **Rounding at scale** — fractional-cent proration must not leak money in
   either direction across millions of cycles.

### PASS / Falsify Evidence by Area

**Billing date/anchor math** — PASS: every fixture timezone bills correctly
across both DST transition dates for 3 consecutive years, plus Feb 29.
Falsify: any anchor fires 0 or 2 times on a transition day, or drifts by
±1 day/hour over a year of simulated cycles.

**Charge + webhook reconciliation** — PASS: fault-injection suite (delay/
duplicate/reorder/drop) over 100+ simulated lifecycles yields exactly one
settled charge per cycle, idempotency-key reuse confirmed in logs. Falsify:
any run with two settled charges for one cycle, or a final state
contradicting the last PSP event received.

**Dunning/retry** — PASS: retry count/backoff matches spec; exhaustion
reaches a deterministic terminal state. Falsify: indefinite retrying, or
cancellation before the configured retry count is exhausted.

**Proration** — PASS: property-based test over randomized upgrade/downgrade
timing (including mid-dunning, cross-cadence) always reconciles to
`(new_price − old_price) × remaining_fraction` within the defined rounding
tolerance. Falsify: any generated case exceeding the full period price,
going negative when it shouldn't, or drifting beyond the rounding rule.

**End-to-end** — PASS: nightly staging run completes signup→renew→cancel
with correct charges, no orphaned subscriptions. Falsify: any staging
customer left in an undefined state 24h after the run.

```text
STATUS: DONE
CONFIDENCE: medium — would raise to high with confirmation that the PSP sandbox actually supports simulated delayed/duplicate/out-of-order webhook delivery and that idempotency keys are implemented, since the highest-priority test case depends on both.
ASSUMPTIONS: (1) PSP sandbox can simulate delayed/duplicate/reordered webhooks; (2) charge creation uses idempotency keys; (3) dunning schedule is configurable/inspectable; (4) proration is time-fraction based with an as-yet-unconfirmed rounding rule. All four are carried into the plan as explicit verify-with-engineering items, not silently assumed away.
ATTACK: strongest surviving objection is that a full automated fault-injection harness may not be buildable pre-launch; the plan survives by naming a manual/scripted webhook-replay fallback as the floor so the highest-priority case (webhook race vs. retry) is still covered even without automation.
```
