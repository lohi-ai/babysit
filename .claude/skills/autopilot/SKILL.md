---
name: autopilot
description: "Run a checkpointed babysit workflow from a short requirement or existing ticket. Use for multi-step autonomous work that should survive context loss: plan, implement, verify, and hand off."
---
# autopilot
A **goal proxy**: the skill owns init — durable ticket state, branch,
requirement, plan — and the harness's work loop owns execution (`/goal` where
supported), with persisted `review-pr`/`qa` verdicts as
the terminal condition. Keep state on disk; safe to re-enter until a terminal
status prints.
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
2. Not a git repo yet → `git init -b main` plus an initial commit first;
   autopilot owns every git operation (repo init, branch, commit, land,
   push) — never bounce one to the user or a step skill.
   Then ensure a ticket dir with `requirement.md` and `checkpoint.json`
   (`bbs ticket ensure`). Work rides the branch the user is on: pass
   `--mode=<branch|worktree>` through **only** when the invocation asked for
   it (`foreman` passes `--mode=worktree` per worker) — never cut or divert on
   your own initiative. See [references/git-flow.md](../references/git-flow.md).
   When seeding `requirement.md` from free text, list open decisions
   explicitly instead of papering over them
   ([references/finding-unknowns.md](../references/finding-unknowns.md)).
   If `ensure` printed `WORKTREE=<path>`, cd there — every later step runs in
   the worktree, and QA lands the branch via `bbs ticket merge-base`.
   Stop here on `--stop-after=requirement`.
3. Pick the archetype workflow ([references/archetypes.md](../references/archetypes.md)):
   named one wins; else route by the shape of the work; ambiguous or ordinary
   production work → `builder`. Several *independent* requirements in one
   invocation → don't batch here: autopilot runs one ticket end-to-end;
   parallel dispatch belongs to the `foreman` skill (one visible worker per
   ticket, each running autopilot). `NEEDS_CONTEXT` only when there is no ticket,
   requirement, plan, manifest, or branch work at all *and* no archetype was
   named — a named archetype is direction enough to proceed.
4. Seed the plan when the routed mode needs one (build mode, size above XS):
   run `plan-draft` now — `plan.md` on disk is what a crashed loop recovers
   from. User-facing work routes through `design-ui` inside `plan-draft`;
   make sure that ran, so the spec and prototype exist *before* the `/goal`
   handoff — design is reviewed before implementation, not discovered after
   it. Stop here on `--stop-after=plan`. Size relaxes *only this step*: an
   XS change still gets the step-2 ticket and the verdict gates — there is no
   inline path, and in a worktree run nothing is ever committed in the primary
   checkout (code reaches it only via `bbs ticket merge-base`/`switch`).
5. Hand the work to the harness's work loop (below). Init never executes
   workflow steps; without `/goal`, transition directly to execution.
## The work loop (`/goal`)
On a harness that supports `/goal`, `/goal <condition>` arms a Stop hook
that blocks stopping until the condition holds. Autopilot cannot arm it
itself — after init, print the handoff and stop (`developer`: the human
copy-pastes it; orchestrators put the block in the spawn prompt).
Without `/goal` support, skip this paste/stop protocol and continue execution
as specified above; `developer` alone never requires a `/goal` handoff.
Two independent flags below replace that paste with a spawned process;
`--stop-after` still wins. If `SPAWNED` is already true,
you are that process: skip the handoff and work. Start-agent is the active
harness resolved above; never default a known Codex or OMP session to Claude.
A third flag, `--verify`, changes how the loop's gates run.
- `--reviewer <agent>` (alias `--review`) — spawn that agent to review
  the plan and prototype. Omit it: no agent review. From the worktree:
  `bbs autopilot spawn-review --ticket "$TICKET" --workflow "$WF" --agent <name>`
  plus `--builder <start-agent>` only when `--auto` is also set (approve
  then spawn-goal). Print pid/log (or ORCA=) and stop.
- `--auto` — spawn `/goal` on the start agent. If `--reviewer` ran, that
  process does it on approve; otherwise
  `bbs autopilot spawn-goal --ticket "$TICKET" --workflow "$WF"` (no
  `--agent` — spawn-goal uses the start agent). Print pid/log and stop.
  Never the copy-paste handoff.
- `--verify` — grade the finished code in a fresh context. At init, record it
  on the ticket (`bbs ticket set-pointer verify true`) so every later path
  reads it back — the paste, `--auto`, a reviewer's greenlight, a cold resume;
  the loop checks `bbs ticket get-pointer verify` as well as its own args. That
  read prints `True`, not `true` — `set-pointer` coerces to a JSON bool and the
  reader renders it Python-style for the bash oracle — so compare
  case-insensitively. A case-sensitive `= "true"` silently drops the run back to
  grading its own diff, which is the one outcome the flag exists to prevent.
  `--auto` no longer depends on that read: spawn-goal appends the routing
  instruction to the goal prompt whenever the pointer is set.
  In the loop, once the implementation is committed, it **replaces** the
  in-session `review-pr` + `qa` steps rather than adding a pass after them:
  `bbs autopilot spawn-verify --ticket "$TICKET" --workflow "$WF"` (add
  `--agent <name>` only to choose a different harness — it is not a model
  selector; Sonnet/Terra are not agent names). Wait
  for it, then read the result from disk with `bbs ticket verdict-status
  --skill review-pr` and `--skill qa`; nothing the child prints is input.
  Do **not** re-run the gates in-session afterwards — `set-verdict` is
  last-writer-wins, so an added in-session pass overwrites the independent
  verdict with the biased one. No verdict at all means the verifier died:
  report `BLOCKED` naming the returned `ORCA=` tab or `LOG=` path, never
  fall back to the in-session pass this replaced.
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

