---
name: prototype
description: Build a fast, throwaway spike to validate one risky technical or product idea before committing to production work. Use to test feasibility, churn a rough proof, or de-risk an assumption — not to ship, and not for UI look-and-feel questions (that is design-ui).
---
# prototype
The Prototyper archetype (see `../references/archetypes.md`). Churn a rough
throwaway proof that answers **one** question: name the single riskiest
assumption and the observable signal that proves or kills it, build the
smallest thing that produces that signal, run it, capture the signal
(output, screenshot via `browse`, timing), and verdict — promote to
`plan-draft`, iterate, or drop. When the spike extends an existing surface,
ground first: read the code behind it and observe the live behavior
(`browse`); if existing code already proves the assumption, report VALIDATED
citing that evidence and route NEXT to `plan-draft` — don't start building.
Quarantine everything (a flagged path, scratch route, `prototype/` dir, or
throwaway branch — never woven into production code) and skip tests, error
handling, and abstractions unless they are the thing being validated. If the
smallest signal seems to require editing production source (schema,
migrations, services, shared components), you are implementing, not spiking
— stop and hand off; repo-wide typecheck/lint is a Builder gate, not a spike
signal. A visual/UI question routes to the `design-ui` skill instead. Report
the real signal or that it could not run — never invent it.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: VALIDATED | INVALIDATED | INCONCLUSIVE
SUMMARY: <assumption tested + signal observed>
NEXT: promote via plan-draft, iterate, or drop the spike
```
