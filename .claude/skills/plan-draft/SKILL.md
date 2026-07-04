---
name: plan-draft
description: Draft a technical plan before implementation. Use when the user asks for a plan, architecture, ticket breakdown, or wants to turn a requirement into plan.md without coding yet.
---

# plan-draft

Make a short plan that a strong model can execute. Avoid ceremony.

**One pass, full depth.** Draft once — don't loop polishing wording or
re-survey — but think at full depth on that pass: the plan is the only
context `implement` inherits, and downstream gates catch bugs, not weak
architecture. Three things must be exact: the goal, the scope boundary
(what's out), and the verification commands. Details may be approximate —
`implement` corrects them in-flight — but record the *why* behind each
choice and any gotcha found while surveying; that context is cheap now and
unrecoverable later.

## Flow

1. Read the requirement and the code paths likely to change.
2. Survey what already exists before proposing anything new. For UI/frontend
   work this is mandatory in an enterprise codebase: list existing components
   (`bbs-design components`) and design tokens (`bbs-design tokens`), and find
   the nearest existing screen/flow that solves a similar problem. For backend
   work, find the established pattern for routes, data access, and errors.
   When the work adds or reshapes a user-facing surface, run `design-ui`
   (or read an existing `pointers.design`) before finalizing the plan — its
   spec and prototype are plan inputs. A plan for frontend work without a
   reviewable prototype gives the human their first look only after
   `implement`, when the change cost is highest.
3. Classify scope as XS, S, M, or L (rubric:
   `../references/ticket-size-rubric.md`) and persist it:
   `bbs-ticket set-pointer ticket_size <size>`.
4. Call out ambiguity only when choosing silently risks wrong code.
5. Write the approach as architecture and basic design: how the change solves
   the user's requirement — data flow, where logic lives, component/API
   boundaries, schema shape — not a step-by-step task list (`implement` owns
   task order). Name the specific existing components, tokens, and patterns
   the plan reuses; flag any genuinely new component/pattern and justify why
   no existing one fits. Consistency with the current product is the default.
   When the plan adds or reshapes an API, specify the contract to best
   practice up front: list endpoints get pagination, filtering, and sorting
   from day one (retrofitting them breaks clients); inputs are validated with
   a clear error shape; outputs follow the project's existing envelope,
   naming, and status-code conventions.
6. For L work, split into ordered sub-tickets with independent verification.
7. Before handoff, re-check the size: if ≥40% of the in-scope items ended up
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
