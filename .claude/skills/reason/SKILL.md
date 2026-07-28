---
name: reason
description: Deliberate-reasoning scaffold that lifts a smaller model's planning, solution design, debugging, and QA thinking toward frontier quality. Use before drafting a plan, choosing between designs, diagnosing a hard bug, writing a QA plan, or whenever the first plausible answer might be wrong. Composable — run another skill "with reason" to harden its decision points.
---
# reason
What separates frontier reasoning is not knowledge — it's discipline at
decision points. A strong model implicitly branches, attacks its own answer,
and verifies before committing; a smaller model converges on the first
plausible answer and narrates it fluently. This skill externalizes that
discipline into five moves. The mechanism is *writing the moves down*:
reasoning you can't see, you can't check — visible sections in the
response, or `$(bbs-ticket path)/reasoning.md` when the work spans
sessions (scratch dir if no ticket).

Benchmarked (`tests/reason-bench`): the scaffold's proven wins are
**quantification under Attack** (every scaffold run computed the magnitude
math every base run skipped) and **assumption gating under Gather** (scaffold
runs turned a silent adoption into a gating spike). Its one measured harm is
**crowding out breadth on list-shaped deliverables** — hence the shape rule.
## First: name the deliverable's shape
The five moves are constant; where the effort goes depends on the artifact:
- **Decision-shaped** (architecture, plan approach, design choice, root
  cause) — Branch and Attack carry the run. This is where the scaffold is
  proven to matter more than model tier.
- **Enumeration-shaped** (QA checklist, test matrix, edge-case list, audit
  findings) — breadth IS the quality bar. Write the full case list FIRST, at
  full width, before any meta-reasoning; then Attack per case. The trace
  never counts against the deliverable's budget: if anything must be cut,
  cut trace, never cases. A beautifully-reasoned plan that lists fewer cases
  than a naive answer has failed.
- **Construction-shaped** (writing the code itself) — run the moves on the
  design decisions, then hand execution to `phased-build`; phase gates and
  harness-proving are its job, not this skill's.
