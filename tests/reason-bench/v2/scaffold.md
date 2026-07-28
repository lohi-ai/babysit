# Reasoning scaffold — five moves, v2

Write every move as a visible, labeled section in your answer, in order,
before the final deliverable. Reasoning you can't see, you can't check.

**First: name the deliverable's shape.**

- **Decision-shaped** (architecture, plan, design choice, root cause) —
  Branch and Attack carry the run.
- **Enumeration-shaped** (test plan, checklist, case matrix, audit) —
  breadth IS the quality bar. Write the full case list FIRST, at full
  width, before any meta-reasoning; then Attack per case. Your reasoning
  sections never count against the deliverable's budget — if anything must
  be cut, cut reasoning, never cases. A beautifully-reasoned plan that
  lists fewer cases than a naive answer has failed.
- **Construction-shaped** (writing code) — run the moves on the design
  decisions, then execute with discipline: checkable criteria before code,
  error paths as part of the contract, a final sweep with adversarial
  inputs beyond your own tests.

## The five moves

1. **Frame** — Restate the task in your own words. Write success criteria
   as *checkable* statements, not "works well". Name what's out of scope.
   If two materially different readings exist, list them — never pick
   silently.
2. **Gather** — Facts before opinions. Keep two labeled lists: **Facts**
   (each cited or directly derivable from the task/domain) and
   **Assumptions** (everything uncited). Don't propose anything until the
   lists exist. An assumption the answer leans on must be verified or
   carried into the output — never absorbed into the narrative. If an
   unverified assumption gates the whole approach, the first element of
   your answer is the spike or question that settles it — not a design
   built on the guess.
3. **Branch** — The first design that comes to mind is a candidate, not
   the answer. Generate 2–3 *genuinely different* candidates — different
   shape, not parameter tweaks of one idea — and score them in bullets
   against Frame's criteria. Pick one with a one-line why plus a switch
   trigger. A candidate you can't argue for in one sentence is a strawman
   and doesn't count.
4. **Attack** — Try to break the pick before building on it:
   - Construct the concrete failing input or counterexample — real values,
     not vibes.
   - **Quantify.** Put numbers on the load-bearing quantities: data
     volume, request rate, backlog after an outage, storage over the
     retention window, drain rate. A one-line estimate kills many designs
     — never skip it on capacity, storage, or latency claims.
   - **Sweep the spec.** Every element you were given — field, column,
     constraint, requirement — is either consumed by your answer or
     explicitly declared out of scope. An unused input is a missed
     requirement, not decoration.
   - Re-check every load-bearing assumption. Steelman the strongest
     rejected candidate.
   An attack that lands sends you back to Branch — that is the scaffold
   working. Record the strongest *surviving* objection.
5. **Verify** — Define the check that would fail if you're wrong, then
   apply it (hand-trace with real values if you cannot execute). Re-read
   Frame last: does the answer meet every success criterion, or did the
   work drift? For enumeration-shaped deliverables the last check is
   breadth: sweep the spec's risk nouns (dates, money, timezones,
   concurrency, retries, permissions, offline) and confirm each has a
   named case in the list.

## Standing rules

An uncited claim is a guess — verify it or list it under Assumptions. Tag
conclusions high/medium/low confidence. Fluency is not evidence — a clean
narrative that skipped Gather is the classic failure this scaffold catches.

## Self-check before finishing

- Every load-bearing claim cited or listed under ASSUMPTIONS?
- Branch candidates genuinely different, or one idea three ways?
- Did Attack produce a concrete counterexample attempt AND at least one
  magnitude estimate where scale matters?
- Every spec element consumed or explicitly declined — nothing silently
  carried unused?
- Enumeration-shaped? Confirm the case list is *wider* than a
  straight-ahead answer would be — reasoning crowding out cases is this
  scaffold's known failure mode.
- Was the Verify check defined before the answer was final, and applied?

End your answer with:

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
CONFIDENCE: high | medium | low — one clause: what would raise it
ASSUMPTIONS: unverified load-bearing assumptions, or "none"
ATTACK: strongest surviving objection and why it doesn't kill the answer
```
