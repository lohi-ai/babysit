---
workflow: prototyper
version: 1
description: Validate one risky assumption with a quarantined throwaway spike. Stops at a learning verdict, never a PR.
needs-state:
  requirement_md: optional
---
# prototyper
The Prototyper archetype (see `../references/archetypes.md`). Use when the work
is "does this even work?" rather than "build this." Optimize for learning speed,
not durability.
## run
> produces: verdict:prototyper
1. Resolve the assumption to test from `requirement.md` or the invocation. If it
   is unclear whether the idea is even worth a spike, run `office-hours` first.
2. Run `prototype`: build the smallest quarantined spike and capture the signal.
3. Checkpoint the verdict and what was learned.
4. Write a handoff: assumption, signal observed, and the one next action.
**Stop conditions**

- `NEEDS_CONTEXT`: the assumption to test cannot be determined.
- `BLOCKED`: the spike cannot be run at all (missing env, no target).
**Final status**
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: VALIDATED | INVALIDATED | INCONCLUSIVE
SUMMARY: <assumption + signal>
NEXT: promote via /bbs:autopilot builder, iterate, or drop the spike
```
