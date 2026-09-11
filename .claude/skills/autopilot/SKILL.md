---
name: autopilot
description: "Run a checkpointed babysit workflow from a short requirement or existing ticket. Use for multi-step work that should survive context loss: plan, implement, verify, and hand off."
---
# autopilot
A **goal proxy**: the skill owns init — durable ticket state, requirement,
plan — and the harness's work loop owns execution (`/goal` where supported),
with persisted `review-pr`/`qa` verdicts as the terminal condition. Keep
state on disk; safe to re-enter until a terminal status prints.
**Autopilot is an assistant, not an orchestrator.** It works on the checkout
it was started in and never manages its own git topology: no branch cutting,
no worktrees, no push, no land, no PR. It commits its own work locally — the
verdict gates and crash-resume need durable commits — and the human closes
out. When a run needs isolation or autonomy (parallel tickets, worktrees,
landing, PRs), that is `foreman`'s job: foreman decomposes the project, creates
the worktrees, starts one autopilot assistant per ticket, coordinates QA, and
owns the finish policy. There is no parent/orchestrate exception inside
autopilot.
## Harness and terminal portability
- Follow [the preamble](../references/preamble.md) and
  [Auto-Decision Framework](../references/auto-decision-framework.md).
  Resolve the active harness from session metadata and exposed tools; use
  the preamble's `AGENT` / `SKILL_REF`, not the terminal brand or an installed
  CLI as evidence of which harness is running.
- Claude Code, Codex, and OMP use the same workflow and disk gates. Adapt
  skill loading, task tracking, subagent dispatch, and waiting to the tools
  actually exposed. No Skill tool → read the resolved skill's `SKILL.md` in
  full and follow its references, preamble/telemetry, and output contract.
  No TaskCreate → use the harness's task/plan tool; if absent, checkpoint
  milestones on disk. Never invent a tool or claim a hook fired.
- Plain terminal, cmux, and Orca are presentation/transport choices, not
  model selectors. Native subagents need no new pane, terminal CLI, or
  orchestration bus. Use terminal-specific transport only when requested
  or already required by the invoking workflow; absence is not a gate.
- `/goal` instructions below apply only where that command is supported.
  Otherwise, after init continue the same work loop in this session, honoring
  `--stop-after` and any invoker-held approval checkpoint. At such a checkpoint
  report the artifacts and a native skill resume instruction, not an
  unsupported `/goal` command. A received `/goal` block on such a harness
  expresses the completion condition; it does not establish a Stop hook.
## Init (every fresh invocation)
1. Resolve the invocation: inline requirement, ticket id, named workflow, or
   resume. A checkpoint with work in flight means loop re-entry (below), not
   init.
2. Not a git repo yet → `git init -b main` plus an initial commit first.
   Then ensure a ticket dir with `requirement.md` and `checkpoint.json`:
   ```bash
   ENSURE_OUT=$(bbs ticket ensure --no-branch)
   TICKET=$(printf '%s\n' "$ENSURE_OUT" | sed -n 's/^TICKET=//p')
   ```
   Parse, never eval — `TICKET_HOME` is an unquoted path. Then carry the id
   explicitly: an `export` here does not survive into the next tool call on
   harnesses with per-call shells, so every later `bbs ticket` /
   `bbs autopilot` invocation runs as `BABYSIT_TICKET="$TICKET" <cmd>` (or
   exports it inside the same shell invocation). `--no-branch` is
   load-bearing too: a repo
   with a handwritten `mode:` key would otherwise divert the cut — autopilot
   always works on the checkout it was started in, never cuts a branch for
   itself, never diverts to a worktree. If the current checkout is already a ticket
   worktree (a foreman put you here), that is fine: work in place; the `qa`
   skill owns the shared-surface protocol. When seeding `requirement.md`
   from free text, list open decisions explicitly instead of papering over
   them ([references/finding-unknowns.md](../references/finding-unknowns.md)).
   Stop here on `--stop-after=requirement`.
3. A parent carrying `manifest.md` is a multi-ticket project: stop init and
   hand it to `foreman`; never dispatch or merge its children here. Otherwise
   pick the archetype workflow ([references/archetypes.md](../references/archetypes.md)):
   named one wins; else route by the shape of the work; ambiguous or ordinary
   production work → `builder`. Several *independent* requirements in one
   invocation → don't batch here: autopilot runs one ticket end-to-end;
   parallel dispatch belongs to the `foreman` skill (one visible worker per
   ticket, each running autopilot). `NEEDS_CONTEXT` only when there is no ticket,
   requirement, plan, manifest, or branch work at all *and* no archetype was
   named — a named archetype is direction enough to proceed.
