# Bulk Edit UX Spec — Admin Console (10,000+ row table)

## Selection model

- **Row checkboxes** in the leftmost column, plus a header checkbox with three states: unchecked, checked ("all on page"), indeterminate (partial page selection).
- Because the table is server-paginated (50/page, 10k+ rows), selection must be **decoupled from loaded rows**: maintain a selection set as `{mode: "include"|"exclude", ids: Set, filterSnapshot}` rather than assuming client-held rows. Checking the header box selects only the 50 visible rows and surfaces a banner: *"All 50 on this page selected. [Select all 8,412 matching records]"* — the classic Gmail pattern. Clicking that link switches to `mode: "exclude"` against the current filter snapshot, so the user can still deselect individual rows (added to `ids` as exclusions) without holding 8,412 ids in memory.
- Changing filters or sort while a cross-page selection is active **clears the selection** and shows a toast explaining why (selection is filter-scoped; changing the filter changes the universe).
- A persistent **selection bar** appears above the table once ≥1 row is selected: count, "Clear selection," and the bulk-action entry point ("Edit N records ▾").

## Initiation and confirmation

- Bulk action menu offers: Set account owner, Change plan, Add tag, Remove tag. Each opens a small side panel (not a full-page nav) with the field's normal input control (owner picker, plan dropdown, tag multiselect).
- Confirmation step is **mandatory and shows the real count and a preview**: *"Set account owner to Jane Doe for 8,412 records matching your filters?"* Include the active filter chips in the dialog so the user re-confirms scope, not just the value. For destructive-leaning actions (remove tag) add a one-line consequence note.
- No silent auto-apply on selection change — the action only fires on explicit "Apply" click, which is disabled until the field has a valid value.
- Large selections (configurable threshold, e.g. >500) get a stronger confirm: require typing the count or a checkbox "I understand this affects 8,412 records" to prevent fat-finger mass edits.

## Progress and completion states

- On Apply, the panel collapses into a **persistent progress toast/bar** (bottom of viewport, not a blocking modal) so the admin can keep working elsewhere in the app while the job runs — job is server-side and long (up to 2 min), so blocking UI is hostile.
- Toast shows: action description, a progress bar driven by server-reported `processed/total` (polled or via SSE/websocket), and elapsed time. Include a "Run in background" affordance implicit in the non-blocking design; a small persistent chip in the nav ("1 bulk job running") lets the user navigate away and back.
- The table itself should optimistically badge affected rows as "Updating…" if still in view, but the source of truth is the completion summary, not client-side guessing.
- On completion, toast becomes a **result summary**: "8,212 updated, 200 failed. [View details] [Undo]". Success-only completions get a lightweight success toast that auto-dismisses in ~6s but stays in a notification/activity log.

## Partial-failure handling

- Never silently drop failures. The result summary always states both counts even if failures are 0.
- "View details" opens a **failure drawer**: a filterable/sortable table of the failed rows with a per-row reason (e.g. "Plan incompatible with active contract," "Owner inactive"). Support export (CSV) for audit/reporting and a "Retry failed rows" button that re-runs the same edit scoped only to the failure set, after the underlying issue is presumably fixed.
- Failed rows are **not** left in an ambiguous state — they visibly retain their prior values; nothing partially applies at the field level (row-level atomicity: either the row's edit fully succeeds or it's untouched).
- If failures exceed a threshold (e.g. >50%), surface a more prominent warning suggesting the filter/value combo may be wrong, rather than just listing 5,000 failed rows.

## Undo / recovery

- Every successful bulk edit generates a **job record** with before/after values per row, retained for a bounded window (e.g. 30 min or until another edit touches the same rows). "Undo" reverts exactly the rows this job changed, not the current filter/selection — protects against undo touching rows edited by someone else since.
- Undo is itself a bulk job with the same progress/failure UX (a revert can partially fail too, e.g. if the field changed again in the meantime).
- Bulk jobs appear in an **activity log** (who, what, when, count, success/fail, revert link) accessible outside the toast lifetime, since 2-minute jobs may outlive the user's attention.

## Edge cases & accessibility

- Concurrent edits: another admin edits a row mid-job — last-write-wins server-side; surface conflicts as failures with reason "Modified concurrently," not silent overwrite.
- Selection persists across the job (don't clear until user dismisses), so retry/undo can reuse it.
- Network drop during polling: reconnect and resume polling by job ID; job continues server-side regardless of client connection.
- Empty selection, zero matching filter, or job cancellation (add a "Cancel" control for in-flight jobs, itself best-effort since server work may already be committed per-row).
- Accessibility: progress toast is an ARIA live region (`polite`) announcing milestones, not every tick; confirmation dialog traps focus and is keyboard-dismissable; failure drawer table is fully keyboard-navigable with row reasons read by screen readers; color is never the sole indicator of success/failure (icon + text).
