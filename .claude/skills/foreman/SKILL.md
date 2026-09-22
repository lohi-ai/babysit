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
Escalate only User Challenges, the non-delegable money/auth/irreversible-data
floor, an explicit human hold, or an action the repo did not authorize.

The delivered outcome is the accepted feature/product, not a count of closed
tickets. Foreman owns scope coverage, review/QA repair dispatches, dependency
updates, and release evidence; the human should not have to discover missing
pieces or coordinate retries. Composed skills' Taste decisions are logged,
not returned as routine approval requests. Keep human involvement for the
escalations above and the repo's explicit finish boundary.

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
   `bbs foreman resource status` — it releases every lease whose Dispatch Orca
   proves terminal and prints each as `RELEASED_LEASE`. A lease has
   no time-based expiry, so anything still listed is held.
3. Cross-check each child against disk: checkpoint freshness, current
   `review-pr`/`qa` verdicts,
   `bbs ticket readiness --action <review|land|pr> --json`, and the finish
   policy. Reconcile finish receipts before worktree-bound reads for cleaned-up
   children. Never infer completion from worker prose or a `worker_done`
   message alone.
4. Report the status of every project Task and supervised worker plus global
   resource use — the full TICKETS/INTEGRATION_QA/RESOURCES snapshot, not only
   state changes — with the project DAG (`bbs ticket dag "$PARENT" --mermaid`,
   **The project DAG**) so the shape rides with the status it explains.
5. Dispatch the maximum admitted ready wave, retry only proven failed/stopped
   Dispatches, and release settled workers and their resource leases when not
   immediately reused. Remain active for the next bounded check; a tick with
   work remaining is not a terminal outcome.
6. Run the eager per-ticket finish pass in dependency order — **Eager
   per-ticket finish**: apply `review`, `land`, or `pr` to every eligible child,
   then release its worker and lease and close its Orca worktree surfaces. Done
   tickets never wait for the project.
7. When every required ticket and integration gate passes, run the Finish
   and cleanup sequence for whatever the eager pass left — the integration
   gate, held or failed lands — report the user-facing terminal result, and
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

1. Run the real `plan-draft` skill against the parent. Slice into
   **independent, testable, releasable units**: a child must stand alone as a
   reviewable change — its own branch, its own `review-pr` + `qa`, its own
   revert. Size each child to the smallest unit that still satisfies those
   three, and never split one coherent change across siblings to widen the DAG
   or fill the worker bound: over-decomposition pays a whole plan/build/QA
   cycle per child and invents ordering the code does not have. If the slices
   only land together, they are one ticket.
   A slice too large for one worker pass is equally not a reason to grow the
   project graph. Dispatch it with a Task spec that tells the `autopilot`
   worker to split the work into sub-tickets or to implement it in explicit
   phases inside its own ticket, and gate on that child's single branch,
   verdict set, and handoff. Any sub-ticket needing its own worktree is linked
   on both sides to the parent and added to the DAG like any other child, so
   foreman stays the only owner of topology.
   Keep genuine ordering as `blocked_by`/`blocks`; never remove a real
   dependency to widen the ready wave or shorten the graph.
   In the parent plan, map every acceptance criterion to its owning child
   and verification evidence, including cross-ticket journeys. Record shared
   interfaces (API/data shape, compatibility, migration order) before their
   consumers start. Shared-file edits need explicit ownership or ordering;
   separate worktrees alone do not make conflicting changes independent.
   Missing required coverage becomes a child or an in-scope repair assignment,
   never an unexplained omission. Escalate only if closing it changes accepted
   product scope or requires a non-delegable decision.
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
   The moment both sides of every relation are linked, emit the DAG with the
   dispatch plan — **The project DAG**.
4. Validate the primary checkout, `git worktree list`, every recorded path,
   branch head, and configured base before dispatch. Recreate a missing clean
   worktree only from its recorded branch. A dirty or divergent worktree is a
   recovery case, not permission to replace it.
