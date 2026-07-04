---
name: office-hours
description: Stress-test an idea before building. Use for startup/product judgment, builder brainstorming, narrowing a wedge, shaping a requirement, or deciding whether an idea is worth implementing.
---

# office-hours

Help the user think before code. Produce a clear decision or design note, not an implementation.

## Flow

1. Identify the mode: startup demand test or builder/design brainstorming.
2. Ask only for context that changes the decision; otherwise infer and label assumptions.
3. Pressure-test the idea: user, pain, current workaround, narrow wedge, proof, and next step.
4. Write a short artifact the next skill can use: problem, audience, scope, non-goals, risks, and recommended next action.
5. If the idea is not ready, say what evidence would change that.

## Rules

- Do not write product code.
- Do not invent customer proof, positioning, revenue, or analytics.
- Prefer one sharp recommendation over a menu of possibilities.
- Route build-ready work to `plan-draft` or `autopilot`.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: DESIGNED | NOT_READY
SUMMARY: <decision + reasoning>
NEXT: plan-draft, gather evidence, or none
```
