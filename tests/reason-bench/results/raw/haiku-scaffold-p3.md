# P3 — Bulk edit across 10,000 rows: Scaffolded Answer

## 1. Frame

**Restated task**: Write a UX spec (≤800 words, no mockups, precise behavioral
description) that a designer and frontend engineer could build from, for a
bulk-edit feature on a B2B admin data table (50 rows/page, 10,000+ records,
server-side filter/sort). Admin filters → selects records → applies one of
{set account owner, change plan, add/remove tag} → server executes (up to
~2 min for large selections) → can partially fail per-row (e.g., 200/10,000
rejected by validation). Must cover: selection model/initiation/confirmation;
progress/completion states; partial-failure handling; undo/recovery; edge
cases; accessibility.

**Success criteria (checkable)**:
- Spec states explicitly how selection scales past one page (select-all
  across a 10,000-row filtered set, not just the 50 visible rows).
- Spec requires a confirmation step that shows the exact scope (count) being
  acted on.
- Spec defines a progress state that does not block the user for the full
  ~2 minutes (survives navigation away / tab close).
- Spec defines a completion state reporting success-count and failure-count
  distinctly.
- Spec defines how a user sees, exports, and retries failed rows specifically
  (not just "an error happened").
- Spec defines an undo/recovery mechanism and states what it can and cannot
  revert.
