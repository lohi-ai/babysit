# P3 — Bulk edit across 10,000 rows — Scaffolded Answer

## Move 1 — Frame

**Restatement:** Write an implementable UX spec (no mockups) for a bulk-edit
feature on a server-side-paginated admin table (50 rows/page, 10,000+ total
rows). Admin filters, selects some subset of matching rows, applies one of a
fixed set of field mutations (set owner / change plan / add-remove tag) to
the whole subset. The mutation runs server-side, can take up to ~2 minutes
for large selections, and can partially fail per-row (e.g., 200/10,000
rejected by validation). Spec must cover: selection model + initiation +
confirmation, progress/completion states, partial-failure handling,
undo/recovery, edge cases, and accessibility. Hard limit: 800 words for the
deliverable itself.

**Two materially different readings — must not pick silently:**

- **(a) "Select all" is a first-class query, not a row list** — selecting
  "all 10,000 matching the current filter" is a distinct, named capability
  (Gmail/Superhuman-style), separate from checking boxes on the loaded page.
- **(b) Selection is bounded to what's been paginated in** — only rows the
  client has actually fetched can be selected; "select all" is just
  "select all *loaded* rows," and reaching 10,000 requires paging through.

These produce different backend payload shapes (filter+exclusions vs literal
ID list) and different UI copy ("all 10,000 customers matching this filter"
vs "these 50"). I resolve this explicitly in Branch/Attack rather than
assuming one.

**Success criteria (checkable):**
1. Explicitly distinguishes "select page" from "select all matching filter."
2. Confirmation step states exact count and scope before submission.
3. Progress states cover queued → running → complete, and specify whether
   the UI blocks navigation during the ~2 min window.
4. Partial failure is surfaced per-row with reasons and a remediation path.
5. Undo/recovery model matches the reality of a server-side mutation (not a
   client-only undo).
6. Named edge cases: selection-vs-filter-change, concurrent edits, pagination
   mid-op, network loss, permission errors mid-batch.
7. Accessibility: keyboard operability, live-region announcements, focus
   management — stated, not just implied.
8. Deliverable ≤ 800 words.

**Out of scope:** visual design, exact API/schema, animation timing, backend
queue architecture, i18n.

## Move 2 — Gather

**Facts (from task):**
- 50 rows/page, 10,000+ total, server-side filter + sort.
- Actions are single-value field mutations: owner, plan, tag add/remove.
- Apply is server-side, up to ~2 minutes for large selections.
- Per-row validation can reject a subset (example given: 200/10,000).
- Audience is a designer + frontend engineer who must build from this.

**Assumptions (uncited — flagged, carried into output where load-bearing):**
- The ~2-minute duration implies async job execution already exists or is
  buildable server-side; the spec assumes a job-with-status model exists to
  build against. *(load-bearing for progress/undo design)*
- No true transactional rollback API is assumed to exist; "undo" is a
  **compensating action** (re-apply prior values), not a database rollback.
  *(load-bearing for undo design — flagged in Attack/output)*
- Rejected rows may be a mix of transient (stale data) and permanent
  (invalid tag) causes; task doesn't specify, so the spec treats reason
  strings as required but doesn't hardcode a taxonomy.
- Selection state persists across pagination within one filtered view (not
  stated, but required for the workflow described to make sense).
- Each field mutation applies one uniform new value to the whole selection
  (not per-row different values in one batch).

## Move 3 — Branch

**A — Blocking modal, foreground job.** Confirm → modal stays open with a
progress bar until done; undo via a 30–60s "soft window" toast before
finalizing. Simple to build, but traps focus/navigation for up to 2 minutes
— actively hostile at the stated worst-case latency.

**B — Background job, notification-center driven.** Confirm kicks a job;
user can navigate away immediately; a persistent job entry (bell/tray) shows
queued → running → done; job detail page lists failures with reasons +
retry; undo is a "Revert job" action for N days, implemented as a new
compensating job from a stored diff.

**C — Optimistic client-side apply, async reconciliation.** Table updates
selected rows instantly; failures roll back individually; undo is trivial
(client already holds old values). Feels fastest, but for a 90-second
server op it lies about state — a filter-by-new-tag right after "success"
would miss rows still in flight, and it doesn't survive navigation/refresh.
Good for sub-second ops, wrong shape here — this is the strawman.

**Pick: B.** One-line why: it's the only shape that doesn't block the UI for
2 minutes *and* naturally carries the history/diff needed for per-row
failure reporting and real undo. **Switch trigger:** if apply time were
reliably sub-second regardless of selection size, A would be simpler and
preferable; if the domain needed compliance-grade rollback, B's
"compensating job" undo would be insufficient and need real transactional
support (out of scope here).

## Move 4 — Attack

**Concrete failing input:** Admin filters region=EU, picks "select all
10,000 matching." Job runs 90s. At t=45s another admin edits customer #4521
directly (also in the batch). The batch reaches row 4521 and overwrites that
concurrent edit silently — no conflict signal. This is a real gap: the spec
must add per-row optimistic-concurrency checking (compare a snapshot
version/timestamp taken at selection time) and surface it as a distinct
failure reason ("skipped — modified after selection"), not lump it in with
validation failures.

**Magnitude check:** a literal ID list for 10,000 rows (~36-byte UUIDs) is
~360KB — plausible but wasteful and stale-prone if selection was built while
paging. This confirms reading (a) is the right default for "select all
matching filter": send filter criteria + a captured timestamp + explicit
exclusions, and resolve IDs server-side at execution time. Manual multi-page
checkbox selection still sends a literal ID list. Both modes coexist.

