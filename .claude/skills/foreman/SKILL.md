---
name: foreman
description: Autonomous Orca orchestrator for large projects made of multiple tickets or dependent features. Decomposes the project, owns branches and worktrees, supervises autopilot workers, coordinates per-ticket and integration QA, and applies the configured finish policy. Requires Orca and its orchestration skill; use autopilot directly for one serial ticket.
---
# foreman

Complete a multi-ticket project without making the human coordinate its parts.
Foreman owns project topology and orchestration; workers own code. Every worker
is a supervised Orca Dispatch running the `autopilot` assistant in a worktree
foreman prepared. Foreman is a persistent goal proxy: one process may disappear,
compact, or restart, but the project goal continues from ticket and Orca state.

Follow [the preamble](../references/preamble.md),
[Auto-Decision Framework](../references/auto-decision-framework.md), and
[worktree protocol](../references/worktrees.md). Shared refs
(`../references/*.md`) are filesystem paths beside this skill's directory, so
read them by path, not as `skill://`. Taste is decided and logged.
Read [the project contract](references/project-contract.md)
in full at entry and cold resume: project approval, durable report, final QA.
Read [execution.md](references/execution.md) for the acceptance contract, CLI
producers, first usable journey, progress/waits and completion boundary.
Escalate at the project design checkpoint (unless `--auto`), or for User
Challenges, the non-delegable money/auth/irreversible-data
floor, an explicit human hold, or an action the repo did not authorize.

The delivered outcome is the accepted feature/product, not a count of closed
tickets. Foreman owns scope coverage, review/QA repair dispatches, dependency
updates, and release evidence; the human should not have to discover missing
pieces or coordinate retries. Composed skills' Taste decisions are logged,
not returned as routine approval requests. Keep human involvement for the
escalations above, the project design checkpoint, and the repo's explicit
finish boundary.

## Ownership boundary

| Owner | Responsibilities |
|---|---|
| **Foreman** | Project decomposition and DAG, branch checkout, worktree create/reuse/cleanup, Orca Run/Task/Dispatch lifecycle, design gates, QA scheduling, integration QA, dependency-order finish. |
| **Autopilot worker** | One ticket on the checkout it receives: requirement/plan, implementation, local commits, `review-pr`, `qa`, verdicts, handoff. It never branches, creates worktrees, pushes, lands, opens PRs, or dispatches siblings. |
| **Orca** | Live Run/Task/Dispatch provenance, injected worker lifecycle, threaded questions, durable Delivery, retries, and terminal ownership. |
| **Babysit ticket state** | Requirements, plans, parent/child relations, manifests, worktree paths, checkpoints, approvals, verdicts, readiness, and finish authorization. |

Foreman does not edit worker code. A clean git operation is coordination; a
merge conflict or integration defect goes to a worker in the affected
worktree. Never force-push, force-remove a worktree, stash or overwrite user
changes, or bypass readiness.

## Orca is the coordination plane

At the start of every fresh invocation or cold resume:

1. Read `~/.claude/skills/orchestration/SKILL.md` in full. Resolve the Orca
   executable exactly as that stub says.
2. Run `ORCA skills get orchestration` with the resolved executable and read
   the full version-matched guide before any other Orca command. `ORCA` here is
   a placeholder, not a literal command or shell variable.
3. Confirm the runtime is reachable and orchestration is enabled using that
   guide. If either is unavailable, report `BLOCKED`; do not fall back to pane
   polling, `bbs foreman mailbox`, a generic subagent API, or guessed flags.
4. Create or bind one Orca Run for the project. Persist its id on the parent
   ticket as `pointers.orca_run`; rebind it on resume. Verify every Task and
   Dispatch with Orca state before describing it as orchestrated.

Use the live guide's preferred supervised loop: create Tasks with dependency
edges, start workers with `worker-start` in exact existing worktrees, wait with
`check --wait`, answer questions by message id, and account for every settled
Dispatch with immediate reuse, `worker-release`, or explicit retention. Do not
cache the guide's CLI grammar in this skill. A wait timeout is a reconcile tick,
not a failure. Use request recovery from the receipt before retrying a mutation
whose response was lost.

## Invocation and durable state

Direct skill invocation from Codex, OMP, or Claude Code inside an Orca terminal
is the default entrypoint. `bbs foreman spawn` is optional recovery/convenience,
not a prerequisite. Before any other mutation, adopt the invoking session:

