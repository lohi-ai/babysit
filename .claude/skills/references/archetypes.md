# The five archetypes

Babysit is the product-building team. As engineering, product, design, and DS
melt into one role, the useful unit of work is no longer the job title — it's
the *archetype* the work needs right now. Babysit maps the Claude Code team's
five archetypes onto skills and autopilot workflows so a single run can act as
whichever teammate the task calls for.

A person spans 2–3 archetypes; so does a babysit run. Pick by the *shape of the
work*, not the file's "type."

There is exactly **one autopilot workflow per archetype**. Each composes the
skills its mandate needs; `builder` additionally selects an internal mode
(child / orchestrate / implement / build / verify) from ticket state.

The archetypes are also the **development lifecycle**, in order: prototype →
build → sweep → grow → maintain. A single ticket runs one archetype end to
end; the *product* moves through them over time. Each workflow's handoff names
the forward edge when a signal warrants it (prototyper → builder promotion,
builder leaving sweep candidates, a now-measurable surface → grower, scale
pressure → maintainer). Within a run, the human reviews **once, at the final
handoff** — prototypes, plans, and intermediate artifacts ride along to that
checkpoint instead of stopping the flow (mid-flow stops are opt-in via
`--stop-after=…`).

| # | Archetype | Mandate | Workflow | Skills it composes |
|---|-----------|---------|----------|--------------------|
| 1 | **Prototyper** | Churn brand-new ideas; most won't ship. Learn one thing fast. | `prototyper` | `office-hours` (judge), `recon` (scout), `prototype` (spike) |
| 2 | **Builder** | Turn a prototype/idea into production-grade product and infra. | `builder` | `plan-draft`, `design-ui`, `implement`, `review-pr`, `qa` |
| 3 | **Sweeper** | Clean up the UI, simplify code and systems, unship, optimize. | `sweeper` | `sweep`, `review-pr`, `qa` |
| 4 | **Grower** | Iterate on a shipped product to improve product-market fit. | `grower` | `growth-experiment`, `conversion-fix`, `copy-rewrite`, `social-content` |
| 5 | **Maintainer** | Keep a mature system secure, reliable, fast, and efficient at scale. | `maintainer` | `maintain` (audit), `investigate` (fix), `qa` |

## Choosing an archetype

- **No artifact exists yet, just a hunch** → Prototyper. Validate before you
  commit to building. `office-hours` for "is this worth it?", `prototype` for
  "does this even work?". Note: a *UI* prototype (mockup of a screen) is not
  this archetype — that's `design-ui` inside Builder, answering "does this
  look right?"; Prototyper spikes test technical/product assumptions.
- **A validated idea or accepted plan exists** → Builder. The default for new
  feature work; `/bbs:autopilot builder "<requirement>"`.
- **The code carries weight it doesn't need** — dead code, duplication,
  over-abstraction, an unused feature, inconsistent UI → Sweeper. Subtract.
  Behavior must not change; tests pass before and after. Triggered by internal
  cruft at *any* maturity, not by scale.
- **The product ships but the funnel underperforms** → Grower. Measure first,
  then scaffold one reversible experiment.
- **A mature system is under production/scale pressure** — load, security
  exposure, reliability incidents, cost → Maintainer. Keep it secure, reliable,
  fast, and cheap *as usage grows*: audit, then apply the smallest safe fix
  (db schema/indexes/partitioning, caching, batching, async background
  processing, query/data optimization, hardening — and architecture changes
  when the structure itself can't absorb the change or scale; big
  restructures become a designed proposal promoted via `builder`).

### Sweeper vs Maintainer

The easy pair to confuse — both touch performance. The tie-breaker is
**behavior-preservation and trigger**:

- **Sweeper** optimizes for the *codebase*: subtract complexity, behavior stays
  byte-identical. Perf comes *for free* from removing wasteful work. Triggered
  by accumulated cruft.
- **Maintainer** optimizes for the *system in production*: sustain it under real
  load. Scaling work (caching, indexing, capacity, data-model changes) may
  change timing/resource behavior. Triggered by scale, security, or cost signals.

"A stable, widely-used feature" is a **Maintainer** trigger. If it *also* needs a
structural cleanup, run Sweeper as a separate behavior-preserving pass — don't
fold a scaling change into a Sweeper run, or its no-behavior-change guard fights
the optimization.

## Invariants every archetype keeps

These hold no matter which teammate the run is acting as:

- Decisions route through [auto-decision-framework.md](auto-decision-framework.md);
  taste decisions are logged, never silently guessed.
- Self-verification before "done" — type-check, tests, or a browser check.
- Bounded blast radius — no force-push, no data loss, no external messages
  without durable authorization.
- Fail loud, fail local — stop with `BLOCKED`/`NEEDS_CONTEXT` over a wrong
  assumption.

The archetypes differ only in *mandate and success criterion*, not in how they
escalate or how carefully they verify.