4. Seed the plan when the routed mode needs one (build mode, size above XS):
   run `plan-draft` through the planner subagent policy below — `plan.md` on
   disk is what a crashed loop recovers from. User-facing work routes through
   `design-ui` inside `plan-draft`; make sure that ran, so the spec and
   prototype exist *before* the `/goal` handoff — design is reviewed before
   implementation, not discovered after it. Stop here on `--stop-after=plan`.
   Size relaxes *only this step*: an XS change still gets the step-2 ticket
   and the verdict gates — there is no inline path.
5. Hand the work to the harness's work loop (below). Init never executes
   workflow steps; without `/goal`, transition directly to execution.

### Planner subagent (`--planner`, `--planner-effort`)
Planning is a fresh-context job when native delegation is available. These
flags select a native model/profile and its reasoning effort.
- `--planner <model>` — use that exact advertised model or profile for
  `plan-draft` and its nested `design-ui` prototype work.
- `--planner-effort <effort>` — use that exact advertised reasoning effort.
- A missing flag is automatic: an explicit model with no effort gets the
  auto-selected effort, and an explicit effort with no model gets the
  auto-selected model. Explicit values always win. If the native tool cannot
  honor an explicit value, report `BLOCKED` naming the unsupported value; do
  not silently substitute it.

For automatic selection, inspect the native subagent tool's advertised models,
profiles, and effort selector first; never invent an ID. Classify from the
requirement and repo evidence: **simple** means an obvious local docs/config or
tiny isolated change with no new contract or state; **critical/hard** means
security, auth, money, irreversible/live-data migration, distributed
concurrency, or a cross-system architecture decision; everything else is
**normal**. Weak evidence stays normal. Apply this routing:

| Harness | Simple | Normal (default) | Critical / hard |
|---------|--------|------------------|-----------------|
| Codex | `gpt-5.6-terra`, `high` | `gpt-5.6-sol`, `high` | `gpt-6-astra`, `high` |
| Claude Code | `opus`, `high` | `opus`, `high` | advertised Fable 5.1 model/profile, `high` |
| OMP | `default` profile, `high` | `slow` profile, `high` | `slow` profile, `high` |

For an automatic choice whose preferred entry is not advertised, use the
nearest capable advertised fallback in that harness and record the limitation;
if no native model/profile selector is exposed, use an inherited/default child.
If native delegation itself is unavailable or forbidden, run the real
`plan-draft` skill in-session and record why. Automatic task-tier/model routing
is a Taste decision: log the tier, selected model/profile, effort (or
unsupported), and evidence through the Auto-Decision Framework.

**OMP launch rule:** OMP's bundled general-purpose `task` subagent is bound to
its `@task` model role and its task tool does not expose a per-child model
selector. Do not use that child for a routed `default` or `slow` planner. Start
one fresh OMP process in the target repo instead, activating the selected role
with `omp --model @default|@slow --thinking <effort> --auto-approve -p
<assignment>` (use the exact explicit model in place of `@<role>` when the user
named one). Add an isolated `--session-dir` and a reasonable `--max-time` bound
so the parent can verify the effective model/effort and cannot wait forever.
The assignment invokes `/plan-draft` with the already-initialized ticket
context; never invoke `/autopilot` in the child, which would recurse and choose
another planner. `--slow <model>` configures the role; it does not activate it,
so it is not a substitute for `--model @slow`. Preserve OMP's normal
configuration root; do not use `PI_CODING_AGENT_DIR`, which replaces that root.
Treat this process as the one planner child, wait for it, and validate its disk
artifacts exactly like a native-tool child. A terminal final response plus the
expected `DONE` verdict and artifacts is completion; if that exact child stays
resident afterward, terminate only its verified PID and record the runtime
concern rather than launching a replacement planner. If the launcher rejects an
automatic role, apply the advertised fallback rule above; if it rejects an
explicit value, report `BLOCKED`.

Before dispatch, persist the resolved values so cold resume cannot choose a
different planner:
```bash
BABYSIT_TICKET="$TICKET" bbs ticket set-pointer planner_model "<resolved-model-or-profile>"
BABYSIT_TICKET="$TICKET" bbs ticket set-pointer planner_effort "<resolved-effort-or-unsupported>"
```
On resume, those pointers win unless the new invocation explicitly supplies a
planner flag; an explicit change overwrites the affected pointer and re-plans
only when the plan has not already passed its checkpoint. Never silently
replace an accepted plan because a later invocation names a different planner.

Dispatch exactly one planner and wait; the parent must not edit the shared
checkout meanwhile. Give the child a complete bounded assignment: the real
`plan-draft` skill reference and resolved `SKILL.md` path; ticket id and
absolute repo/worktree/ticket paths; requirement and checkpoint paths; the
selected model/profile and effort; permission to write the plan, design,
prototype-only route/artifacts, pointers, handoff, and `plan-draft` verdict;
and no implementation, git, push, or close-out authority. The child loads and
executes `plan-draft`, which invokes `design-ui` when required; do not split
those two writers across parallel children. Require its status body and exact
artifact paths.

