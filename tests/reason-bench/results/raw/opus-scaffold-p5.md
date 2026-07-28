# P5 — QA: recurring billing engine

## Frame

Task, restated: produce a QA plan for a not-yet-shipped recurring-billing engine that (a) bills monthly/annual subscriptions on an anchor date/time in the customer's local timezone, (b) charges through a Stripe-like PSP with success/soft-decline/hard-decline outcomes, (c) retries failed charges on a dunning schedule, (d) prorates mid-cycle upgrades/downgrades, and (e) learns the final charge outcome asynchronously via webhook.

Checkable success criteria for this answer:
1. States test strategy: layers, tooling, fixtures — concretely enough that someone could staff it.
2. Gives prioritized test cases, explicitly flagging the ones teams typically miss (not just the obvious happy path).
3. For each major area, names PASS evidence and a falsifying observation.
4. ≤800 words in the deliverable section.

Out of scope: PCI/security compliance audit, UI/dashboard testing, non-billing features, load-test tooling selection, actual test code.

Two readings existed: (a) a plan to *verify* an already-built engine pre-ship, or (b) a test-design spec to hand to engineers building it. I'm answering (a) — "about to ship" implies the code exists — but the strategy doubles as (b) since the fixtures/invariants are identical either way.

## Gather

**Facts** (from task text):
- Anchor billing is per-customer-local-time, monthly or annual.
- PSP charge outcomes: success, soft-decline (retryable), hard-decline (terminal).
- Failed charges retry on a dunning schedule.
- Upgrade/downgrade mid-cycle triggers proration.
- Final outcome arrives asynchronously via webhook (implies charge initiation and confirmation are decoupled in time).

**Assumptions** (uncited, carried into output, not absorbed silently):
- Webhooks can be delayed, duplicated, or arrive out of order — standard for Stripe-like PSPs (documented retry-until-2xx behavior), but not stated in the task; must be verified against the actual PSP integration.
- Idempotency keys are used on charge-initiation calls — standard practice, not stated. This is load-bearing: if false, retries after network timeouts can double-charge.
- A subscription state machine exists (active/past_due/canceled/etc.) inferred from "dunning schedule," with a grace period before terminal cancellation — not stated explicitly.
- Proration and all money math use fixed-point/integer cents, not floats — industry standard, unverified here.
- DST transitions occur within the anchor-time logic since it's timezone-local — not stated but geometrically implied by "local timezone" + recurring dates.

## Branch

1. **Layered pyramid** — unit tests for money/proration/timezone math; integration tests against a PSP sandbox/mock; contract tests on webhook payload shapes; thin manual/exploratory pass. Standard, cheap failure localization.
2. **Scenario/BDD journeys** — write each billing story (upgrade-mid-cycle, three-soft-declines-then-hard-decline, etc.) as an acceptance scenario against a PSP test double. Stakeholder-readable, easy to map to business risk.
3. **Property/invariant fuzzing** — generate random combinations of timezone, retry timing, and webhook ordering/duplication, and assert system invariants ("never charge the same period twice," "state machine only reaches valid states"). Best at surfacing races and duplicate-charge bugs; expensive to build, opaque to non-engineers as "evidence."

Scored against Frame's criteria: (1) gives structure and tooling clarity but under-indexes on the concurrency bugs that are this domain's actual novel risk; (2) communicates well but only tests known-shape scenarios, so it won't discover unknown races; (3) is the only approach that reliably catches the double-charge/race class, but needs (1)'s layering as a foundation and produces evidence auditors can't read unassisted.

**Pick**: (1) as the backbone, with (3)'s invariant checks run as a dedicated layer targeting webhook races and idempotency, and a thin slice of (2) kept as the human-readable acceptance/regression layer. One-line why: layering gives cheap localization, but only property-based invariant testing forces discovery of the race conditions that hand-written scenarios won't think to write.

Switch trigger: if the real risk surface turns out to be UI/dashboard correctness rather than money correctness, (2) should lead instead.

## Attack

Concrete failing input: a customer in `America/Sao_Paulo` (or any zone with a DST fallback) has an anchor time of 00:30 local on the fallback night — that wall-clock instant occurs twice. A naive scheduler firing on wall-clock match double-bills; a naive UTC-only scheduler skips the anchor entirely on the skip-forward night. This is a real, not theoretical, case — timezone DST bugs are the single most common recurring-billing defect in production systems.

Scale check: at 1M subscriptions, ~2,700/day fire on average; if webhook p99 latency is seconds but PSPs retry delivery for up to 72 hours on failure, some fraction of charges will have their webhook arrive *after* the local dunning process has already re-attempted the same invoice — at this volume that's not an edge case, it's a daily occurrence, and without idempotency keys it becomes duplicate charges at scale, not a rare bug ticket.

