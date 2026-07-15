---
name: fix-pr
description: Address unresolved review comments on an open pull request — fix in the ticket worktree, reply in-thread, resolve threads, push. Use after a human or bot review leaves comments on a PR.
---
# fix-pr
Work a PR's unresolved review threads to zero. One repo's PR per invocation — a cross-repo ticket's sibling PR needs its own run in that repo.
## Flow
1. Resolve the PR: `bbs-ticket get-pointer pr`, else a PR URL/number from conversation, else the current branch's PR (`gh pr view --json url,number`). None → `NEEDS_CONTEXT` naming what's missing. Resolve `GH_ACCOUNT` the same way create-pr does (`eval "$(bbs-secrets load)"` → `gh auth switch -u "$GH_ACCOUNT"` when set).
2. Fetch unresolved threads — GraphQL only; REST cannot list resolution state:
   ```bash
   gh api graphql -f query='
   query($owner:String!,$repo:String!,$pr:Int!){
     repository(owner:$owner,name:$repo){
       pullRequest(number:$pr){
         reviewThreads(first:100){ nodes{
           id isResolved isOutdated path line
           comments(first:20){ nodes{ databaseId author{login} body } } } } } } }' \
     -F owner="$OWNER" -F repo="$REPO" -F pr="$NUMBER" \
     --jq '.data.repository.pullRequest.reviewThreads.nodes[] | select(.isResolved|not)'
   ```
3. Fix each thread in the ticket's **own worktree** (`bbs-ticket resolve` gives the path), never the primary checkout. Same discipline as `review-pr --fix`: apply what's right, skip with a stated reason what isn't (wrong, out of scope, or a genuine disagreement — those go back to the reviewer as a reply, not a silent skip).
4. Fixes that change behavior get re-verified before push: re-run the surface's check, and when other tickets are in flight follow the qa-lease protocol (`qa-lease acquire` → `switch <ticket>` → check → `release`).
5. Commit in the worktree and `git push` the ticket branch.
6. Close the loop per thread: reply in-thread via REST, resolve via GraphQL (resolution is GraphQL-only):
   ```bash
   gh api "repos/$OWNER/$REPO/pulls/comments/$DATABASE_ID/replies" -f body="<what changed + commit sha, or why skipped>"
   gh api graphql -f query='mutation($t:ID!){ resolveReviewThread(input:{threadId:$t}){ thread{ id } } }' -F t="$THREAD_ID"
   ```
   Skipped-by-disagreement threads get the reply but stay unresolved — the reviewer closes them.
## Rules
- Never force-push; never rewrite published history to "clean up" fix commits.
- Don't resolve a thread you didn't act on or answer.
- Comments demanding out-of-scope work: reply proposing a follow-up ticket, leave unresolved, note it in the summary.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PR_FIXED
PR: <url>
SUMMARY: <n threads addressed, m skipped + why; commits pushed>
NEXT: human re-review
```
