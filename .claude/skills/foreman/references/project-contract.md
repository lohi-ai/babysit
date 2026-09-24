# Foreman project contract

Read with the Foreman skill at entry and cold resume. The parent is the durable
review and delivery unit; a closed worker/worktree is never proof of delivery.

## Project design checkpoint

Before child creation, worktrees, or production dispatch:

1. Write parent `requirement.md` and run `plan-draft`. For user-facing work,
   invoke `design-ui` to produce a coherent `design.md` and `prototype.html`
   covering the whole product journey, including transitions across proposed
   tickets and important empty/error states. Inspect the prototype. For a
   non-UI project, provide interfaces, example inputs/outputs and a workflow
   in `design.md`; explain why a visual prototype is N/A. This is product
   design, not the `prototype` skill's feasibility spike.
2. Draft `manifest.md`: proposed seed keys, scope, dependencies, shared
   contracts, acceptance ownership and integration checks. This is a preview
   of decomposition, not permission to spawn. Keep runtime ticket ids and
   status in relations/report, so progress does not rewrite approved design.
3. Write the structured `project.json` acceptance contract through
   `bbs foreman contract` (see execution.md), then publish with the parent in scope:

   ```bash
   BABYSIT_TICKET="$PARENT" bbs ticket approval publish --kind project-plan \
     --note "Review the project plan, design/prototype, and proposed tickets"
   ```

   The record fingerprints requirement, plan, design, prototype, manifest and the structured project contract
   (respecting their pointers). `approval status` returns `stale` when those
   artifacts change or disappear; a stale record cannot be approved. Refresh
   the artifacts and re-publish it. Never overwrite a pending review's
   artifacts while a human is reviewing; redirect/rework is a new checkpoint.
4. Default is **human review once for the project**. Present links to the
   actual artifacts plus the proposed tickets. Route the decision through the
   preamble's invocation channel and persist a human answer with
   `approval resolve`; dashboard uses its existing approval panel. For an
   Orca-dispatched Foreman, the coordinator must relay this human checkpoint,
   not substitute its own Taste answer. Wait for an explicit answer; no
   timeout or idle period is approval. `redirected` means rework/re-publish;
   `dropped` stops this project. Only current `approved` unlocks children.
5. **`--auto` delegates the human design reviews to Foreman**, including a
   revised parent design within the authorized scope. It still creates and
   inspects the same artifacts, fills the same five-line design rubric with
   named evidence, publishes `project-plan`, and uses
   `approval self-resolve --foreman "$FOREMAN_ID" --rubric-file <path>`.
   Record the decision in the usual telemetry. Never fake a human verdict.
   Pass the flag to `bbs foreman adopt ... --auto` on explicit invocation;
   `bbs foreman spawn ... --auto` also persists it. `auto: true` on the
   Foreman record survives cold restart, adoption, and omitted flags on resume.
   Old records default to human project review. Existing hold/grant bounds,
   non-delegable decisions, code review, QA and configured finish policy still
   apply. `--auto` does not authorize merging, pushing, or expanding scope.
6. After approval, Foreman reviews child plans autonomously against the
   accepted parent artifacts and revision; humans need not read every child
   plan. Child-specific implementation detail stays on that child. A material
   product/design change pauses affected production work and returns to this
   parent checkpoint; unaffected work can continue. Before each new dispatch
   and at finish, re-read `approval status` rather than trusting an old message.

Keep project review distinct from `kind=plan` child reviews: default child
autonomy is unchanged. Do not downgrade a parent to `plan` to bypass its gate.
This checkpoint replaces the generic final Taste confirmation for Foreman;
do not add another routine human review at the end. Under `--auto`, log Taste
decisions without a confirmation prompt; unresolved User Challenges still route
through the preamble.
Resume an existing running project from current artifacts/evidence; do not
discard its workers or commits to replay setup. If no current parent approval
exists, gather its project design checkpoint before new production dispatch.

## Durable project report

At every reconciliation tick, after a delivery/gate transition, and before the
terminal heartbeat, atomically replace parent `report.md` (write a sibling
temporary file, then rename). Set `pointers.report` to this path. Keep the
latest complete snapshot there; existing ticket history/handoffs retain the
event trail. The report remains readable with `bbs foreman report <parent>`
from the repo even after the coordinator and worktrees close.

Use this compact shape; links must lead to actual artifacts or receipts:

```markdown
# <Project title> — <parent>
Observed at: <UTC time> | Foreman: <id> | Run: <id> | Finish: <policy>
Project review: <human/auto, current approval revision, artifact links>
Execution: <verified/required tickets; running/queued/blocked counts>
Delivery: <PENDING / REVIEW_READY / PR_READY / LANDED_LOCAL / MERGED_REMOTE / UNKNOWN>
Integration QA: <PENDING / PASS / FAIL / STALE / N/A reason> — <branch>@<SHA>, <evidence>

| Ticket / work | Worker / phase | Branch / verified head | Review / QA | PR / merge | Cleanup / blocker |
|---|---|---|---|---|---|
| <id + meaningful title> | <Dispatch + current step> | <branch + SHA> | <evidence links> | <URL/state or land receipt> | <worktree/terminal state; blocker> |

Remaining: <what still has to happen, who owns it, next action>
```

