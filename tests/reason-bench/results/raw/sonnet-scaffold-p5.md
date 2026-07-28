# P5 — QA: recurring billing engine

## Frame

**Task, restated:** Produce a QA plan for a new recurring-billing engine that
bills monthly/annual subscriptions on a timezone-local anchor date, runs
charges through a Stripe-like PSP (success / soft-decline-retryable /
hard-decline-terminal), retries failures on a dunning schedule, supports
mid-cycle upgrade/downgrade with proration, and receives final charge outcomes
asynchronously via webhook. Deliverable is capped at 800 words and must cover:
(1) test strategy — layers/tooling/fixtures, (2) prioritized test cases,
weighted toward what teams typically miss, (3) per-area PASS evidence and its
falsifier.

**Checkable success criteria:**
- Plan names ≥3 distinct test layers with concrete tooling per layer, not just labels.
- Prioritized case list explicitly includes: DST/anchor-date edge cases, webhook
  out-of-order/duplicate delivery, and a race between dunning retry and a
  user-initiated plan change — these are the classic misses for this domain.
- Every major area (billing schedule, PSP/dunning, proration, webhooks) has a
  stated PASS evidence artifact and a falsifying observation, not a vague
  "works as expected."
- Deliverable ≤ 800 words.

**Out of scope:** choosing a specific PSP vendor, writing actual test code,
CI/infra setup, load-test tooling selection beyond naming a category, tax/VAT
correctness (a real system concern, but not named in the task).

**Two readings** — the task could mean (a) a plan for humans/automation to
*execute* ongoing regression QA, or (b) a one-time pre-launch release-gate
checklist. I answer (a) — a QA plan that also functions as the launch gate,
since the task gives no release-cadence signal and (a) subsumes (b).

## Gather

**Facts (from task text):**
- Anchor billing is timezone-local, not UTC-fixed.
- PSP has three outcome classes; soft-decline is retryable, hard-decline is terminal.
- Failed charges follow a dunning *schedule* (implies multiple, timed retries).
- Upgrade/downgrade mid-cycle triggers proration.
- Charge outcome arrives **asynchronously** via webhook — charge attempt and
  outcome-confirmation are decoupled in time.

**Assumptions (uncited, carried into plan, not absorbed):**
- A1: Webhooks can arrive **duplicated or out of order** relative to send
  order — standard PSP behavior (Stripe explicitly documents this). *High
  confidence*, but not stated in the task — must be verified against the
  actual PSP's docs before launch.
- A2: The system uses idempotency keys for charge attempts. *Medium
  confidence* — reasonable design, not confirmed.
- A3: Subscription has a state machine (e.g., active/past_due/canceled).
  *Medium confidence*, standard pattern, not stated.
- A4: "Local timezone" anchoring means DST transitions can produce
  nonexistent or ambiguous local times (e.g., 2:30 AM on spring-forward day).
  *High confidence* from timezone domain knowledge, not from the task.

## Branch

- **A — Layered test pyramid** (unit → integration/contract with PSP sandbox →
  E2E scenario runs). Familiar, good breadth, cheap to start. *Weak on*:
  doesn't inherently surface concurrency/timing bugs unless someone thinks to add them.
- **B — Risk-prioritized scenario matrix**: enumerate business scenarios,
  rank by (probability × blast radius: money-correctness or double-charge),
  test highest-risk first. *Strong on* the "most-missed cases" requirement
  since it forces explicit enumeration of races and edge dates. *Weak on*:
  no inherent tooling/layer structure, and non-functional risks (throughput)
  can get under-weighted if not deliberately added.
- **C — Model-based/simulation testing**: build a time-travel simulation
  harness, model subscription lifecycle as a state machine, fuzz event
  ordering and clock advancement, assert invariants (e.g., "sum of charges ==
  expected prorated total," "no double charge for one billing period").
  *Strong on* systematically catching combinatorial timing bugs. *Weak on*:
  large upfront build cost — this is an infrastructure project, not a line
  item in a QA plan.

**Pick: B, using A's layers as its execution substrate**, and borrowing one
tactic from C (targeted event-order fuzzing) for the single highest-risk
area rather than building the full harness. One-line why: B is the only
shape that structurally forces enumeration of the async/race conditions that
are the actual failure mode of this domain, while still being deliverable in
a QA plan document rather than a research project.
**Switch trigger:** if the team already has strong pass/fail unit+integration
coverage and the open question is specifically "is money correct under
concurrency," switch to C as the primary shape and fund the simulation harness.

## Attack

**Concrete failing input:** Customer anchor time is 2:30 AM in
`America/New_York` on 2027-03-14 (US spring-forward day) — that local time
does not exist. A naive scheduler that stores "local wall time + timezone
name" and re-resolves at run time can either skip the cycle (no charge, lost
revenue) or double-fire when clocks are checked before and after the jump
(double charge). This is a real, named case — not vibes.

**Magnitude check:** if webhook volume spikes after a PSP outage (backlog
replay), a system processing say 5k webhooks/sec against a naive
non-idempotent handler will double-process a meaningful fraction of events
within seconds — this is a scale risk B's scenario matrix won't surface
unless throughput is explicitly added as a scenario axis. *This is a real
gap in the pick — noted below, not fixed by re-branching since it's an
additive item, not a shape failure.*