## The five moves
### 1. Frame
Restate the task in your own words. Write success criteria as *checkable*
statements ("`npm test` passes and the 409 path has a test", not "works
well"). Name what's out of scope. If two materially different readings
exist, list them and classify per the
[Auto-Decision Framework](../references/auto-decision-framework.md) —
never pick silently.
### 2. Gather
Facts before opinions. Read the actual code, run the actual commands, and
keep two labeled lists: **Facts** (each cited — `file:line`, command +
output, commit) and **Assumptions** (everything uncited). Don't propose
anything until the lists exist. An assumption the answer leans on must be
verified (promoted to Facts) or carried into the output — never absorbed
into the narrative. If an unverified assumption gates the whole approach
(is the engine OT or CRDT?), the plan's first step is the spike that
settles it — not a design built on the guess.
### 3. Branch
The first design that comes to mind is a candidate, not the answer.
Generate 2–3 *genuinely different* candidates — different shape, not
parameter tweaks of one idea — and score them in bullets against Frame's
criteria and constraints. Pick one with a one-line why plus a switch
trigger (what evidence would flip the choice). A candidate you can't argue
for in one sentence is a strawman and doesn't count.
### 4. Attack
Try to break the pick before building on it:
- Construct the concrete failing input or counterexample — walk the code
  with real values, not vibes.
- **Quantify.** Put numbers on the load-bearing quantities: data volume,
  request rate, backlog after an outage, payload size, drain rate. A
  one-line estimate kills many designs — this is the scaffold's single
  most proven move; never skip it on capacity, storage, or latency claims.
- **Sweep the spec.** Every element you were given — field, column,
  constraint, requirement — is either consumed by the answer or explicitly
  declared out of scope. An unused `secret` column is a missed signing
  requirement, not decoration.
- Re-check every load-bearing assumption against code, not memory.
- Steelman the strongest rejected candidate: what does it do better, and
  why is that acceptable to lose?
An attack that lands sends you back to Branch — that is the scaffold
working, not a failure. Record the strongest *surviving* objection. If
attacks kill two different candidates in a row, stop treating it as a
design problem: the task is under-specified — escalate `NEEDS_CONTEXT`
with both corpses as evidence (delivery per
[preamble.md](../references/preamble.md#one-mode-two-escalation-channels)).
### 5. Verify
Define the check *before* declaring done — a command, test, or browser step
that would fail if you're wrong — then run it. Re-read Frame last: does the
answer meet every success criterion, or did the work drift to a
subproblem? For enumeration-shaped deliverables the last check is breadth:
sweep the spec's risk nouns (dates, money, timezones, concurrency, retries,
permissions) and confirm each has a named case in the list.
## Standing rules
- **Cited or suspect** — any claim about the codebase without a citation is
  a guess; verify it or move it to Assumptions.
- **Mark confidence** — tag conclusions high/medium/low. Low confidence on
  a load-bearing conclusion means verify now, not average and continue.
- **Stuck? Change representation** — split the question into independent
  sub-questions and settle each with its own evidence; write the state
  table; enumerate the cases exhaustively; trace one concrete input end to
  end; or invert (what would have to be true for this to be false?).
- **Fluency is not evidence** — a clean narrative that skipped Gather is
  the classic small-model failure this skill exists to catch.
## Depth scaling
The scaffold is a lens, not a form — scale to stakes, don't ceremonialize:
| Stakes | Moves |
|--------|-------|
| One-line mechanical fix | Frame + Verify, two sentences total |
| Standard feature / bug | All five, compact sections |
| Architecture, security, irreversible | All five written to `reasoning.md`; Attack makes ≥2 counterexample attempts |
## Applying to the common artifacts
- **Plan** (with `plan-draft`) — decision-shaped. Branch produces the 2–3
  approach shapes before the Approach section; Attack findings feed
  Unknowns; a gating assumption becomes Phase 0. The trace stays in the
  response; `plan.md` stays thin.
- **Solution / implementation** — Attack the design before writing code;
  then `phased-build` owns execution; Verify with the failing-if-wrong
  check after.
- **Root cause** (with `investigate`) — Branch = competing theories, Attack
  = the toggle test that discriminates them.
- **QA plan** (with `qa`) — enumeration-shaped: enumerate flows by user
  intent × state at full width first (month boundaries, DST, mid-cycle
  changes, retries, races — the spec's risk nouns), keep the trace out of
  the plan doc, and only then Attack: for each expected PASS, what evidence
  would falsify it?
## Composition
Invoked alongside another skill ("implement X with reason"), run that
skill's flow and apply the five moves at each Taste-or-harder decision
point. With `phased-build` the split is clean: reason owns the decisions,
phased-build owns the execution gates. Standalone (`/bbs:reason <task>`),
run the moves on the task and deliver whatever artifact it calls for.
## Self-check, then output
Before emitting the status block, audit your own trace — fix, don't
narrate, any miss:
- Every load-bearing claim cited? Uncited ones listed under ASSUMPTIONS?
- Were the Branch candidates genuinely different, or one idea three ways?
- Did Attack produce a *concrete* counterexample attempt (real values) and
  at least one magnitude estimate where scale matters?
- Every spec element consumed or explicitly declined — nothing silently
  carried unused?
- Enumeration-shaped? Confirm the case list is *wider* than a
  straight-ahead answer would be, not narrower — trace crowding out cases
  is this scaffold's known failure mode.
- Was the Verify check defined before the answer was final, and run?
- Does the answer meet Frame's criteria — the ones written, not remembered?
Then the artifact the task asked for, followed by:
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: REASONED(<moves run, e.g. frame,gather,branch,attack,verify>)
CONFIDENCE: high | medium | low — <one clause: what would raise it>
ASSUMPTIONS: <unverified load-bearing assumptions, or "none">
ATTACK: <strongest surviving objection and why it doesn't kill the answer>
```
