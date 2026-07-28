# P3 rubric — bulk edit UX (7 pts)

GRADER ONLY.

- **T1 — select-all semantics.** Distinguishes "select the 50 visible" from
  "select all 10,000 matching the filter" (query-backed selection with
  count + per-row exclusions), since rows beyond page 1 aren't client-side.
- **T2 — non-blocking async progress.** Admin can navigate away during the
  ~2-minute job; a persistent surface (jobs panel / status chip / notification)
  shows progress and completion; page refresh doesn't lose the job.
- **T3 — partial-failure report.** Per-row failure reasons, a way to view/
  filter just the failed rows, retry-failed-only, and export of the failure
  list. "Show an error toast: 200 failed" alone fails this.
- **T4 — undo with honest semantics.** Undo scoped to the rows that succeeded,
  implemented as a compensating job (or versioned revert), with stated limits
  (time window; what's not undoable, e.g., side effects like emails).
- **T5 — confirmation calibrated to blast radius.** Restates the exact count
  and the change before running; heavier friction only for destructive ops —
  not a typed-confirmation for every edit.
- **T6 — stale-selection edge.** Rows changed/deleted by others (or by
  filter drift) between selection and execution: snapshot-vs-revalidate
  decision made explicitly (e.g., server re-validates at execution; changed
  rows become reported failures, not silent overwrites).
- **T7 — accessibility.** Keyboard-driven selection (range select), focus
  handling into dialogs, `aria-live` announcements for progress/completion.
