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
[worktree protocol](../references/worktrees.md). Taste is decided and logged.
Escalate only User Challenges, the non-delegable money/auth/irreversible-data
floor, an explicit human hold, or an action the repo did not authorize.

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
  checkout without cutting it, persist the requirement, run `plan-draft`, and
  decompose L work into bounded child tickets. A list of already-independent
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
   its current Dispatch, and its supervised worker.
3. Cross-check each child against disk: checkpoint freshness, current
   `review-pr`/`qa` verdicts, `bbs ticket readiness --json`, and the finish
   policy. Never infer completion from worker prose or a `worker_done`
   message alone.
4. Report the status of every project Task and supervised worker — the full
   TICKETS/INTEGRATION_QA snapshot, not only state changes.
5. Dispatch only newly ready work, retry only proven failed/stopped
   Dispatches, and release settled workers not immediately reused. Then stay
   active for the next bounded check: a tick with work remaining is not a
   terminal outcome.
6. When every required ticket and integration gate passes, run the Finish
   and cleanup sequence, report the user-facing terminal result, and only
   then write the terminal `done` heartbeat described there.

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
`bbs foreman ensure <id>` to recreate a missing terminal and `bbs foreman watch
<id> --once` to refresh an idle one. Both paths re-enter with the skill and
foreman id; a harness without an exact conversation handle cold-starts instead
of resuming an ambiguous "last" chat. Machine sleep or shutdown pauses work;
the next ensure/resume continues it from durable state.

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

## Decompose and prepare topology

1. Run the real `plan-draft` skill against the parent. Prefer vertical slices
   that are independently implementable and verifiable. Keep genuine ordering
   as `blocked_by`/`blocks`; avoid artificial chains deeper than 3–4 tasks.
2. For each accepted seed, run `bbs ticket ensure --mode=worktree
   --from-input-file "$SEED_PATH" --reason foreman-decompose` from the
   canonical repo/base. `ensure` owns the ticket id, branch naming, and
   initial checkout — it only cuts on its slow path, so never pre-create the
   ticket id: a resolved `BABYSIT_TICKET` forces the fast-path no-op and no
   worktree is made. Parse and persist its `TICKET`/`BRANCH`/`WORKTREE`
   output; never `eval` it. Existing child → read `manifest.yaml` and reuse
   its exact branch/worktree instead of calling `ensure` again.
3. Initialize each child as a sub-ticket from inside its own worktree:
   `bbs ticket init --parent <parent> --origin-type sub_ticket --seed <seed
   path> --plan <parent plan> --position <n> --worktree <path>`. Running it
   from the worktree records the ticket branch in `pointers.branch`; from
   the primary it would record `main`. Write `requirement.md`, link both
   sides of every relation, and assign parent and children to this foreman.
4. Validate the primary checkout, `git worktree list`, every recorded path,
   branch head, and configured base before dispatch. Recreate a missing clean
   worktree only from its recorded branch. A dirty or divergent worktree is a
   recovery case, not permission to replace it.
5. A dependent child starts only after its prerequisites passed per-ticket
   gates. Bring prerequisite branch heads into the dependent worktree with a
   normal recorded merge before dispatch. On conflict, leave the conflict to a
   supervised worker on that ticket; foreman never edits the resolution.

Resolve the worker bound on every fresh invocation or resume:

```bash
MAX_WORKERS="$(bbs config get parallel_max_workers 2>/dev/null || true)"
[ -n "$MAX_WORKERS" ] || MAX_WORKERS=16
```

An explicit value must be a positive integer; otherwise report `BLOCKED` with
the invalid value. Create all Orca Tasks and their dependency edges before
starting the first ready wave. Orca does not infer scheduling or filesystem
conflicts: foreman dispatches only ready Tasks up to `MAX_WORKERS` and keeps
one writer per child worktree. The bound is per foreman: `F` concurrent foremen
can request up to `F × MAX_WORKERS`, so each still respects actual Orca/host
capacity rather than treating 16 as a launch quota.