**Re-check assumption:** "selection persists across pagination" — but what
if the admin changes the *filter* mid-selection? The criteria no longer
describe the same set. Spec must state: changing filter/sort clears
selection with a confirming toast ("Selection cleared — filter changed").

**Steelman rejected A:** for small selections (tens of rows, sub-second),
blocking is simpler and arguably fine. But the task states worst case is ~2
minutes, and the UI can't assume small — so B is kept as the uniform model;
small jobs just complete fast enough to feel synchronous.

This attack lands but doesn't overturn Branch's pick — it adds two required
elements (concurrency-check failure reason, selection-clears-on-filter-change)
to the Move-5 output.

## Move 5 — Verify

**Check:** could an engineer implement create-job → poll/subscribe →
job-detail directly from the deliverable, with no missing states? Trace:
select → confirm (count+scope) → POST creates job → tray shows "Queued" →
"Running 4,200/10,000" → "Complete: 9,800 succeeded, 200 failed" → job
detail lists failed rows + reasons (validation / concurrency-conflict /
permission) + CSV export + "Retry failed" → "Revert" available 24h,
enqueues a compensating job. Every Frame criterion (1–8) is hit, including
the two Attack additions. Confidence: **high** on structure, **medium** on
the exact undo time window (24h is a plausible default, not derived from the
task).

---

# Final Deliverable — Bulk Edit UX Spec

**Selection.** Two selection modes, both visible in the UI: (1) manual —
checkboxes per row plus "select all on this page," selection persists across
pagination within the *same* filter/sort; (2) scoped — after selecting all
on the visible page, a banner offers "Select all 10,000 customers matching
this filter." Manual mode sends a literal ID list to the backend; scoped
mode sends the filter criteria plus a captured timestamp (IDs resolved
server-side at execution, avoiding stale/oversized payloads). Changing the
filter or sort clears the current selection and shows a toast: "Selection
cleared — filters changed."

**Initiation & confirmation.** The bulk-action bar appears once ≥1 row is
selected, listing only the three supported actions. Choosing one opens a
confirmation dialog stating: the exact count and scope ("10,000 customers
matching region = EU"), the field and new value, and, for counts above a
threshold (e.g. 500), a note that large changes can take up to ~2 minutes
and will continue in the background. Confirm requires an explicit button
click — no Enter-to-submit — since this is a wide-blast-radius action.

**Progress & completion.** Confirming enqueues a background job; the admin
is never blocked from navigating away. A persistent job entry appears in a
notification tray with states Queued → Running (live count, e.g.
"4,200/10,000 processed") → Complete. A toast fires on completion whether or
not the admin is still on the page. Small selections that finish in
under ~1s can skip straight to "Complete" without ever visibly showing
"Running."

**Partial failure.** Completion always reports both counts: "9,800 succeeded,
200 failed." Clicking opens a job-detail view listing every failed row with
a specific reason drawn from a small fixed set: validation failure (e.g.
invalid tag), permission denied, or conflict (row modified by someone else
after selection was made). The view supports CSV export of failures and a
"Retry failed rows" action that re-runs only that subset as a new job — it
never silently skips or auto-retries.

**Undo / recovery.** Because the mutation is already server-side and
committed per row as it succeeds, undo is a *compensating* job, not a
transactional rollback: the job detail view offers "Revert this change" for
24 hours, which re-applies each affected row's prior value (captured in the
job's diff log at execution time) as a new job, subject to the same
progress/failure reporting. Revert is itself confirmed and itself
undoable-in-principle, avoiding a special-cased "undo of an undo."

**Edge cases.** (1) Concurrent edit: per-row conflict check against the
version/timestamp captured at selection time; conflicting rows are skipped
and reported, never silently overwritten. (2) Filter changes mid-selection:
selection clears, confirmed via toast. (3) Network loss during a running
job: the job is server-owned and unaffected; the client simply resumes
polling/subscribing on reconnect. (4) Permission changes mid-batch (e.g. an
account is reassigned away from the admin's scope during the 2-minute
window): treated as a per-row failure with a "permission denied" reason, not
a silent skip.

**Accessibility.** All bulk-action controls and the confirmation dialog are
fully keyboard-operable (tab order, Esc to cancel, focus returns to the
triggering control on close). Job-state changes (Queued/Running/Complete)
are announced via an `aria-live="polite"` region in the notification tray,
not just visually. The dialog and job-detail table have programmatic
row/column headers so failure reasons are announced per row, not just shown
as color/icon.

*(~640 words)*

```text
STATUS: DONE
CONFIDENCE: medium — high on the overall shape (async job, per-row failure
  log, compensating-job undo); medium on specific defaults like the 24h
  revert window and the 500-row "may take up to 2 min" threshold, which are
  reasonable but not derived from anything in the task.
ASSUMPTIONS: (1) no true transactional rollback exists server-side, so undo
  is a compensating job, not a DB rollback; (2) selection persists across
  pagination within a stable filter/sort; (3) each bulk action applies one
  uniform new value across the whole selection. All three are load-bearing
  for the undo and selection sections above and are unverified against any
  actual backend.
ATTACK: strongest surviving objection — a row edited by another admin
  mid-batch could be silently overwritten with no conflict signal. Handled
  by adding a per-row optimistic-concurrency check with a distinct "conflict"
  failure reason, folded into the partial-failure section; doesn't invalidate
  the chosen background-job architecture, only adds one required failure
  category to it.
```
