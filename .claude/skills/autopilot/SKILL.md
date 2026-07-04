---
name: autopilot
description: Run a checkpointed babysit workflow from a short requirement or existing ticket. Use for multi-step autonomous work that should survive context loss: plan, implement, verify, and hand off.
---

# autopilot

Autopilot is the composition layer. Keep the skill small; store durable state on disk.
Claude Code's `/goal` owns the outer loop. Autopilot owns the per-ticket state
machine and must be safe to re-enter until it prints a terminal status.

The pack runs **one workflow per archetype** (see
[references/archetypes.md](../references/archetypes.md)) — `prototyper`,
`builder`, `sweeper`, `grower`, `maintainer`. Guide every invocation to the
archetype that matches the shape of the work.

## Flow

1. Resolve invocation: inline requirement, ticket id, named archetype workflow, or resume from branch/checkpoint.
2. Ensure a ticket directory exists with `requirement.md` and `checkpoint.json`. Where the ticket's branch lives is the repo's git-flow `mode` (`trunk` / `branch` / `worktree` in `.babysit/git-flow.yaml` — see [references/git-flow.md](../references/git-flow.md)); an invocation may override it for one run with `--mode=<m>`, passed through to `bbs-ticket ensure --mode <m>`. If `ensure` prints `WORKTREE=<path>` (safe-cut divert, or `worktree` mode where every ticket gets one), cd into that worktree — every later step runs there, never in the original checkout, and QA lands the branch on the primary via `bbs-ticket merge-base`. Stop here when the invocation passed `--stop-after=requirement`.
3. Pick the archetype workflow. If the invocation names one (`prototyper`, `sweeper`, `grower`, `maintainer`), use it. Otherwise route by the shape of the work using the choosing rules in [references/archetypes.md](../references/archetypes.md) — a pure-subtraction requirement is `sweeper`, a metric-moving ask on a shipped product is `grower`, a scale/security/reliability audit or reported bug is `maintainer`, an unvalidated hunch is `prototyper`. When the shape is ambiguous or it's ordinary production work, default to `builder`, which selects its own mode (child / orchestrate / implement / build / verify). Emit `NEEDS_CONTEXT` only when there is no ticket, requirement, plan, manifest, or branch work and no archetype was named.
4. Before each step, re-read workflow file, checkpoint, ticket files, and git state.
5. Run only remaining steps. After each step, write a checkpoint and handoff note.
6. Always run QA before final handoff, and persist its verdict with
   `bbs-ticket set-verdict --skill qa` (real PASS/FIXED, or DONE_WITH_CONCERNS
   naming the blocker when QA cannot run). The PR gate reads this verdict; an
   unwritten one stalls handoff at a human checkpoint.
7. Stop with a clear status block: `DONE`, `DONE_WITH_CONCERNS`, `NEEDS_CONTEXT`, or `BLOCKED`.

## Outer loop (`/goal`)

Autopilot is one re-entrant pass; `/goal` owns retries. Autopilot cannot
invoke `/goal` itself — so when the ask is unattended end-to-end work and the
run is not already inside a loop, process the requirement first (Flow steps
1–2: ticket seeded, `requirement.md` written), then hand the user the
augmented goal line and stop:

```
/goal run /bbs:autopilot <workflow> <ticket> until STATUS: DONE —
success: qa verdict PASS/FIXED persisted, branch pushed;
stop and surface NEEDS_CONTEXT or BLOCKED verbatim, do not work around them.
```

The augmentation matters: ticket id + workflow + success criteria make every
`/goal` iteration self-sufficient — a fresh session re-derives the rest from
the checkpoint, so retries survive context death. When already looping
(`/goal` re-entry, orchestrator, `SPAWNED=true`), skip the guidance and just
run; the loop reads the terminal status block.

## Step Skills

- Planning uses `plan-draft`.
- Coding uses `implement`.
- Static landing review uses `review-pr`.
- QA uses `qa`; when full QA has no runnable target, record the fallback and use `browse` or a narrow local check.
- Debug/fix loops use `investigate`.

`create-pr` is never run inside autopilot. Autopilot stops at a QA-verified
branch handoff; the human runs `create-pr` after reviewing the result.

## Rules

- Never depend on conversation memory across steps.
- Spend rigor asymmetrically: requirement and plan are correct-enough,
  single-pass artifacts; `review-pr` and `qa` are the strict gates — their
  persisted verdicts are what the push/PR hook enforces.
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
