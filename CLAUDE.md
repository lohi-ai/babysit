# CLAUDE.md

## 1. Think Before Coding

**Don't hide confusion. Surface tradeoffs.**

Before implementing:

- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:

- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:

- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:

- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:

```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

## babysit

Babysit is a Claude Code skill pack for **autonomous** workflows — scheduled runs, background jobs, CI loops, anything where no human is at the keyboard to approve or course-correct.

It is the product-building *team*: skills and workflows are organized around the
**five archetypes** of how that team works — Prototyper, Builder, Sweeper,
Grower, Maintainer. A run acts as whichever teammate the task needs, picked by
the shape of the work rather than a job title. The mapping (which skills and
workflows realize each archetype, and how to choose) lives in
[.claude/skills/references/archetypes.md](.claude/skills/references/archetypes.md).
When adding a skill, place it under the archetype whose mandate it serves.

## Working principles

The name is the point: *babysit is what you do when you don't need a babysitter*. Skills here should prefer decisions Claude can make and verify alone over decisions that need a human in the loop.

When writing or adapting a babysit skill:
- **Prefer decisions over prompts** — default to deciding and logging the decision. Reach for `AskUserQuestion` only when the alternative is a wrong assumption that would land wrong code (see below).
- **Strong self-verification** — run the type-checker, tests, or browser check before declaring done; don't rely on a human to notice regressions.
- **Bounded blast radius** — never force-push, never drop DBs, never send messages without explicit durable authorization (see top-level instructions on reversibility).
- **Observable, not steerable** — emit telemetry (see below) so the human can audit *after the fact* instead of steering in the moment.
- **Fail loud, fail local** — on unresolvable ambiguity, stop with a clear `BLOCKED`/`NEEDS_CONTEXT` status rather than guessing silently.

### Loose skills, strict workflows

Skills and workflows enforce different layers of rules:

- **Workflows enforce strict prerequisites** — they're the orchestration layer.
  When a workflow routes into a step that dispatches a sub-skill, the workflow
  hard-gates on inputs the sub-skill needs (`pointers.requirement`, `pointers.plan`,
  `origin.plan` for sub-tickets, ticket-on-feat-branch, commits-ahead, branch-pushed).
  See `.claude/skills/autopilot/workflows/*.md` and the workflow frontmatter
  `needs-state:` declarations for the canonical pattern.
- **Skills relax those rules** — a skill can be invoked many ways: composed by
  a workflow (where the gate already ran upstream), by `/bbs:<skill>` from a
  developer terminal where the user has pasted a requirement/plan into context
  instead of writing it to disk, by an orchestrator that supplies docs via
  conversation, or as part of a partial recovery after a crash. A skill that
  refuses to run because `pointers.requirement` is empty cripples every
  non-workflow invocation. Instead, prefer **graceful fallback**: read the doc
  from `pointers.<key>` when present, fall back to conversation context when
  not, and only emit `NEEDS_CONTEXT` when neither source has the input.

The four most common over-strict patterns to avoid in skills (loosen if you
find them — workflows already cover the strict path):
1. **Doc-existence gates** — `pointers.{requirement,plan,design}` missing → don't
   exit 1; read from conversation context, fall back to disk-glob, only
   `NEEDS_CONTEXT` if both are empty AND the skill genuinely cannot proceed.
2. **Ticket-id required** — derive from branch when present, work without it
   when not (skip ticket-state writes silently, log a one-line note).
3. **Branch shape** — `feat/<ticket>_*` should be a hint, not a precondition.
   A skill running on `main` with conversation-supplied context is a valid
   shape.
4. **Git-dirty refusal** — pushed/dirty/clean is the workflow's concern (it
   wraps release gates with the appropriate policy). A skill that does
   read-only or contained work shouldn't refuse a dirty tree.

Rule of thumb: when adding a gate to a skill, ask "does the workflow that
calls this skill already enforce this?" If yes, the skill check is dead
weight that breaks legitimate non-workflow invocations. Push the gate up to
the workflow and let the skill assume it ran.

### Decisions run through the Auto-Decision Framework

The *core* of babysit is `.claude/skills/references/auto-decision-framework.md`.
It classifies every decision as **Mechanical**, **Taste**, or **User
Challenge**, gives 6 principles for auto-answering, and routes User Challenges
to the right surface for the invocation mode. Every skill in the pack references
it — the Mechanical/Taste classifier and the "replaces judgment, not analysis"
rule are the core of how babysit decides unattended.

When adding or editing a skill, don't write new ad-hoc prompts. Classify the
decision point, let the framework route it, and log every Taste decision to
`~/.babysit/analytics/decisions.jsonl`.

### One mode, two escalation channels — design skills accordingly

Every skill always runs autonomously: decisions go through the Auto-Decision
Framework, and skills *never* prompt mid-flight for taste, style, or cosmetic
choices. The only thing that varies between runs is the **delivery channel**
for `NEEDS_CONTEXT`, picked by `AGENT_ROLE` (or legacy `GT_ROLE`):

- `AGENT_ROLE=developer` (default, no env var) — render `NEEDS_CONTEXT` as a
  single `AskUserQuestion`. The human is at the Claude Code terminal.
- `AGENT_ROLE=mayor|general|scanner|...` — emit the structured `NEEDS_CONTEXT`
  block. An orchestrator (babysit-office, gastown, cron) relays via its own
  channel. `AskUserQuestion` here would hang the run.

When you write a skill, don't branch the *behavior* by mode — branch only the
*delivery surface* for User Challenges and the final taste-decision gate. The
analysis, artifacts, and decision logic are identical either way.

**The operational rules — when to escalate, the `NEEDS_CONTEXT` format the
orchestrator expects, the `INVOKER` values — live in
[.claude/skills/references/preamble.md § One mode, two escalation channels](.claude/skills/references/preamble.md#one-mode-two-escalation-channels).**
That file is loaded at skill-invocation time regardless of which repo the skill
runs in. *This* `CLAUDE.md` is only in context when someone is working inside
the babysit repo itself; non-`developer` invocations from babysit-office or
gastown won't see it. Keep runtime rules in preamble.md; keep authoring
guidance here.

## `autopilot` is the entry point for multi-step work

`autopilot` is the skill that makes the rest of babysit usable unattended. Core units plan, implement, review, and QA; domain skills stay directly invocable. `autopilot` is a **goal proxy**: init owns durable state (ticket, branch, requirement, plan), then Claude Code's `/goal` owns the work loop — inside it the model works free-form with full context, the workflow file is a mode router + gate list rather than a script, and the persisted `review-pr`/`qa` verdicts are the terminal condition.

When to reach for it (and how to think about it when editing it):

- **The composition problem it solves is context, not control flow.** Chaining `plan-draft` → `implement` ad-hoc can lose the plan and handoff state if the session crashes or gets compacted. `autopilot` runs the workflow in one session, and checkpoints all state to disk (`checkpoint.json`, `plan.md`, `requirement.md`, `handoffs/`) after each step. A fresh session after a crash re-reads the workflow + checkpoint and picks up at the next step — no conversation memory required.
- **Prefer `/bbs:autopilot <workflow> <ticket>` over hand-chaining skills** whenever the work is >1 heavy skill, or when the user's ask could be "build this whole thing." Inline free-text (`/bbs:autopilot <one-line requirement>`) routes to the `builder` workflow, which creates the ticket, seeds `requirement.md`, and picks its own mode (child / orchestrate / implement / build / verify) from ticket state.
- **Workflows are markdown, not code.** `.claude/skills/autopilot/workflows/*.md` (builtins) and `.claude/workflows/*.md` (per-project). Steps are `## <step-id>` headings with optional `> needs:` / `> produces:` directives. Workflow frontmatter declares `needs-state:` prerequisites so autopilot's Assign phase can route deterministically. Adding a new workflow is a file, not a refactor. See [authoring.md](.claude/skills/autopilot/references/authoring.md).
- **When editing autopilot or its workflows**, disk state must always be *sufficient*: on cold start or resume, re-read the workflow file and checkpoint, re-derive `$TICKET` from the branch. That's the crash-survival contract — anything that makes resume *require* in-context state is a regression. But sufficiency is not amnesia: within a continuous session the model keeps using what it already learned; a rule that forces a healthy session to behave like a cold one caps run quality at the worst case.

### Human checkpoints shape where workflows stop

Workflows are split along the four points where a human actually adds value:

1. **Requirement accepted** → `requirement.md` on the ticket. Autopilot drafts it in Flow steps 1–2 and stops at `--stop-after=requirement` if requested; requirement drafting is part of autopilot, not a separate skill.
2. **Plan accepted** → `plan.md` on the ticket. Owner: autopilot init via `plan-draft` (builder build mode covers the case init didn't seed it); stops at `--stop-after=plan` if requested.
3. **QA ready** → branch implemented, reviewed, checked with `qa` or a named fallback. Owner: `builder` (implement / build / child / verify modes) — the default end-to-end stop.
4. **PR ready** → human reviews the QA handoff and invokes `create-pr`. Autopilot does not create PRs.

When adding or editing a workflow, be explicit about which checkpoint it stops at, and make sure the final step's handoff comment ends with a `Next:` line pointing at the human's next action (read + accept plan, review QA evidence, run `create-pr`, etc.). A workflow that crosses a checkpoint without stopping should say so in its frontmatter description (see `builder.md`).

Pick the workflow whose stop-point matches the checkpoint you want to own. `/bbs:autopilot <workflow> <ticket>` runs the whole thing end-to-end in one session; if it crashes mid-workflow, re-dispatching resumes from the last checkpoint on disk.

## Skill inventory by `INVOKER` compatibility

When composing an autonomy workflow (scheduled run, orchestrator pipeline, CI
loop), pick only from the **`INVOKER`-agnostic** column. The
**`developer`-only** skills hang when run without a human — use them from a
terminal or wrap them in an orchestrator that can relay `NEEDS_CONTEXT`.

| Compatibility | Skills |
|---------------|--------|
| **`INVOKER`-agnostic** (safe to chain unattended) | `analytics-review`, `autopilot`, `browse`, `conversion-fix`, `copy-rewrite`, `create-pr`, `design-ui`, `growth-experiment`, `implement`, `investigate`, `maintain`, `plan-draft`, `prototype`, `qa`, `recon`, `review-pr`, `social-content`, `sweep`, `triage` |
| **`developer`-only** (require a human at the keyboard) | `office-hours`, `setup-project` |

Rules of thumb when wiring a workflow:
- Treat "`developer`-only" as a hard stop for non-`developer` orchestrators.
  If you need that capability unattended, add a code path that emits
  `NEEDS_CONTEXT` instead of calling `AskUserQuestion`.
- Before adding a new skill to the `INVOKER`-agnostic column, confirm it
  follows the single-mode pattern in
  [preamble.md](.claude/skills/references/preamble.md) and routes decisions
  through the Auto-Decision Framework.

## Layout

```
babysit/
├── bin/
│   ├── bbs-env        # env resolve / is-set / list-prefix / prompt (auto-loads .env.base)
│   ├── bbs-db         # postgres snapshot / restore / list (per-rig rotation)
│   ├── bbs-config     # read/write ~/.babysit/config.yaml
│   ├── bbs-slug       # derive slug / ticket / branch from git remote + branch
│   ├── bbs-autopilot  # checkpoint + timeline runner behind the autopilot skill
│   ├── bbs-ticket     # ticket identity (the big bin)
│   ├── bbs-design     # query DESIGN.md tokens / suggest products / list components / ux-check
│   ├── bbs-update-check, bbs-upgrade, bbs-telemetry-log, bbs-codex-competitive
│   ├── hooks/         # plugin hook executables (pre-tool-gate, verify-skill-output, clean-handoff-check)
│   └── setup-skills   # Symlinks bin/bbs-* into ~/.claude/
├── hooks/hooks.json   # plugin hook wiring (artifact-gated approval — see docs/artifact-gated-approval.md)
├── tests/             # shell + python suites for bins, workflows, and autopilot integration
├── docs/              # roadmap, identity, operations, artifact-gated-approval
├── web/               # snapshot dashboard (Vite/React) over ~/.babysit state
└── .claude/
    └── skills/        # see "Skill inventory by invocation mode" above
```

Skills are namespaced with the `bbs:` prefix when installed globally (e.g. `bbs:implement`).

## Ticket identity

A ticket is identified by three signals, in priority order:

1. **`BABYSIT_TICKET`** env var — set by autopilot's §0.X Workspace phase or
   by `bbs-ticket session attach <id>`. Always wins.
2. **`tickets/<ticket>/manifest.yaml`** — durable identity anchor; one row
   per repo with `name / branch / canonical / worktree / base / pushed`.
3. **Branch regex** `^(feat|fix|chore|bug|refactor|hotfix)/<ticket>_<slug>` —
   the legacy fallback. Pre-`manifest.yaml` ticket dirs still resolve here.

`bbs-ticket resolve` walks the ladder; conflicts (e.g. `BABYSIT_TICKET=A` on
a `feat/B_…` branch) exit 2 with a 3-line BLOCK. There is exactly one
identity codepath. Schema lives in [docs/identity.md](docs/identity.md).

Sessions persist at `~/.babysit/sessions/<id>.yaml` via the preamble
session-writer hook on every skill invocation. `bbs-ticket session list /
attach / end` manage them; `attach` echoes `export BABYSIT_TICKET=…` so a
fresh shell can recover identity after a crash.

## Install

```
./bin/setup-skills           # symlinks into ~/.claude/skills/bbs:*
./bin/setup-skills --uninstall
```

## Releasing — version bumps

When bumping the version (any change to `VERSION`), **always update `.claude-plugin/marketplace.json` in the same commit**. The plugin loader uses that file to detect upgrades — a stale version there means `/plugin marketplace update babysit` reports nothing to bump and users stay on the old skills.

Three places must stay in sync:

| File | Field |
|------|-------|
| `VERSION` | bare version string, e.g. `1.4.2` |
| `.claude-plugin/marketplace.json` | `metadata.version` |
| `.claude-plugin/marketplace.json` | `plugins[0].version` |

Quick check: `grep -r "version" .claude-plugin/ VERSION` — all three should show the same value.

## Telemetry

Logged events land under `~/.babysit/analytics/skill-usage.jsonl`. Because babysit runs unattended, telemetry is the *primary* feedback channel — treat it as load-bearing, not decoration.
