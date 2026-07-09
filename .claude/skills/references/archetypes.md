# The five archetypes
Babysit maps work to five archetypes — the useful unit is the *shape of the
work*, not a job title. One autopilot workflow per archetype; they are also
the product lifecycle in order (prototype → build → sweep → grow → maintain).
A ticket runs one archetype end to end; the human reviews once, at the final
handoff (mid-flow stops are opt-in via `--stop-after=…`); each workflow's
handoff names the forward lifecycle edge when a signal warrants it.

| # | Archetype | Mandate | Workflow | Skills it composes |
|---|-----------|---------|----------|--------------------|
| 1 | **Prototyper** | Churn brand-new ideas; most won't ship. Learn one thing fast. | `prototyper` | `office-hours` (judge), `recon` (scout), `prototype` (spike) |
| 2 | **Builder** | Turn a prototype/idea into production-grade product and infra. | `builder` | `plan-draft`, `design-ui`, `implement`, `review-pr`, `qa` |
| 3 | **Sweeper** | Clean up the UI, simplify code and systems, unship, optimize. | `sweeper` | `sweep`, `review-pr`, `qa` |
| 4 | **Grower** | Iterate on a shipped product to improve product-market fit. | `grower` | `growth-experiment`, `conversion-fix`, `copy-rewrite`, `social-content` |
| 5 | **Maintainer** | Keep a mature system secure, reliable, fast, and efficient at scale. | `maintainer` | `maintain` (audit), `investigate` (fix), `qa` |
## Choosing
- Unvalidated hunch, no artifact yet → **Prototyper** (a *UI* mockup is not
  this — that's `design-ui` inside Builder).
- Validated idea or accepted plan → **Builder**, the default for new work.
- Code carries weight it doesn't need (dead code, duplication, unused
  feature, inconsistent UI) → **Sweeper**. Behavior must not change.
- Shipped product, underperforming funnel → **Grower**. Measure first, one
  reversible experiment.
- Mature system under production pressure (load, security, reliability,
  cost) → **Maintainer**: audit, then the smallest safe fix; big restructures
  become a designed proposal promoted via `builder`.
**Sweeper vs Maintainer** (both touch perf): Sweeper subtracts complexity,
behavior stays byte-identical, triggered by cruft; Maintainer sustains the
system under real load and may change timing/resource behavior, triggered by
scale/security/cost. "A stable, widely-used feature" is a Maintainer trigger;
if it also needs structural cleanup, run Sweeper as a separate pass.