1. Resolve `FOREMAN_ID`. A managed spawn, dashboard wake, or watchdog nudge
   supplies `--foreman-id <id>` and it wins. On a first direct invocation,
   choose a stable project-scoped id (prefer `fm-<parent>` when the parent
   exists). On a compacted or bare continuation, omit the id only when this
   terminal was already adopted; the command recovers its one recorded id.
2. Name the agent that is executing this skill and run the corresponding form:

   ```bash
   bbs foreman adopt "$FOREMAN_ID" --agent claude  # Claude Code
   bbs foreman adopt "$FOREMAN_ID" --agent omp     # OMP
   bbs foreman adopt "$FOREMAN_ID" --agent codex   # Codex
   ```

   Append `--auto` only when explicitly requested in this invocation; adoption
   persists it on the Foreman record. It delegates human design review, not
   artifact creation, code review, QA, holds, or finish authorization. A bare
   resume preserves the recorded mode; never infer `--auto` from an unattended
   invocation, a profile, or an old child approval.
   Run exactly one matching form, not all three. On a continuation whose id is
   not in context, use `bbs foreman adopt --agent <current-agent>`.
3. Treat adoption failure as `BLOCKED`. Adoption resolves the active Orca
   terminal, cross-checks its agent identity, renames it to `bbs foreman <id>`,
   persists the agent/workspace binding, and is idempotent for that same
   terminal. It refuses another live terminal, agent, repo, or Foreman id.

Never use low-level `bbs foreman register` for direct skill invocation and
never borrow another live terminal's id. When running multiple foremen in one
repo, give each a distinct id and parent project. Direct invocation inherits
the current CLI's permission mode; the session must already permit unattended
tool use if nobody will be present to answer harness-level approval prompts.

- **Free-text project** — create a parent project ticket on the current
  checkout without cutting it, persist the requirement, run `plan-draft`,
  draft bounded child seeds, and pass **Project design checkpoint** before
  creating child tickets/worktrees or dispatching production work. A list of
  already-independent
  requests still gets one parent so the project has one completion condition.
- **Parent ticket or project id** — attach to its recorded Run and resume the
  first unmet gate.
- **Child ticket** — find its parent and resume the owning project; do not run a
  second coordinator for one child.
- **Bare invocation / watchdog nudge** — reconcile assigned projects from
  `bbs foreman inbox`, ticket manifests, Orca Runs/Tasks/Dispatches, git
  worktrees, verdicts, and readiness, then continue.
- **`stop <project|ticket>`** — stop or retain the exact supervised worker by
  the live Orca recovery rules. Leave ticket branches and worktrees intact and
  resumable unless the user separately asked to remove them.

Before creating/binding a Run, changing the DAG, creating worktrees,
dispatching, running integration QA, or finishing, atomically claim the parent:

```bash
BABYSIT_TICKET=<parent> bbs ticket claim "$FOREMAN_ID"
```

The same owner is an idempotent success. Another owner is a hard fence: remain
read-only, identify that foreman and its Run, and report `BLOCKED`. Human
`assign` is an explicit ownership transfer, not a command two live foremen may
race. One parent has one mutating foreman; different parents may run in
parallel, including inside the same repository.

The parent ticket is the durable project record. Its `requirement.md`,
`plan.md`, and `manifest.md` define the objective and decomposition; `children`
and `relations` define the DAG. Each child records its parent, position, seed,
branch, worktree, Orca task/dispatch ids, and verdicts. Persist ids immediately
after each successful external mutation. Terminal handles are routing metadata,
never recovery identity.

Every wake runs the reconcile tick in **Status reconciliation** below — never
act on remembered status. Initialize the harness's native task list at entry
from the parent, children, and DAG (rebuild it from ticket + Orca state on
cold resume), and keep it mirrored at every tick; disk and Orca state remain
authoritative.

## Repository profile and autonomy

Resolve policy from the repository on every fresh invocation or cold resume;
never infer it from branch names:

```bash
eval "$(bbs autopilot git-flow)"
# BBS_PROFILE=pet | startup | enterprise
```

[Git flow](../references/git-flow.md) is canonical. The three profiles change
the cost-of-mistake gates, not how many teammates Foreman is allowed to manage:

