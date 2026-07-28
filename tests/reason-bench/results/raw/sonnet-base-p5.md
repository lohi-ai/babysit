# QA Plan — Recurring Billing Engine

## Test Strategy: Layers, Tooling, Fixtures

**Layers**
1. **Unit** — anchor-date math (DST, month-end, leap year), proration formulas, dunning-schedule state machine, currency rounding. Property-based/table-driven tests over a wide input matrix, not hand-picked examples.
2. **Integration** — billing engine ↔ PSP sandbox (Stripe test mode), webhook receiver ↔ engine, DB transaction boundaries. Verify idempotency keys, signature verification, retry semantics.
3. **System/E2E with time control** — full subscription lifecycle run against a virtual clock (time-travel/freeze), so multi-month dunning and annual renewals run in seconds, not real time.
4. **Chaos/failure injection** — delayed, duplicated, out-of-order, and dropped webhooks; PSP timeouts; DB failure mid-transaction; clock skew between engine and PSP.
5. **Reconciliation** — nightly job diffing PSP ledger vs. internal invoice/subscription state; this is itself a test surface, not just a prod safety net.

**Tooling**: controllable clock/time-travel harness, PSP test-mode webhooks with real signature headers, an idempotency-replay harness (fire same webhook N times), contract tests pinned to PSP's webhook schema, observability assertions (structured logs/metrics per state transition).

**Fixtures**: customers spanning timezones incl. half-hour-offset zones (India) and DST-observing zones (US/EU) plus a 30-minute-DST outlier (Lord Howe Island); anchor dates on Jan 31, Feb 29, and the DST transition dates themselves; monthly + annual plans; PSP test cards mapped to soft-decline and hard-decline codes; multi-currency accounts for rounding tests.

## Prioritized Test Cases (highest-value / most-missed first)

1. **Anchor date on month-end** — subscribe on Jan 31; verify Feb/Apr/Jun billing lands on a sane date (28/29/30) consistently every cycle, not just once.
2. **DST transitions** — anchor time falls in the "spring-forward" nonexistent hour or the "fall-back" ambiguous hour in the customer's local zone; verify a single, correct, non-duplicated charge.
3. **Webhook idempotency** — replay the identical final-outcome webhook 2–5 times; assert exactly one charge/state transition is applied.
4. **Out-of-order webhooks** — deliver a retry-success webhook before an earlier decline webhook (or vice versa); assert final state resolves to the *last true outcome*, not last-received.
5. **Race: customer action vs. in-flight billing run** — upgrade/downgrade submitted while a charge attempt or its webhook is in flight; assert no double-charge, no lost proration.
6. **Dunning during a plan change** — customer upgrades while already in a dunning retry cycle; verify retries don't stack with the new charge and the schedule resets/cancels correctly.
7. **Proration rounding drift** — repeated upgrade→downgrade→upgrade within one cycle; assert net charge matches a single closed-form calculation, no cent drift from sequential adjustments.
8. **Cancellation mid-dunning** — cancel while a retry is scheduled; assert the scheduled retry is actually suppressed (not just marked cancelled while a queued job still fires).
9. **PSP timeout/ambiguous response** — charge request times out with unknown PSP-side outcome; assert the engine doesn't charge twice on retry and doesn't leave the invoice permanently "pending" (reconciliation must resolve it).
10. **Webhook auth** — tampered payload/signature is rejected; replay of an old, valid-signature webhook outside its timestamp tolerance is rejected.
11. **Customer timezone change** — anchor recalculates without producing a skipped or duplicated cycle.
12. **Hard decline is terminal** — never retried, regardless of concurrent PSP-side retry noise.

## Evidence for PASS / What Falsifies It

**Billing schedule & anchors** — PASS: matrix run (timezones × anchor edge-dates × DST) produces exactly one correctly-dated charge per cycle, verified via audit log counts. Falsified by: any cycle with 0 or 2+ charges, or a date computed in UTC instead of customer-local time.

**Payment retry/dunning** — PASS: soft-decline follows the documented backoff schedule to completion (success or terminal), hard-decline suspends immediately with zero retries, all transitions match the designed state machine. Falsified by: a hard-decline that gets retried, a retry loop with no terminal state, or dunning continuing after cancellation.

**Proration** — PASS: invoice line items for upgrade/downgrade match an independently computed reference formula across the fixture matrix, including repeated changes in one cycle. Falsified by: rounding drift across repeated changes, negative/uncapped credits, or proration double-applied on a race with a scheduled renewal.

**Webhook/async handling** — PASS: replay and out-of-order harnesses show idempotent, order-independent convergence to correct final state; tampered/expired signatures are rejected in a dedicated auth test. Falsified by: duplicate financial effects from a replayed webhook, final state depending on delivery order, or an unauthenticated webhook accepted.

**Reconciliation/observability** — PASS: the nightly diff job flags zero unexplained discrepancies across a full simulated billing month including chaos-injected failures. Falsified by: any charge visible in the PSP ledger with no corresponding internal record (or vice versa) that isn't auto-resolved and alerted.
