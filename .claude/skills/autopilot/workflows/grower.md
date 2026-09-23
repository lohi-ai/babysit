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
> produces: verdict:grower
1. Read product positioning, the target surface, and actual funnel or analytics
   evidence. Name the baseline, target metric, activation moment, and exposure
   unit. Missing product data may still yield an instrumentation-first
   experiment, but never a fabricated market-fit conclusion.
2. Run the matching growth skill: `growth-experiment` to rank experiments,
   `conversion-fix` for an activation surface, `copy-rewrite` for positioning,
   `social-content` for acquisition. Pick by the invocation; default to
   `growth-experiment`.
3. If asked to implement, scaffold the smallest flagged, reversible variant with
   exposure and conversion tracking. An unmeasurable variant is `BLOCKED`, not
   shipped. Otherwise stop at the ranked recommendation.
4. If code changed, in the current autopilot session run `review-pr --fix` and
   then `qa` under its gate policy. Persist and read back both verdicts, apply the
   **Repair until the final change passes** loop, and require
   `bbs ticket readiness --action review --json` to report `data.ready=true`
   for the final committed tree. Use `bbs ticket set-verdict --skill review-pr`
   and `bbs ticket set-verdict --skill qa`; completion prose is not evidence.
5. Commit any scaffolded variant locally. Autopilot never pushes, lands, or
   opens a PR — close-out is the human's `create-pr` (or the dispatching
   foreman's).
6. Write a handoff: metric, winning experiment + constraining assumption, and
   what shipped behind which flag. When a signal warrants, name the forward
   lifecycle edge after `create-pr`: load, cost, or reliability pressure seen
   while measuring → `maintainer`; characterized removable weight or a measured
   hot path → `sweeper`. A ranked recommendation without observed
   outcome stays in `grower`; it does not claim market fit.
**Stop conditions**

- `NEEDS_CONTEXT`: no metric/target can be determined, or analytics needed to
  rank are unavailable and cannot be inferred.
- `BLOCKED`: a scaffolded variant fails verification and cannot be fixed locally.
**Final status**
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: RANKED | SCAFFOLDED
SUMMARY: <metric + winner + flag/verification if implemented>
LIFECYCLE: grower -> grower | maintainer | sweeper | stop
TRIGGER: <experiment result needed, operational signal, characterized cruft, or explicit stop evidence>
NEXT: if RANKED, collect the experiment result; if SCAFFOLDED, human review + /bbs:create-pr
```
