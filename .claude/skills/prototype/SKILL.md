---
name: prototype
description: Build a fast, throwaway spike to validate one risky technical or product idea before committing to production work. Use to test feasibility, churn a rough proof, or de-risk an assumption — not to ship, and not for UI look-and-feel questions (that is design-ui).
---

# prototype

The Prototyper archetype (see `../references/archetypes.md`). Churn a rough
proof that answers **one** question; most prototypes won't ship, so optimize for
learning speed, not durability.

## Flow

1. Name the single riskiest assumption this spike must validate, and the
   observable signal that proves or kills it.
2. If the spike improves or extends an existing surface, ground in reality
   first: read the code behind it and observe the live behavior (`browse`).
   A spike built on a guessed baseline validates nothing. If grounding shows
   existing code already proves the assumption, the spike is done — report
   VALIDATED citing that evidence and route NEXT to `plan-draft`. "It's
   proven, so I'll just build it" is the Builder's move, not this skill's.
3. Build the smallest thing that produces that signal. Isolate it: a flagged
   path, a scratch route, a `prototype/` dir, or a throwaway branch — never woven
   into production code.
4. Run it and capture the signal (output, screenshot via `browse`, timing).
5. Verdict: does the assumption hold? State what you learned and the one thing
   to do next — promote to a real `plan-draft`, iterate the spike, or drop it.

## Rules

- A visual/UI question ("does this screen look right?") is `design-ui`'s job,
  not a spike. Do not answer it here: invoke the `design-ui` skill via the
  Skill tool (skill: `design-ui`), passing the user's original request as args,
  then stop this flow.
- This is throwaway by default. Skip tests, error handling, and abstractions
  unless they are the thing being validated.
- Keep it quarantined so it is trivial to delete; do not refactor production
  code to accommodate a spike.
- The deliverable is an isolated runnable artifact the user can see or run,
  plus a verdict — never a production diff. If the smallest thing that
  produces the signal seems to be editing production source (schema,
  migrations, services, shared components), you are implementing, not
  spiking: stop and hand off. Repo-wide typecheck/lint passing is a Builder
  gate, not a spike signal.
- Do not invent the result. Run the spike and report the real signal, or say it
  could not be run.
- A spike worth shipping is a Builder decision — hand off to `plan-draft`, do
  not quietly productionize it here.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: VALIDATED | INVALIDATED | INCONCLUSIVE
SUMMARY: <assumption tested + signal observed>
NEXT: promote via plan-draft, iterate, or drop the spike
```
