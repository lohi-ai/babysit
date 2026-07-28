# QA Plan — Recurring Billing Engine

## Test Strategy

**Layers**
1. **Unit** — date/anchor math, proration formulas, dunning schedule state machine, webhook signature verification, idempotency-key handling. Pure functions, no network — run these thousands of times against generated timezone/date fixtures.
2. **Integration** — billing engine ↔ PSP sandbox (Stripe test mode or a mock PSP with scriptable responses), DB transaction behavior under concurrent writes.
3. **Contract/simulation** — webhook delivery: out-of-order, duplicate, and delayed delivery, replayed via a harness that can hold/reorder/duplicate events on demand.
4. **End-to-end** — real clock-driven runs (or time-travel via a mockable clock) through full cycles: create → bill → dunning → cancel/reactivate.
5. **Chaos/property-based** — fuzz anchor dates across all timezones and DST boundaries, fuzz proration inputs, fuzz webhook race timing.

**Tooling/fixtures**
- Mockable/injectable clock (never `Date.now()` directly) so tests can jump to exact anchor instants.
- PSP sandbox with scriptable outcomes: success, soft-decline, hard-decline, delayed webhook, duplicate webhook, out-of-order webhook, webhook never arrives.
- Timezone fixture matrix: at least one zone with DST (US/Eastern), one without (UTC), one with a fractional offset (India, Nepal), one that changed DST rules historically (a zone whose tzdata was patched), and one southern-hemisphere DST zone (opposite season).
- Golden-file proration calculations checked against manual spreadsheet math.
- Idempotency-key store to verify replay safety.

## Prioritized Test Cases (commonly missed first)

1. **DST transitions on the anchor date itself.** A customer anchored to 2:30am on the day clocks spring forward (time doesn't exist) or fall back (time occurs twice). Verify the engine picks one deterministic, documented resolution — not a crash or double-bill.
2. **Month-end anchors.** Subscription started Jan 31 — what happens in February (28/29 days) and in 30-day months? Must be consistent (e.g., always last day of month) not silently drift the anchor forward each cycle.
3. **Leap year (Feb 29) annual anchors** — must not skip a year or crash in non-leap years.
4. **Webhook arrives before the synchronous charge-attempt response returns** (race), and **webhook arrives twice** (duplicate delivery) — must be idempotent and not double-apply state.
5. **Webhook never arrives** — timeout/reconciliation job must poll PSP and reconcile, not leave subscription stuck "pending" forever.
6. **Out-of-order webhooks** — a stale "soft-decline" event arriving after a later "success" event must not regress state.
7. **Proration during upgrade AND a failed/retrying charge simultaneously** — what does downgrade proration do to a customer currently in dunning?
8. **Timezone change of the customer** (moves, or account setting changes) mid-cycle — does the anchor recompute in-place (potentially causing skip/double-bill in the transition month) or preserve the original instant?
9. **Currency/PSP rounding** in proration — fractional cents, rounding direction, and whether rounding errors compound over many cycles.
10. **Retry exhaustion → hard decline transition** — after N dunning attempts, does it correctly terminate (cancel/downgrade) exactly once, not repeatedly.
11. **Concurrent upgrade requests** (double-click) during proration — must not create two proration credits.
12. **Clock skew between app server and PSP** causing anchor computed a few seconds off a month boundary.

## Evidence & Falsification per Area

**Billing schedule / anchor dates**
- PASS evidence: automated matrix run across the timezone/DST/month-end fixture set showing exactly one charge attempt per cycle, at the documented resolution time, with no skipped or duplicate cycles over a simulated 3-year span.
- Falsifies PASS: any DST/month-end case producing zero or two charges in a cycle, or anchor date drifting cycle-over-cycle.

**Payment processing (success/soft/hard decline)**
- PASS evidence: PSP sandbox scripted for each outcome; engine transitions subscription to the correct state (active/past_due/canceled) and this state matches PSP's own record after reconciliation.
- Falsifies PASS: any state divergence between engine and PSP truth, or a soft-decline treated as terminal (or vice versa).

**Dunning/retry**
- PASS evidence: retry attempts occur exactly per the documented schedule (verified via clock-injection), terminate after max attempts, and customer is notified per configured cadence.
- Falsifies PASS: retries continuing past max, retries on a hard-decline, or silent retry-loop with no terminal state.

**Proration**
- PASS evidence: unit tests matching hand-computed golden values for upgrade/downgrade at arbitrary points in a cycle, including edge cases (0 days remaining, exact boundary).
- Falsifies PASS: any mismatch vs. golden value, or negative/undefined credit.

**Async webhook handling**
- PASS evidence: chaos harness replaying duplicate/out-of-order/delayed webhooks all converge to correct final state; idempotency-key store prevents double-processing.
- Falsifies PASS: any replay producing a different final state than a single clean delivery, or a missing-webhook case never reconciled within SLA.
