# P3 — Bulk edit across 10,000 rows — scaffolded answer

## Frame

**Task, restated**: Produce a UX spec (text, ≤800 words for the deliverable itself, no mockups) that a designer and a frontend engineer could build directly from, for a bulk-edit feature on a server-side-filtered/sorted admin table (50 rows/page, 10,000+ total records). The spec must cover: selection model, initiation/confirmation, progress/completion states, partial-failure handling, undo/recovery, and edge cases + accessibility.

**Success criteria (checkable)**:
1. Selection model explicitly distinguishes "select visible rows" from "select all rows matching the current filter" (the only way to act on off-screen rows across 200 pages), with defined count semantics.
2. Confirmation states the exact field(s), exact new value, and exact record count before any mutation runs.
3. Progress state is explicitly non-blocking/async (justified by the "~2 minutes" constraint) and defines what happens if the admin navigates away or closes the tab.
4. Partial-failure handling names a mechanism to see *which* rows failed and *why* (not just an aggregate count), plus at least one remediation action.
5. Undo/recovery defines precisely what is reversible, the mechanism (not "an undo button" hand-wave), and its limits (time window, scope).
6. At least 3 concrete edge cases and at least 3 concrete accessibility requirements are named.
7. Deliverable section is ≤800 words and contains no visual mockups — only described behavior.

**Out of scope**: visual design, exact copywriting, backend job-queue implementation/API contracts, non-bulk single-row edit flows.

