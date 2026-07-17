---
name: create-pr
description: Prepare and create a pull request from the current branch. Use when code is ready to push, the user asks for a PR, or a babysit handoff is ready for human review.
---
# create-pr
Create a reviewable PR without merging it.
## Flow
1. Inspect git status, branch, commits, and remote. Resolve `base_branch` and `mode` from `.babysit/git-flow.yaml` (fall back to the repo's default branch and `mode: branch`). Honor the mode — see below. Load repo env: `eval "$(bbs-secrets load)"`; if `GH_ACCOUNT` is set, `gh auth switch -u "$GH_ACCOUNT"` before any push or `gh pr` call (multi-account machines fail with "Repository not found" on the wrong account).
2. Read requirement, plan, implementation handoff, and verification evidence when present. Carry them into the PR body as a short reviewer explainer: context and intent, where new code meets existing behavior, deviations from the plan (implement handoff's `## Deviations`), QA evidence. End the body with a **Reviewer quiz**: 2–3 questions probing what the diff alone can't show — behavior that rides on existing code paths, the consequence of a deviation, what else the change can reach — with answers collapsed in a `<details>` block so the reviewer self-checks before merging.
3. Resolve mechanical version or changelog requirements only when the repo requires them.
4. If `origin/<base_branch>` has moved since the cut, run `bbs-ticket refresh` first (no-op when current) so CI tests the change against the latest base. Commit remaining intended changes, push the ticket branch, and open the PR against `base_branch`. If `push: false`, stop with `BLOCKED` naming the policy instead of pushing. After the PR opens, persist it: `bbs-ticket set-pointer pr <url>` — `board --pr` and `fix-pr` resolve the PR from this pointer.
5. Return the PR URL, title, summary, tests, and concerns. Cross-repo tickets: a sibling repo's change needs its own create-pr run there; list the sibling repo + branch in the summary instead of fanning out.
## Git-flow mode
The PR is always cut from the **ticket branch** and targets `base_branch`.

- **branch** (default) — the current branch is the ticket branch; push it and open the PR.
- **worktree** — the ticket branch lives in a worktree and the base checkout carries throwaway `merge-base` integration merges. Run from the worktree (`bbs-ticket resolve` gives the path); never push the base checkout, or those merges leak into the PR.
- **trunk** — work rides a shared branch that bundles other tickets, so there is no per-ticket branch to PR. Stop with `BLOCKED`: trunk mode lands on the shared branch, not via a bundled PR.
## Compose PR (multiple tickets, one PR)
When the human reviewed a composed surface (worktree mode, `bbs-ticket switch <t1> <t2> …`) and wants the set to land together: cut `compose/<date>` from `origin/<base_branch>`, `git merge --no-edit` each ticket branch in (a conflict → `BLOCKED` naming the pair; resolve on the ticket branch, not the compose branch), push it, and open one PR whose body lists every ticket with its evidence per step 2. Never push the base checkout itself — the compose branch reproduces the same merges on a PR-able branch. Run `set-pointer pr <url>` for each member ticket.
## Rules
- Never force-push.
- Do not include unrelated working-tree changes.
- Do not claim checks passed unless they ran.
- Do not merge; landing and deployment are outside this skill.
- If authentication or remote configuration is missing, emit exact setup guidance.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PR_CREATED
PR: <url>
SUMMARY: <title + checks>
NEXT: human review — pass the quiz before merging
```