5. A dependent child starts only after its prerequisites passed per-ticket
   gates. Bring prerequisite branch heads into the dependent worktree with a
   normal recorded merge before dispatch. Persist the exact prerequisite SHAs
   in its Task assignment/handoff. On conflict, leave the conflict to a
   supervised worker on that ticket; foreman never edits the resolution.
   If a prerequisite is repaired later, invalidate affected dependents and
   parent integration evidence. At a settled worker boundary, merge the new
   prerequisite revision into each affected dependent and dispatch repair /
   re-verification in dependency order. Never merge into a live worker's tree
   or accept its old gates as proof for a dependency revision it never tested.

Resolve the worker bound on every fresh invocation or resume:

```bash
MAX_WORKERS="$(bbs config get parallel_max_workers 2>/dev/null || true)"
[ -n "$MAX_WORKERS" ] || MAX_WORKERS=8
```

An explicit `MAX_WORKERS` value must be a positive integer; otherwise report
`BLOCKED` with the invalid value. It is a per-foreman ceiling, not the host
safety limit. Every ready Task must also reserve machine-global weighted
capacity through `bbs foreman resource` before `worker-start`; the broker
atomically serializes all foremen and derives a host CPU/RAM budget
from the current host. `parallel_global_units` may lower that automatic budget
but never raise it. Current CPU or memory pressure queues new work without
stopping a running worker.

Classify the Task from its requirement, plan, and acceptance commands:

| Profile | Use for |
|---|---|
| `plan` | planning, design feedback, and other read-only work |
| `standard` | ordinary implementation, compilation, and tests |
| `android-simulator` | Android emulator/device acceptance |
| `ios-simulator` | iOS simulator acceptance |
| `local-ml` | local model loading, training, or inference |

If workload evidence is ambiguous between `standard` and a heavy profile, use
the heavy profile. Simulator profiles reserve the shared mobile stack and GPU;
`local-ml` reserves the GPU. Before each new or reused Dispatch:

```bash
RESOURCE_OUT="$(bbs foreman resource reserve "$FOREMAN_ID" \
  --ticket "$TICKET" --task "$ORCA_TASK_ID" --profile "$RESOURCE_PROFILE")"
```

Parse `ADMISSION` and `LEASE` from the output; never `eval` it. `queued` means
leave that Task pending and dispatch other admitted work: resource backpressure
is not a failed attempt. `reserved` means immediately persist the lease id as
`pointers.resource_lease` on that ticket, then call `worker-start`. If worker
creation fails, release the lease before retrying. Keep one writer per child worktree;
never exceed `MAX_WORKERS` even when global capacity remains.
A reservation is keyed by Foreman + Orca Task and is idempotent across resume.
It deliberately never expires on a clock: a sleeping laptop can resume a live
worker hours later. Release it with
`bbs foreman resource release "$RESOURCE_LEASE"` only after Orca proves the
Dispatch terminal, then clear `pointers.resource_lease`. When reusing a settled
worker for a new Dispatch, release the old Task's lease and reserve the new
Task's profile first. `bbs foreman resource status` already reconciles durable
leases against live Dispatches on every wake; a lease that survives it is
unproven and stays held, blocking capacity rather than risking duplicate heavy
work.

Every worker Task spec must establish the execution envelope before naming its
ticket work: this is a supervised Orca Dispatch, its effective
`AGENT_ROLE=orca`, and it is already spawned. It also names the resource
profile Foreman reserved for this Task. The worker invokes the installed skill
directly in that turn, skips any developer `/goal` copy/paste handoff, uses
Orca `ask` for a genuine User Challenge, and follows the injected lifecycle
through exactly one `worker_done`. This statement in the Task spec is
load-bearing because `worker-start --agent` does not expose an environment
option; never assume a coordinator shell export reached the worker process.

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