Every worker Task spec must establish the execution envelope before naming its
ticket work: this is a supervised Orca Dispatch, its effective
`AGENT_ROLE=orca`, and it is already spawned. The worker invokes the installed
skill directly in that turn, skips any developer `/goal` copy/paste handoff,
uses Orca `ask` for a genuine User Challenge, and follows the injected
lifecycle through exactly one `worker_done`. This statement in the Task spec is
load-bearing because `worker-start --agent` does not expose an environment
option; never assume a coordinator shell export reached the worker process.

## Two-phase ticket dispatch

Every child has two supervised Tasks so design review is agent-independent:

1. **Plan Task** — start a fresh worker in the child's exact worktree. Its spec
   names the ticket and requires the installed babysit `autopilot` skill with
   `builder <ticket> --stop-after=plan`. Do not hand-write a harness-specific
   `/bbs:` or `$bbs:` prefix. The worker must persist its plan verdict and send
   Orca `worker_done` from the injected lifecycle.
2. **Design gate** — after `worker_done`, read the requirement, plan, design,
   and prototype from ticket paths and verify coverage, host consistency,
   reuse, prototype inspection, and scope. Publish the babysit plan approval,
   then run `bbs ticket approval self-resolve` with named evidence. This
   approval is the safety authority. Mirror its result to an Orca decision gate
   for the Build Task so the DAG cannot run ahead; never resolve the Orca gate
   independently.
3. **Build Task** — after approval, immediately reuse the settled plan worker
   with a fresh Dispatch when the live guide allows it; otherwise release it
   and start a worker in the same child worktree. Its spec requires
   `autopilot builder <ticket>` and forbids topology and close-out. Autopilot
   implements, commits, runs `review-pr` then `qa`, persists both verdicts, and
   reports `worker_done`.

An incomplete design rubric gets at most two feedback Dispatches, each naming
the missing evidence. Then mark the ticket `BLOCKED`. The non-delegable floor,
human hold, or grant bound routes through the preamble's human channel. Worker
questions are answered by Orca `reply`: Mechanical/Taste from requirements and
the decision framework; User Challenges escalate through the same channel.

Accept evidence, never completion prose. For every Build Task, read current
`review-pr` and `qa` verdicts, `qa-evidence`, git HEAD, checkpoint freshness,
and `bbs ticket readiness --json` for the intended action. A stale/missing
verdict or `ready:false` is unfinished even when Orca accepted `worker_done`.
Edits after a gate invalidate it and require a new worker pass.

## QA ownership

Workers execute per-ticket QA; foreman owns when and where it runs.

- The worktree `qa` skill owns the `bbs ticket surface` lifecycle and the
  shared primary surface protocol. Foreman treats lease contention as queued
  work and never runs competing surface mutations.
- Independent tickets may complete per-ticket gates in any order. Dependents
  wait for prerequisite gates and branch integration.
- When two or more tickets interact, or the parent criteria describe a
  cross-ticket journey, add one **Integration QA Task** depending on every
  relevant Build Task. Acquire the parent surface lease, use
  `bbs ticket surface compose` to compose exactly those child branches on
  the primary checkout, and dispatch a
  QA worker there against the parent requirement and plan.
- Integration QA is read-only on the composed primary. It persists parent QA
  evidence but does not fix code there. A finding becomes a follow-up Dispatch
  to the owning child worktree; rerun that child's review/QA and then rebuild
  and rerun the Integration QA Task. Release the parent lease on every terminal
  outcome.
- If tickets are demonstrably independent and no parent journey crosses them,
  record why integration QA is `N/A`; current per-ticket evidence still gates
  completion.