| Profile | Derived review / QA | Default finish |
|---|---|---|
| `pet` | low-effort review, smoke QA | `review`; explicit `finish: land` may merge locally |
| `startup` | medium review, standard QA | `review`; explicit `finish: pr` may open PRs |
| `enterprise` | high-effort review, strict QA | `review`; explicit `finish: pr` may open PRs |

All three profiles use the same autonomous decision framework and the maximum
safe ready wave: dispatch every admitted ready Task up to `MAX_WORKERS` and the
machine-global resource budget. Never serialize independent work merely because
the repo is `pet`, and never buy a stronger model merely because it is
`enterprise`. A profile scales verification breadth and the authorized finish
venue; ticket evidence controls model routing. Taste remains self-resolved
unless an explicit hold or bounded grant says otherwise.

## Status reconciliation

A "check status" nudge, a bounded `check --wait` timeout, a `watch --once`
refresh, and a dashboard wake all run the same idempotent reconcile tick —
a full project reconciliation, never a liveness-only reply. One tick:

1. re-adopt the current session idempotently and heartbeat the foreman
   record, then re-read `bbs foreman inbox "$FOREMAN_ID"`, control state, and
   parent/child relations before reading Orca mail. Repeat the inbox read
   after every Delivery or wait timeout and immediately before finish.
   `paused` or `cancelled` means no new dispatch; leave current files and
   commits in place.
2. Bind the recorded Orca Run and read the live state of every project Task,
   its current Dispatch, and its supervised worker. Run
   `bbs foreman resource status` — it reconciles all foremen, releases terminal
   Dispatch leases, and stops proven exited agents before reclaiming their
   slots. It also reclaims reservations with no new Dispatch after ten minutes
   when their owner heartbeat is stale or missing. Each release is printed as
   `RELEASED_LEASE`; anything still listed remains held. Live workers have
   no time-based expiry, even if their foreman disappeared.
3. Cross-check each child against disk: checkpoint freshness, its recorded
   workflow, handoff, lifecycle signal, and git state. Code-bearing children
   additionally require current `review-pr`/`qa` verdicts and
   `bbs ticket readiness --action <review|land|pr> --json`; evidence-only
   prototype, recommendation, or audit children require their workflow verdict
   and acceptance evidence but never fabricated code gates. Reconcile finish
   receipts before worktree-bound reads for cleaned-up children. Never infer
   completion from worker prose or a `worker_done` message alone.
4. Atomically refresh the parent `report.md` using **Durable project report**
   in the project contract, including observation time and evidence links.
   Report the status of every project Task and supervised worker plus global
   resource use — the full TICKETS/INTEGRATION_QA/RESOURCES snapshot, not only
   state changes — with the project DAG (`bbs ticket dag "$PARENT" --mermaid`,
   **The project DAG**) so the shape rides with the status it explains.
5. After the first usable journey passes product review, dispatch the maximum admitted ready wave; before then admit that journey and its prerequisites. Retry only proven failed/stopped
   Dispatches, and release settled workers and their resource leases when not
   immediately reused. Remain active for the next bounded check; a tick with
   work remaining is not a terminal outcome.
6. Run the eager per-ticket finish pass in dependency order — **Eager
   per-ticket finish**: apply `review`, `land`, or `pr` to every eligible child,
   then release its worker and lease and close its Orca worktree surfaces.
   Settled workers can be released immediately; local lands wait for all
   per-ticket surface mutations and any pre-land integration gate to settle.
7. When per-ticket and any pre-land integration gates pass, run the Finish
   and cleanup sequence for whatever the eager pass left — held or failed
   handlers, then mandatory final Integration QA on the delivered
   branch — report the user-facing terminal result, and
   only then write the terminal `done` heartbeat described there.

## Persistent goal and long-horizon loop

When the harness exposes a persistent goal facility, run Foreman under one
goal. Reuse a compatible active goal; otherwise create it from the user's
Foreman request. Its objective must name `FOREMAN_ID`, the owned parent ticket,
the installed `foreman` skill as the protocol to reload on every continuation,
and the terminal condition: every required ticket and integration gate is
complete under the configured finish policy, or a structured
`BLOCKED`/`NEEDS_CONTEXT` handoff is persisted. Keep detailed instructions in
this skill and project artifacts, not in the goal text.

Compaction is a cold-resume boundary. On the first pass after compaction,
re-invoke/re-read this skill, the preamble, and the live Orca orchestration
guide, then re-adopt the current terminal before reconciling disk and Orca
state. A wait timeout, idle prompt,
context compaction, rate-limit pause, closed terminal, or process restart is not
a terminal goal outcome. Never complete the goal for one of those conditions.

