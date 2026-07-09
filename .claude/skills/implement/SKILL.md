---
name: implement
description: Implement a scoped code change from the user's request, an accepted plan.md, or ticket context. Use for feature work, bug fixes, endpoints, UI changes, integrations, and contained refactors.
---
# implement
Build the smallest correct change; this file only sets the babysit-specific
guardrails.
- Read the request, plan (including its `## Reuse` and `## Design` sections),
  and nearby code before editing. If the plan's `## Files` list has collapsed
  to ≤3 entries of trivial doc/comment-only work, downgrade `ticket_size` one
  tier using the downgrade hook in `../references/ticket-size-rubric.md` (it
  writes the audit-log line).
- Reuse before writing: before creating any util, helper, or component, check
  the plan's `## Reuse` section, then grep shared/lib/util dirs and the
  nearest similar feature. A new shared util or abstraction is a plan
  decision, not an ad-hoc call.
- UI: reuse the design system — components, tokens (`bbs-design tokens`; if
  CLAUDE.md/AGENTS.md declares a design doc at a non-root path, pass
  `--design <path>` and treat it as authoritative), spacing, interaction
  patterns. The prototype behind `pointers.design` is the accepted look —
  build to it. No new one-off component, color, font size, or layout when the
  project has one. New user-facing surface with no design spec
  (`pointers.design` empty, nothing in conversation) → invoke the `design-ui`
  skill via the Skill tool first; never improvise a layout the human first
  sees after implementation.
- API surfaces follow best practice by default, even when the plan is silent
  — an unpaginated list endpoint is a bug, not a simplification.
- When an edge case forces a deviation from the plan: pick the conservative
  option — smallest change, most reversible, preserves the plan's intent —
  log it (see Ticket Mode), and keep going. Never block on a reversible
  choice; never silently absorb a deviation.
- Verify with the narrowest meaningful command (tests, typecheck, lint,
  build, or browser check) and summarize changed files, verification, and
  remaining risk.
## Ticket Mode
When running inside babysit, read `requirement.md`, `plan.md`, and the checkpoint if present. Write concise handoff notes for what changed and how it was verified, plus a `## Deviations` section when any occurred — one entry each:
```
- **<short title>** — Plan said: <expectation> · Found: <reality — cite file/symbol> · Chose: <option> — because <one line>
```
`qa` seeds test cases from this section and the final handoff surfaces it.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: BUILT | FIXED | CHANGED
SUMMARY: <files changed + verification>
NEXT: <human next action or "none">
```