After it returns, inspect the artifacts, confirm the result belongs to this
attempt, and read `bbs ticket verdict-status --skill plan-draft`. Missing or
inadequate output is not a plan: retry once with the next stronger advertised
automatic option, or report `BLOCKED` for an explicit planner. Record the child
handle, selected model/profile, effort, artifact paths, and result in the
checkpoint/handoff.
## The work loop (`/goal`)
On a harness that supports `/goal`, `/goal <condition>` arms a Stop hook
that blocks stopping until the condition holds. Autopilot cannot arm it
itself — after init, print the handoff and stop (`developer`: the human
copy-pastes it; orchestrators put the block in the spawn prompt).
Without `/goal` support, skip this paste/stop protocol and continue execution
as specified above; `developer` alone never requires a `/goal` handoff.
If `SPAWNED` is already true, you are that process: skip the handoff and work.
For a `developer` handoff **on a harness supporting `/goal`**, the handoff
**is the whole final message and must be the very last thing on screen** —
nothing after it. The template and mandatory copy-paste rules below apply
only to that supported handoff, not to direct execution or a native resume
instruction on other harnesses. Init may have run `plan-draft` /
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

👉 Copy the block below and paste it into <the current agent> to build it:

/goal <ticket> is done: work committed locally, qa verdict PASS/FIXED persisted
via bbs ticket set-verdict, review-pr verdict persisted, handoff note written —
or a NEEDS_CONTEXT / BLOCKED status block printed verbatim.
Work it: <SKILL_REF>autopilot <workflow> <ticket>
```
The preamble is mandatory whenever `plan-draft`/`design-ui` produced those
artifacts — it is the design checkpoint, not decoration; keep it in plain
words and never assume the human knows git or babysit internals.
The preamble prints the current agent and `SKILL_REF`; substitute both values
in this template and never print the angle-bracket placeholders. The
`👉 Copy … paste it into <agent>` line is mandatory in every supported
`developer` `/goal` handoff — a non-technical user must never guess that the block
is a thing they paste. Orchestrators (non-`developer`) skip the preamble and
the copy-paste line and put only the `/goal` block in the spawn prompt.
Inside the loop (re-entry, orchestrator, `SPAWNED=true`), skip the handoff
and work. Cold session: recover from checkpoint, ticket files, workflow file,
git state first; warm session: keep going with what's in context. Treat the
workflow file as mode router + gate list, not a script — pick the mode from
durable state, honor the gates, and do everything between them the way a
direct session would. Checkpoint at milestones (plan seeded, implementation
verified, findings fixed, QA verdict), not per step. Mirror those milestones
into the harness's native task list at loop entry — rebuilt from checkpoint
+ `plan.md` on cold re-entry — and close each as its gate passes; step skills
add their finer tasks to the same list. The task list is the visible progress
view, disk stays the brain. End every pass with the status block below.

When available, read `bbs autopilot snapshot --json` once at loop entry and
accept it only when its envelope is `schema_version: 2` and `ok: true`. Use its
identity, policy, artifact digests, gates, and obligations as the common
read-only packet; read every listed artifact that is required for the chosen
action. An unavailable/unsupported packet falls back to the legacy reads.
Never treat an `ok: true` snapshot or a projected gate as readiness or finish
permission: AP-03's readiness evaluator is the authorization boundary.
## Step Skills
"Run `<skill>`" means load and execute that skill through the active harness's
skill mechanism (or the full-file fallback above), never approximate its job
inline. Use `SKILL_REF` in prompts: Claude Code `/bbs:<name>`, Codex
`$bbs:<name>`, OMP `/<name>`. Use the installed skill name for tool calls.
Planning: `plan-draft`. Coding: `implement`. Landing review: `review-pr`.
QA: `qa` (no runnable target → record the fallback, use `browse` or a narrow
local check). Debug: `investigate`. Closing out is the human's `create-pr`
(or foreman's finish policy) — never autopilot's.
### Automatic review / QA subagents
Applies to every workflow's `review-pr` and `qa` steps, without an opt-in
flag.
1. **Select from actual capabilities, automatically.** Inspect the native
   subagent tool schema and advertised models/agent profiles before dispatch:
   - **Claude Code:** prefer `sonnet` through the subagent tool's model
     selector when supported, with an agent allowed to read, edit, and run
     the required checks.
   - **Codex:** prefer `terra` only when the session advertises that model
     or an agent profile explicitly mapped to it. Use the exposed model
     selector or that configured profile; do not assume `spawn_agent`
     accepts a `model` argument or that an agent type is a model name.
   - **OMP / other harnesses:** use an advertised smaller coding-capable
     model/profile with the required tools; never guess a model ID or assume
     a read-only scout can perform `review-pr --fix` or QA fixes.
   Honor an explicit user model choice. Otherwise choose the advertised
   smaller capable option; if model selection is unavailable, use a native
   child with its inherited/default model and record that limitation.
   If native delegation is unavailable or forbidden, execute the real skill
   in-session and record why.
   Capability routing is Mechanical; a judgment-based model escalation is
   Taste and is logged via the framework, without prompting.
2. **Dispatch one gate at a time:** `review-pr --fix`, wait and integrate its
   fixes, then `qa` on the resulting change. Never run these mutating gates
   concurrently with each other or with implementation. The parent retains
   ticket/checkpoint ownership and commits. Step workers do not invoke
   autopilot or recursively delegate these gates.
3. **Give each child a complete, bounded assignment:** skill reference and
   resolved file path; ticket id and absolute ticket/repo paths;
   requirement, plan and relevant handoff paths; exact review base/range;
   acceptance criteria, available check commands, QA URL/surface;
   permitted edits and no git/close-out authority. Use a fresh
   task context, not a copy of the implementation conversation. Require the
   actual skill, evidence paths, changed files, unresolved findings, and its
   status/verdict body. Require real runtime QA, including a relevant
   error/empty/validation/responsive case, or the skill's named fallback.
   A child lacking required browser/runtime access returns that limitation,
   not a fabricated PASS; route the gate to a capable child or the parent.
4. **Accept evidence, not completion text.** Record the child handle,
   selected model (or unknown/inherited), gate, reviewed revision and
   evidence paths in the existing checkpoint/handoff. Wait with the native
   tool; parent must not edit the shared checkout meanwhile. If the harness
   isolates child edits, integrate them before the next gate. Inspect the
   report and unresolved findings; persist each accepted body with
   `BABYSIT_TICKET="$TICKET" bbs ticket set-verdict --skill <review-pr|qa> --body-file <path>` unless
   the skill already persisted it, then read `BABYSIT_TICKET="$TICKET" bbs ticket verdict-status`.
   Require evidence from this attempt and the current change, not an old
   DONE left on disk. A crash, missing report, or inadequate check is not a
   pass: retry on a capable model or record BLOCKED. Never overwrite a
   child's failure with a parent-authored success without fixing and
   re-verifying. Subsequent edits invalidate affected gates; run them again
   before finish. On cold resume recover the recorded attempt and artifacts;
   do not start a duplicate writer while its child is still running.
## Rules
- Disk state must always be enough for a cold session to resume — but disk is
  the backup, not the brain; in a live session use everything already learned.
- Full reasoning depth at every step; requirement and plan are single-pass on
  wording, not on thinking. `review-pr` and `qa` are the strict gates — their
  persisted verdicts are what the push/PR hook enforces. `review-pr` only
  reports findings, so persisting its verdict is yours: the body needs a
  first-column `STATUS: DONE` line (a `VERDICT: PASS` prose line is not a
  status; `set-verdict` refuses a body without one).
- Git scope is exactly: `git init` on an unborn repo, and committing the
  work on the current branch. Never push, land, merge-base, or open a PR,
  and never cut a branch for itself — close-out is the human's
  (`create-pr`) or the dispatching foreman's (its finish policy). Step skills
  are infra-isolated — they edit the working tree and never commit; commit
  their output yourself at each milestone.
- `INVOKER=developer`: lead every stop — handoff, `NEEDS_CONTEXT`, final
  status — with one plain-language sentence saying what happened and the
  exact next command to paste; a non-technical user must be able to keep
  the build moving without knowing git.
- Never force-push, drop data, or send external messages.
- Always run QA before final handoff and persist the verdict with
  `BABYSIT_TICKET="$TICKET" bbs ticket set-verdict --skill qa` (real PASS/FIXED, or
  DONE_WITH_CONCERNS naming the blocker). "Implemented but not QA'd" is
  incomplete; happy-path-only QA is incomplete — include at least one
  validation/error/empty/responsive case.
- Leave a clean handoff: work committed, no debug leftovers in the diff,
  checkpoint current. When a commit lands after the step's checkpoint, run
  `BABYSIT_TICKET="$TICKET" bbs autopilot checkpoint --refresh` or the Stop-time audit flags it stale.
- Keep the final handoff short: branch, files changed, QA evidence, next
  human action. A truly human-only decision → `NEEDS_CONTEXT` naming the
  exact missing input.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PLANNED | BUILT | FIXED | HANDOFF
SUMMARY: <branch, QA evidence, concerns>
NEXT: human review, then /bbs:create-pr — or, under a foreman, whatever its
finish policy does with the ticket
```
