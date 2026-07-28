# UX Spec — Bulk Edit Across Filtered Records

## 1. Selection model, initiation, confirmation

**Selection model:** Checkbox per row plus a header checkbox with three states (none / page / all-matching). Selecting the header checkbox selects only the current page (≤50 rows) by default. Immediately below the table header, when 1+ rows on the page are checked, show an inline banner:

> "50 records on this page are selected. **Select all 10,482 records matching this filter.**"

Clicking the link switches selection mode from an explicit row-ID set to a **filter-scoped selection** (store the active filter/sort query, not 10,000 IDs). This is the only way to select beyond the loaded page — there is no "select all" that silently means "select all in the database." The count is always the live filtered count, not a cached one.

Any active selection persists across pagination and re-sort, but **any filter change clears the selection** and shows a toast: "Selection cleared because filters changed." This avoids the ambiguous case of a filter-scoped selection outliving the filter that defined it.

**Initiation:** A bulk-action toolbar appears once ≥1 row is selected, showing the count and available actions (Set owner, Change plan, Add/remove tag). Choosing an action opens a modal with the standard editor for that field.

**Confirmation:** The modal always restates: exact record count, the filter criteria in plain language, the field being changed, and old→new value (or "adding tag X" / "removing tag X"). For selections above a threshold (e.g. >500) or filter-scoped "all matching," require a typed confirmation ("type the number of records to confirm") — this is a G-Suite/AWS-style friction gate proportional to blast radius. Below the threshold, a single "Apply to N records" button suffices, no typed confirmation.

## 2. Progress and completion states

Because the operation runs up to ~2 minutes, it must be **async and non-blocking**: submitting closes the modal, shows a persistent progress toast/panel ("Updating 10,482 records… 34% · 3,560/10,482"), and the admin can navigate away — the table, other tabs, even logging out and back in should still show progress (poll or reconnect via a job-status endpoint keyed by job ID). A bulk-jobs panel (bell icon or dedicated "Jobs" tab) lists in-flight and past jobs so the admin never loses track of one they started.

Progress bar shows percentage and row counts, driven by real server-reported progress, not a fake animation. Cancel is available while running for jobs not yet complete; canceling stops further processing but does **not** roll back rows already committed (state clearly labeled "Canceled — 3,560 of 10,482 completed before cancellation").

On completion, the toast becomes a summary: "Updated 10,282 of 10,482 records. 200 failed — View details." Clicking opens a results panel, not just a dismissible toast, since failures need action.

## 3. Partial-failure handling

The job produces a downloadable/viewable **result report**: a table of failed rows with record ID/name and the specific validation reason per row (e.g. "Plan 'Enterprise-Legacy' incompatible with region"), grouped by error type when reasons repeat, so the admin isn't reading 200 individual messages. Successes are not re-shown in detail — only counts.

Provide "Retry failed rows" only if the fix is something the admin can address in-panel (e.g. re-run against a corrected value); otherwise provide "Export failed rows to CSV" so they can fix data externally and re-select via filter. Failure is never silent — no bulk job ever reports only "Done" when a failure occurred.

## 4. Undo / recovery

Because this mutates up to 10,000 rows, provide a time-boxed **Undo** (e.g. 30 seconds via toast, matching Gmail-style undo) for the whole job before it's queued — cancels before any server writes happen. Once server-side writes have started, "Undo" is no longer instantaneous; instead offer **"Revert this change"** as a distinct, explicit action from the Jobs panel for a bounded window (e.g. 24 hours), which re-runs the job in reverse using a stored diff (old value per row) — clearly labeled as a new job with its own confirmation, count, and progress, not a silent rollback. Partial-failure rows are excluded from revert scope since they were never changed.

## 5. Edge cases & accessibility

- **Concurrent edits:** if a row is modified by another admin/process mid-job, treat as a failure with reason "Record changed since selection," not a silent overwrite.
- **Selection drift:** filter-scoped selection is evaluated at job-execution time against the live matching set; if the count shifts between confirm and execute, show the executed count in the summary, not the confirmed one, and note the discrepancy.
- **Empty/zero-result actions:** disable bulk toolbar entirely when 0 rows selected; disable "Select all matching" if the filter returns 0 rows.
- **Accessibility:** progress toast is an ARIA live region (`aria-live="polite"`) so screen readers announce completion without interrupting other work; the confirmation modal traps focus and its typed-confirmation input has a clear label + error state; the failed-rows table is a real table with row/column headers, not divs, and is keyboard-navigable; color is never the sole signal for success/failure (icon + text label required).
