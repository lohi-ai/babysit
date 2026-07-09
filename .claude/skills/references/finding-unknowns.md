# Finding Unknowns

The requirement and plan are a *map*; the codebase is the *territory*. Every
divergence is an unknown, and unknowns are where unattended runs land wrong
code — cheap to find early, a revert to find late. The
[Auto-Decision Framework](auto-decision-framework.md) routes what you find;
this file is about finding it.

| Quadrant | Autonomous action |
|----------|-------------------|
| Known knowns (stated) | Confirm against the code; don't re-derive |
| Known unknowns (open decisions) | List explicitly; route each through the ADF |
| Unknown knowns (unwritten quality bars) | Surface via a reactable artifact at a checkpoint (prototype, plan, QA screenshots) — never a mid-run question |
| Unknown unknowns | **The main target** — dig them out of the territory with evidence |

**Evidence or cut:** every claimed unknown must cite a file path, commit,
doc, or search result — uncited risks are boilerplate ("handle errors"),
cut them. A few sharp cited items beat twenty vague ones.

**Git archaeology** — whoever last did a similar task left a checklist: the
most recent similar commit's file footprint (`git log --grep` +
`git show --stat`) is a ready-made touchpoint list, and a "fix"/"polish"
commit shortly after it is a pothole map — the exact spots where their plan
diverged from reality.

**Durability:** unknowns found mid-run die with the context window — write
them into ticket artifacts (`requirement.md` open-decision list, `plan.md`,
a handoff's `## Deviations`), never only in conversation.

Per phase: requirement seeding lists open decisions instead of papering over
them; `plan-draft` runs the blindspot pass and leads with volatile decisions;
`implement` logs deviations rather than absorbing them; `qa` derives the
change's reach independently of the producer's claims; `investigate` checks
history for prior fixes in the failing area before theorizing.
