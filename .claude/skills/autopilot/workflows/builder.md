---
workflow: builder
version: 1
description: Turn an idea, requirement, or accepted plan into a production-grade, QA-verified branch. The default path for new product work; absorbs full-build, plan-only, implement, orchestrate, sub-ticket, and verify-only modes by reading state.
needs-state:
  requirement_md: optional
---

# builder

The Builder archetype (see `../references/archetypes.md`). One workflow, several
modes — pick the mode from durable state each time you resume, never from
conversation memory.

### mode selection

Re-read ticket state, checkpoint, and git status, then choose the **first**
matching mode:

| Mode | Condition | What it does |
|------|-----------|--------------|
| **child** | `origin_type = sub_ticket` | Implement + QA only this child's scope; hand off to the parent. |
| **orchestrate** | `manifest.md` exists | Run each child ticket through `builder` (child mode), merge into the parent, QA the integrated parent. |
| **implement** | `plan.md` exists, no manifest | Implement the accepted plan. |
| **build** | `requirement.md` exists, `plan.md` absent | Plan first, then implement. |
| **verify** | none of the above, but a non-base branch has commits | QA-only pass on existing work. |

If none match and there is no ticket/requirement, stop with `NEEDS_CONTEXT`.

## run

> produces: verdict:builder + qa:checked + git:branch-ready

1. Ensure ticket, branch, and checkpoint exist. Determine the mode above and
   record it in the checkpoint.
   **Readiness gate:** if `bbs-autopilot probe` reports
   `state_repo_configured=0` — no `.babysit/git-flow.yaml` at the git toplevel —
   the repo isn't set up for unattended code-writing work. Stop with
   `NEEDS_CONTEXT` recommending `/bbs:setup-project` instead of guessing
   branch/QA policy. `state_landing_doc=0` (no CLAUDE.md or AGENTS.md)
   is a warning to note in the handoff, not a stop.
2. **build mode only (skip when autopilot init already seeded `plan.md`):**
   run `plan-draft`; for user-facing work it routes
   through `design-ui`, so the plan carries the UI spec and a reviewable
   prototype. Write/update `plan.md` unless the task is XS. Do not stop for
   prototype or plan review — the human reviews everything once, at the final
   handoff. Stop here only if the invocation passed `--stop-after=plan`.
3. **build / implement / child modes:** run `implement` against the requirement,
   plan, and (for child mode) only the child scope.
4. **orchestrate mode:** run each child in manifest order via `builder`,
   checkpoint each merged child, then merge completed children into the parent.
5. Run `review-pr`; fix mechanical findings and persist the verdict with
   `bbs-ticket set-verdict --skill review-pr` — the push gate reads it.
   (Skip in verify mode.)
6. Run `qa` against the requirement's acceptance criteria, the plan's
   `## Verification`, and the implement handoff — not just the diff.
   When the ticket lives in a worktree and the dev server runs in
   the primary checkout, land the branch there first with
   `bbs-ticket merge-base` (BLOCKED on conflict — resolve in the worktree);
   QA fixes commit in the worktree, then re-run `merge-base` before
   re-testing. If full QA has no runnable target, record the blocker and run
   the strongest fallback (`browse` for UI, else a narrow local check). Persist the
   verdict with `bbs-ticket set-verdict --skill qa`.
7. Commit and push when policy allows.
8. Write a handoff: mode, branch, changed files, prototype path (when
   `design-ui` produced one — the human reviews it here, not mid-flow), QA
   evidence, concerns, next action. When a signal warrants, name the forward
   lifecycle edge after `create-pr`: cruft deliberately left behind →
   `sweeper`; surface now live and measurable → `grower`.
   In child mode the handoff targets the parent orchestrate run. For a
   cross-repo change, list every touched repo with its branch — a handoff
   naming only one repo is incomplete.
   Before writing it, confirm clean state: diff free of debug leftovers,
   nothing uncommitted, checkpoint current.

### Sub-ticket branch shape

Child branches are slash-namespaced under the parent
(`feat/<parent>/<NNN>_<child-id>_<slug>`) so they never collide with the
parent's own underscore branch (`feat/<parent>_<slug>`). The shape is
load-bearing — keep these two constructions verbatim.

```bash
# orchestrate mode (dispatch side): build each child branch from the manifest.
# $CHILD is the decomposition's child id; $POS is the 3-digit seed index.
CHILD_BRANCH="feat/${TICKET}/${POS}_${CHILD}_${SLUG}"
```

```bash
# child mode (worker side): re-derive this child's own branch from the parent.
# $PARENT_ID is the parent ticket; $TICKET is this child's id.
CHILD_BRANCH="feat/${PARENT_ID}/${POS}_${TICKET}_${SLUG}"
```

Fork the child from the parent `feat/` branch (not base); check it out if it
already exists.

### Cross-repo tasks (related repos)

A task that spans a sibling repo (e.g. FE and BE in separate checkouts):
`setup-project` records siblings — meaning in
`AGENTS.md` § Related Repos, machine-local paths in `.babysit/.env`
(`RELATED_*_REPO`). When the requirement or plan needs changes in a related
repo:

1. Resolve its path (`grep '^RELATED_' .babysit/.env`). If the path exists
   and that repo has its own `.babysit/git-flow.yaml`, work there directly:
   cd in, run `bbs-ticket ensure` (the safe-cut gate cuts a worktree off that
   repo's base), implement + QA with the same loop (`merge-base`, fixes in
   the worktree), and link the tickets from each side with
   `bbs-ticket set-sibling --role <fe|be|shared> --repo <name> --ticket <id>`.
2. If the path is unset/absent, or the repo isn't configured for autonomous
   runs, don't guess: stop with `NEEDS_CONTEXT` naming the repo and the slice
   of the requirement it owns, so the human fills `.babysit/.env` or runs
   `/bbs:autopilot` in that repo themselves.
3. The handoff lists every touched repo with its branch.

**Stop conditions**

- `NEEDS_CONTEXT`: missing requirement, missing credentials, or a human-only
  decision (no plan to implement and none can be drafted safely).
- `BLOCKED`: QA/verification fails and cannot be fixed locally, merge conflict,
  or no changes produced.

**Final status**

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: BUILT
SUMMARY: <mode, branch, files, QA evidence>
NEXT: human review, then /bbs:create-pr
```