Use bounded rolling `check --wait` calls so each timeout becomes a full
reconcile/heartbeat tick. Bound each wait with the configured reconciliation
interval — `bbs config get foreman_status_interval` seconds, default 3600 —
the same value `bbs foreman watch` uses for its status-prompt default, so the
two never drift. Deliveries (`worker_done`, escalation, question) return from
the wait immediately; the interval is only the missed-event/restart/stale-state
backup. An unset or empty key means 3600; a present value that is not a
positive integer of seconds is invalid — stop and report it rather than
guessing, and never let a bad value shrink the wait into a tight loop. For
multi-day work, an external scheduler may run
`bbs foreman ensure <id>` to recreate a missing terminal. The watcher needs
no scheduler: `adopt` and `spawn` auto-start an unscoped detached
`bbs foreman watch`; its global flock keeps one unscoped watcher, while a
scoped watcher uses an id-specific flock and cannot block other foremen. The
watcher exits once no foreman has an open Orca terminal;
`bbs foreman watch <id> --once` remains the manual refresh for an idle one.
Both paths re-enter with the skill and foreman id; a harness without an exact
conversation handle cold-starts instead of resuming an ambiguous "last" chat.
Machine sleep or shutdown pauses work; the next ensure/resume continues it from
durable state.

## Live ticket and change-request intake

The assigned ticket set is the durable intake queue. A dashboard assignment
wakes the running foreman immediately; a CLI-created assignment is still found
on the next bounded reconcile tick. Terminal prose or an Orca message may wake
the coordinator, but it is not accepted scope until represented on disk.

- A new feature slice is a normal child ticket linked on both sides to the
  owned parent, assigned to `FOREMAN_ID`, and added to the Orca DAG exactly
  once. Create missing Tasks before dispatching the next ready wave.
- A change request after planning or implementation began becomes a new child
  ticket labeled `change-request`, with its own requirement and explicit
  dependency/impact relations. Do not rewrite a settled ticket or silently
  change an active Dispatch's accepted scope.
- If the request supersedes active work, pause the affected ticket, preserve its
  worktree, and use a fresh plan/build Dispatch for the replacement scope. If it
  is additive, let unaffected work continue and schedule the CR by dependency.
- Any accepted child-set or parent-criteria change makes prior Integration QA
  stale. Recompose and rerun it. If intake changes during finish, stop after the
  current safe handler boundary, reconcile the new DAG, and never report the
  old child snapshot as project completion.
- Every accepted intake that moved an edge or added a ticket re-emits the DAG
  in the same reply — **The project DAG**.

## Decompose and prepare topology

Read [topology](references/topology.md) before creating or changing children. It owns accepted seeds, worktrees, dependencies and global resource admission.

## The project DAG

`bbs ticket dag` renders the owned parent's graph from ticket state alone —
`children` plus `relations.blocked_by`/`blocks` — layered into waves, with
blockers that live outside the subtree and dangling child ids labelled rather
than dropped. Read-only: it never writes ticket state, so it is safe to run on
every tick.

```bash
bbs ticket dag "$PARENT"              # ASCII listing — the default for a terminal
                                      #   with no renderer
bbs ticket dag "$PARENT" --mermaid    # a fenced flowchart LR block — the form to show
bbs ticket dag "$PARENT" --json       # the same model, machine-readable
```

**Show the mermaid form**, not the ASCII listing: OMP renders the fenced block
in-TUI, and on Claude Code and Codex it is still readable as source. Print the
command's output verbatim — never re-type the edges, and never describe the
graph from memory.

Emit it in the reply at the three moments the graph is the answer:

1. **Topology built** — once decomposition has created the children and linked
   both sides of every relation, the DAG goes in the same reply as the dispatch
   plan. It is an audit surface, not a new approval wait; proceed with admitted
   work unless an explicit hold or User Challenge applies.
2. **Topology changed** — a new child ticket, an accepted change request, or any
   `blocks`/`blocked_by` edit. The graph the coordinator acts on and the graph
   the human last saw must not drift.
3. **Status wake** — the full snapshot in **Status reconciliation** carries it
   alongside the TICKETS/RESOURCES rows.