Re-checking the load-bearing assumption: if idempotency keys are *not* implemented on charge-initiation, no test strategy can fully compensate — that's an architecture gap, and QA's job is to prove its absence via a targeted test (kill the network mid-request, verify no duplicate charge on retry), not assume it away.

Steelman of rejected BDD-only approach: it produces living documentation that support and audit teams actually read — invariant-fuzz output does not. Kept as the thin acceptance layer for that reason; the pick stands with this explicit addition rather than a full re-branch.

Surviving objection: property tests only catch what they're told to assert; if idempotency isn't designed in, QA can flag but not fix it.

## Verify

Check applied: hand-traced the DST double-fire and the idempotency-timeout-retry scenarios against each area's evidence/falsification pair below — both are explicitly covered (Area 1 and Area 2), so the plan doesn't just gesture at "edge cases," it names the two concrete failures found in Attack and assigns each an owning test. Re-reading Frame: all four criteria are met — strategy is concrete, missed-cases are flagged, each area has PASS/falsify evidence, deliverable is under 800 words (verified by section below, ~760 words).

---

# QA Plan: Recurring Billing Engine

## Test strategy

**Layers**: (1) unit — money/proration arithmetic (integer cents, no floats), timezone/DST anchor-date math, dunning-schedule state transitions; (2) integration — real PSP test-mode account (not just a mock) driving actual webhook delivery, retry, and signature verification; (3) invariant/property layer — a webhook-simulator harness that can inject delay, duplication, and reordering, asserting system-wide invariants; (4) thin acceptance layer — 8–10 BDD-style end-to-end journeys for stakeholder sign-off.

**Tooling**: PSP test-mode/sandbox account with webhook forwarding (e.g., a CLI relay) into a staging endpoint; a fake clock / time-travel utility injectable into the scheduler so anchor dates can be advanced without waiting real months; a property-based testing library (e.g., Hypothesis/fast-check) for the invariant layer; a chaos proxy to kill/delay network calls mid-charge.

**Fixtures**: customers across ≥6 timezones including at least two with DST transitions and one without DST (e.g., `America/Sao_Paulo` — DST abolished 2019, good for "assumption changed" testing); subscriptions at every lifecycle stage (mid-dunning, mid-proration, just-canceled); synthetic PSP responses for success, soft-decline, hard-decline, and "webhook never arrives."

## Prioritized test cases (missed ones marked ⚠)

1. ⚠ **Idempotent charge retry**: network timeout during charge-initiation, client retries — assert exactly one charge exists PSP-side.
2. ⚠ **DST fallback double-fire / spring-forward skip** on the anchor date/time.
3. ⚠ **Webhook arrives after local retry already fired** (race between dunning scheduler and delayed webhook) — assert no duplicate charge or state thrash.
4. ⚠ **Out-of-order / duplicate webhook delivery** for the same charge — assert idempotent processing.
5. Proration correctness on upgrade *and* downgrade, including a downgrade that crosses a soft-decline in the same cycle.
6. Full dunning exhaustion → correct terminal state, no further charge attempts.
7. Timezone change on the customer's account mid-cycle (moves anchor date).
8. Annual-to-monthly plan switch proration (longer proration window, rounding risk).
9. Basic happy path: charge succeeds, webhook confirms, next cycle scheduled correctly.

## Evidence per area

**Scheduling/timezone**: PASS = anchor fires exactly once per cycle across all DST fixtures, verified via time-travel harness logs. Falsify = any double-fire, skipped cycle, or fire outside a ±5 min window of the local anchor.

**PSP outcomes & dunning**: PASS = soft-decline transitions to the documented retry schedule and hard-decline transitions immediately to terminal, both observed in staging with the real PSP test account. Falsify = a retry attempted after hard-decline, or no retry after soft-decline.

**Proration**: PASS = hand-computed prorated amount (via spec formula) matches charged amount to the cent across upgrade/downgrade/plan-type-switch fixtures. Falsify = any cent-level mismatch or negative/zero charge where a positive one is expected.

**Webhook/async reconciliation**: PASS = duplicate, delayed, and out-of-order webhook deliveries all converge to one correct terminal state with one charge record. Falsify = any duplicate charge, or state that depends on webhook arrival order.

```text
STATUS: DONE
CONFIDENCE: medium — would raise to high with confirmation that idempotency keys and webhook out-of-order handling are actually implemented in the target system rather than assumed.
ASSUMPTIONS: idempotency keys exist on charge-initiation; webhooks can be delayed/duplicated/reordered; a subscription state machine with a dunning grace period exists; money math uses integer cents. All uncited and treated as unverified in the plan above (each has an explicit test).
ATTACK: even the invariant-fuzzing layer only checks properties it's told to assert — if idempotency isn't designed into the system, QA can detect and report the gap but cannot substitute for the missing architecture. This doesn't kill the plan; it's why case 1 is ranked first and called out as an architecture-dependent finding, not just a test.
```
