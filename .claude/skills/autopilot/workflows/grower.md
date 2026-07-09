---
workflow: grower
version: 1
description: Iterate on a shipped product to improve product-market fit — measure, propose ranked experiments, then scaffold one reversible variant.
needs-state:
  requirement_md: optional
---
# grower
The Grower archetype (see `../references/archetypes.md`). Use to move a metric on
a product that already ships. Measure before you build; scaffold only one
reversible experiment per run.
## run
> produces: verdict:grower + qa:checked
1. Read product positioning, the target surface, and any available funnel or
   analytics context. Name the metric and activation moment.
2. Run the matching growth skill: `growth-experiment` to rank experiments,
   `conversion-fix` for an activation surface, `copy-rewrite` for positioning,
   `social-content` for acquisition. Pick by the invocation; default to
   `growth-experiment`.
3. If asked to implement, scaffold the smallest flagged, reversible variant with
   exposure and conversion tracking. Otherwise stop at the ranked recommendation.
4. If code changed: run `review-pr`, then `qa` (or the strongest fallback), and
   persist the verdict with `bbs-ticket set-verdict --skill qa`.
5. Commit and push any scaffolded variant when policy allows.
6. Write a handoff: metric, winning experiment + constraining assumption, and
   what shipped behind which flag. When a signal warrants, name the forward
   lifecycle edge after `create-pr`: load, cost, or reliability pressure seen
   while measuring → `maintainer`.
**Stop conditions**

- `NEEDS_CONTEXT`: no metric/target can be determined, or analytics needed to
  rank are unavailable and cannot be inferred.
- `BLOCKED`: a scaffolded variant fails verification and cannot be fixed locally.
**Final status**
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: RANKED | SCAFFOLDED
SUMMARY: <metric + winner + flag/verification if implemented>
NEXT: approve the experiment, or human review then /bbs:create-pr
```