- Spec names concurrency/permission/network edge cases, not just happy path.
- Spec includes concrete accessibility behavior (not just "make it
  accessible").
- Deliverable ≤800 words, text-only, no mockups.

**Out of scope**: visual design/styling, exact API contracts, precise
microcopy wording, analytics/telemetry, single-row edit flow, tag data model.

**Two materially different readings worth flagging**: (a) selection could
mean *bounded* selection only (checkboxes on loaded rows, capped at some max
like 500) vs. (b) *selection-by-criteria* — "select all N matching the
current filter," including rows never rendered to the client. Given the task
explicitly sets up 10,000+ records with server-side filtering as the
motivating scale for bulk edit, (b) is the primary reading; I still spec the
bounded case since small selections are common too, but the architecture is
chosen for (b).

## 2. Gather

**Facts** (from task text):
- Table: 50 rows/page, 10,000+ total, server-side filter/sort.
- Ops: set account owner, change plan, add/remove tag.
- Execution is server-side, up to ~2 minutes for large selections.
- Execution can partially fail via per-row validation (example given: 200 of
  10,000 rejected).
- Domain is a B2B admin console (internal/ops users acting on customer
  records tied presumably to billing/ownership).

**Assumptions** (uncited — carried into the spec explicitly, not absorbed):
- A1: "Select all matching filter" is required, not just per-page selection
  — load-bearing for the whole architecture choice. *(medium-high
  confidence; the 10k+/server-filter setup implies it, but not stated
  outright.)*
- A2: Execution must be asynchronous (server-side job, not a held-open HTTP
  request) — a 2-minute synchronous request would exceed typical
  browser/gateway timeouts (~30–60s). Load-bearing.
- A3: No existing generic audit-log system is assumed available; undo is
  specified to be self-contained (the job stores its own prior-values)
  rather than depending on unconfirmed infra.
- A4: Multiple admins can act on the same customer records concurrently —
  plausible for a multi-seat B2B console, not stated in the task; kept as an
  edge case rather than assumed absent.
- A5: Undo is time-boxed (not indefinite) — standard practice, not stated.

## 3. Branch

**Candidate A — Client-held ID list + blocking modal ("wait and watch")**:
checkboxes accumulate an ID array across pages (capped, e.g. 500); confirm
opens a modal with a spinner that stays open until the server responds.
*Score*: fails A1 (can't reasonably hold/scale a 10k ID array as the primary
selection path, and doesn't match the existing server-side-filter
architecture); fails the non-blocking criterion (2 minutes of blocked UI,
and closing the tab loses the operation). Simplest to build, weakest fit.

**Candidate B — Selection-by-criteria + async server job + non-blocking
progress/notification**: selection can be "select all N matching filter"
(snapshotted to an ID set at confirm time); execution is a server-side job
returned immediately; progress surfaces as a persistent, dismissible banner
plus a "Jobs" panel the user can leave and return to; completion, partial
failure, and undo all hang off the job record. *Score*: satisfies A1 (scales
to 10k+), A2 (non-blocking, survives navigation), and gives partial-failure
and undo a natural home (the job's per-row results). Best fit.

**Candidate C — Client-driven batched loop** (client fetches the full
matching ID list, then loops calling the API in chunks of ~500, updating a
local progress bar): cheaper to build (no job-queue infra) but the operation
lives in browser memory — closing the tab or losing network mid-loop leaves
an ambiguous, non-resumable partial state, and undo has no durable record of
what changed. Worth naming as a cheap MVP fallback, not chosen here.

**Pick: Candidate B.** One-line why: it's the only shape that scales to
"select all 10,000 matching filter," survives the up-to-2-minute duration
without blocking the user, and naturally produces a job/result record to
hang partial-failure reporting and undo off of. **Switch trigger**: if PM
confirms selections are always small (≤500) and near-instant (no "up to 2
minutes" case), Candidate A's simpler blocking modal becomes viable and
cheaper — but that contradicts the given facts, so not chosen here.

## 4. Attack

**Concrete failing input**: Admin A filters "Plan = Free" (4,000 rows),
clicks "select all 4,000 matching filter," confirms "Change Plan → Pro."
While the job runs, Admin B (a different session) changes 100 of those same
rows from Free to Enterprise. If the job re-evaluates the filter at
execution time, it may touch rows Admin A never actually saw pass the
filter; if it uses a naively cached ID list with a blind overwrite, those
100 rows get force-changed back to Pro moments after Admin B set them to
Enterprise — silent data loss, and neither admin is warned. **Fix folded
into the design**: snapshot IDs at confirm time (so the shown count is
exactly what's touched), and give every row update an optimistic-concurrency
check (row version/updated-at); a row that changed since the snapshot is
skipped and reported as a failure ("modified by another user"), never
silently overwritten. This is written into the deliverable below.

**Scale check**: 10,000 rows in ~120s ⇒ ~83 rows/sec required server
throughput — ordinary for indexed row validation+write, not a design killer;
it does confirm that a synchronous HTTP request (30–60s typical timeout)
could not carry this operation, reinforcing A2.

**Re-checked assumptions**: A1 stands as primary case; bounded selection
still specified as the common case regardless. A2 stands per the throughput
math above. A3 — spec avoids depending on an unconfirmed generic audit log
by having the job store its own prior-values.

**Steelman of rejected Candidate C**: genuinely cheaper if there's no
job-queue infrastructure yet, and might be a pragmatic v1. Rejected because
the task explicitly separates "progress states" from "completion states" as
distinct spec asks, implying the async, resumable model is expected, not a
client-side loop that dies on tab-close.

**Strongest surviving objection**: the optimistic-concurrency/version-check
requirement adds real backend complexity beyond what the PM literally asked
for ("bulk edit"), and could be seen as scope creep. It doesn't kill the
answer because omitting it leaves a plausible, non-contrived silent
data-loss bug in a multi-admin tool — so it's kept as a named edge case in
the deliverable (not folded silently into "core requirements"), letting the
team consciously accept or descope the risk.

## 5. Verify

**Check**: walk the deliverable against every Frame criterion — select-all
across filter (yes, snapshot-based), confirmation shows exact count (yes),
non-blocking progress surviving navigation (yes, banner + Jobs panel),
completion reports success/fail counts separately (yes), failed rows
viewable/exportable/retryable (yes), undo defined with scope limits (yes —
only rows actually changed, time-boxed), edge cases named (concurrency,
stale selection, permissions, network drop), accessibility concrete (live
region, focus trap, keyboard reachability) — all present below.

**Hand-trace of the Attack scenario against the written spec**: Admin A
selects 4,000 rows at t=0 (IDs snapshotted); Admin B changes row X at t=30s;
job reaches row X at t=90s, version check fails, row X is recorded as
"skipped — modified by another user," appears in the failed-rows list with
that reason, is not overwritten. Matches the mitigation designed in Attack —
confirms it actually made it into the deliverable text rather than staying
only in this reasoning.

**Re-read Frame last**: all five requested areas covered, edge cases and
accessibility included, no mockups, deliverable is text-only behavioral
description. Proceeding to final deliverable (~780 words, under the 800
limit).

---

# Deliverable — Bulk Edit UX Spec

### 1. Selection model, initiation, confirmation

Two selection modes, both from the table toolbar:
- **Manual**: checkbox per row; checked rows persist across pagination (a
  "N selected across pages" chip shows the running count, with a "clear
  selection" action).
- **Select all matching filter**: checking "select all on this page" surfaces
  a banner — "All 50 on this page are selected. Select all 10,000 matching
  the current filter" (link). Clicking it switches to criteria-based
  selection; the exact row IDs are **snapshotted server-side at the moment
  of confirmation**, not re-queried at execution time, so the count and the
  rows acted on are provably identical to what the user saw.

**Initiation**: a "Bulk edit" toolbar action enables once ≥1 row is
selected. It opens a form scoped to one of the three ops (owner / plan /
tag) at a time — not composable, to keep validation and undo tractable.

**Confirmation**: a modal states the operation and exact scope in plain
language ("Change plan to Pro for 10,000 records matching your filter"). For
selections over a threshold (e.g., >100 rows) or any owner/plan change, the
confirm button requires typing the record count or "CONFIRM." The modal also
shows a duration estimate ("May take up to 2 minutes; you can navigate
away").

### 2. Progress and completion states

The job runs server-side, asynchronously. On confirm, the modal closes and a
persistent, dismissible **progress banner** appears, also logged in a "Jobs"
panel reachable from the main nav so the user can leave and return. The
banner shows: operation description, live count ("3,400 / 10,000
processed"), elapsed time, and Cancel (stops only unprocessed rows;
already-applied rows are not rolled back by Cancel).

On completion, the banner becomes a result summary: "9,800 succeeded, 200
failed" with "View details" / "Dismiss." Completed jobs stay in the Jobs
panel for a fixed retention window (e.g., 30 days) as a lightweight audit
trail.

### 3. Partial-failure handling

"View details" opens a table of only the failed rows with a per-row reason
(e.g., "Plan 'Legacy' cannot be changed while contract is active," "Row
modified by another user since selection"). Actions: **Export failed rows**
(CSV) and **Retry failed** (re-runs the same op against only the failed
IDs, through the same confirm → job → result cycle). Success rows are never
rolled back because of unrelated failures elsewhere — failure is per-row,
not all-or-nothing.

### 4. Undo / recovery

The job itself — not a generic audit log — records the **prior value of the
changed field, per row actually modified** (not per row selected; failed or
skipped rows were never touched). The completion banner and Jobs panel both
expose "Undo" for a fixed window (e.g., 4 hours), reapplying prior values to
exactly the rows the job changed. Undo runs as its own job with the same
progress/completion/partial-failure lifecycle (a row can fail to un-apply if
edited again since). Add/remove-tag operations are additionally
self-reversible via the opposite tag action at any time, independent of the
undo window, since tag changes are idempotent.

### 5. Key edge cases & accessibility

- **Concurrent edits**: every row update carries an optimistic-concurrency
  check (row version/updated-at). A row changed by someone else after
  selection is skipped and reported as a failure, never silently
  overwritten.
- **Stale "select all"**: IDs are snapshotted at confirm time, so later
  inserts/deletes matching the filter are not swept in.
- **Permission gaps**: rows the acting admin can't modify (e.g., cross-team
  ownership) fail per-row with a permission-denied reason, not a blanket
  rejection.
- **Network drop / tab close**: the job runs independent of the client
  connection; reopening the app shows current progress from the Jobs panel —
  nothing lost or duplicated.
- **Accessibility**: selection checkboxes and the "select all matching
  filter" link are keyboard-reachable with proper checked/group state; the
  progress banner is an `aria-live="polite"` region so screen readers hear
  state changes without stealing focus; the confirm modal traps focus and
  returns it to the triggering control on close; the failed-rows table and
  CSV export are fully keyboard-operable.

```text
STATUS: DONE
CONFIDENCE: medium — would raise to high with PM confirmation that
  select-all-across-filter (A1) is truly required and that no existing
  job-queue/audit-log infra already dictates a different shape.
ASSUMPTIONS: A1 (select-all-matching-filter is required, not just bounded
  per-page selection), A2 (execution must be async/non-blocking given the
  ~2min duration), A3 (no existing generic audit log — undo is
  self-contained in the job), A4 (concurrent multi-admin edits are
  possible), A5 (undo is time-boxed). All carried explicitly into the spec
  rather than silently assumed.
ATTACK: optimistic-concurrency/version-checking adds backend complexity
  beyond the literal "bulk edit" ask and could be judged scope creep; kept
  as a named edge case (not a buried core requirement) because omitting it
  leaves a plausible silent data-loss bug in a multi-admin tool — doesn't
  kill the answer, but the team should consciously decide whether to build
  or descope it.
```