"Show the DAG", "what's the DAG", or any equivalent ask is this command against
the owned parent. A parent whose children have no children prints as a single
wave of leaf nodes — that is the honest answer, not a reason to fall back to a
prose summary. Any intake that changes the topology re-emits the graph in the
same reply (**Live ticket and change-request intake**), and the graph rides in
the terminal snapshot until the project finishes.

## Worker model and effort routing

Read [worker routing](references/worker-routing.md) before allocating or retrying a worker. The canonical harness/model table and launch receipts govern model selection.

## Two-phase ticket dispatch

Read `pointers.workflow` before creating Tasks. `builder` uses two supervised
Tasks so design review is agent-independent; `prototyper`, `sweeper`, `grower`,
and `maintainer` normally use one execution Task because their own workflow
establishes the baseline, experiment, or audit before changing code. Never hard-code every
child back to `builder`.

Non-builder production work that adds or reshapes a user-facing surface, or
has an explicit plan hold or bounded grant, must also pass the Plan Task and
Design gate before its Execution Task. Preserve its recorded workflow in both
dispatches. If that need emerges during execution, stop before the affected
edits and return to this checkpoint; an experiment or audit does not replace
plan approval.

The accepted parent plan/design/prototype and their approval revision go in
every child Task spec. A child plan must match that product contract; Foreman
reviews child implementation detail autonomously. A material change to the
accepted product returns to the parent checkpoint before affected work starts.

1. **Plan Task** — start a fresh worker in the child's exact worktree on the
   Plan route. Its spec names the ticket and requires the installed babysit
   `autopilot` skill with `<workflow> <ticket> --stop-after=plan`. Require
   `plan-draft` and, for user-facing changes, its `design-ui` artifacts before
   completion; no implementation in this Task. Do not
   hand-write a harness-specific `/bbs:` or `$bbs:` prefix. The worker must
   persist its plan verdict and send Orca `worker_done` from the injected
   lifecycle.
2. **Design gate** — after `worker_done`, read the requirement, plan, design,
   and prototype from ticket paths and verify coverage, host consistency,
   reuse, prototype inspection, and scope. Publish the babysit plan approval,
   then run `bbs ticket approval self-resolve` with named evidence. This
   approval is the safety authority. Mirror its result to an Orca decision gate
   for the Build or Execution Task so the DAG cannot run ahead; never resolve
   the Orca gate independently.
3. **Builder Build Task** — after approval, hard tickets archive and `worker-release`
   the planner, release its resource lease, reserve the Build profile, and
   start a fresh normal worker in the same worktree. Simple and normal tickets
   may reuse the settled planner only when every route and resource field
   matches. Its spec requires `autopilot builder <ticket>` and forbids topology
   and close-out. Autopilot consumes the accepted disk plan, implements,
   commits, runs `review-pr` then `qa`, persists both verdicts, and reports
   `worker_done`.
4. **Non-builder Execution Task** — start one worker in the child's exact
   worktree with `autopilot <workflow> <ticket>`. Its spec includes the same
   topology and close-out prohibitions. If it changes code, it must return
   current review, QA, readiness, and final-tree evidence. If it is an
   evidence-only prototype, recommendation, or audit, it must return the
   workflow verdict, artifact paths, acceptance evidence, and lifecycle signal.
   A prototype's quarantined spike is never a releasable branch: its worker
   archives the signal in ticket storage and restores the checkout before
   `worker_done`.

An incomplete design rubric gets at most two feedback Dispatches, each naming
the missing evidence. Then mark the ticket `BLOCKED`. The non-delegable floor,
human hold, or grant bound routes through the preamble's human channel. Worker
questions are answered by Orca `reply`: Mechanical/Taste from requirements and
the decision framework; User Challenges escalate through the same channel.

Accept evidence, never completion prose. For every code-bearing Task, read
current `review-pr` and `qa` verdicts, `qa-evidence`, git HEAD, checkpoint
freshness, and `bbs ticket readiness --action <review|land|pr> --json` for the
intended action. Read `ok` and `data.ready`, not just exit 0. A stale/missing
verdict or `ready:false` is unfinished even when Orca accepted `worker_done`.
For an evidence-only Task, verify the archetype verdict, artifacts, acceptance
mapping, clean worktree, and lifecycle trigger instead; any code delta promotes
it to the code-bearing gate path.
Edits after a gate invalidate it and require a new worker pass.
Read the verdict bodies: `DONE_WITH_CONCERNS` is acceptable only for
nonblocking residuals, never missing acceptance coverage or a required runtime
check. Turn a repairable failed gate into a bounded follow-up Dispatch with
the reproduction, evidence paths, owning scope, and required rechecks; do not
ask the human to review the code or perform QA. Carry the same blocker's retry
count across replacement Dispatches and resumes.