The pack's canonical harness → model list is
[model routing](../references/model-routing.md): the tier definitions, each
harness's ladder with capacity and list prices, the phase-routing table, and
the tier → model rows. Read it before dispatch. It is the one place those IDs
are written down, so never invent a model ID and never read an agent type name
as a model name.

Classify the ticket once as `simple`, `normal`, or `critical` / `hard` from its
requirement, plan, and acceptance commands. Then route by phase:

- Plan and design-feedback Dispatches use the ticket tier. Hard planning takes
  the strongest normally routed planner: Codex `gpt-5.6-sol` at `high`, Claude
  `opus` at `high`, or OMP `@slow` under the current canonical table.
- Build, review, and per-ticket QA use `simple` for a simple ticket and
  `normal` otherwise. Hardness does not make ordinary implementation occupy
  the planning rung: a hard ticket drops to the normal row for Build.
- Integration QA classifies the composed parent surface independently and may
  use the hard route for cross-system or non-delegable reasoning.

Almost every implementation runs on the routine rung — `gpt-5.6-sol`, `opus`,
`@default`. The top rung (`gpt-6-astra`, Fable 5.1) costs 2–2.5x the workhorse
per token and a foreman multiplies that across a batch, so a `critical`
classification alone never buys it. Escalate only under the shared table's
trigger: floor or cross-system work whose workhorse attempt already came back
short. Name the trigger beside the model, log the cost, and keep escalation to
the Plan or gate Dispatch that needs it. Ordinary Build still returns to the
normal route.

Take the row for the harness this worker actually runs on — the CLI
`worker_agent` selects for this run, which `bbs foreman worker-command
--prompt <text>` resolves and preflights (its printed command leads with that
agent). A model ID from another harness's ladder is not a valid start for this
one. Pass both values on a fresh-worker start:

```bash
orca orchestration worker-start --task "$ORCA_TASK_ID" --worktree current \
  --agent <worker agent> --model <model> --effort <effort> --json
```

`--effort` requires `--model`, neither combines with `--terminal`, and both
apply only where that agent and model take them. Read the receipt:
`launch.effective` is the model the worker actually got, and a receipt that
does not echo the request means the start ran without it — record that
limitation, never assume the route was honored. An explicit phase-specific
model or effort the user named wins over the table.

Two cases start without a model flag, each recorded with the Dispatch's
handoff: the harness or its connected worker server does not accept launch
preferences, or the harness has no ladder in the shared table. An `omp` worker
keeps its role binding and a `grok` worker its `grok-4.6` default; persist
that resolved value rather than leaving it empty. Phase routing still applies
to them as recorded intent — a hard `omp` ticket releases its planner and
starts a fresh Build worker like any other — but both phases run on the bound
role, so `planner_model` and `worker_model` persist the same resolved value.

Persist Plan and Build routes separately so resume and retry cannot silently
collapse them:

```bash
BABYSIT_TICKET="$TICKET" bbs ticket set-pointer planner_model "<model-or-role>"
BABYSIT_TICKET="$TICKET" bbs ticket set-pointer planner_effort "<effort-or-unsupported>"
BABYSIT_TICKET="$TICKET" bbs ticket set-pointer worker_model "<model-or-role>"
BABYSIT_TICKET="$TICKET" bbs ticket set-pointer worker_effort "<effort-or-unsupported>"
```

A hard ticket always archives and releases its settled planner, releases the
Plan resource lease, then starts a fresh normal Build worker rather than
`--terminal` reuse — even when Codex maps both phases to `gpt-5.6-sol`. Simple
and normal tickets may reuse a settled worker only when the exact agent,
model/role, effort, and resource profile match. Re-classify and overwrite these
pointers only when accepted scope changes. Phase route → model is a Taste
decision: log the ticket tier, phase, harness, model, effort, and classifying
evidence through the Auto-Decision Framework.

This routes workers. Foreman's own session model is whatever `foreman_agent`
launched on; it cannot be changed mid-run, and a multi-day coordinator should
stay on a routine rung rather than charging top-rung rates for reconciliation.

