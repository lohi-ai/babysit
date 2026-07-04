---
name: create-pr
description: Prepare and create a pull request from the current branch. Use when code is ready to push, the user asks for a PR, or a babysit handoff is ready for human review.
---

# create-pr

Create a reviewable PR without merging it.

## Flow

1. Inspect git status, branch, base branch, commits, and remote.
2. Read requirement, plan, implementation handoff, and verification evidence when present.
3. Resolve mechanical version or changelog requirements only when the repo requires them.
4. Commit remaining intended changes, push the branch, and create the PR.
5. Return the PR URL, title, summary, tests, and concerns.

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
