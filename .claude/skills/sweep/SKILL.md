---
name: sweep
description: Simplify and shrink working code without changing behavior. Use to remove dead code, unship unused features, cut complexity, tidy UI, or optimize a measured hot path.
---

# sweep

The Sweeper archetype (see `../references/archetypes.md`). Make the system
smaller and clearer while keeping behavior identical. Less code, same output.

## Flow

1. Establish the safety net first: identify and run the tests / typecheck /
   build that prove current behavior. Record the green baseline. If none exist,
   add the narrowest characterization check before touching anything.
2. Find the highest-leverage cleanup: dead code, duplicated logic, an unused
   feature or flag, an over-built abstraction, or a measured slow path.
3. Apply one focused, behavior-preserving change.
4. Re-run the same safety net. It must be as green as the baseline.
5. Repeat for the next item, or stop and hand off.

## Rules

- Behavior must not change. If a "cleanup" alters output, it is a feature
  change — stop and route it to `plan-draft`/`implement`.
- Optimize only what you measured. No speculative performance work. Perf
  driven by production scale or load (caching, indexes, capacity) is
  `maintain`'s job — sweep only removes measured waste, behavior byte-identical.
- UI tidying: look at the rendered surface (`browse`) before and after — the
  test safety net does not prove visual sameness.
- Unship only what is provably unused; name the evidence (no callers, flag off
  everywhere, analytics flat). When unsure, flag it for a human, do not delete.
- Stay surgical — do not bundle unrelated refactors into one sweep.
- Removing code is the goal; net-negative diffs are a good sign.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: SIMPLIFIED | OPTIMIZED | UNSHIPPED
SUMMARY: <what shrank + lines removed + baseline still green>
NEXT: review-pr, then human review and create-pr
```
