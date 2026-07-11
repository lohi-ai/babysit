---
workflow: builder
version: 1
description: Turn an idea, requirement, or accepted plan into a production-grade, QA-verified branch. The default path for new product work; absorbs full-build, plan-only, implement, orchestrate, sub-ticket, and verify-only modes by reading state.
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
| **orchestrate** | `manifest.md` exists | Run each child ticket through `builder` (child mode), merge into the parent, QA the integrated parent. |
| **implement** | `plan.md` exists, no manifest | Implement the accepted plan. |
| **build** | `requirement.md` exists, `plan.md` absent | Plan first, then implement. |
| **verify** | none of the above, but a non-base branch has commits | QA-only pass on existing work. |
If none match and there is no ticket/requirement, stop with `NEEDS_CONTEXT`.
## run
> produces: verdict:builder + qa:checked + git:branch-ready
1. Ensure ticket, branch, and checkpoint exist; record the mode in the
   checkpoint.
   **Bootstrap gate:** `bbs-autopilot probe` reporting
   `state_repo_configured=0` — no `.babysit/git-flow.yaml` at the git
   toplevel — is not a stop: branch policy is mechanical, and a
   non-technical invoker can't answer it. Seed the documented defaults and
   keep going:
   ```bash
   TOP=$(git rev-parse --show-toplevel)
   BASE=$(git symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null | sed 's|^origin/||')
   [ -n "$BASE" ] || for b in main master; do git show-ref -q --verify "refs/heads/$b" && BASE=$b && break; done
   git remote get-url origin >/dev/null 2>&1 && PUSH=true || PUSH=false
   mkdir -p "$TOP/.babysit"
   printf 'base_branch: %s\nbranch_prefix: feat\npush: %s\nmode: branch\n' \
     "${BASE:-main}" "$PUSH" > "$TOP/.babysit/git-flow.yaml"
   ```
   Record the seeded defaults in the handoff and recommend
   `/bbs:setup-project` for the QA harness (`qa.yaml`, credentials) — that
   part is not guessable. `state_landing_doc=0` (no CLAUDE.md or AGENTS.md)
   stays a warning to note in the handoff, not a stop.
2. **build mode only (skip when init already seeded `plan.md`):** run
   `plan-draft` (user-facing work routes through `design-ui`, so the plan
   carries the UI spec + prototype). Write `plan.md` unless the task is XS.
   Don't stop for plan/prototype review — the human reviews once, at the
   final handoff — unless `--stop-after=plan` was passed.
3. **build / implement / child modes:** run `implement` against the
   requirement, plan, and (child mode) only the child scope. `implement`
   leaves the working tree dirty by design — commit its output here. Skills
   are infra-isolated: every branch, commit, land, and push in this workflow
   is autopilot's own step, never a skill's.
4. **orchestrate mode:** run each child in manifest order via `builder`,
   checkpoint each merged child, then merge completed children into the parent.
5. Run `review-pr --fix` (applies fixes to the working tree) and persist the
   verdict with `bbs-ticket set-verdict --skill review-pr` — the push gate
   reads it. (Skip in verify mode.)
6. Run `qa` against the requirement's acceptance criteria, the plan's
   `**Verify:**` line, and the implement handoff — not just the diff. QA needs
   the change on the served surface first: in-place checkouts QA directly; a
   worktree change lands via `bbs-ticket merge-base` before QA. If
   `merge-base` BLOCKs because a diverted primary isn't on base, QA in the
   worktree itself (it holds the complete change); a merge-conflict BLOCK is
   instead resolved in the worktree, committed, and `merge-base` re-run. QA
   fixes land the same way: the `qa` skill only edits files — commit its
   fixes in the worktree yourself, then re-run `merge-base` before
   re-testing. No runnable target → record the blocker and run the strongest
   fallback (`browse` for UI, else a narrow local check). Persist the verdict
   with `bbs-ticket set-verdict --skill qa`.
7. Commit and push when policy allows.
8. Write a handoff: mode, branch, changed files, deviations from the plan
   (the implement handoff's `## Deviations`), prototype path when `design-ui`
   produced one, QA evidence, concerns, next action — and, when a signal
   warrants, the forward lifecycle edge after `create-pr` (leftover cruft →
   `sweeper`; surface now live and measurable → `grower`). Child mode targets
   the parent orchestrate run. Cross-repo: list every touched repo with its
   branch. Write it so a non-technical owner can act: lead with what was
   built and where to see it (URL), and give the next action as a
   copy-paste command. Confirm clean state first: no debug leftovers,
   nothing uncommitted, checkpoint current.
### Sub-ticket branch shape
Child branches are slash-namespaced under the parent so they never collide
with the parent's own underscore branch. Load-bearing — keep both
constructions verbatim; fork the child from the parent `feat/` branch (not
base), check it out if it already exists.
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
### Cross-repo tasks (related repos)
`setup-project` records siblings: meaning in `AGENTS.md` § Related Repos,
machine-local paths in `.babysit/.env` (`RELATED_*_REPO`). **Prefer the
current repo** — cross into a sibling only for the slice that genuinely
cannot be done here, and do the minimum there. Steps 5–6 are repo-relative:
land and QA each repo's change against *its own* base, once per repo touched.
1. Resolve the path (`grep '^RELATED_' .babysit/.env`). Unset path, or
   sibling has no `.babysit/git-flow.yaml` → don't guess: `NEEDS_CONTEXT`
   naming the repo and its slice of the requirement.
2. `cd` there, `bbs-ticket ensure` (safe-cut gate applies), implement, then
   link both sides:
   `bbs-ticket set-sibling --role <fe|be|shared> --repo <name> --ticket <id>`.
3. Before QA, run `bbs-ticket merge-base` from inside the sibling worktree —
   skipping it means QA tests a stale base there. Fixes commit in the sibling
   worktree; re-run `merge-base` before re-testing.
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
