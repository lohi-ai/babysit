# Auto-Decision Framework

**This is the core of babysit.** Every skill in the pack routes decisions through
this framework so runs produce correct work without steering, and only the
choices a human actually needs to make get surfaced.

Paired with [§ One mode, two escalation channels](preamble.md#one-mode-two-escalation-channels):
*this framework* decides *whether* a decision needs to reach a human at all;
`INVOKER` decides *how* it reaches them (inline `AskUserQuestion` when
`developer`, structured `NEEDS_CONTEXT` block otherwise).

## The rule, in one paragraph

Classify every decision as **Mechanical**, **Taste**, or **User Challenge**.
Mechanical → decide silently. Taste → decide using the 6 principles, log the
rationale, surface at the final gate when `INVOKER=developer`. User Challenge →
never auto-decide; emit `NEEDS_CONTEXT` (rendered as `AskUserQuestion` when
`INVOKER=developer`, structured block otherwise). Auto-decide replaces the
human's *judgment*, not the *analysis* — still do every check, still produce
every artifact.

## The 6 Decision Principles

Use these to auto-answer every Mechanical and Taste decision. When two
principles conflict, the per-phase tiebreakers at the bottom of this file apply.

1. **Correctness over completeness** — Babysit runs unattended; a wrong
   implementation is worse than an incomplete one. When a guess would land
   incorrect code, stop and classify as User Challenge. Otherwise, pick the
   approach that covers more of the specified scope.
2. **Bounded blast radius** — Fix what the task asks for, plus direct
   importers of modified files. Auto-approve expansions that are in-radius AND
   < 5 files AND no new infra. Anything wider is a User Challenge.
3. **Pragmatic** — If two options fix the same thing, pick the cleaner one.
   Five seconds choosing, not five minutes deliberating.
4. **DRY** — Duplicates existing functionality? Reject. Reuse what's in the
   codebase. Grep before writing.
5. **Explicit over clever** — 10-line obvious fix beats 200-line abstraction.
   Pick what a new contributor reads in 30 seconds.
6. **Bias toward verified action** — Decide, log, ship, self-verify. Telemetry
   and tests are the feedback channel, not a human checkpoint. Flag concerns
   in the completion status; don't block on them.

## Decision Classification

Every decision point in a skill falls into exactly one of three tiers:

### 🟢 Mechanical — auto-decide silently

One clearly right answer. No rationale needed; no audit entry needed.

Examples:
- Which test framework to use → whatever the repo already uses.
- Whether to run the type-checker after an edit → always yes.
- Whether to fix a confirmed bug inside the task scope → always yes.
- Import style, formatting, quote choice → match the file being edited.

### 🟡 Taste — auto-decide, log to the audit trail

Reasonable engineers could disagree. Decide using the 6 principles, then write
one line to the decision log so a human can audit after the fact. When
`INVOKER=developer`, also surface at the final gate for one-shot override.

Three natural sources:
1. **Close approaches** — two viable options with different tradeoffs.
2. **Borderline scope** — in blast radius but 3-5 files, or ambiguous radius.
3. **Tool disagreement** — e.g. the type-checker and a linter pull opposite
   directions, and both have a valid point.

**Audit log format** — append one JSON line to
`~/.babysit/analytics/decisions.jsonl` (correlated by `$_SESSION_ID` from the
preamble):

```bash
printf '{"ts":"%s","session":"%s","skill":"%s","tier":"taste","principle":"%s","decision":"%s","rationale":"%s"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$_SESSION_ID" "$_SKILL_NAME" \
  "P5-explicit" "split into two files" "10 lines in each is easier to read than 200 in one" \
  >> ~/.babysit/analytics/decisions.jsonl
```

Every Taste decision gets a row. No silent Taste decisions — without the log,
a human can't audit an unattended run.

### 🔴 User Challenge — never auto-decide

The task's stated direction is wrong, ambiguous, or would cause incorrect work.
The human must make the call.

Triggers:
- Requirement has multiple plausible interpretations that produce materially
  different code.
- Irreversible or high-blast-radius action (deploy, migration, destructive
  write, external send) without prior durable authorization.
- Missing config, credentials, or ticket scope that can't be inferred from
  the repo.
- Security-sensitive change where the safe choice isn't obvious.
- You'd be rejecting or rewriting what the user asked for (merge, split, add,
  remove features the user specified).

