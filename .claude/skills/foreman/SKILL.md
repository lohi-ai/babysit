---
name: foreman
description: Autonomous Orca orchestrator for large projects made of multiple tickets or dependent features. Decomposes the project, owns branches and worktrees, supervises autopilot workers, coordinates per-ticket and integration QA, and applies the configured finish policy. Requires Orca and its orchestration skill; use autopilot directly for one serial ticket.
---
# foreman

Complete a multi-ticket project without making the human coordinate its parts.
Foreman owns project topology and orchestration; workers own code. Every worker
is a supervised Orca Dispatch running the `autopilot` assistant in a worktree
foreman prepared.

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

The parent ticket is the durable project record. Its `requirement.md`,
`plan.md`, and `manifest.md` define the objective and decomposition; `children`
and `relations` define the DAG. Each child records its parent, position, seed,
branch, worktree, Orca task/dispatch ids, and verdicts. Persist ids immediately
after each successful external mutation. Terminal handles are routing metadata,
never recovery identity.

At every tick, re-read control state. `paused` or `cancelled` means no new
dispatch; leave current files and commits in place. Never act on remembered
status. Mirror project progress into the native task list when available, but
ticket + Orca state remain authoritative.

## Decompose and prepare topology

1. Run the real `plan-draft` skill against the parent. Prefer vertical slices
   that are independently implementable and verifiable. Keep genuine ordering
   as `blocked_by`/`blocks`; avoid artificial chains deeper than 3–4 tasks.
2. Materialize each accepted seed as a child ticket with
   `origin.type=sub_ticket`, parent, position, requirement, and both sides of
   every relation. Assign parent and children to this foreman.
3. For each child, run `bbs ticket ensure --mode=worktree` from the canonical
   repo/base. This command owns branch naming and initial checkout. Parse and
   persist its ticket, branch, and worktree output; never `eval` it. Existing
   child → read `manifest.yaml` and reuse its exact branch/worktree instead of
   calling `ensure` again.
4. Validate the primary checkout, `git worktree list`, every recorded path,
   branch head, and configured base before dispatch. Recreate a missing clean
   worktree only from its recorded branch. A dirty or divergent worktree is a
   recovery case, not permission to replace it.
5. A dependent child starts only after its prerequisites passed per-ticket
   gates. Bring prerequisite branch heads into the dependent worktree with a
   normal recorded merge before dispatch. On conflict, leave the conflict to a
   supervised worker on that ticket; foreman never edits the resolution.

`MAX_WORKERS` comes from `parallel_max_workers` (default 3). Create all Orca
Tasks and their dependency edges before starting the first ready wave. Orca
does not infer scheduling or filesystem conflicts: foreman dispatches only
ready Tasks up to the limit and keeps one writer per child worktree.

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

- The worktree `qa` skill owns `merge-base`, `qa-lease`, and the shared primary
  surface protocol. Foreman treats lease contention as queued work and never
  runs competing surface mutations.
- Independent tickets may complete per-ticket gates in any order. Dependents
  wait for prerequisite gates and branch integration.
- When two or more tickets interact, or the parent criteria describe a
  cross-ticket journey, add one **Integration QA Task** depending on every
  relevant Build Task. Acquire the parent QA lease, use `bbs ticket switch` to
  compose exactly those child branches on the primary checkout, and dispatch a
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
- `land` — run `bbs ticket land` in dependency order. It merges locally and
  never pushes.
- `pr` — invoke the real `create-pr` skill once per child in dependency order.
  Never replace it with raw git/GitHub commands.

After a successful `land` or `pr`, release the settled Orca worker, archive its
readable output through Orca, and remove only its verified-clean non-primary
worktree with ordinary `git worktree remove`. Keep branches. Under `review`, or
on any failure/hold, keep the worktree. Never use `--force`, broad worktree
removal, or terminal-close commands in place of Orca `worker-release`.

## Resume reconciliation

Cold resume must be sufficient with no conversation memory:

1. Resolve parent and children from ticket relations; read controls,
   manifests, checkpoints, verdicts, and finish policy.
2. Bind the recorded Orca Run and list its Tasks. For each Task, inspect the
   current Dispatch and supervised worker state using the live guide.
3. Cross-check each Task against the child worktree and disk gate it claims to
   own. Never synthesize `worker_done` or a PASS to repair disagreement.
4. Recover a lost mutation by request receipt; keep waiting for live workers;
   retry only proven failed/stopped attempts; release every settled worker not
   immediately reused.
5. Recreate only missing safe topology, dispatch the next ready wave, run any
   outstanding integration gate, then finish when all terminal conditions hold.

## Output

Report only state changes, escalations, and terminal evidence; normal worker
activity lives in Orca and the task board.

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: ORCHESTRATED(<completed>/<total>)
PROJECT: <parent>  RUN: <orca-run>  FINISH: <review|land|pr>
TICKETS: <ticket branch QA review readiness result; one row each>
INTEGRATION_QA: <PASS evidence | N/A reason | BLOCKED evidence>
SUMMARY: <project outcome and remaining concern>
NEXT: <only the action left by finish policy>
```
