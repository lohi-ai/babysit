---
name: create-pr
description: Prepare and create a pull request from the current branch. Use when code is ready to push, the user asks for a PR, or a babysit handoff is ready for human review.
---

# create-pr

Create a reviewable PR without merging it.

## Flow

1. Inspect git status, branch, commits, and remote. Resolve `base_branch` and `mode` from `.babysit/git-flow.yaml` (fall back to the repo's default branch and `mode: branch`). Honor the mode — see below.
2. Read requirement, plan, implementation handoff, and verification evidence when present.
3. Resolve mechanical version or changelog requirements only when the repo requires them.
4. Commit remaining intended changes, push the ticket branch, and open the PR against `base_branch`. If `push: false`, stop with `BLOCKED` naming the policy instead of pushing.
5. Return the PR URL, title, summary, tests, and concerns.

## Git-flow mode

The PR is always cut from the **ticket branch** and targets `base_branch`.

- **branch** (default) — the current branch is the ticket branch; push it and open the PR.
- **worktree** — the ticket branch lives in a worktree and the base checkout carries throwaway `merge-base` integration merges. Run from the worktree (`bbs-ticket resolve` gives the path); never push the base checkout, or those merges leak into the PR.
- **trunk** — work rides a shared branch that bundles other tickets, so there is no per-ticket branch to PR. Stop with `BLOCKED`: trunk mode lands on the shared branch, not via a bundled PR.

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
NEXT: human review
```
