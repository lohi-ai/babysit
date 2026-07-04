# Artifact-gated approval via plugin hooks

**Status:** BUILT (2026-05-31). Both hooks live in `hooks/hooks.json` +
`bin/hooks/{pre-tool-gate,verify-skill-output}`, validated against all decision
paths. Ships when the plugin is enabled (`/reload-plugins` or reinstall to pick
them up). The gate policy below is the as-built default — adjust the verdict
requirements / deny-vs-ask per stage in `pre-tool-gate` if needed.

**Update (2026-05-31, mid-tier):** the three CK artifacts babysit produced
in-flight but never persisted — `verification`, `risk-gate`, `adversarial` —
are now typed, validated artifacts (`bbs-ticket set-evidence`/`evidence-status`)
written by `implement` / `review-pr` and audited by Hook B. The
hard-stage **gate is unchanged** (still `verdicts/` only). See
[§ Typed evidence](#typed-evidence-the-mid-tier-gap-close-2026-05-31).

**As-built gate policy** (safety-first — never bricks legitimate ad-hoc work):

| Situation | Decision |
|-----------|----------|
| Command isn't push/PR/merge | `defer` (hook never fires in prod — `if`-matched) |
| Hard stage, no ticket resolves (ad-hoc shell) | `defer` (fail-open) |
| Hard stage, required verdict missing | `ask` (human checkpoint) |
| Hard stage, verdict `BLOCKED`/`NEEDS_CONTEXT` | **`deny`** + reason naming the skill to run |
| Hard stage, verdict `DONE`/`DONE_WITH_CONCERNS` | `defer` (no objection) |

`push` gates on `review-pr` (legacy `review` fallback); `pr` and `merge` gate
on **both** `review-pr` AND `qa`. PR creation is babysit's real
handoff-to-human boundary (autopilot never merges), so QA is required there —
not only at merge, a stage the babysit flow never reaches. Deploy-command
gating deferred to a future iteration (commands are project-specific — read
from deploy config later).

## The problem (why prompts aren't enough)

A skill can print `STATUS: DONE / VERDICT: PASS` without having done the work —
the status block is **self-reported text**, not something the harness verifies.
This is the "model grading its own ethics exam" failure: a 9.6/10 self-score
that ships a regression. Prompt instructions ("you must verify before shipping")
are advisory — the model can skip them. **A hook is executed by the harness and
cannot be talked around.** That's the control mechanism prompts can't be.

## What babysit already has (the evidence layer)

Unlike ClaudeKit's scheme — which mandates 5 *new* JSON artifacts
(`context-snippets`, `risk-gate`, `verification`, `review-decision`,
`adversarial-validation`) — babysit **already writes the evidence** through
`bbs-ticket`:

| Artifact | Written by | Holds |
|----------|-----------|-------|
| `verdicts/<skill>.md` | `set-verdict` | `STATUS:`/`VERDICT:` per skill (review-pr, qa, implement…) |
| `reviews/<skill>.md` | `set-review` | full review body (findings, fixes, score) |
| `review-log.jsonl` | review-pr | per-commit status, critical count, quality score |
| `handoffs/<NNN>-<skill>.md` | `add-handoff` | change brief (SUMMARY/FILES/BLAST_RADIUS) |
| `~/.babysit/analytics/decisions.jsonl` | `bbs-learnings-log decision` | every Taste/Mechanical auto-decision |

So babysit doesn't need new artifacts — it needs a **hook that checks the
artifacts it already produces** before an irreversible action. That keeps the
skill prose light (guidance, per the project's guide-not-force philosophy) and
puts the *enforcement* at the harness boundary. This is "loose skills, strict
hooks."

### Typed evidence (the mid-tier gap-close, 2026-05-31)

Three of ClaudeKit's five — `verification`, `risk-gate`, `adversarial` — were
work babysit *did* in-flight but never persisted as a structured, checkable
artifact. These are now written through `bbs-ticket set-evidence --kind
<kind>` (validated on write; canonical `evidence/<kind>/result.json`) and read
back with `bbs-ticket evidence-status --kind <kind>` → `none|valid|malformed`
— the same categorical, score-free shape as `verdict-status`. Producers:

| Kind | Owner | Required fields |
|------|-------|-----------------|
| `verification` | `implement` | `result` (PASS/FAIL) |
| `adversarial` | `review-pr` | `disproven`, `unverified` (arrays) |

Schemas: [handoff-contracts § Typed evidence](../.claude/skills/references/handoff-contracts.md).
The remaining two CK artifacts map to existing babysit artifacts
(`context-snippets` → requirement.md + implement contract + handoffs;
`review-decision` → `verdicts/review-pr.md` + `review-log.jsonl`).

**Mid-tier policy (deliberate):** typed evidence is **audited, not gated**.
Hook B logs `evidence: none|valid|malformed` per producer skill to
`skill-usage.jsonl`; Hook A's hard-stage **deny/ask still keys only on
`verdicts/` (review-pr at push, review-pr + qa at PR/merge)** — it does *not*
require the full 5-artifact bundle. This closes the "the hook *can* check
them" gap (the artifacts now exist and are structured) without the heaviest
"all 5 + PASS or no push" enforcement. Tightening the gate to require typed
evidence is a one-line change in `pre-tool-gate` if/when wanted.

Current hook state: `plugin.json` declares **no** Claude Code hooks. The only
hook is `bin/hooks/pre-commit` (a git hook — workflow lint + secret-leak guard).
The preamble "session-writer hook" is inline bash, not a harness hook.

## What Claude Code hooks enable (verified 2026-05-31)

- **Plugins ship hooks** via `hooks/hooks.json` (auto-discovered; no `plugin.json`
  change). Reference scripts with `${CLAUDE_PLUGIN_ROOT}`. They merge with
  user/project hooks when the plugin is enabled.
- **`PreToolUse` blocks** a tool call: emit
  `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"…"}}`
  — the model sees the reason and reacts. A `matcher: "Bash"` + `if: "Bash(git
  push *)"` targets exactly the hard-stage commands.
- **`PostToolUse` after `Skill`** fires with the skill's text output in
  `tool_result.text`. It **cannot block** (the skill already ran) but can warn
  via `systemMessage` / add `additionalContext` and log. This is the surface for
  "verify the output of all skills."

## Proposed design — two hooks, reusing existing artifacts

### Hook A — hard-stage gate (`PreToolUse(Bash)`, blocking)

`${CLAUDE_PLUGIN_ROOT}/bin/hooks/pre-tool-gate`. Matches the irreversible
commands and checks the ticket's verdict artifacts are present **and** PASS
before allowing them. Resolves the ticket via `bbs-ticket resolve`; if no ticket
resolves (ad-hoc shell), `defer` (don't gate non-workflow work).

| Stage (matched command) | Required artifacts | Allow when |
|-------------------------|--------------------|-----------|
| `git push …` | `verdicts/review-pr.md` (or `implement`) | present AND not `BLOCKED`/`NEEDS_CONTEXT` |
| `gh pr create …` / `glab mr create …` | `verdicts/review-pr.md` + `verdicts/qa.md` | both `PASS` / `FIXED` / `DONE*` |
| `gh pr merge …` | `verdicts/review-pr.md` + `verdicts/qa.md` | both `DONE` / `PASS` |
| deploy cmds (configurable) | `verdicts/ship.md` review chain | review chain `DONE` |

On a miss → `permissionDecision: "deny"` with a reason naming the missing/failed
artifact and the skill to run (`/bbs:review-pr`, `/bbs:qa`). **Score never
auto-approves** — the gate keys on categorical verdicts (`DONE`/`PASS`/`BLOCKED`),
never on a numeric score. A `BLOCKED` verdict with evidence always denies.

### Hook B — verdict-contract verifier (`PostToolUse(Skill)`, audit)

`${CLAUDE_PLUGIN_ROOT}/bin/hooks/verify-skill-output`. Matches `Skill`, parses
`tool_result.text` for a well-formed terminal block (`STATUS:` ∈
{DONE, DONE_WITH_CONCERNS, BLOCKED, NEEDS_CONTEXT} + a `VERDICT:` line per
[handoff-contracts](../.claude/skills/references/handoff-contracts.md)). Missing
or malformed → `systemMessage` warning + a row in
`~/.babysit/analytics/skill-usage.jsonl` (telemetry is babysit's primary
feedback channel). Can't block, but makes a skipped/garbled verdict *visible*
instead of silently passing — complements autopilot's existing in-session
Verify-post step (which already re-checks declared artifacts).

### Hook C — clean-handoff audit (`Stop`, audit)

`${CLAUDE_PLUGIN_ROOT}/bin/hooks/clean-handoff-check`. When the session ends
with a resolvable ticket, checks the two objective clean-state signals:
uncommitted changes in the worktree, and a `checkpoint.json` older than the
last commit. Dirty exit → `systemMessage` warning + a
`clean-handoff-audit` row in `skill-usage.jsonl`. Never blocks the stop; no
ticket resolves → silent (same fail-open rule as Hook A). Rationale: a session
that ends dirty degrades the next cold session's recovery — clean state is
part of "done", not housekeeping.

### Retry / escalate

Lives in the **skill/workflow**, not the hook (the hook only allows/denies). On
a deny, the dispatching workflow step surfaces the reason as its `BLOCKED` /
`NEEDS_CONTEXT` status; the human (or orchestrator) resolves and re-dispatches.
No bypass flag — matching ClaudeKit's "fail twice → escalate, don't bypass."

## Tension to resolve

The user earlier asked for **short, guide-not-force** skills and **no heavy
harness** ([[babysit-skill-style-brevity]]). This proposal is consistent *only*
because the enforcement lives in a hook (the harness layer), not in skill prose,
and reuses existing artifacts rather than mandating new ones. If we instead
pushed artifact-creation rules into every skill body, that would be the heavy
harness the user rejected. **Keep skills as guidance; put the gate in the hook.**

## Open decisions (need user sign-off before building)

1. **Ship Hook A at all?** It denies `git push` / `gh pr merge` when evidence is
   missing — powerful, but it will block legitimate ad-hoc pushes if the
   ticket-resolution / `defer`-when-no-ticket logic is wrong. Blast radius is real.
2. **Exact gate policy** — the table above is a starting point. Which commands,
   which artifacts per stage, and how strict (deny vs `ask`)?
3. **Hook B scope** — all `bbs:` skills, or only the work skills?
4. **Deploy-command matching** — deploy commands vary per project
   (`fly deploy`, `vercel`, `gh workflow run`). Read from `.babysit/git-flow.yaml`
   / deploy config, or skip deploy gating in v1?
