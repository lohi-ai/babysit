---
name: plan-draft
description: Draft a technical plan before implementation. Use when the user asks for a plan, architecture, ticket breakdown, or wants to turn a requirement into plan.md without coding yet.
---
# plan-draft
Make a short plan that a strong model can execute. Avoid ceremony.
**One pass, full depth.** Draft once — don't loop polishing wording or
re-survey. Three things must be exact: the goal, the scope boundary
(what's out), and the verification commands. Details may be approximate —
`implement` corrects them in-flight — but record the *why* behind each
choice and any gotcha found while surveying.
## Flow
1. Read the requirement and the code paths likely to change; derive what the
   requirement doesn't say: flows sharing state or routes with the change,
   existing behavior it would alter, implied cases (permissions,
   empty/error states, concurrent edits, existing-data migration). Carry
   each derived item into `## Scope` (marked *derived*) or `## Risks` —
   never resolve one silently — citing a file, commit, or doc; uncited items
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
4. Write the approach as architecture and basic design: how the change solves
   the user's requirement — data flow, where logic lives, component/API
   boundaries, schema shape — not a step-by-step task list (`implement` owns
   task order). Name the specific existing components, tokens, and patterns
   the plan reuses; flag any genuinely new component/pattern and justify why
   no existing one fits. A new or reshaped API gets its contract specified to best
   practice up front (pagination on list endpoints from day one, validated
   inputs, the project's existing envelope and status-code conventions).
   Order the plan body by volatility: decisions a human is most likely to
   tweak first — data model, API/type contracts, user-facing behavior —
   mechanical refactoring last.
5. For L work, split into ordered sub-tickets with independent verification.
6. Before handoff, re-check the size: if ≥40% of the in-scope items ended up
   deferred to follow-up tickets, downgrade `ticket_size` one tier using the
   downgrade hook in `../references/ticket-size-rubric.md` (it writes the
   audit-log line).
## Plan Format
```markdown
# Plan

## Goal
## Scope
## Approach     # architecture & basic design: data flow, boundaries, where logic lives
## Design       # FE work only: design.md pointer + prototype path for early review
## Reuse        # existing utils/components/tokens/patterns to follow; new ones + why
## Files
## Verification
## Risks
## Next
```
Keep each section brief. Prefer concrete file paths and commands over process language.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PLANNED(<XS|S|M|L>) | DECOMPOSED(<N>)
PLAN: <path or inline summary>
NEXT: implement or ask for missing context
```