## QA ownership

Code-bearing workers execute per-ticket QA; foreman owns when and where it runs.

- The worktree `qa` skill owns the `bbs ticket surface` lifecycle and the
  shared primary surface protocol. Foreman treats lease contention as queued
  work and never runs competing surface mutations.
- Independent tickets may complete their applicable per-ticket gates in any
  order. Dependents wait for prerequisite evidence and branch integration.
- When tickets interact, add a **Pre-land integration QA Task** before `land`.
  Acquire the parent surface lease and use `bbs ticket surface compose` to
  test the covered branches before landing. This is preliminary evidence;
  it never replaces final Integration QA on the delivered branch.
- Every code-bearing project has a **Final Integration QA Task** after its
  finish handlers succeed. Follow **Final integration QA** in the project
  contract: landed `<base>` for `land`, retained `qa/<parent>` composition for
  `pr` or `review`. Reserve its resource profile and dispatch a QA worker
  against the parent requirement, approved design, and acceptance map.
- Integration QA is read-only on the primary. It persists parent evidence;
  findings become repair Dispatches in owning child worktrees, recreating a
  cleaned-up worktree from its recorded branch when needed. Re-run child
  review/QA, update the land or PR, and rerun final Integration QA. A retained
  local land displaced by a later per-ticket composition must be restored
  through the normal land handler and verified before final QA.
- Record exact tested branch/head, base revision, child/PR heads, approved
  artifact revision, executed checks, and evidence paths. Any covered revision
  or acceptance change invalidates the result. Independent tickets still need
  the final project acceptance check; `N/A` is only for wholly evidence-only
  projects, with a reason and acceptance evidence.

One failed child blocks its dependents, not unrelated ready work. Retry only a
Dispatch proven failed/stopped, using the live guide's `retry-of` contract and
the same recorded worktree. After three failures or a circuit-breaker, mark the
ticket blocked with evidence and continue any independent Tasks. The project
cannot report `DONE` while a required child or integration gate is blocked.

## Eager per-ticket finish

Read [delivery](references/delivery.md) when a child settles or delivery needs recovery. It owns dependency-order finish, exact PR heads, worker release and worktree cleanup.

## Finish and cleanup

The eager pass finishes most children; this sequence is the fallback for
what it could not — held lands, failed handlers, mandatory final Integration
QA on the delivered branch, and the terminal heartbeat.

Seal verified children with `bbs foreman seal` before finish/cleanup as described
in execution.md. Start the finish handlers only after every parent acceptance criterion has
per-ticket or preliminary integration evidence,
every required code-bearing child has current passing `review-pr` + `qa`
evidence, every evidence-only child has its workflow verdict and artifacts,
any required Pre-land integration QA passed, and readiness allows
the exact action (`land`, `pr`, or `review`) for each code-bearing child. Apply
the repo's single handler in dependency order:

```bash
eval "$(bbs autopilot git-flow)"   # BBS_FINISH=review | land | pr
```

- `review` — keep the clean committed branch and Git worktree for the human;
  optionally compose it with `bbs ticket serve` when asked. The checkout stays,
  but its Orca terminals and harnesses do not.
- `land` — if integration QA (or any `surface compose`/`serve`) left a
  scratch composition on the primary, run `bbs ticket surface revert` first.
  `land`
  itself BLOCKs when the `bbs-serving` marker is nonempty — scratch
  composition must be discarded, never merged onto. Then run
  `bbs ticket land` in dependency order. It merges locally and never pushes.
- `pr` — invoke the real `create-pr` skill once per child in dependency order.
  Never replace it with raw git/GitHub commands.

After the handlers, run **Final integration QA** from the project contract.
Do not set parent completion or the terminal heartbeat until it passes on the
exact delivered branch (or is justified `N/A` for evidence-only work). Persist
the final report before closing the coordinator.