## Two-phase ticket dispatch

Every child has two supervised Tasks so design review is agent-independent:

1. **Plan Task** — start a fresh worker in the child's exact worktree on the
   Plan route. Its spec names the ticket and requires the installed babysit
   `autopilot` skill with `builder <ticket> --stop-after=plan`. Do not
   hand-write a harness-specific `/bbs:` or `$bbs:` prefix. The worker must
   persist its plan verdict and send Orca `worker_done` from the injected
   lifecycle.
2. **Design gate** — after `worker_done`, read the requirement, plan, design,
   and prototype from ticket paths and verify coverage, host consistency,
   reuse, prototype inspection, and scope. Publish the babysit plan approval,
   then run `bbs ticket approval self-resolve` with named evidence. This
   approval is the safety authority. Mirror its result to an Orca decision gate
   for the Build Task so the DAG cannot run ahead; never resolve the Orca gate
   independently.
3. **Build Task** — after approval, hard tickets archive and `worker-release`
   the planner, release its resource lease, reserve the Build profile, and
   start a fresh normal worker in the same worktree. Simple and normal tickets
   may reuse the settled planner only when every route and resource field
   matches. Its spec requires `autopilot builder <ticket>` and forbids topology
   and close-out. Autopilot consumes the accepted disk plan, implements,
   commits, runs `review-pr` then `qa`, persists both verdicts, and reports
   `worker_done`.

An incomplete design rubric gets at most two feedback Dispatches, each naming
the missing evidence. Then mark the ticket `BLOCKED`. The non-delegable floor,
human hold, or grant bound routes through the preamble's human channel. Worker
questions are answered by Orca `reply`: Mechanical/Taste from requirements and
the decision framework; User Challenges escalate through the same channel.

Accept evidence, never completion prose. For every Build Task, read current
`review-pr` and `qa` verdicts, `qa-evidence`, git HEAD, checkpoint freshness,
and `bbs ticket readiness --action <review|land|pr> --json` for the intended
action. Read `ok` and `data.ready`, not just exit 0. A stale/missing verdict
or `ready:false` is unfinished even when Orca accepted `worker_done`.
Edits after a gate invalidate it and require a new worker pass.
Read the verdict bodies: `DONE_WITH_CONCERNS` is acceptable only for
nonblocking residuals, never missing acceptance coverage or a required runtime
check. Turn a repairable failed gate into a bounded follow-up Dispatch with
the reproduction, evidence paths, owning scope, and required rechecks; do not
ask the human to review the code or perform QA. Carry the same blocker's retry
count across replacement Dispatches and resumes.

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
  the primary checkout, classify and reserve the Integration QA Task's global
  resource profile, and dispatch a QA worker there against the parent
  requirement and plan. Under `land`, a child covered by a pending
  Integration QA Task holds its land until that gate passes — `surface
  compose` resets local base to `origin/<base>` and would discard an early
  merge. Children outside the coverage set land eagerly.
- Before accepting integration QA, reconcile the parent acceptance map against
  executed checks on the composed surface. Record the exact child branch SHAs,
  base revision, and parent requirement/plan used. All required product journeys
  must have passing evidence; per-ticket PASS counts alone cannot prove this.
  Any covered revision or acceptance change invalidates the integration result.
- Integration QA is read-only on the composed primary. It persists parent QA
  evidence but does not fix code there. A finding becomes a follow-up Dispatch
  to the owning child worktree — under `pr`, where the child may already be
  PRed and its worktree removed, recreate the worktree from its recorded
  branch and let the fix update that PR — then rerun that child's review/QA
  and rebuild and rerun the Integration QA Task. Release the parent lease on
  every terminal outcome.
- If tickets are demonstrably independent and no parent journey crosses them,
  record why integration QA is `N/A`; current per-ticket evidence still gates
  completion.

