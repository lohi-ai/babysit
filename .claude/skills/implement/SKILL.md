---
name: implement
description: Implement a scoped code change from the user's request, an accepted plan.md, or ticket context. Use for feature work, bug fixes, endpoints, UI changes, integrations, and contained refactors.
---

# implement

Build the smallest correct change. Let the model do the detailed reasoning; this file only sets the guardrails.

## Flow

1. Read the request, plan (including its `## Reuse` and `## Design` sections),
   and nearby code before editing. For UI work, look at how similar screens are
   already built, and read the design spec + prototype (`pointers.design`)
   when present — the prototype is the accepted look; build to it.
   If the plan's `## Files` list has collapsed to ≤3 entries of trivial
   doc/comment-only work, downgrade `ticket_size` one tier using the downgrade
   hook in `../references/ticket-size-rubric.md` (it writes the audit-log line).
2. State a short success criterion when the task spans more than one step.
3. Make surgical changes that match the repo's style.
4. Verify with the narrowest meaningful command: tests, typecheck, lint, build, or browser check.
5. Summarize changed files, verification, and remaining risk.

## Guardrails

- Reuse before writing: before creating any util, helper, or component,
  check the plan's `## Reuse` section, then grep shared/lib/util dirs and the
  nearest similar feature for one that already exists. Follow the established
  pattern for the layer being touched (routes, data access, errors, state).
  A new shared util or abstraction is a plan decision, not an ad-hoc call.
- Reuse the existing design system for UI: existing components, design tokens
  (`bbs-design tokens`; if CLAUDE.md/AGENTS.md declares a design doc at a
  non-root path, pass it with `--design <path>` and treat it as authoritative),
  spacing, and interaction patterns. Do not introduce a
  new one-off component, color, font size, or layout when the project already
  has one — enterprise products need a consistent UI/UX. A genuinely new
  component is a plan decision (the `## Reuse` section), not an ad-hoc call.
- New user-facing surface with no design spec or prototype (`pointers.design`
  empty, nothing in conversation)? Invoke the `design-ui` skill via the Skill
  tool (skill: `design-ui`) first — it produces the spec and a reviewable
  prototype — then build to it. Do not improvise a
  layout the human sees for the first time after implementation.
- API surfaces follow best practice by default, even when the plan is silent:
  list endpoints ship with pagination, filtering, and sorting; inputs are
  validated with a clear error shape; outputs match the project's existing
  envelope, naming, and status-code conventions. An unpaginated list endpoint
  is a bug, not a simplification.
- Do not add speculative options, broad refactors, or cleanup unrelated code.
- If requirements conflict, surface the tradeoff before coding.
- If you find a bug while implementing, fix root cause first; do not patch symptoms.
- Do not hide failing verification. Report the command and failure clearly.

## Ticket Mode

When running inside babysit, read `requirement.md`, `plan.md`, and the checkpoint if present. Write concise handoff notes for what changed and how it was verified.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: BUILT | FIXED | CHANGED
SUMMARY: <files changed + verification>
NEXT: <human next action or "none">
```
