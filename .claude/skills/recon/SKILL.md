---
name: recon
description: Evaluate an external repository, library, or tool against the current project. Use for adoption decisions, architecture comparisons, or requests to explore and borrow from another project.
---

# recon

Understand the target, compare it to the local need, then recommend steal or pass.

## Flow

1. Identify the current project's concrete problem and constraints.
2. Inspect the external target's architecture, maintenance state, license, dependencies, and core implementation.
3. Compare concepts and costs against existing local patterns.
4. Recommend `copy`, `refactor`, `rebuild`, or `pass`.
5. Implement only when the user requested adoption and the recommendation is clear.

## Rules

- Prefer primary sources: source code, official docs, releases, and license.
- Separate attractive ideas from code worth importing.
- Account for maintenance, security, dependency, and migration cost.
- Preserve attribution and license obligations.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT
VERDICT: STEAL(<approach>) | PASS
SUMMARY: <fit, costs, recommendation>
NEXT: <implementation action or none>
```
