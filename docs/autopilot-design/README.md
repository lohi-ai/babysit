# Autopilot: intelligence and token efficiency

Status: proposed technical design, not implemented. Baseline: `dac1eb5` plus the existing working-tree edits to autopilot and builder inspected during this planning session. Those edits add harness portability and independent gate workers; this proposal preserves that direction. No ticket resolved for this checkout, so these documents are repository artifacts without ticket-pointer writes.

The intended change is system-wide in its contracts, incremental in delivery. Autopilot should spend model context on understanding the product, code, and failures. The CLI should answer questions such as which ticket is active, which policy applies, what changed, and whether verification covers that change.

The simplest useful version is a typed state read and a common readiness evaluator over existing storage. Add bounded context and worker recovery on top of that. A new workflow engine, vector database, or mandatory swarm would add cost before establishing that it improves outcomes.

| Document | Purpose |
|---|---|
| [plan.md](plan.md) | Short entry point for a future implementing agent |
| [architecture.md](architecture.md) | Current evidence, ownership, execution and context design |
| [contracts.md](contracts.md) | Proposed CLI, durable schemas, freshness and concurrency semantics |
| [delivery.md](delivery.md) | Eight ordered implementation slices, compatibility and rollback |
| [evaluation.md](evaluation.md) | Quality, token measurement, regression matrix and rollout criteria |

## Design decisions

1. Keep technical judgment with the model. Go evaluates facts and prerequisites; it does not prescribe every coding step.
2. Reuse Layout C, `identity.Resolve`, git-flow resolution, evidence, locking, QA leases, and existing harness adapters. Extract shared helpers only where multiple consumers need them.
3. Save tokens by avoiding repeated instructions, stale-state investigation, full-log output, and duplicate verification. Keep acceptance criteria, constraints, unresolved risks, and mandatory skill instructions intact.
4. Bind evidence to the actual code, requirements, policy, and test surface. A short success sentence cannot authorize a release.
5. Preserve current-branch operation, direct skills without a ticket, explicit isolation requests, and durable finish authorization.
6. Make changes measurable and reversible. The thresholds below are proposed acceptance targets, not observed improvements.

An alternative is to shorten all skills first. That is a useful later step, but it leaves duplicated policy decisions and weak freshness checks in place. Another alternative is a fixed state-machine executor; this design uses a small deterministic gate evaluator while allowing the model to investigate, implement, and repair freely between gates.

## Completion criteria for the future implementation

The same canonical facts must drive CLI explanations, workflow routing, hooks, and close-out. A fresh session must recover the correct next obligation without the prior conversation. Warm sessions must avoid repeatedly loading unchanged orchestration material. A stale or failed verification must never be accepted as ready. Paired evaluations must demonstrate lower total token use without reduced completion or defect detection.