/goal <ticket> is done: qa verdict PASS/FIXED persisted via bbs ticket set-verdict,
review-pr verdict persisted, branch pushed, closed out per the repo's finish
policy, handoff note written — or a NEEDS_CONTEXT / BLOCKED status block
printed verbatim.
Work it: <SKILL_REF>autopilot <workflow> <ticket>
```
The preamble is mandatory whenever `plan-draft`/`design-ui` produced those
artifacts — it is the design checkpoint, not decoration; keep it in plain
words and never assume the human knows git or babysit internals. The
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
## Step Skills
"Run `<skill>`" means load and execute that skill through the active harness's
skill mechanism (or the full-file fallback above), never approximate its job
inline. Use `SKILL_REF` in prompts: Claude Code `/bbs:<name>`, Codex
`$bbs:<name>`, OMP `/<name>`. Use the installed skill name for tool calls.
Planning: `plan-draft`. Coding: `implement`. Landing review: `review-pr`.
QA: `qa` (no runnable target → record the fallback, use `browse` or a narrow
local check). Debug: `investigate`. Closing out: `create-pr`, and only when the
repo asked for it (below).
### Automatic review / QA subagents
Applies to every workflow's `review-pr` and `qa` steps, without an opt-in
flag. `--reviewer` remains plan/prototype review. Explicit `--verify` remains
the process-isolated route above and takes precedence: run
`bbs autopilot spawn-verify`, not a native subagent, even when Sonnet/Terra
is available. That CLI's `--agent` selects a harness, not a smaller model;
automatic native model selection below does not configure that process.
Do not dispatch a second pair of native gate workers in the parent.
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
   in-session and record why. This fallback never overrides `--verify`.
   Capability routing is Mechanical; a judgment-based model escalation is
   Taste and is logged via the framework, without prompting.
2. **Dispatch one gate at a time:** `review-pr --fix`, wait and integrate its
   fixes, then `qa` on the resulting change. Never run these mutating gates
   concurrently with each other or with implementation. The parent retains
   ticket/branch/checkpoint ownership, commits, QA surface preparation and
   leases, push, and finish policy. Step workers do not invoke autopilot or
   recursively delegate these gates.
3. **Give each child a complete, bounded assignment:** skill reference and
   resolved file path; ticket id and absolute ticket/repo/worktree paths;
   requirement, plan and relevant handoff paths; exact review base/range;
   acceptance criteria, available check commands, QA URL/surface and lease
   ownership; permitted edits and no git/close-out authority. Use a fresh
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
   `bbs ticket set-verdict --skill <review-pr|qa> --body-file <path>` unless
   the skill already persisted it, then read `bbs ticket verdict-status`.
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
- Git is autopilot's job end to end. Step skills are infra-isolated — they
  edit the working tree and never branch, commit, or push; commit their
  output yourself at each milestone.
- `INVOKER=developer`: lead every stop — handoff, `NEEDS_CONTEXT`, final
  status — with one plain-language sentence saying what happened and the
  exact next command to paste; a non-technical user must be able to keep
  the build moving without knowing git.
- Never force-push, drop data, or send external messages.
- **Close out per the repo's policy**, as the last step before the handoff and
  only once qa and review-pr are both persisted DONE:
  ```bash
  eval "$(bbs autopilot git-flow)"     # → BBS_FINISH: review | land | pr
  ```
  `land` → `bbs ticket land "$TICKET"` (merge into local `$BBS_BASE_BRANCH`).
  `pr` → run `create-pr` through the harness's skill mechanism (push +
  open the PR against base). `review` (default) → the human closes it out.
  The key is the repo's standing authorization and the only thing that decides
  this — never close out on your own initiative. Never with a verdict missing
  either: `land` refuses unverified work outright, and the PR hook *asks* on a
  missing verdict, which with nobody at the pane is a stall rather than a stop.
  Report what happened on the `NEXT:` line: `LANDED: <branch> → <base>`,
  `PR: <url>`, or the human's `/bbs:create-pr`.
- Always run QA before final handoff and persist the verdict with
  `bbs ticket set-verdict --skill qa` — under `--verify` the spawned verifier
  writes it and the parent reads it back (real PASS/FIXED, or
  DONE_WITH_CONCERNS naming the blocker). "Implemented but not QA'd" is
  incomplete; happy-path-only QA is incomplete — include at least one
  validation/error/empty/responsive case.
- Leave a clean handoff: work committed, no debug leftovers in the diff,
  checkpoint current. When a commit lands after the step's checkpoint, run
  `bbs autopilot checkpoint --refresh` or the Stop-time audit flags it stale.
- Keep the final handoff short: branch, files changed, QA evidence, next
  human action. A truly human-only decision → `NEEDS_CONTEXT` naming the
  exact missing input.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PLANNED | BUILT | FIXED | HANDOFF
SUMMARY: <branch, QA evidence, concerns>
NEXT: what the finish policy left for the human — review + /bbs:create-pr
(`review`), review the landed base + push (`land`), or review the PR (`pr`)
```
