# Bulk Edit UX Spec — 10,000-row Admin Table

## Selection Model

- Row checkboxes + header checkbox for "select all on this page" (max 50).
- After checking the header box, show an inline bar: **"All 50 selected on this page. Select all 10,000 matching records →"** (link, not auto-triggered). Clicking it sets a `selectAllMatchingFilter: true` flag scoped to the *current filter/sort query*, not raw IDs — required since 10k rows can't reasonably be held as a client-side ID list, and the set must reflect the filter, not a snapshot.
- Individual deselection while "all matching" is active is supported via an `excludedIds` set overlay (standard "select all + exceptions" pattern). Selection count reflects `matchCount - excludedIds.length`.
- Selection persists across pagination but is **cleared** if the admin changes the filter (selection is filter-scoped; changing filters invalidates the match set and shows a toast: "Selection cleared — filter changed").
- Sticky selection toolbar appears once ≥1 row is selected: shows count, "Edit N records" button, "Clear selection".

## Initiation & Confirmation

- "Edit N records" opens a modal/panel with the field picker (owner, plan, tags) and value input. Only one field-change per bulk action to keep validation and rollback simple.
- Confirmation step shows: exact count, the field + new value, and a preview of a small sample (e.g., 5 affected rows) so the admin can sanity-check before committing.
- For selections above a threshold (e.g., >500 rows) or any destructive-leaning change (removing a tag, reassigning owner away from self), require typing the count or record type name to confirm — friction proportional to blast radius.
- Confirmation button is disabled until the count is confirmed loaded (no submitting against a stale/still-counting selection).

## Progress & Completion States

- Since the job can take up to 2 minutes, submission immediately returns a **job ID** and switches the UI to an async job model — never a blocking spinner.
- A persistent, dismissible job-status toast/panel (bottom-right, like a file upload tray) shows: job description, live progress bar (rows processed / total), elapsed time, and a "Run in background" affordance — the admin can navigate away and the table remains usable.
- Table rows currently being processed show a subtle in-row pending indicator (e.g., a small spinner icon in the row) if the admin stays on the page; polling or a websocket updates rows as they complete.
- On completion, the toast becomes a summary: "9,800 of 10,000 updated. 200 failed. [View details] [Undo]". A notification-center entry logs the job permanently (with timestamp, actor, and a link to details) even if the toast is dismissed — jobs of this size must be auditable after the fact.

## Partial-Failure Handling

- Failure is the expected case at this scale, not an error state — never show a red full-page error for partial failure.
- "View details" opens a report: a filterable table of the 200 failed rows with a per-row rejection reason (e.g., "Plan 'Enterprise' invalid for suspended account"), grouped/counted by reason so patterns are visible (e.g., "180 failed: invalid plan for status").
- Provide "Retry failed rows" (re-runs only the failed subset, useful after a fix) and "Export failed rows as CSV" for offline handling.
- Successful rows are never rolled back because others failed — partial success commits per-row, atomic per record, not all-or-nothing across the batch.

## Undo / Recovery

- Every bulk job writes a reversible change log (old value → new value per row) server-side, scoped to that job ID.
- "Undo" is available for a bounded window (e.g., 30 minutes, or until another bulk edit touches overlapping rows) and re-runs as its own tracked job with the same progress/completion UX — undo is not instantaneous for 10k rows either.
- If some rows were subsequently modified by another action after the bulk edit, undo skips those with a reported exception list (same "partial" pattern), never silently overwriting newer data.

## Edge Cases & Accessibility

- Selection count / match count can change between confirm and submit (concurrent edits by other admins) — server recomputes the match set at execution time and reports drift ("Note: 40 records no longer match your filter and were excluded") rather than failing outright.
- Concurrent bulk jobs on overlapping rows: second job's affected rows are locked/queued or flagged as conflicts, never silently interleaved.
- Empty selection state disables "Edit" with a tooltip explaining why.
- All progress/toast/report UI is announced via `aria-live="polite"` (job progress) and `aria-live="assertive"` (completion/failure summary) so screen-reader users get async status without polling. Modal and job panel are fully keyboard-navigable; failed-rows table supports keyboard sort/filter same as main table.
