---
workflow: builder
version: 1
description: Turn one ticket's idea, requirement, or accepted plan into a production-grade, QA-verified change on the current checkout. The default path for serial product work and foreman-dispatched child tickets; multi-ticket parent orchestration belongs to foreman.
needs-state:
  requirement_md: optional
---
# builder
The Builder archetype (see `../references/archetypes.md`). One workflow,
several modes — pick the mode from durable state each time you resume, never
from conversation memory.
### mode selection
Re-read ticket state, checkpoint, and git status; choose the **first** match:

| Mode | Condition | What it does |
|------|-----------|--------------|
| **child** | `origin_type = sub_ticket` | Implement + QA only this child's scope; hand off to the parent. |
| **implement** | `plan.md` exists, no manifest | Implement the accepted plan. |
| **build** | `requirement.md` exists, `plan.md` absent | Plan first, then implement. |
| **verify** | none of the above, but a non-base branch has commits | QA-only pass on existing work. |
A ticket with `manifest.md` is a multi-ticket project, not a builder mode:
stop with `BLOCKED` and hand it to `foreman`. If no mode matches and there is
no ticket/requirement, stop with `NEEDS_CONTEXT`.
## run
> produces: verdict:builder + qa:checked + git:committed
1. Ensure ticket and checkpoint exist; record the mode in the checkpoint.
   Prefer a valid v2 `bbs autopilot snapshot --json` packet for these facts,
   otherwise use the legacy resolver. Work happens on the current checkout —
   autopilot never cuts a branch or worktree (a foreman that dispatched you
   already placed you in one; work in place). No `.babysit/git-flow.yaml` is
   a valid `pet` policy from the resolver; never create or rewrite it while
   executing. Record missing QA configuration as a handoff warning and point
   at `$bbs:setup-project` when it matters.
2. **build mode only (skip when init already seeded `plan.md`):** run
   `plan-draft` (user-facing work routes through `design-ui`, so the plan
   carries the UI spec + prototype). Write `plan.md` unless the task is XS.
   Don't stop for plan/prototype review — the human reviews once, at the
   final handoff — unless `--stop-after=plan` was passed.
3. **build / implement / child modes:** run `implement` against the
   requirement, plan, and (child mode) only the child scope. `implement`
   leaves the working tree dirty by design — commit its output here. Skills
   are infra-isolated: they edit files; every commit in this workflow is
   autopilot's own step.
4. Run `review-pr --fix` via SKILL.md's **Automatic review / QA subagents**
   policy (applies fixes to the working tree), then persist and read back the
   verdict with `bbs ticket set-verdict --skill review-pr` and
   `bbs ticket verdict-status --skill review-pr` — the pre-push hook reads it
   when the human (or foreman) pushes.
   Verify mode may reuse review only if its persisted DONE covers the current
   change; otherwise run it too.
5. Run `qa` via the same automatic subagent policy, after review fixes are
   integrated, against the requirement's acceptance criteria, the plan's
   `**Verify:**` line, and the implement handoff — not just the diff. The
   `qa` skill owns the test surface: on a normal checkout it tests the
   running dev server directly; inside a ticket worktree it runs the
   surface lease/compose protocol itself (see `qa` SKILL.md § Flow step 2 and
   `../references/worktrees.md`). No runnable target → record the blocker
   and run the strongest fallback (`browse` for UI, else a narrow local
   check). Commit any QA fixes, then persist the verdict with
   `bbs ticket set-verdict --skill qa`.
6. Commit everything. Autopilot stops here: never push, land, or open a PR —
   close-out is the human's `create-pr`, or the dispatching foreman's finish
   policy.
7. Write a handoff: mode, branch, changed files, deviations from the plan
   (the implement handoff's `## Deviations`), prototype path when `design-ui`
   produced one, QA evidence, concerns, next action — and, when a signal
   warrants, the forward lifecycle edge after `create-pr` (leftover cruft →
   `sweeper`; surface now live and measurable → `grower`). Child mode targets
   the parent foreman run. Cross-repo: list every touched repo with its
   branch. Write it so a non-technical owner can act: lead with what was
   built and where to see it (URL), and give the next action as a copy-paste
   command. Confirm clean state first: no debug leftovers, nothing
   uncommitted, checkpoint current.
### Foreman-dispatched children
`origin.type=sub_ticket` scopes child mode; the current checkout is already the
branch/worktree foreman prepared. Never derive or check out a child branch in
this workflow. The child handoff targets its parent and leaves dependency
integration, project QA, and close-out to foreman.
### Cross-repo tasks (related repos)
`setup-project` records siblings: meaning in `AGENTS.md` § Related Repos,
machine-local paths in the workspace registry (`bbs config workspace show`), which
is the authority; `RELATED_*_REPO` in `.babysit/.env` is the fallback for
repos outside a workspace. **Prefer the
current repo** — cross into a sibling only for the slice that genuinely
cannot be done here, and do the minimum there. Steps 4–5 are repo-relative:
review and QA each repo's change against *its own* base, once per repo touched.
1. Resolve the path (`bbs config workspace show`, else `grep '^RELATED_'
   .babysit/.env`). Unset path, or
   sibling has no `.babysit/git-flow.yaml` → don't guess: `NEEDS_CONTEXT`
   naming the repo and its slice of the requirement.
2. Work the sibling inside one subshell so its identity never leaks back
   into the parent repo (autopilot never cuts, even in a sibling):
   ```bash
   (
     cd "$sibling"
     ENSURE_OUT=$(bbs ticket ensure --no-branch)
     export BABYSIT_TICKET=$(printf '%s\n' "$ENSURE_OUT" | sed -n 's/^TICKET=//p')
     # … all sibling-side implement / bbs ticket / qa calls run here …
   )
   ```
   Then link both sides:
   `bbs ticket set-sibling --role <fe|be|shared> --repo <name> --ticket <id>`.
3. Run `qa` in each repo you touched; the `qa` skill owns that repo's
   surface protocol. Persist that repo's result on the sibling ticket
   (`BBS_TICKET=<sibling> bbs ticket set-verdict --skill qa`).
4. The handoff lists every touched repo with its branch.
**Stop conditions**

- `NEEDS_CONTEXT`: missing requirement, missing credentials, or a human-only
  decision (no plan to implement and none can be drafted safely).
- `BLOCKED`: QA/verification fails and cannot be fixed locally, merge
  conflict, or no changes produced.
**Final status**
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: BUILT
SUMMARY: <mode, branch, files, QA evidence>
NEXT: human review, then /bbs:create-pr
```