One failed child blocks its dependents, not unrelated ready work. Retry only a
Dispatch proven failed/stopped, using the live guide's `retry-of` contract and
the same recorded worktree. After three failures or a circuit-breaker, mark the
ticket blocked with evidence and continue any independent Tasks. The project
cannot report `DONE` while a required child or integration gate is blocked.

## Finish and cleanup

Finish only after every required child has current `review-pr` + `qa` DONE,
the parent integration gate passed or is justified `N/A`, and readiness says
the exact action is allowed. Apply the repo's single handler in dependency
order:

```bash
eval "$(bbs autopilot git-flow)"   # BBS_FINISH=review | land | pr
```

- `review` — leave clean committed branches/worktrees for the human; optionally
  compose them with `bbs ticket serve` when asked.
- `land` — if integration QA (or any `surface compose`/`serve`) left a
  scratch composition on the primary, run `bbs ticket surface revert` first.
  `land`
  itself BLOCKs when the `bbs-serving` marker is nonempty — scratch
  composition must be discarded, never merged onto. Then run
  `bbs ticket land` in dependency order. It merges locally and never pushes.
- `pr` — invoke the real `create-pr` skill once per child in dependency order.
  Never replace it with raw git/GitHub commands.

Archive every settled worker's readable output through Orca, then call
`worker-release` unless immediately reusing it; that is the worker-terminal
close operation. Only after a successful `land` or `pr` — `land` evaluates
readiness inside the recorded worktree and BLOCKs on chdir if it is gone —
remove its verified-clean non-primary worktree with ordinary
`git worktree remove`, and keep its branch. Under `review`, or on any
failure/hold, keep the worktree recoverable. Never use `--force`, broad
worktree removal, or terminal-close
commands in place of Orca `worker-release`.

The terminal write is the `done` heartbeat, and it is last:

```bash
bbs foreman heartbeat "$FOREMAN_ID" --status done
```

`Record.Status == done` is the only completion signal the external watcher
consumes — it drops the foreman from the watch set on that value alone.
Write it only after every durable gate, the finish handler, worker release,
and eligible worktree cleanup have all succeeded; the record never completes
from prose, a `worker_done` message, or a printed status block. A blocked,
paused, or cancelled project never writes `done` — it reports `BLOCKED` and
keeps its ordinary heartbeat.

## Resume reconciliation

Cold resume must be sufficient with no conversation memory:

1. Re-read this skill and the live Orca guide; resolve `FOREMAN_ID`, re-adopt
   the current terminal, re-claim the parent, and reconstruct the active
   persistent goal.
2. Resolve parent and children from ticket relations and the current foreman
   inbox; read controls,
   manifests, checkpoints, verdicts, and finish policy.
3. Bind the recorded Orca Run and list its Tasks. For each Task, inspect the
   current Dispatch and supervised worker state using the live guide.
4. Cross-check each Task against the child worktree and disk gate it claims to
   own. Never synthesize `worker_done` or a PASS to repair disagreement.
5. Recover a lost mutation by request receipt; keep waiting for live workers;
   retry only proven failed/stopped attempts; release every settled worker not
   immediately reused.
6. Recreate only missing safe topology, dispatch the next ready wave, run any
   outstanding integration gate, then finish when all terminal conditions hold.

## Output

Report only state changes, escalations, and terminal evidence; normal worker
activity lives in Orca and the task board. One exception: a status wake
("check status" nudge or an explicit status request) always prints the full
snapshot — every project Task and supervised worker — even when nothing
changed, because the nudge exists to learn whether the foreman is wedged.

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED | IN_PROGRESS
VERDICT: ORCHESTRATED(<completed>/<total>)
PROJECT: <parent>  RUN: <orca-run>  FINISH: <review|land|pr>
TICKETS: <ticket branch QA review readiness result; one row each>
INTEGRATION_QA: <PASS evidence | N/A reason | BLOCKED evidence>
SUMMARY: <project outcome and remaining concern>
NEXT: <only the action left by finish policy>
```