One failed child blocks its dependents, not unrelated ready work. Retry only a
Dispatch proven failed/stopped, using the live guide's `retry-of` contract and
the same recorded worktree. After three failures or a circuit-breaker, mark the
ticket blocked with evidence and continue any independent Tasks. The project
cannot report `DONE` while a required child or integration gate is blocked.

## Eager per-ticket finish

A child whose durable gates pass finishes at the tick that observes it —
done tickets never wait for the project. Merged code reaches base early and
the ticket's worker, lease, and worktree free up for the next wave. A child
is eligible when all of these hold:

- current `review-pr` + `qa` verdicts are DONE or DONE_WITH_CONCERNS;
- `bbs ticket readiness --action <land|pr|review> --json` allows that exact
  action — under `review` it still guards a dirty tree, an active attempt,
  and stale evidence, even though Foreman performs no merge or remote write;
- every prerequisite child has itself finished (landed, PRed, or — under
  `review` — gates passed); dependency order is preserved, never reordered;
- under `land`, the child is not covered by a pending Integration QA Task —
  `surface compose` resets local base to `origin/<base>` and would discard
  an early merge. Covered children hold until that gate passes; children
  outside the coverage set land at the next tick.

Foreman runs the handler itself — a clean git operation is coordination, not
code — in dependency order, one child at a time:

- `land` — `bbs ticket land <child>` from the primary checkout. Revert any
  scratch composition first (`bbs ticket surface revert`); `land` BLOCKs on
  a nonempty `bbs-serving` marker. It merges locally and never pushes.
- `pr` — invoke the real `create-pr` skill for that child as soon as its
  gates pass; a PR does not mutate base, so integration coverage does not
  hold it. Persist the result as `pointers.pr` on the child.
- `review` — no merge is authorized. Run Orca worktree close-out and keep the
  clean branch and Git worktree for human inspection.

After a successful `review`, `land`, or `pr`: archive the settled worker's
output, `worker-release` it, release the resource lease, and run Orca worktree
close-out. After `land` or `pr`, also remove the verified-clean non-primary Git
worktree with ordinary `git worktree remove`; keep the branch. On any failure
or hold, close settled terminals but keep the Git worktree recoverable.

**Orca worktree close-out** — `worker-release` closes only the one agent
terminal its Dispatch owns. Before any bulk close, prove nothing supervised is
still live in that worktree: every Dispatch recorded on the ticket is settled,
and `orca orchestration worker-list --run <run_id> --terminal-state active
--include-remote --json` shows no worker placed at that path — a `reclaimable`
row there gets its own `worker-release` first. Then close every other terminal
and harness process owned by that exact ticket worktree:

```bash
orca terminal close --worktree path:<worktreePath> --all --json
orca tab list --worktree path:<worktreePath> --json        # then per row:
orca tab close --page <browserPageId> --json
orca emulator list --worktree path:<worktreePath> --json   # then per row:
orca emulator kill --emulator <id> --json
orca terminal list --worktree path:<worktreePath> --json   # verify: zero rows
orca worktree set --worktree path:<worktreePath> \
  --workspace-status <in-review|completed> --json          # in-review under
                                                          #   review/pr, completed under land
orca automations list --json                               # land/pr only: rows whose
                                                          #   runContext.path matches
orca automations edit <id> --disabled --json               #   disable, keep history
git worktree remove <worktreePath>                          # land/pr only; last
```

The bulk terminal close is mandatory even under `review`: it stops setup
shells, agent harnesses, and configured terminal tabs instead of leaving zombie
processes beside a dormant checkout. Never run it while a Dispatch is active or
unverifiable. `selector_not_found` on a `path:` selector means Orca tracks
nothing there — the clean case, not an error. A surface that refuses to close
keeps the Git worktree recoverable like any other hold. Never substitute
`orca worktree rm`: it also tries to delete the checked-out local branch, which
the ticket keeps — so after `git worktree remove` the Orca worktree record
stays behind pointing at a deleted path. That stale card is expected; it is
not a reason to run `worktree rm`, which would delete the branch outright once
the checkout is gone.