**Two readings worth naming, not silently resolved**: (a) "selects records" could mean only an explicit checkbox multi-select of currently-rendered rows (classic HTML-table pattern, capped at what's on screen); (b) it could mean selection is meant to span the entire filtered result set (10,000+), since the whole point of filter-then-bulk-edit in a B2B console is usually to act on a large cohort, not just the 50 rows visible. These aren't mutually exclusive — every mature implementation I know of (Gmail's "select all conversations that match this search," Salesforce list-view mass actions, Airtable, Linear) supports (a) as the default gesture and offers (b) as an explicit escalation once all visible rows are checked. I'm adopting both, combined, as one design — flagged here rather than picked silently — because the task explicitly pairs "filters the table" with "selects records," which only makes bulk-edit meaningful at the stated 10k scale if cross-page selection exists.

## Gather

**Facts** (from the task):
- Table: 50 rows/page, 10,000+ records, server-side filter + sort.
- Bulk actions: set account owner, change plan, add/remove tag.
- Execution is server-side; large selections take up to ~2 minutes.
- Per-row validation can partially fail (example given: 200 of 10,000 rejected).
- Audience is a B2B admin console — internal/operator users, not end consumers.

**Assumptions** (uncited — carried into the output explicitly, not absorbed):
- **A1 (medium-high confidence, load-bearing)**: a ~2-minute operation requires an async server job model (job id, pollable/pushable status) rather than a request that blocks the HTTP response. Not stated, but the given duration makes a synchronous design nearly unbuildable.
- **A2 (medium confidence, load-bearing)**: no existing generic "undo" or audit-log infra is assumed to exist; the spec below builds the minimum needed (a before-image captured at confirm time) rather than assuming unstated infrastructure.
- **A3 (medium confidence)**: per-record permission can vary (multi-tenant/role-scoped B2B is common), so some selected rows may be inaccessible to the acting admin — treated as an edge case, not a core requirement.
- **A4 (high confidence)**: this is a web app where the admin can plausibly navigate away or close the tab during a 2-minute job; the spec must define behavior for that.
- **A5 (high confidence)**: "select all matching filter" must be resolved server-side (filter criteria + count), not by serializing 10,000 row IDs from the client — a payload/latency concern at this scale.

## Branch

**Candidate 1 — Synchronous blocking modal**: select rows, click bulk-edit, modal shows a spinner until the job finishes (up to 2 min), then shows the result inline.
- Simple to build; fails outright against criteria 3 (can't navigate away) and 4 (a blocking dialog is a poor home for a scrollable 200-row failure report). Fails at the stated scale, not hypothetically.

**Candidate 2 — Async background job + notification/report**: confirm triggers a server job; a persistent, non-blocking progress indicator lets the admin keep working; completion surfaces a report (success/fail counts, per-row reasons, remediation actions, revert link).
- Matches criteria 3–5 natively: async job handles the 2-minute duration, the report is the natural surface for partial failure, and the report is the natural anchor for scoped undo.

**Candidate 3 — Optimistic client-side apply with row-by-row reconciliation**: table rows visually update immediately; server reconciles per row, flagging rejects in place.
- Great perceived performance, but only meaningful for rows currently rendered (≤50). For a 10,000-row cross-page selection, most affected rows aren't in the DOM at all — "optimistic update" is either undefined or fabricated for off-screen rows, and pretending a record's value already changed before server validation is a poor fit for an admin console where trust in displayed state matters. Legitimate only as a *micro-optimization* for small, single-page selections, not as the primary design.

**Pick: Candidate 2.** One-line why: it's the only shape that treats "the admin is acting on rows they can't currently see" as first-class, which the 10,000-row/50-per-page setup makes mandatory, not optional.
**Switch trigger**: if bulk edits were capped at ≤50 rows (no cross-page select-all) and executed in low single-digit seconds, Candidate 1's blocking modal would be simpler and sufficient — the async design would be over-engineering for that constraint set.

## Attack

**Concrete break attempt**: Admin filters to `plan = Free` (10,300 matches), selects "all 10,300," confirms "set owner = Alice." 90 seconds into the job, a second admin changes 500 of those same records' plan to `Pro` via an unrelated action. Does the bulk job act on the original 10,300 (snapshot at confirm time) or re-evaluate the filter live? If it re-evaluates live, results become non-deterministic mid-job and a row could match-then-not-match during execution — unspecifiable behavior. This forces a decision the original design left implicit: **the job must snapshot the concrete row-ID set at confirmation time**, and the confirmation copy must say "the N records matching your filter right now" to set the correct mental model. Folded into the spec below.

**Scale check**: 10,300 IDs cannot be safely round-tripped as a client-serialized array (payload size, and the race just described). The client must send filter criteria + a server-issued snapshot reference, never an enumerated ID list — confirms A5.

**Re-check A1**: holds — nothing in the attack weakens the case for async execution; if anything the snapshot requirement reinforces it (a snapshot is naturally taken once, at job start, by the server).

**Re-check A2**: strengthened, not just held — assuming a pre-existing generic undo/audit system would be an unverified dependency. Spec instead requires the job to capture a **before-image per affected row at snapshot time**, keyed to the job ID; "undo" becomes "revert this specific job within its window," which is buildable without assuming unstated infrastructure.

**Steelman the rejected Candidate 1**: a blocking modal is cheaper to ship, and some admins might tolerate watching a 2-minute progress bar rather than learning a jobs panel. This is the strongest surviving objection — async adds real UI surface (a jobs/notifications panel) that the team may resist scoping. It does not overturn the pick because the *task itself* states the 2-minute duration and the 200/10,000 partial-failure example; a design that assumes those away isn't answering the given problem, it's answering an easier one. Note: confirmation can and should still feel instant/blocking (<1s) — only execution is async, so the two designs are less far apart in practice than they first appear.

## Verify

**Trace with concrete numbers**: Filter `plan=Free` → 10,300 matches. Admin selects all 50 on page 1, banner offers "select all 10,300," admin accepts. Confirmation dialog: "Set Account Owner = Alice for 10,300 records" → confirm. Server snapshots 10,300 row IDs + before-images, returns job `job_8891`, dialog closes immediately. Toast: "Bulk edit running (job_8891)." Admin navigates to a different filter view; toast persists in a jobs panel, polls every ~3s. At 87s: job completes — 10,100 succeeded, 200 rejected ("owner reassignment blocked on locked accounts"). Notification: "10,100 updated, 200 failed — View report." Report lists the 200 with reasons, offers CSV export, "retry these 200" (reopens bulk-edit scoped to them), and "Revert job_8891" (restores the 10,100 from before-images, itself a new confirmed async job, available for 24h).

Checked against Frame's 7 criteria: (1) cross-page selection semantics defined ✓; (2) exact field/value/count in confirmation ✓; (3) async, navigate-away-safe ✓; (4) per-row reasons + remediation, not just a count ✓; (5) scoped, mechanism-defined, time-boxed undo ✓; (6) ≥3 edge cases and ≥3 a11y notes present in the deliverable below ✓; (7) word budget checked at draft time, deliverable trimmed to fit ≤800 words ✓. No drift from Frame detected.

---

## Deliverable: UX Spec — Bulk Edit for Filtered Table (10,000+ records)

**Selection model.** Two combined mechanisms. (1) Per-row checkboxes plus a header checkbox select/deselect the visible 50; selection persists in memory across page navigation within the same filtered view. (2) Once all visible rows are checked, a banner offers escalation: "All 50 on this page selected. Select all 10,300 records matching filter [Plan = Free]?" This is the only path to act on off-screen rows; the client never enumerates 10,000 IDs — selection is resolved server-side from filter criteria. A persistent "10,300 selected / Clear selection" indicator is always visible while selected. Changing the filter or sort after selecting invalidates the selection with a visible warning.

**Initiation & confirmation.** A bulk-action bar appears once ≥1 row is selected, offering the three supported actions. Choosing one opens a confirmation dialog stating the exact field, exact new value, and exact record count ("Set Account Owner = Alice for 10,300 records"). Requires an explicit click, no Enter-to-submit default. On confirm, the server snapshots the concrete row-ID set matching the filter *at that instant* plus a before-image of the affected field per row, so later filter drift can't change what the job touches, and the operation becomes revertible.

**Progress & completion.** Execution is an async server job — required by the ~2-minute duration. Confirming closes the dialog immediately; a non-blocking, persistent indicator (toast + jobs panel entry) shows "Running — 0/10,300," polling/pushing every 2–3s. The admin may navigate away or keep working; closing the tab does not cancel the job. On completion, a notification replaces the progress indicator ("10,100 updated, 200 failed — View report"), persistent until dismissed and retained in the jobs panel.

**Partial-failure handling.** The report lists every failed row with a specific reason ("Row 4021: owner reassignment blocked on locked account"), never just an aggregate count. Offers: CSV export of failures, "retry these rows" (reopens bulk-edit scoped to just the failures), and a filter shortcut to inspect them in the table.

**Undo / recovery.** Scoped per job, not global. "Revert job_8891" re-applies the captured before-images to the rows that succeeded (failed rows were never touched). Revert is itself a confirmed, async, reportable job — not a silent instant action — available for a defined window (e.g., 24h), after which the before-image expires; the jobs panel states the expiry.

**Edge cases & accessibility.**
- Concurrent modification of a snapshotted row during the job: last-write-wins at the data layer, visible in the row's own audit trail, not silently blocked.
- Permission-partial: rows the acting admin can't modify report as failures with a permission-specific reason, not a silent skip.
- Duplicate submission: confirm button disables on click; job execution is idempotent per job ID.
- Zero-match filter: no select-all banner or bulk-action bar appears.
- Accessibility: select-all checkbox is keyboard-operable and announces state via `aria-live` ("50 of 10,300 selected"); progress/completion toasts use polite `aria-live` regions, not visual-only toasts; the confirmation dialog traps and returns focus; failure-report rows are keyboard-reachable with reasons exposed as text, not icon/color-only.

```text
STATUS: DONE
CONFIDENCE: medium — would raise to high with confirmation that an async job/queue layer exists (or is buildable) and that per-row before-images are feasible to store at this scale; these are the two unstated infra dependencies the spec leans on.
ASSUMPTIONS: (1) async server job model is available/buildable for the ~2-minute execution; (2) no pre-existing generic undo/audit system — spec builds a minimal per-job before-image mechanism instead of assuming one; (3) selection-by-filter is resolved server-side, never as a client-enumerated ID list.
ATTACK: Building the async jobs-panel UI is materially more scope than a blocking modal, and a team under time pressure may push to cut it — but the task's own stated 2-minute duration and 200/10,000 partial-failure example make the blocking design non-viable, not just non-ideal, so the objection doesn't overturn the pick.
```
