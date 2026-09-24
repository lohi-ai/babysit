---
name: autopilot
description: "Deliver one ticket end-to-end from a requirement or accepted plan to a releasable, locally committed change. Own implementation, review fixes, QA, and evidence across resumes; use foreman for multi-ticket projects."
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
## Delivery contract
For production work, own the ticket through release readiness: implementation,
code review, fixes, regression checks, and QA are agent work, not a checklist
to hand back to the human. Keep the existing plan checkpoint and explicit
`--stop-after` boundaries; after execution starts, do not stop for routine
review findings, cosmetic choices, or locally repairable test failures.
Composed skills' Taste decisions are logged and summarized in the handoff,
not separate approval requests. Genuine User Challenges still escalate.

`DONE` means every acceptance criterion has current evidence, no material
finding remains, and the committed change can proceed to the authorized
release step without more implementation or verification. It does not mean
published or deployed. `DONE_WITH_CONCERNS` is only for nonblocking residuals;
a missing required check or unmet criterion is `BLOCKED`/`NEEDS_CONTEXT`, not
releasable. Explicit prototype, recommendation-only, or audit-only workflows
keep their learning/report outcome and must not claim release readiness.

Record acceptance criteria and their check/evidence mapping in the existing
plan and handoff. Derive checks from the requirement and repository release/CI
commands, including relevant failure paths; add build, packaging, migration,
compatibility, or rollout checks only where the change needs them. A named
fallback must prove the same required behavior; a unit check cannot stand in
for a required browser journey merely because the browser is unavailable.

## Lifecycle loop

Autopilot advances **one evidence-bounded ticket through one archetype**. It
never recursively invokes itself or turns a suggested next stage into accepted
scope. A foreman may chain lifecycle tickets in a DAG; a serial run leaves the
next stage in its handoff.

The product loop is `prototyper -> builder -> grower -> maintainer`, with
`sweeper` inserted only when a measured simplification opportunity exists.
Promotion is evidence-gated:

- `prototyper -> builder`: the risky assumption was validated and the handoff
  names the production requirement; throwaway spike code is not promoted.
- `builder -> grower`: the feature is released or has an authorized experiment
  venue, and a target metric plus instrumentation are available.
- `builder|grower|maintainer -> sweeper`: current behavior is characterized and
  specific removable weight or a measured hot path was found.
- `grower -> maintainer`: a shipped experiment exposed a reliability, security,
  cost, dependency, or scale signal.
- `maintainer -> grower|builder`: analysis found a product experiment or a
  structural change; maintain does not smuggle that larger scope into its fix.

Every workflow handoff ends with `LIFECYCLE: <stage> -> <next|stop>` and
`TRIGGER: <observed evidence or missing signal>`. `stop` is a successful answer
when the next stage would require fabricated validation, unavailable production
data, or scope the ticket did not authorize. Under a foreman, those two lines
are durable routing evidence: create or unblock the next child only when the
trigger is satisfied.
## Harness and terminal portability
- Follow [the preamble](../references/preamble.md) and
  [Auto-Decision Framework](../references/auto-decision-framework.md) —
  shared refs (`../references/*.md`) are filesystem paths beside this skill's
  directory, so read them by path, not as `skill://`.
  Resolve the active harness from session metadata and exposed tools; use
  the preamble's `AGENT` / `SKILL_REF`, not the terminal brand or an installed
  CLI as evidence of which harness is running.
- Claude Code, Codex, and OMP use the same workflow and disk gates, on the
  same rule: every step runs in this session. Adapt skill loading and task
  tracking to the tools actually exposed. No Skill tool → read the resolved
  skill's `SKILL.md` in full and follow its references, preamble/telemetry,
  and output contract.
  No TaskCreate → use the harness's task/plan tool; if absent, checkpoint
  milestones on disk. Never invent a tool or claim a hook fired.
- Plain terminal, cmux, and Orca are presentation/transport choices, not
  model selectors. Use terminal-specific transport only when requested
  or already required by the invoking workflow; absence is not a gate.