Failure routing — never blind-retry an unchanged state:

- surface-lease contention → leave the child eligible; the next tick retries;
- stale or `ready:false` readiness → return the child to verification
  (re-run the affected gate in its worktree) before landing;
- merge conflict → a supervised repair Dispatch in the child's worktree
  resolves it (merge `origin/<base>` in, never local base); keep the
  worktree and do not retry the land until that Dispatch settles;
- a `land` BLOCK that is not a conflict (dirty primary, off-base checkout,
  scratch marker) → report it in the tick output and stop retrying until the
  primary state changes;
- a discarded merge — a `surface compose`/`revert` reset base after the
  land, so the branch is no longer an ancestor — → re-land at the next tick;
  if the worktree was already removed, recreate it from the recorded branch
  first (`land` evaluates readiness inside it);
- a `create-pr` failure → retry once at the next tick, then mark the child
  blocked with evidence.

On resume, recognize a finish receipt before evaluating worktree-bound
readiness. Persist each successful handler's action, verified branch/head and
dependency SHAs, gate evidence paths, and PR URL or landed revision in the
child handoff before removing its worktree. A `pointers.pr` link or
`git merge-base --is-ancestor` result is a recovery lead, not proof of current
acceptance. Verify the receipt still matches current scope and revisions; for
`pr`, read the PR's state and head (an open or merged PR's head must match the
verified revision; later unverified commits and closed-unmerged PRs are not a
successful finish). For `land`, verify the
recorded head remains in base. Reuse valid evidence without recreating a
worktree just to run readiness; missing/stale proof requires reconstruction
from the recorded branch and re-verification. For a changed PR head, first
fetch and inspect that actual head; do not re-verify an obsolete local branch
and call the remote change covered. Preserve any divergent local work and
dispatch reconciliation without force-pushing. Recover a lost receipt from
actual handler state and existing gate evidence, never by assuming success.

## Finish and cleanup

The eager pass finishes most children; this sequence is the fallback for
what it could not — integration-QA-held lands, failed lands awaiting repair,
and the terminal heartbeat — and it still owns the integration gate.

Finish only after every parent acceptance criterion has passing evidence,
every required child has current passing `review-pr` + `qa` evidence,
the parent integration gate passed or is justified `N/A`, and readiness says
the exact action (`land`, `pr`, or `review`) is allowed. Apply the repo's
single handler in dependency order:

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

Archive every settled worker's readable output through Orca, then call
`worker-release` unless immediately reusing it; that closes only the
Dispatch-owned agent terminal. After every successful `review`, `land`, or
`pr`, run the Orca worktree close-out from **Eager per-ticket finish**. Only
after `land` or `pr` remove the verified-clean non-primary Git worktree with
ordinary `git worktree remove`, keeping its branch. A failure or hold keeps the
Git worktree but never justifies a settled orphan terminal; archive, release,
and bulk-close it once Orca proves no Dispatch remains active. Never use
`--force`, broad Git worktree removal, or terminal-close commands in place of
Orca `worker-release`.

The terminal write is the `done` heartbeat, and it is last:

```bash
bbs foreman heartbeat "$FOREMAN_ID" --status done
```

`Record.Status == done` is the only completion signal the external watcher
consumes. Write it only after every durable gate, finish handler, worker
release, and eligible worktree cleanup succeeded; the record never completes
from prose, a `worker_done` message, or a printed status block. The user-facing
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
3. Bind the recorded Orca Run and list its Tasks. For each Task, inspect the
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
TICKETS: <ticket branch QA review readiness result; one row each>
RESOURCES: <global used/budget, host pressure, queued heavy Tasks>
INTEGRATION_QA: <PASS evidence | N/A reason | BLOCKED evidence>
SUMMARY: <parent acceptance coverage, release readiness, remaining concern>
NEXT: <only the action left by finish policy>
```
