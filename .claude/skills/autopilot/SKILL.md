---
name: autopilot
description: Run a checkpointed babysit workflow from a short requirement or existing ticket. Use for multi-step autonomous work that should survive context loss: plan, implement, verify, and hand off.
---
# autopilot
A **goal proxy**: the skill owns init — durable ticket state, branch,
requirement, plan — and `/goal` owns the work loop, where the model works the
way it would for a direct ask and the persisted `review-pr`/`qa` verdicts are
the terminal condition. Keep state on disk; safe to re-enter until a terminal
status prints.
## Init (every fresh invocation)
1. Resolve the invocation: inline requirement, ticket id, named workflow, or
   resume. A checkpoint with work in flight means loop re-entry (below), not
   init.
2. Not a git repo yet → `git init -b main` plus an initial commit first;
   autopilot owns every git operation (repo init, branch, commit, land,
   push) — never bounce one to the user or a step skill.
   Then ensure a ticket dir with `requirement.md` and `checkpoint.json`
   (`bbs-ticket ensure`; git-flow `mode` from `.babysit/git-flow.yaml`, one-run
   override `--mode=<m>` — see [references/git-flow.md](../references/git-flow.md)).
   When seeding `requirement.md` from free text, list open decisions
   explicitly instead of papering over them
   ([references/finding-unknowns.md](../references/finding-unknowns.md)).
   If `ensure` printed `WORKTREE=<path>`, cd there — every later step runs in
   the worktree, and QA lands the branch via `bbs-ticket merge-base`.
   Stop here on `--stop-after=requirement`.
3. Pick the archetype workflow ([references/archetypes.md](../references/archetypes.md)):
   named one wins; else route by the shape of the work; ambiguous or ordinary
   production work → `builder`. Several *independent* tickets or
   `+`-separated requirements in one invocation → `conductor` (parallel
   batch: one background worker per ticket, QA serialized on
   `bbs-ticket qa-lease`, integration pass, aggregate handoff). `NEEDS_CONTEXT` only when there is no ticket,
   requirement, plan, manifest, or branch work at all *and* no archetype was
   named — a named archetype is direction enough to proceed.
4. Seed the plan when the routed mode needs one (build mode, size above XS):
   run `plan-draft` now — `plan.md` on disk is what a crashed loop recovers
   from. User-facing work routes through `design-ui` inside `plan-draft`;
   make sure that ran, so the spec and prototype exist *before* the `/goal`
   handoff — design is reviewed before implementation, not discovered after
   it. Stop here on `--stop-after=plan`.
5. Hand the work to `/goal` (below). Init never executes workflow steps.
## The work loop (`/goal`)
`/goal <condition>` arms a Stop hook that blocks the session from stopping
until the condition holds. Autopilot cannot arm it itself — after init, print
the handoff and stop (`developer`: the human copy-pastes it; orchestrators put
the `/goal` block at the front of the spawn prompt).
For `developer`, the handoff **is the whole final message and must be the very
last thing on screen** — nothing after it. Init may have run `plan-draft` /
`design-ui`, which print their own reports; do **not** let those be the last
thing the human sees. Condense any step-skill report into the pointer lines
below and drop the rest, so the copy-paste line is what the eye lands on. Lead
with a fixed **Review-before-you-paste** preamble carrying the pointers init
produced — one line each, omit a line only when that artifact does not exist —
then the `/goal` block, fenced on its own so it is one-click copyable:
```
Ready for <ticket>. Before you paste, review what will be built:
  plan:      <plan.md path>
  prototype: <prototype path>
Redirect the design now if it's wrong — otherwise you're one paste from done.

👉 Copy the block below and paste it into Claude Code to build it:

/goal <ticket> is done: qa verdict PASS/FIXED persisted via bbs-ticket set-verdict,
review-pr verdict persisted, branch pushed, handoff note written — or a
NEEDS_CONTEXT / BLOCKED status block printed verbatim.
Work it: /bbs:autopilot <workflow> <ticket>
```
The preamble is mandatory whenever `plan-draft`/`design-ui` produced those
artifacts — it is the design checkpoint, not decoration; keep it in plain
words and never assume the human knows git or babysit internals. The
`👉 Copy … paste it into Claude Code` line is mandatory in every `developer`
handoff — a non-technical user must never have to guess that the fenced block
is a thing they paste. Orchestrators (non-`developer`) skip the preamble and
the copy-paste line and put only the `/goal` block in the spawn prompt.
Inside the loop (re-entry, orchestrator, `SPAWNED=true`), skip the handoff
and work. Cold session: recover from checkpoint, ticket files, workflow file,
git state first; warm session: keep going with what's in context. Treat the
workflow file as mode router + gate list, not a script — pick the mode from
durable state, honor the gates, and do everything between them the way a
direct session would. Checkpoint at milestones (plan seeded, implementation
verified, findings fixed, QA verdict), not per step. Mirror those milestones
into the native task list (TaskCreate) at loop entry — rebuilt from checkpoint
+ `plan.md` on cold re-entry — and close each as its gate passes; step skills
add their finer tasks to the same list. The task list is the visible progress
view, disk stays the brain. End every pass with the status block below.
## Step Skills
"Run `<skill>`" means a real Skill-tool invocation (`bbs:<name>` when
installed as the plugin, bare `<name>` inside the babysit repo), never doing
its job inline — the invocation fires the hooks.
Planning: `plan-draft`. Coding: `implement`. Landing review: `review-pr`.
QA: `qa` (no runnable target → record the fallback, use `browse` or a narrow
local check). Debug: `investigate`. `create-pr` never runs inside autopilot —
the human runs it after reviewing the handoff.
## Rules
- Disk state must always be enough for a cold session to resume — but disk is
  the backup, not the brain; in a live session use everything already learned.
- Full reasoning depth at every step; requirement and plan are single-pass on
  wording, not on thinking. `review-pr` and `qa` are the strict gates — their
  persisted verdicts are what the push/PR hook enforces.
- Git is autopilot's job end to end. Step skills are infra-isolated — they
  edit the working tree and never branch, commit, or push; commit their
  output yourself at each milestone.
- `INVOKER=developer`: lead every stop — handoff, `NEEDS_CONTEXT`, final
  status — with one plain-language sentence saying what happened and the
  exact next command to paste; a non-technical user must be able to keep
  the build moving without knowing git.
- Never force-push, drop data, send external messages, or create PRs.
- Always run QA before final handoff and persist the verdict with
  `bbs-ticket set-verdict --skill qa` (real PASS/FIXED, or
  DONE_WITH_CONCERNS naming the blocker). "Implemented but not QA'd" is
  incomplete; happy-path-only QA is incomplete — include at least one
  validation/error/empty/responsive case.
- Leave a clean handoff: work committed, no debug leftovers in the diff,
  checkpoint current. When a commit lands after the step's checkpoint, run
  `bbs-autopilot checkpoint --refresh` or the Stop-time audit flags it stale.
- Keep the final handoff short: branch, files changed, QA evidence, next
  human action. A truly human-only decision → `NEEDS_CONTEXT` naming the
  exact missing input.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PLANNED | BUILT | FIXED | HANDOFF
SUMMARY: <branch, QA evidence, concerns>
NEXT: human review, then /bbs:create-pr
```
