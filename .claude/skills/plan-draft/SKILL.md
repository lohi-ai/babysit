---
name: plan-draft
description: Draft a technical plan before implementation. Use when the user asks for a plan, architecture, ticket breakdown, or wants to turn a requirement into plan.md without coding yet.
---
# plan-draft
Make a short plan that a strong model can execute. Avoid ceremony.
**Deep survey, thin artifact.** Draft once — don't loop polishing wording or
re-survey. `plan.md` is **5–15 lines total**: only what the human must react
to and what `implement` can't re-derive — the goal, the scope boundary
(what's out), the architecture shape, cited unknowns, and the verification
commands. Task order, file lists, and step detail belong to `implement`.
Record the *why* behind a choice in one clause, not a paragraph.
## Flow
1. Read the requirement and the code paths likely to change; derive what the
   requirement doesn't say: flows sharing state or routes with the change,
   existing behavior it would alter, implied cases (permissions,
   empty/error states, concurrent edits, existing-data migration, query
   access paths at production data volume — an aggregate or filter the
   declared indexes can't bound is a plan-level decision:
   index/denormalize/cache). Carry
   each derived item into **Unknowns** (marked *derived*) — never resolve
   one silently — citing a file, commit, or doc; uncited items
   are guesses, cut them. Mine git history too — the git-archaeology recipe
   in `../references/finding-unknowns.md` turns the last similar commit into
   a touchpoint checklist and pothole map.
2. Survey what already exists before proposing anything new. For UI/frontend
   work this is mandatory in an enterprise codebase: list existing components
   (`bbs-design components`) and design tokens (`bbs-design tokens`), and find
   the nearest existing screen/flow that solves a similar problem. For backend
   work, find the established pattern for routes, data access, and errors.
   When the work adds or reshapes a user-facing surface, read the existing
   design spec from `pointers.design` if present; otherwise invoke the
   `design-ui` skill via the Skill tool before finalizing the plan — its
   spec and prototype are plan inputs.
3. Classify scope as XS, S, M, or L (rubric:
   `../references/ticket-size-rubric.md`) and persist it:
   `bbs-ticket set-pointer ticket_size <size>`.
4. Write **Approach** as architecture, not a task list (`implement` owns task
   order): data flow, where logic lives, component/API boundaries, schema
   shape — plus the existing components/tokens/patterns reused; flag a
   genuinely new one with a one-clause why. A new or reshaped API's contract
   is one line here (`implement` fills in best practice). Lead with what a
   human is most likely to tweak: data model, API/type contracts,
   user-facing behavior.
5. For L work, split into ordered sub-tickets with independent verification.
6. Before handoff, re-check the size: if ≥40% of the in-scope items ended up
   deferred to follow-up tickets, downgrade `ticket_size` one tier using the
   downgrade hook in `../references/ticket-size-rubric.md` (it writes the
   audit-log line).
## Plan Format
```markdown
# Plan
**Goal:** <one line>
**Out of scope:** <one line>
**Approach:** <2–4 lines — data flow, boundaries, where logic lives; reuse; volatile decisions first>
**Unknowns:** <2–5 cited bullets — derived scope, risks, potholes; the plan's main value>
**Verify:** <exact commands or checks>
**Design:** <FE work only — design.md pointer + prototype path>
```
5–15 lines total. Concrete file paths and commands, no process language —
a plan the human reads in 30 seconds beats one they skip.
## Native plan mode
Developer session already in plan mode → present the finished draft through
`ExitPlanMode` (native approval is the "plan accepted" checkpoint) and do the
writes — `plan.md`, `set-pointer` — after approval, since plan mode blocks
them. Unattended runs never enter plan mode; `plan.md` on disk is the
accepted plan.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PLANNED(<XS|S|M|L>) | DECOMPOSED(<N>)
PLAN: <path or inline summary>
NEXT: implement or ask for missing context
```