**Delivery by `INVOKER`** (see
[§ One mode, two escalation channels](preamble.md#one-mode-two-escalation-channels)):

| `INVOKER` | Delivery |
|-----------|----------|
| `developer` | Render the framing below as a single `AskUserQuestion`. If declined or unclear, fall through to printing the structured `NEEDS_CONTEXT` block. |
| `mayor`, `general`, `scanner`, any non-`developer` | Print the structured `NEEDS_CONTEXT` block with the framing below. Orchestrator relays via its own channel. |

**User Challenge framing** (applies to both deliveries — the human sees the
same structure regardless of surface):

```
WHAT YOU SAID: <the original direction>
WHAT WE RECOMMEND: <the change, if any — else "no change, need clarification">
WHY: <1-2 sentences, citing which principle>
WHAT WE MIGHT BE MISSING: <explicit blind spots>
IF WE'RE WRONG, THE COST IS: <downside if the original direction was right>
OPTIONS: A) <option>  B) <option>  C) <option>  [2-4 labeled, mutually exclusive]
```

The user's original direction is the default. The skill must make the case for
change, not the other way around.

**Security / feasibility exception:** if the challenge is a security or
feasibility *risk* (not a preference), prefix with:
`⚠️ FLAGGED AS RISK, NOT PREFERENCE:` so the urgency isn't buried in taste-level
framing.

## What "Auto-Decide" Means (and Doesn't)

Auto-decide replaces the HUMAN'S *judgment* with the 6 principles. It does
NOT replace the *analysis*. Every check the skill would run interactively must
still run — the only thing changing is who picks between viable options.

**You MUST still:**
- Read the code, diffs, and files the task references.
- Produce every artifact the skill promises (tests, reports, diffs).
- Catch every issue the skill is designed to catch.
- Self-verify (type-check, tests, browser check) before declaring done.
- Log every Taste decision.

**You MUST NOT:**
- Skip analysis because "I'll just auto-decide."
- Compress a phase into one line.
- Log a Mechanical decision as Taste to feel thorough (or vice versa — under-logging is
  worse than over-logging).
- Silently auto-decide a User Challenge.

## Terse Output (Mechanical Decision)

Output compression is a Mechanical decision — the skill auto-selects the mode
based on who consumes the output. No human approval needed; no Taste log entry.

### Modes

| Mode | Consumer | Rules |
|------|----------|-------|
| **Full** | Machine — checkpoint.json, telemetry JSONL, status lines | Drop articles, fragments OK, short synonyms. Pattern: `[thing] [action] [reason]`. No preamble. |
| **Dense** | Downstream model — handoff files, plan.md, requirement.md | Complete sentences, information-dense. Keep the why, constraints, and gotchas — a later step with no conversation memory rebuilds context from these files. Cut filler, never information. |
| **Lite** | Human — NEEDS_CONTEXT blocks, final gate, AskUserQuestion, terminal output | No filler/hedging, keep articles + full sentences. Professional but tight. |
| **Normal** | Auto-clarity override | Full prose for: security warnings, destructive ops, multi-step sequences where fragment order risks misread, user confused or repeating question. Resume terse after. |

### How to apply

At every output boundary, classify the consumer:
- Writing structured state a tool parses (checkpoint, telemetry) → **Full**
- Writing a file a later model session reads (handoff, plan, requirement) → **Dense**
- Rendering text a human will read inline → **Lite**
- Security/destructive/ambiguous → **Normal** (auto-clarity)

Skills that already have their own terse-output rules (for example, finding
format) take precedence — this section fills the gap for skills that don't.

## The Final Gate (`INVOKER=developer` only)

When `INVOKER=developer` and the skill has completed its work, surface all
Taste decisions at once before declaring DONE. This is the *only*
`AskUserQuestion` a correctly-functioning skill needs in this mode (besides
any User Challenge the skill already raised mid-run).

Format:

```
## <skill-name> complete

### Summary
<1-3 sentences>

### Your Choices (taste decisions)
**Choice 1: <title>** — picked <X> (principle <N>). <Y> also viable:
  <1-sentence downstream impact if you pick Y>

[...]

### Auto-decided silently: <count> — see ~/.babysit/analytics/decisions.jsonl
```

AskUserQuestion options:
- **A)** Approve as-is (accept all recommendations)
- **B)** Override one or more — specify which
- **C)** Ask about a specific decision
- **D)** Reject and redo

Cognitive load management:
- 0 taste decisions → skip the gate entirely, just report `DONE`.
- 1-5 → flat list.
- 6+ → something is off; the skill was probably making decisions that should
  have been Mechanical. Flag at the top: "High ambiguity run ([N] taste
  decisions) — review carefully."

## Non-`developer` `INVOKER`: No Gate, Dense Log

When `INVOKER` is anything other than `developer`, skip the gate — there's no
one at the terminal to show it to. Instead:
- Every Taste decision is already in `decisions.jsonl` (the audit trail is
  mandatory; this is how the orchestrator's post-hoc review works).
- The completion `STATUS` line is the only interactive surface. If there are
  unresolved User Challenges, `STATUS: NEEDS_CONTEXT`. Otherwise,
  `STATUS: DONE` or `DONE_WITH_CONCERNS` with concerns in `SUMMARY`.

## Conflict Resolution — per-phase tiebreakers

The 6 principles don't fully order; when two collide, the dominant pair
depends on what the skill is doing:

| Phase / skill type | Dominant principles | Why |
|---|---|---|
| Design / architecture | P1 (correctness) + P4 (DRY) | Get the shape right; reuse existing patterns. |
| Implementation | P5 (explicit) + P3 (pragmatic) | Ship readable code fast. |
| Quality (bug-scan, implement, browse) | P1 (correctness) + P2 (bounded) | Cover edge cases, don't balloon scope. |
| Frontend / UI | P1 (correctness) + P3 (pragmatic) | Cover states; pick practical. |
| Ops / migration / deploy | P2 (bounded) + P6 (verified action) | Smallest safe step, verified. |

## How Skills Reference This File

Near the top of SKILL.md, after the title:

```markdown
## Decisions

This skill resolves decision points via the
[Auto-Decision Framework](../references/auto-decision-framework.md). Every
decision site is classified (Mechanical / Taste / User Challenge) and User
Challenges escalate via `NEEDS_CONTEXT`, rendered as `AskUserQuestion` only
when `INVOKER=developer`. Do not add ad-hoc prompts.
```

Inside the skill, when hitting a decision point, classify → act → log in that
order. Never skip the classification step.