Reconcile rows from ticket/DAG, Orca, current gate evidence, and actual finish
receipts. Include missing children as UNKNOWN, not as absent rows. Label
unreachable sources UNKNOWN with last observation time; a missing worktree or
`worker_done` never establishes merge or QA. Show title and active task, not
only opaque ticket/Dispatch ids. Track required scope explicitly; cancelled
work is not completed acceptance unless the scope was explicitly changed.

Execution, delivery, and cleanup are separate axes. `PR_READY` requires the
expected PRs at verified heads plus final integration PASS, with at least one
still open; it means **not fully merged** and reports the merged/total count.
`LANDED_LOCAL` requires verified presence on local base plus
final PASS, and explicitly says whether remote delivery is verified or still
unpushed/unknown. `MERGED_REMOTE` requires observed remote merge evidence;
do not infer it from a local ancestor test. `REVIEW_READY` requires final PASS
and retained clean branches/worktrees. Evidence-only projects state their
artifact delivery explicitly. Until the required final gate passes, delivery
is PENDING even if every individual handler ran successfully.

The CLI prints this as a **saved snapshot**, never a fresh live verification.
On a status request Foreman first reconciles, persists, and then shows it.
Never rely on this report alone to unlock a gate on resume.

## Final integration QA

Every code-bearing project has a final Integration QA Task. It runs **after
the selected finish handlers and before the Foreman `done` heartbeat**, even
when child tickets are independent. A pre-land integration check is additional
evidence, not a substitute. Wholly evidence-only projects may record N/A with
acceptance evidence. Use a read-only QA worker; fixes go to owning children.

1. Reconcile the complete required child set and current receipt/PR heads.
   Acquire the parent surface lease in every participating primary checkout;
   refresh its TTL during long tests and serialize with other projects.
   Record the original branch/HEAD and the current scratch marker before
   changing a surface. Require a clean checkout and no in-progress Git op.
2. **`finish: land`**: after the last per-ticket QA/scratch composition,
   revert only the known scratch composition before landing. Do not discard
   unrelated retained local lands; reconcile them or block that reset. Land
   required children in dependency order, verify their receipt heads remain
   on `<base>`, then test that actual `<base>` HEAD in the primary checkout.
   Never run `surface compose`, `surface revert`, `serve`, or a reset after
   those lands as preparation/cleanup for final QA. Keep the retained base.
3. **`finish: pr` or `review`**: fetch and record the intended base revision
   (`origin/<base>` for PRs; the configured local base for local review).
   Create a retained `qa/<parent>` branch with `git switch -c` from that
   exact revision in the leased primary checkout. Merge the verified child
   SHAs in dependency order with ordinary Git merges; for PRs, verify/fetch
   the actual current PR heads, not stale local branch tips. A missing head
   or failed fetch is BLOCKED, never permission to test an older revision.
   Record branch/base/child SHAs immediately on the parent in
   `integration-qa.md`. Never move `<base>` to build this composition.
   If the QA branch already exists, reuse it only when its recorded manifest
   and head exactly match; otherwise retain it and create a new uniquely
   suffixed `qa/<parent>-<attempt>` branch. No `-B`, force update or deletion.
   On conflict, abort only the merge this attempt started, preserve the QA
   branch, and dispatch conflict repair to an owning child before rebuilding.
4. Dispatch the QA worker on that **already prepared primary surface** with
   parent identity, branch/HEAD, approved artifact revision, and the full
   child/base manifest. Explicitly require the `qa` skill's Foreman final
   integration mode: no composing, resetting, topology changes, code fixes,
   or nested surface release. Foreman owns the lease. Runtime must serve
   that exact tree; probe the changed behavior after preparing/restarting it.
   Run the parent acceptance journeys including cross-ticket transitions and
   adjacent regressions, not just a union of child PASS counts.
5. Persist `integration-qa.md` with commands, results, runtime identity,
   evidence links and all tested revisions; set `pointers.integration_qa`.
   Persist the parent `qa` verdict too. Check branch/HEAD, clean tree, child
   heads/PR heads, base revision and approval revision again after the tests.
   Changed inputs mean STALE and a rerun, not PASS. Under `land`, compare the
   tested final base HEAD; under `pr`/`review`, compare the source base too.
6. Always finish environment cleanup while still holding the lease. Under
   `pr`/`review`, switch back to the recorded original branch only when safe;
   preserve its HEAD, the QA branch, and the prior scratch marker. Do not use
   `surface revert` as restoration. Under `land`, leave the tested base HEAD
   intact. Release leases on every terminal outcome; a restoration failure
   is a blocker reported with the recoverable checkout, never forced away.
7. On FAIL/STALE, dispatch child repairs and rerun their review/QA and finish
   handlers before rebuilding final QA. If a child's QA reset displaced
   retained lands, re-land and verify every required head before testing base
   again. Only current PASS (or justified evidence-only N/A), successful
   handlers and cleanup permit the final report and `done` heartbeat.

## Executable completion

Use the before/after evidence attempts and `bbs foreman complete` protocol in
[execution.md](execution.md). Markdown reports remain readable context; the shared
CLI/dashboard evaluator is the finish gate. Never infer readiness from command
exit success alone: inspect `data.ready` and its reasons.