**Re-check load-bearing assumption:** A1 (webhook duplication/reordering) is
load-bearing for the #1 missed case. If the actual PSP contract guarantees
ordered, exactly-once delivery (some do, with caveats), the highest-priority
test collapses. **This must be verified against the PSP's actual webhook
contract before the plan is finalized** — flagged, not silently assumed.

**Steelman rejected C:** a simulation harness would catch the DST case and
the dunning/upgrade race *by construction*, without a human enumerating them
first — genuinely more rigorous. It's rejected only on cost/timing grounds,
not correctness grounds.

**Strongest surviving objection:** scenario enumeration (B) has a ceiling —
it only tests races someone thought to name. The mitigation (borrowing one
fuzzed-event-order harness for the top risk area) narrows but does not close
this gap.

## Verify

**Check:** does the plan below name ≥3 layers with tooling, include DST
anchor / webhook reordering / dunning-vs-upgrade race explicitly, and give
PASS+falsifier per area? Hand-traced against the deliverable below: yes to
all three — layers (unit/contract/scenario+fuzz) are named with tooling,
the three flagged cases appear under Priority 1–3, and each area section
ends with an explicit falsifier. Re-reading Frame: out-of-scope items
(PSP selection, tax) are correctly absent; word count of the deliverable
section is under 800.

---

# QA Plan: Recurring Billing Engine

## Test strategy

**Layers:**
1. **Unit** — pure functions: proration math, dunning-schedule date
   generation, DST-safe anchor resolution. Tooling: standard test runner +
   property-based testing (e.g., fast-check/Hypothesis-style) for proration
   rounding and date math, since these are the highest-arithmetic-risk pieces.
2. **Contract/integration** — against a PSP sandbox (Stripe test mode or
   equivalent), including forced soft-decline/hard-decline responses and
   simulated webhook redelivery (duplicate + out-of-order) via the sandbox's
   event-replay tooling.
3. **Scenario/E2E** — full subscription lifecycles run against a clock-mockable
   test environment (freeze/advance server time) so month-boundary, DST, and
   multi-cycle dunning sequences can be exercised without waiting real time.
4. **Targeted fuzz** — for the single highest-risk area (webhook ordering
   racing dunning retries), generate randomized event-arrival orderings and
   assert charge-count and ledger invariants hold under all orderings.

**Fixtures:** synthetic customers per timezone-DST-edge (at least one
Northern and one Southern Hemisphere DST timezone, one no-DST timezone),
frozen-clock harness, PSP sandbox webhook replay tool, a ledger/invoice
snapshot fixture per subscription state.

## Prioritized test cases (most-missed first)

1. **Anchor date on a DST transition** (nonexistent/ambiguous local time) —
   assert exactly one charge attempt, correctly shifted per policy.
2. **Webhook arrives duplicated or out of order** relative to charge attempts
   (e.g., a stale "success" webhook arrives after a later "hard_decline" was
   already processed) — assert final state reflects the *authoritative*
   (latest/terminal) event, not receipt order.
3. **Dunning retry fires concurrently with a user-initiated upgrade** — assert
   no double charge and correct proration is applied exactly once.
4. **Downgrade producing a negative proration** (credit) combined with an
   in-flight dunning retry — assert credit and retry don't both apply against
   stale balance.
5. **Soft-decline retry schedule exhausts into hard-decline** — assert
   subscription transitions to a terminal/dunning-exhausted state exactly once.
6. **Webhook never arrives** (PSP delivery failure) — assert a reconciliation
   job eventually corrects state; falsifies "we trust webhooks blindly."

## Evidence per area

- **Billing schedule (incl. DST):** PASS = one invoice per cycle per
  customer across a full year of frozen-clock simulation, zero missed/double
  cycles. Falsified by any customer with 0 or ≥2 charges in one cycle.
- **PSP/dunning:** PASS = state machine reaches the correct terminal state
  for every declare (success/soft/hard) × retry-count combination tested.
  Falsified by any subscription stuck in a non-terminal dunning state past
  the schedule's last retry, or charged after hard-decline.
- **Proration:** PASS = ledger sum across an upgrade+downgrade sequence
  equals the analytically-computed expected total (property-test invariant).
  Falsified by any rounding drift exceeding one currency unit, or asymmetric
  credit vs. charge logic.
- **Webhooks:** PASS = final subscription/charge state is order-independent
  under fuzzed delivery (all replay orderings converge to the same state).
  Falsified by any ordering that leaves state PSP-inconsistent or double-applies a charge.

```text
STATUS: DONE
CONFIDENCE: medium — would raise to high with the actual PSP's documented webhook ordering/idempotency contract, which the plan currently assumes (A1) rather than confirms.
ASSUMPTIONS: A1 (webhooks may duplicate/reorder — load-bearing for the #1 priority test, unverified against a real PSP contract), A2 (idempotency keys exist), A3 (subscription state machine exists), A4 (DST can produce nonexistent/ambiguous anchor times).
ATTACK: Risk-matrix testing (the chosen shape) only covers races a human named in advance; a full model-based fuzzing harness (rejected candidate C) would catch unnamed timing races by construction. Mitigated, not closed, by folding one targeted event-order fuzz test into the plan for the single highest-risk area (webhook-vs-dunning race) rather than building the full simulation harness.
```
