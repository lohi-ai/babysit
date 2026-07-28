# P5 — QA: recurring billing engine

A new recurring-billing engine is about to ship. Behavior:

- Subscriptions bill monthly or annually on the anchor date/time in the
  customer's local timezone.
- Payment runs through a third-party PSP (Stripe-like): a charge can succeed,
  soft-decline (retryable), or hard-decline (terminal).
- Failed charges retry on a dunning schedule.
- Customers can upgrade/downgrade mid-cycle, with proration.
- The PSP reports final charge outcomes asynchronously via webhook.

Write the QA plan:

- Test strategy: layers, tooling, and fixtures you need
- The prioritized test cases — especially the ones most teams miss
- For each major area, the evidence that would mark it PASS, and what would
  falsify that PASS

Limit: 800 words.
