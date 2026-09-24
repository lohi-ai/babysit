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
short, or a repeated implementation failure with a named changed hypothesis.
Name the trigger beside the model, log the cost when observed, and limit the
escalation to the affected Dispatch. Ordinary Build still returns to the
normal route when no implementation capability gap has been demonstrated.

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
pointers when accepted scope changes or a bounded failed attempt demonstrates
a capability gap. Preserve the failure evidence and route the next launch;
never replace a live writer. Phase route → model is a Taste
decision: log the ticket tier, phase, harness, model, effort, and classifying
evidence through the Auto-Decision Framework.

This routes workers. Foreman's own session model is whatever `foreman_agent`
launched on; it cannot be changed mid-run, and a multi-day coordinator should
stay on a routine rung rather than charging top-rung rates for reconciliation.