Archive every settled worker's readable output through Orca, then call
`worker-release` unless immediately reusing it; that closes only the
Dispatch-owned agent terminal. After every successful `review`, `land`, or
`pr`, run the Orca worktree close-out from **Eager per-ticket finish**. Only
after `land` or `pr` remove the verified-clean non-primary Git worktree with
`bbs ticket worktree-remove` (bounded retry for transient NTFS open handles),
keeping its branch. A failure or hold keeps the
Git worktree but never justifies a settled orphan terminal; archive, release,
and bulk-close it once Orca proves no Dispatch remains active. Never use
`--force`, broad Git worktree removal, or terminal-close commands in place of
Orca `worker-release`.

After child close-out, `foreman complete` sets the parent ticket to `done`
when every required change landed locally or every child was evidence-only.
Under `review` or while any created PR is still open, it sets the parent to
`in_review`; only an observed merge closes those PR-backed tickets. This ticket
status records delivery state separately from the coordinator completion.

The terminal write is validated completion, and it is last:

```bash
bbs foreman readiness "$PARENT" --action finish --json
bbs foreman complete "$PARENT" --foreman "$FOREMAN_ID"
```

Require `data.ready: true` before complete. The completion receipt is the
only completion signal the external watcher accepts alongside `Record.Status
== done`. `heartbeat --status done` cannot bypass it. Complete only after every
durable gate, finish handler, worker release, and eligible worktree cleanup
succeeded; the record never completes from prose, a `worker_done` message, or a
printed status block. The user-facing
terminal report comes first. After a short delivery grace, the external
`bbs foreman watch` closes the exact adopted Foreman terminal tab, which stops
the coordinator harness without closing unrelated terminals in its shared
Orca worktree. A blocked, paused, or cancelled project never writes `done` — it
reports `BLOCKED` and keeps its ordinary heartbeat.

## Resume reconciliation

Cold resume must be sufficient with no conversation memory:

1. Re-read this skill and the live Orca guide; resolve `FOREMAN_ID`, re-adopt
   the current terminal, re-claim the parent, and reconstruct the active
   persistent goal.
2. Resolve parent and children from ticket relations and the current foreman
   inbox; read controls,
   manifests, checkpoints, verdicts, and finish policy.
3. Re-read recorded `auto`, parent approval status and artifact revision,
   `report.md`, and final integration evidence. A stale approval returns to the
   project checkpoint; a report is a saved observation, never new permission.
   Bind the recorded Orca Run and list its Tasks. For each Task, inspect the
   current Dispatch and supervised worker state using the live guide.
4. Cross-check each Task against the child worktree and disk gate it claims to
   own. Reconcile finish receipts using **Eager per-ticket finish** before
   worktree-bound reads; a valid finished child need not retain its worktree.
   Reconcile prerequisite revisions and the parent acceptance map too. Never
   synthesize `worker_done` or a PASS to repair disagreement.
5. Recover a lost mutation by request receipt; keep waiting for live workers;
   retry only proven failed/stopped attempts; release every settled worker not
   immediately reused.
6. Recreate only missing safe topology, dispatch the next ready wave, run any
   outstanding integration gate, then finish when all terminal conditions hold.

## Output

Report only state changes, escalations, and terminal evidence; normal worker
activity lives in Orca and the task board. One exception: a status wake
("check status" nudge or an explicit status request) always prints the full
snapshot — every project Task and supervised worker, plus the project DAG —
even when nothing changed, because the nudge exists to learn whether the
foreman is wedged.

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED | IN_PROGRESS
VERDICT: ORCHESTRATED(<completed>/<total>)
PROJECT: <parent>  RUN: <orca-run>  PROFILE: <pet|startup|enterprise>  FINISH: <review|land|pr>
REVIEW_MODE: <human|auto>  PROJECT_APPROVAL: <pending|approved|stale>
REPORT: <parent report.md>  OBSERVED_AT: <UTC timestamp>
EXECUTION: <verified/required; active workers; blockers>
DELIVERY: <REVIEW_READY|PR_READY|LANDED_LOCAL|MERGED_REMOTE|PENDING|UNKNOWN>
TICKETS: <ticket title worker phase branch/head QA review PR/merge cleanup; one row each>
RESOURCES: <global used/budget, host pressure, queued heavy Tasks>
INTEGRATION_QA: <PASS tested-branch@SHA evidence | N/A reason | BLOCKED evidence>
SUMMARY: <parent acceptance coverage, release readiness, remaining concern>
NEXT: <only the action left by finish policy>
```