- `/goal` instructions below apply only where that command is supported — and
  support is verified for the harness actually running, never inferred from
  the brand or its absence: all three supported harnesses expose it today
  (Claude Code as `/goal`; `codex features list` for Codex's `goals` feature;
  `omp config get goal.enabled` for OMP's built-in goal mode). Assume support
  unless that harness's own probe or command list says otherwise: dropping the
  handoff silently drops the design checkpoint, which is the worse error. Only
  on a harness that genuinely lacks it, after init continue the same work loop
  in this session, honoring `--stop-after` and any invoker-held approval
  checkpoint. At such a checkpoint report the artifacts and a native skill
  resume instruction, not an unsupported `/goal` command. A received `/goal`
  block on such a harness expresses the completion condition; it does not
  establish a Stop hook.
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
   Before edits, record the starting HEAD, integration base, and pre-existing
   working changes in the checkpoint/handoff. Preserve that baseline on resume
   so the full ticket remains reviewable after its milestone commits.
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
4. Seed the plan when the routed mode needs one (build mode, size above XS)
   or `--stop-after=plan` explicitly requests it, regardless of archetype:
   run `plan-draft` in this session, per the planning policy below — `plan.md` on
   disk is what a crashed loop recovers from. User-facing work routes through
   `design-ui` inside `plan-draft`; make sure that ran, so the spec and
   prototype exist *before* the `/goal` handoff — design is reviewed before
   implementation, not discovered after it. Stop here on `--stop-after=plan`.
   Size relaxes *only this step*: an XS change still gets the step-2 ticket
   and the verdict gates — there is no inline path.
5. Hand the work to the harness's work loop (below). Init never executes
   workflow steps; without `/goal`, transition directly to execution.

### Planning runs in this session
`plan-draft` — including the `design-ui` it invokes when the work adds or
reshapes a user-facing surface — executes in the current autopilot session, on
the session's model. Never dispatch it to a native child, a second session, or
an external process: a routed planner is a second model, a second usage bill,
and a second context to reconcile, selected by a run that can see neither its
cost nor its state. There is no `--planner` flag and no per-step model
selector. The planning model is a **launch** decision: start autopilot on the
model you want to plan with, and that model plans, implements, and gates.

The step owns one writer, not one child: load and execute the real `plan-draft`
skill here, in this context, with the same bounded assignment a dispatched
child would have received — the resolved `SKILL.md` path; ticket id and
absolute repo/ticket paths; requirement and checkpoint paths; permission to
write the plan, design spec, prototype artifacts, pointers, `plan-draft`
verdict, and handoff — and no implementation, git, push, or close-out
authority.

Then inspect the artifacts, confirm they belong to this attempt, and read
`bbs ticket verdict-status --skill plan-draft`. Missing or inadequate output is
not a plan: re-run it in-session once, naming the gap. If the plan is
inadequate because this session's model cannot carry it, that is a launch
problem rather than a dispatch problem — stop with `NEEDS_CONTEXT` naming the
model to restart the run on, instead of silently planning at a lower tier.
Never silently replace an accepted plan. Record the session model, artifact
paths, and result in the checkpoint/handoff.
## The work loop (`/goal`)
On a harness that supports `/goal`, `/goal <condition>` arms a Stop hook
that blocks stopping until the condition holds. Autopilot cannot arm it
itself — after init, print the handoff and stop (`developer`: the human
copy-pastes it; orchestrators put the block in the spawn prompt).
Without `/goal` support, skip this paste/stop protocol and continue execution
as specified above; `developer` alone never requires a `/goal` handoff.
If `SPAWNED` is already true, you are that process: skip the handoff and work.
An authenticated, current Orca Dispatch preamble is equivalent evidence even
when a legacy state echo says `SPAWNED=false`: honor the Task spec's effective
`AGENT_ROLE=orca`, execute in this turn, and use the injected lifecycle instead
of producing a developer `/goal` handoff.
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

/goal <ticket> is done: acceptance criteria verified, work committed locally,
current review-pr and qa gates passed, no unresolved material findings,
release readiness checked, evidence and handoff persisted —
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
### Managed project evidence

For a v2 child, read [verification.md](references/verification.md) before the
review/QA loop. Use its producer to capture gate subjects before checks and
archive results afterwards. Keep parent acceptance IDs, dependency revisions,
and write scope in the durable plan/handoff; report milestones and bounded waits
through `bbs foreman progress`. These are evidence responsibilities, not topology
or sibling orchestration. Standalone v1 skills keep their existing fallback.

### Current-session gates (`review-pr`, `qa`)
Applies to every workflow's `review-pr` and `qa` steps, without an opt-in
flag. Both gates run in this session, on the session's model: no autopilot
step is ever dispatched to a child, a second session, or an external process.
1. **Run review in the current session.** Always load and execute the real
   `review-pr --fix` skill in the current autopilot session, on the session's
   model. Never dispatch the whole review skill to a native child or external
   process, even when a smaller or different model is available. This
   execution boundary is load-bearing: `review-pr` at medium and higher effort
   uses the Task tool for its own independent finder and verifier agents, and
   making it a child creates nested delegation that some harnesses reject.
   The review skill's own Task fan-out is allowed and required. Honor its
   resolved effort; if the current session cannot provide the required
   fan-out, record `BLOCKED` rather than delegating the whole skill or silently
   lowering review effort.
2. **Run QA in this session too.** After the review pass and its fixes, load
   and execute the real `qa` skill here and run the checks yourself — no child
   dispatch, no per-harness model selection, no worker to wait on. A gate is
   exactly where you want an auditable bill and an auditable context, and
   handing verification to an agent the run cannot see gives up both. If the
   session lacks what a check needs (no browser, no runnable target), the
   skill's named fallback applies and the limitation is recorded; a missing
   capability is never a fabricated PASS and never a reason to spin up a
   child.
3. **Run one mutating gate at a time:** finish the current-session
   `review-pr --fix` pass and its fixes before starting `qa`. Never run
   either gate concurrently with implementation or with the other gate. The
   session retains ticket/checkpoint ownership and commits, and a gate run
   never invokes autopilot or recursively delegates another gate.
4. **Scope each gate from the ticket, not from memory:** the resolved skill
   path; ticket id and absolute ticket/repo paths; requirement, plan and
   relevant handoff paths; acceptance criteria, available check commands, QA
   URL/surface; permitted edits and no git/close-out authority. Read those
   artifacts rather than replaying the implementation narrative, so the gate
   judges the revision on disk, not what you intended to write. Require the
   actual skill, evidence paths, changed files, unresolved findings, and its
   status/verdict body. Require real runtime QA, including a relevant
   error/empty/validation/responsive case, or the skill's named fallback.
   Lacking required browser/runtime access, return that limitation, not a
   fabricated PASS.
   Record the ticket's starting/base revision before implementation and pass
   the complete ticket diff as the review target, including uncommitted work.
   On a trunk checkout use the recorded starting revision; on a feature branch
   use its intended integration base. Do not let a pushed feature's upstream
   or `HEAD~1` reduce a multi-commit review to an empty or last-commit diff.
5. **Accept evidence, not completion text.** Record the session model,
   reviewed revision, report, fixes, and evidence paths in the
   checkpoint/handoff. A gate owns the checkout while it runs — no editing the
   same files until its report is in. Inspect each report and unresolved
   finding; persist each accepted body with
   `BABYSIT_TICKET="$TICKET" bbs ticket set-verdict --skill <review-pr|qa> --body-file <path>` unless
   the skill already persisted it, then read `BABYSIT_TICKET="$TICKET" bbs ticket verdict-status`.
   Require evidence from this attempt and the current change, not an old
   `DONE` left on disk. A crash, missing report, or inadequate check is not a
   pass: fix and re-run the gate, or record `BLOCKED`. Never overwrite a failed
   gate with a parent-authored success without fixing and re-verifying.
   Subsequent edits invalidate affected gates; run them again before finish.
   On cold resume recover any recorded gate attempt and artifacts; do not
   start a duplicate run while one is still in flight.
6. **Require cleanup evidence from QA.** Its verdict is incomplete without a
   `CLEANUP:` line confirming that its exact browser session and every
   QA-owned auxiliary process exited, while identifying any pre-existing or
   coordinator-owned resource intentionally retained. Do not infer cleanup
   from `STATUS: DONE` or from a browser screenshot.
### Repair until the final change passes
Apply this loop to every production workflow before its final handoff:

1. Triage every review/QA finding. Fix in-scope defects autonomously; use
   `investigate` when the cause is unclear. Record evidence for refuted or
   nonblocking findings. Required behavior is never deferred just to get a
   green verdict; unrelated improvements stay outside the ticket.
2. After fixes, commit only the ticket's changes and rerun affected checks.
   Any code change invalidates both gates: QA fixes return to review, and
   review fixes return to QA. Finish with review and QA covering the same
   final tree, base, requirement, and plan. Identical-content commits alone
   do not require repeating checks when evidence proves those inputs match.
3. Persist failures, attempted fixes, and their results in the checkpoint or
   handoff. Retry with a changed hypothesis; three failed attempts at the same
   blocker end in `BLOCKED` with reproduction and exact missing capability or
   decision. Never reset that count on resume, or count a live check/wait as a
   failure. Keep completing independent in-scope work before escalating.
4. Reconcile the acceptance/evidence mapping, commit remaining ticket work,
   refresh the checkpoint, and read both verdict bodies. Run
   `BABYSIT_TICKET="$TICKET" bbs ticket readiness --action review --json`.
   Inspect `ok` and `data.ready`, not just exit 0; any false/error result is
   unfinished. For v2 runs, Markdown verdicts alone do not satisfy readiness:
   persist gate attempts and typed verification evidence through the installed
   CLI's supported contract, never downgrade the checkpoint to bypass it.
   Legacy readiness checks verdict status only, so also verify clean ticket
   state, freshness, coverage, and unresolved findings from the actual reports.
   If a sub-skill reports `DONE_WITH_CONCERNS` for blocked required coverage,
   retain its report and persist a `BLOCKED` gate body naming the unmet check;
   a permissive fallback status cannot make production work releasable.
   Never rewrite old evidence to make it appear current.

Leave one release handoff: acceptance results, final revision, review and QA
evidence, nonblocking residuals, and the exact remaining release action. The
human should not need to repeat review or QA to discover whether it is ready.
### Process cleanup gate
Autopilot owns the lifecycle of every external process its steps start. Keep an
ownership ledger in the checkpoint/handoff as processes are launched: exact
browser session and namespace, supervised server/watcher/recorder/proxy
handles, simulator UDIDs and drivers. Record the relevant pre-existing state
first; a shared dev server, human browser, or Foreman-owned leased surface is
not yours to stop.

On every terminal path — success, `NEEDS_CONTEXT`, `BLOCKED`, or a failed step
that can still run cleanup — close owned resources and wait for exit before
printing the status block. `qa`/`browse` must perform their own scoped
finalizers; autopilot reads their `CLEANUP:` evidence and then closes anything
started by other steps. Do not leave a server or browser running for review:
durable screenshots/logs are the handoff. Never use broad
`pkill`/`killall`, `agent-browser close --all`, or simulator-wide shutdown for
routine cleanup; preserve resources whose ownership is uncertain.

Verify the owned browser session is absent, supervised processes exited, and
the simulator state returned to its recorded baseline. Retry a scoped close
once. If any owned process remains, persist its exact resource/PID and cleanup
attempt, then return `BLOCKED`; neither release readiness nor a passing QA
verdict overrides this gate. A crash may prevent the finalizer, so browser
steps also set the bounded idle timeout required by `browse`, but that backstop
does not replace normal cleanup.

## Rules
- Disk state must always be enough for a cold session to resume — but disk is
  the backup, not the brain; in a live session use everything already learned.
- Full reasoning depth at every step; requirement and plan are single-pass on
  wording, not on thinking. `review-pr` and `qa` are the strict gates — their
  persisted verdicts are what the push/PR hook enforces. `review-pr` only
  reports findings, so persisting its verdict is yours: the body needs a
  first-column `STATUS:` line reflecting the result (`DONE`,
  `DONE_WITH_CONCERNS`, `BLOCKED`, or `NEEDS_CONTEXT`). A `VERDICT: PASS`
  prose line is not a status; `set-verdict` refuses a body without one.
- Git scope is exactly: `git init` on an unborn repo, and committing the
  work on the current branch. Never push, land, compose the shared surface,
  or open a PR, and never cut a branch for itself — close-out is the human's
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
  BLOCKED/NEEDS_CONTEXT naming the blocker). "Implemented but not QA'd" is
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
SUMMARY: <acceptance results, final revision, review/QA evidence, release readiness>
LIFECYCLE: <current stage> -> <next stage | stop>
TRIGGER: <observed promotion evidence or missing signal>
CLEANUP: <owned processes stopped and verified; pre-existing/shared resources retained>
NEXT: /bbs:create-pr for the verified change — or, under a foreman, whatever its
finish policy does with the ticket
```
