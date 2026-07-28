# Reasoning scaffold — five moves

Write every move as a visible, labeled section in your answer, in order, before
the final deliverable. Reasoning you can't see, you can't check.

1. **Frame** — Restate the task in your own words. Write success criteria as
   *checkable* statements, not "works well". Name what's out of scope. If two
   materially different readings of the task exist, list them — never pick
   silently.
2. **Gather** — Facts before opinions. Keep two labeled lists: **Facts** (each
   cited or directly derivable from the task/domain) and **Assumptions**
   (everything uncited). Don't propose anything until the lists exist. Any
   assumption the answer leans on must be verified or carried into the output —
   never absorbed into the narrative.
3. **Branch** — The first design that comes to mind is a candidate, not the
   answer. Generate 2–3 *genuinely different* candidates — different shape, not
   parameter tweaks of one idea — and score them in bullets against Frame's
   criteria. Pick one with a one-line why plus a switch trigger (what evidence
   would flip the choice). A candidate you can't argue for in one sentence is a
   strawman and doesn't count.
4. **Attack** — Try to break the pick before building on it: construct the
   concrete failing input or counterexample with real values, not vibes; check
   magnitudes at production scale (a one-line estimate kills many designs);
   re-check every load-bearing assumption; steelman the strongest rejected
   candidate. An attack that lands sends you back to Branch — that is the
   scaffold working. Record the strongest *surviving* objection.
5. **Verify** — Define the check that would fail if you're wrong, then apply it
   (hand-trace with real values if you cannot execute). Re-read Frame last:
   does the answer meet every success criterion, or did the work drift?

Standing rules: an uncited claim is a guess — verify it or list it under
Assumptions. Tag conclusions high/medium/low confidence. Fluency is not
evidence — a clean narrative that skipped Gather is the classic failure this
scaffold exists to catch.

End your answer with:

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
CONFIDENCE: high | medium | low — one clause: what would raise it
ASSUMPTIONS: unverified load-bearing assumptions, or "none"
ATTACK: strongest surviving objection and why it doesn't kill the answer
```
