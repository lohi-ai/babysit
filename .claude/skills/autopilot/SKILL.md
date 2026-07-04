---
name: autopilot
description: Run a checkpointed babysit workflow from a short requirement or existing ticket. Use for multi-step autonomous work that should survive context loss: plan, implement, verify, and hand off.
---

# autopilot

Autopilot is a **goal proxy**: the skill owns init — durable ticket state,
branch, requirement, plan — and Claude Code's `/goal` owns the work loop,
where the model runs the way it would for a direct ask (full reasoning, one
continuous context, no step ceremony) and babysit's persisted-verdict gates
are the terminal condition. Keep the skill small; store durable state on
disk. Autopilot must be safe to re-enter until it prints a terminal status.

The pack runs **one workflow per archetype** (see
[references/archetypes.md](../references/archetypes.md)) — `prototyper`,
`builder`, `sweeper`, `grower`, `maintainer`. Guide every invocation to the
archetype that matches the shape of the work.

## Init (every fresh invocation)

1. Resolve invocation: inline requirement, ticket id, named archetype workflow, or resume from branch/checkpoint. A checkpoint with work already in flight means this is loop re-entry (below), not init.
2. Ensure a ticket directory exists with `requirement.md` and `checkpoint.json`. Where the ticket's branch lives is the repo's git-flow `mode` (`trunk` / `branch` / `worktree` in `.babysit/git-flow.yaml` — see [references/git-flow.md](../references/git-flow.md)); an invocation may override it for one run with `--mode=<m>`, passed through to `bbs-ticket ensure --mode <m>`. If `ensure` prints `WORKTREE=<path>` (safe-cut divert, or `worktree` mode where every ticket gets one), cd into that worktree — every later step runs there, never in the original checkout, and QA lands the branch on the primary via `bbs-ticket merge-base`. Stop here when the invocation passed `--stop-after=requirement`.
3. Pick the archetype workflow. If the invocation names one (`prototyper`, `sweeper`, `grower`, `maintainer`), use it. Otherwise route by the shape of the work using the choosing rules in [references/archetypes.md](../references/archetypes.md) — a pure-subtraction requirement is `sweeper`, a metric-moving ask on a shipped product is `grower`, a scale/security/reliability audit or reported bug is `maintainer`, an unvalidated hunch is `prototyper`. When the shape is ambiguous or it's ordinary production work, default to `builder`, which selects its own mode (child / orchestrate / implement / build / verify). Emit `NEEDS_CONTEXT` only when there is no ticket, requirement, plan, manifest, or branch work and no archetype was named.
4. Seed the plan when the routed mode needs one (builder build mode, size above XS): run `plan-draft` now — it front-loads the codebase survey and routes user-facing work through `design-ui`, and `plan.md` on disk is what a crashed loop session recovers from. Stop here when the invocation passed `--stop-after=plan`.
5. Hand the work to `/goal` (below). Init never executes workflow steps itself.

## The work loop (`/goal`)

`/goal <condition>` arms a session-scoped Stop hook that blocks the session
from stopping until the condition holds, judged against the session's final
message each time it tries to stop. Autopilot cannot arm it itself, so after
init print the handoff line and stop. `INVOKER=developer`: the human runs
it. Orchestrators: put the same line at the front of the spawn prompt —
`/goal` supports non-interactive sessions.

```
/goal <ticket> is done: qa verdict PASS/FIXED persisted via bbs-ticket set-verdict,
review-pr verdict persisted, branch pushed, handoff note written — or a
NEEDS_CONTEXT / BLOCKED status block printed verbatim.
Work it: /bbs:autopilot <workflow> <ticket>
```

The escape clause is load-bearing: `NEEDS_CONTEXT` / `BLOCKED` *satisfies*
the goal, so the loop terminates on escalation instead of grinding against a
missing input. The augmentation matters too: ticket id + workflow + success
criteria make every iteration self-sufficient — a fresh session re-derives
the rest from the checkpoint, so retries survive context death.

When already inside the loop (`/goal` re-entry, orchestrator,
`SPAWNED=true`), skip the handoff and work:

- Cold session: recover first — checkpoint, ticket files, workflow file, git
  state (the preamble prints the recovery block). Warm session: keep going
  with what's already in context.
- Treat the workflow file as mode router + gate list, not a script: pick the
  mode from durable state, honor the gates — `review-pr`, `qa`, verdicts
  persisted via `bbs-ticket set-verdict`, clean handoff; the push/PR hook
  reads them — and do everything between the gates the way a direct session
  would.
- Always run QA before final handoff, and persist its verdict with
  `bbs-ticket set-verdict --skill qa` (real PASS/FIXED, or
  DONE_WITH_CONCERNS naming the blocker when QA cannot run). An unwritten
  verdict stalls handoff at a human checkpoint.
- Checkpoint at milestones — plan seeded, implementation verified, review
  findings fixed, QA verdict — enough for the next cold session to resume,
  not per-step ceremony.
- End every pass with the status block below; the Stop hook judges the goal
  against it.

## Step Skills

- Planning uses `plan-draft`.
- Coding uses `implement`.
- Static landing review uses `review-pr`.
- QA uses `qa`; when full QA has no runnable target, record the fallback and use `browse` or a narrow local check.
- Debug/fix loops use `investigate`.

`create-pr` is never run inside autopilot. Autopilot stops at a QA-verified
branch handoff; the human runs `create-pr` after reviewing the result.

## Rules

- Never *require* conversation memory: disk state (checkpoint, plan,
  handoffs) must always be enough for a cold session to resume. But disk is
  the backup, not the brain — in a live session, use everything already
  learned, and write it down so a cold session could continue.
- Full reasoning depth at every step. Requirement and plan are single-pass
  artifacts — the cap is on wording iterations, not on thinking; plan
  quality is the ceiling on `implement` quality. `review-pr` and `qa` are
  the strict gates — their persisted verdicts are what the push/PR hook
  enforces, but they catch bugs, not weak architecture.
- Never force-push, drop data, or send external messages.
- Do not create pull requests.
- Treat "implemented but not QA'd" as incomplete unless QA is impossible and the blocker is named.
- Treat happy-path-only QA as incomplete; final QA evidence must include a local target/blocker and at least one validation, error, empty, or responsive case.
- If a human decision is truly required, write `NEEDS_CONTEXT` with the exact missing input.
- Keep final handoff short: branch, files changed, QA evidence, next human action.
- Leave a clean handoff: work committed, no debug leftovers (stray logs,
  commented-out code, scratch files) in the diff, checkpoint and handoff note
  current. A session that ends dirty isn't `DONE`. When a commit lands after
  the step's checkpoint (a QA fix, a review fix, the final commit), run
  `bbs-autopilot checkpoint --refresh` so the checkpoint moves past that commit
  — otherwise the Stop-time clean-handoff audit flags it as stale.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PLANNED | BUILT | FIXED | HANDOFF
SUMMARY: <branch, QA evidence, concerns>
NEXT: human review, then /bbs:create-pr
```
