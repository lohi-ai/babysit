## Decompose and prepare topology

Before step 2, pass **Project design checkpoint** in the project contract.
Step 1 drafts seeds and interfaces; it does not authorize child worktrees or
production dispatch. Reuse current approved artifacts on resume.

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
   In the parent `manifest.md`, map every acceptance criterion to its proposed
   child seed and verification evidence, including cross-ticket journeys. Keep
   the parent plan thin; manifest seed keys stay stable when ticket ids are
   allocated after approval (record those ids in ticket relations/report).
   Assign each
   child exactly one archetype workflow and persist it with
   `BABYSIT_TICKET="$TICKET" bbs ticket set-pointer workflow <workflow>`;
   default to `builder` only when the work shape is ordinary production work.
   For a requested lifecycle loop, model promotion as dependency edges and
   create or unblock the next child only when the prerequisite handoff's
   `LIFECYCLE` and `TRIGGER` evidence satisfy autopilot's Lifecycle loop. Do not
   create or pre-admit speculative grower or maintainer work before its metric or
   operational signal exists. Record shared
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
   Bind the accepted seed with `bbs foreman bind "$PARENT" --seed <key>
   --child "$TICKET"`, initialize code children with contract version 2 per
   execution.md, and include their parent acceptance IDs in the Task.
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
[ -n "$MAX_WORKERS" ] || MAX_WORKERS=4
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
never exceed `MAX_WORKERS` even when global capacity remains. The broker also
checks `parallel_max_workers` atomically, counting current reservations rather
than historical worker rows.
A reservation is keyed by Foreman + Orca Task and is idempotent across resume.
After an interruption or a delayed launch, heartbeat the foreman and repeat
`reserve` immediately before `worker-start`; persist the returned lease id again.
Each replacement reservation has a new id, so an old cleanup cannot release it.
A launch reservation with no new Dispatch is reclaimed after ten minutes if its
owner is stale or missing. A live Dispatch never expires merely because the
foreman stopped heartbeating or the laptop slept.

Both `reserve` and `status`, plus the detached watcher, reconcile all foremen's
leases. An exited agent whose Dispatch is still active is stopped by exact
Dispatch id, then its terminal state is verified before its lease is released.
A live agent or unverifiable remote host retains capacity; `RESOURCE_HELD`
explains unresolved recovery. Follow Orca's recovery evidence for these workers,
not repeated blind waits or a manual release based only on age. Count only
current held reservations toward the worker ceiling, and retry stopped Tasks
through their failed Dispatch, preserving worktrees and checkpoints.
Release a settled reservation with
`bbs foreman resource release "$RESOURCE_LEASE"`, then clear
`pointers.resource_lease`. When reusing a settled worker for a new Dispatch,
release the old Task's lease and reserve the new Task's profile first.

Every worker Task spec must establish the execution envelope before naming its
ticket work: this is a supervised Orca Dispatch, its effective
`AGENT_ROLE=orca`, and it is already spawned. It also names the resource
profile Foreman reserved for this Task. The worker invokes the installed skill
directly in that turn, skips any developer `/goal` copy/paste handoff, uses
Orca `ask` for a genuine User Challenge, and follows the injected lifecycle
through exactly one `worker_done`. This statement in the Task spec is
load-bearing because `worker-start --agent` does not expose an environment
option; never assume a coordinator shell export reached the worker process.
