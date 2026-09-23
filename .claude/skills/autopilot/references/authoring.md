# Workflow Authoring
Autopilot workflows are short Markdown files under
`.claude/skills/autopilot/workflows/` or project-local `.claude/workflows/`.
## Shape
```markdown
---
workflow: example
version: 1
description: One-line purpose.
needs-state:
  ticket: required
  requirement_md: required
---

# example

## run

> produces: verdict:example

1. Read durable ticket state.
2. Invoke the smallest relevant skill.
3. Verify output.
4. Checkpoint and write a handoff.
```
## State Fields
- `ticket`: ticket identity resolved.
- `requirement_md`: non-empty requirement exists.
- `plan_md`: non-empty plan exists.
- `plan_approved`: `plan-draft` completed.
- `manifest_md`: decomposed child manifest exists.
- `origin_type`: `standalone` or `sub_ticket`.
- `commits_ahead`: commits exist beyond base.
- `branch_pushed`: remote branch exists.
Use `required`, `optional`, `present`, `absent`, numeric checks such as `1+`,
or exact string values.
## Rules
- Keep workflows declarative; skills do task-level reasoning.
- Re-read files and checkpoint before every resumed step.
- Every `##` step needs a `> produces:` directive.
- End with `STATUS`, `VERDICT`, `SUMMARY`, `LIFECYCLE`, `TRIGGER`, and `NEXT`.
- A workflow that may change production code uses autopilot's current-session
  `review-pr` then `qa` gates, persists both verdicts, repairs until they cover
  the same final tree, and requires readiness. Evidence-only outcomes do not
  fabricate code gates; any code delta takes the production path.
- Lifecycle promotion is evidence, not scope expansion. Name the observed
  trigger; use `stop` when the next archetype needs unavailable product or
  operational data.
- Emit `NEEDS_CONTEXT` only for genuinely missing human input.
- Never force-push, destroy data, or send external messages.
## Validate
```bash
./bin/bbs autopilot lint-workflow .claude/skills/autopilot/workflows/<name>.md
```
