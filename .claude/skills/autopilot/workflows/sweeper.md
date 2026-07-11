---
workflow: sweeper
version: 1
description: Behavior-preserving simplification of an existing branch or area — establish a green baseline, shrink, re-verify.
needs-state:
  branch_pushed: optional
---
# sweeper
The Sweeper archetype (see `../references/archetypes.md`). Use when working code
should get smaller or faster without changing behavior.
## run
> produces: verdict:sweeper + qa:checked
1. Read the target area, branch, and git diff. Run the existing tests /
   typecheck / build and record the green baseline.
2. Run `sweep`: apply focused, behavior-preserving cleanups (dead code,
   duplication, unused features, measured hot paths).
3. Run `review-pr --fix` (applies fixes to the working tree).
4. Run `qa` (or the strongest fallback) and confirm behavior matches the
   baseline. Persist the verdict with `bbs-ticket set-verdict --skill qa`.
5. Commit and push when policy allows.
6. Write a handoff: what shrank, net line delta, and baseline-still-green proof.
   When a signal warrants, name the forward lifecycle edge after `create-pr`:
   surface shipped and measurable → `grower`; scale/security/cost signal
   observed while sweeping → `maintainer`.
**Stop conditions**

- `NEEDS_CONTEXT`: no safety net exists and behavior cannot be characterized.
- `BLOCKED`: the baseline is red before changes, or a cleanup changes behavior
  and cannot be made behavior-preserving.
**Final status**
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: SWEPT
SUMMARY: <what shrank + line delta + QA evidence>
NEXT: human review, then /bbs:create-pr
```
