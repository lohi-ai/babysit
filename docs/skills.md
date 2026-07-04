# Skill index

Skills carry specialized workflows; Opus handles ordinary judgment inline.
The pack is organized around the [five archetypes](../.claude/skills/references/archetypes.md)
of a product-building team — pick by the shape of the work, not the job title.

| Archetype | Skills |
|-----------|--------|
| Prototyper | `office-hours`, `recon`, `prototype` |
| Builder | `autopilot`, `plan-draft`, `design-ui`, `implement`, `review-pr`, `qa`, `browse`, `create-pr` |
| Sweeper | `sweep`, `review-pr`, `qa` |
| Grower | `conversion-fix`, `copy-rewrite`, `growth-experiment`, `social-content` |
| Maintainer | `maintain`, `investigate`, `qa`, `analytics-review`, `triage` |
| Setup | `setup-project` |

## Archetype workflows

`autopilot` runs exactly one workflow per archetype. Name the archetype, or let
`autopilot` route production work to `builder` from ticket state.

| Workflow | Use when | Stops at |
|----------|----------|----------|
| `/bbs:autopilot prototyper "<idea>"` | Validate a risky assumption with a throwaway spike | learning verdict |
| `/bbs:autopilot builder "<requirement>"` | Build new production work (auto-selects build/plan/implement/orchestrate/verify) | QA-verified branch |
| `/bbs:autopilot sweeper` | Simplify / unship / optimize without changing behavior | QA-verified branch |
| `/bbs:autopilot grower "<metric>"` | Rank or scaffold a growth experiment | ranked plan or scaffolded variant |
| `/bbs:autopilot maintainer` | Audit security/deps/reliability/scale, or root-cause a bug | hardened/fixed branch |

## Which skill when

| Need | Skill |
|------|-------|
| End-to-end checkpointed work through QA handoff | `/bbs:autopilot` |
| Plan without coding | `/bbs:plan-draft` |
| Implement a scoped change | `/bbs:implement` |
| Validate a risky idea with a throwaway spike | `/bbs:prototype` |
| Simplify, unship, or optimize without changing behavior | `/bbs:sweep` |
| Audit security, deps, reliability, or scale and harden | `/bbs:maintain` |
| Root-cause a bug | `/bbs:investigate` |
| Turn babysit telemetry into ticket-ready findings | `/bbs:analytics-review` |
| Classify and unblock a stalled/BLOCKED run | `/bbs:triage` |
| Focused browser check | `/bbs:browse` |
| Full test/fix browser loop | `/bbs:qa` |
| Pre-landing code review | `/bbs:review-pr` |
| Create a pull request after human review | `/bbs:create-pr` |
| Product or UI ideation | `/bbs:office-hours`, `/bbs:design-ui` |
| Configure repo | `/bbs:setup-project` |
| Evaluate external code | `/bbs:recon` |
| Marketing and growth | `/bbs:conversion-fix`, `/bbs:copy-rewrite`, `/bbs:growth-experiment`, `/bbs:social-content` |
