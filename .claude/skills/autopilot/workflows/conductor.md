---
workflow: conductor
version: 1
description: Run several INDEPENDENT tickets in parallel — one background worker per ticket, QA serialized on the shared test surface via qa-lease, plus a combined integration pass. Stops at the QA-ready checkpoint for the whole batch; the human reviews the aggregate handoff and runs create-pr per ticket.
needs-state:
  ticket: optional
  requirement_md: optional
---
# conductor

The Builder archetype at batch width. Orchestrate mode (builder) runs
*sub-tickets of one parent, sequentially*; conductor runs *unrelated tickets
concurrently*. Requires `mode: worktree` semantics: implementation is
embarrassingly parallel (each ticket edits only its own worktree; `review-pr
--fix` too), and QA is the one contended stage — there is ONE primary checkout
with node_modules + dev server, so QA sessions serialize on
`bbs-ticket qa-lease` while everything else overlaps.

## intake

> produces: batch:seeded

1. Members come as ticket ids and/or inline requirements (`+`-separated).
   For each: `bbs-ticket ensure` (worktree diversion applies — record
   `WORKTREE=`), seed `requirement.md` for inline items, listing open
   decisions instead of papering over them. No members resolvable →
   `NEEDS_CONTEXT`.
2. The conductor gets its own ticket (`bbs-ticket ensure`); persist the batch
   there as `batch.md` — one row per member: id, worktree, branch, status
   (`queued | running | passed | blocked | failed`). Named `batch.md`, not
   `manifest.md`, so builder's mode table never mistakes a member or resume
   for orchestrate mode. `batch.md` + checkpoint are the crash contract: a
   re-entered conductor re-reads them and resumes dispatch/monitor — never
   re-dispatches a member whose row already says `passed`.

## dispatch

> produces: workers:running

Spawn one background worker agent per `queued` member, at most
`MAX_WORKERS` at a time:

```bash
MAX_WORKERS="$(bbs-config get parallel_max_workers 2>/dev/null || true)"
[ -n "$MAX_WORKERS" ] || MAX_WORKERS=2
```

The default is deliberately small — worktrees are cheap, but workers still
compete for CPU, the dev server, and the browser. Worker spawn prompt,
verbatim skeleton (fill `<...>`):

```
export BABYSIT_TICKET=<id> BABYSIT_SPAWNED=true AGENT_BROWSER_NAMESPACE=<id>
cd <worktree>
Work ticket <id> to completion via the builder workflow
(/bbs:autopilot builder <id>): qa verdict PASS/FIXED persisted via
bbs-ticket set-verdict, review-pr verdict persisted, branch pushed per
policy, handoff note written — or a NEEDS_CONTEXT / BLOCKED status block
printed verbatim. Other tickets run in parallel: before QA, follow the
qa-lease protocol (bbs-ticket qa-lease acquire → bbs-ticket switch <id> →
qa, fixes committed in the worktree + re-switch → set-verdict →
qa-lease release). The lease is per repo: if your ticket crosses into a
sibling repo (RELATED_*_REPO), acquire a qa-lease in every repo you QA on
for the same session, persist that repo's qa verdict on its sibling ticket,
and release all leases when done. Never edit outside your worktree.
```

Mark each dispatched row `running`, checkpoint.

## monitor

> produces: batch:settled

On each worker completion, **verify from disk — never from the worker's own
report**:

```bash
BBS_TICKET=<id> bbs-ticket verdict-status --skill qa          # DONE | DONE_WITH_CONCERNS
BBS_TICKET=<id> bbs-ticket verdict-status --skill review-pr   # DONE | DONE_WITH_CONCERNS
```

plus commits ahead of base on the member branch and branch-pushed when policy
says push. A cross-repo member (`BBS_TICKET=<id> bbs-ticket get siblings`
non-empty) gets the same qa check per sibling ticket — a missing sibling
verdict is the same gap as a missing member verdict. Verdicts present and
sane → row `passed`. Worker claims done but a
verdict is missing → one re-dispatch naming the exact gap. Worker returned
BLOCKED / NEEDS_CONTEXT or went silent past a generous timeout (check its
checkpoint and `bbs-ticket session list`) → run `triage`; recoverable → one
re-dispatch, else row `blocked`/`failed` with the reason captured for the
handoff. After every transition: update `batch.md`, checkpoint, dispatch the
next `queued` member. A dead worker holding the qa-lease resolves itself —
the lease goes stale (default 60 min) and the next taker steals it.

## integrate

> produces: qa:integrated

Skip when fewer than two members passed. Otherwise the members' individual
PASS verdicts were each measured on `base + that ticket alone` — this step
checks they also hold together:

1. `bbs-ticket qa-lease acquire` as the conductor's own ticket, then
   `bbs-ticket switch <passed tickets...>`. A conflict BLOCKs naming the
   ticket: re-dispatch that worker to merge base into its branch in its
   worktree, resolve, commit; re-run switch. Passed members with siblings →
   mirror the gate in each touched sibling repo: acquire that repo's
   qa-lease (same conductor ticket), `switch <sibling tickets of passed
   members...>` there, so every surface serves base + the passed batch.
2. QA only the *intersecting* flows — shared files, routes, state, and
   fixtures between members, derived the way `qa` derives unknown-unknowns
   from the requirement. Members provably disjoint → skip the pass and log
   the Taste decision (`decisions.jsonl`) instead of testing theater.
3. Failures are fixed in the offending ticket's worktree (re-dispatch that
   worker with the evidence), never on the surface; re-run switch and
   re-test.
4. Persist the result on the conductor's ticket via
   `bbs-ticket set-verdict --skill qa`, then `bbs-ticket qa-lease release`
   in every repo a lease was acquired in.

## handoff

> produces: verdict:conductor

Write the aggregate handoff on the conductor's ticket, readable by a
non-technical owner:

- table: ticket | branch | review verdict | qa verdict | status;
- integration result (or the logged disjoint-skip);
- note the surface is left serving base + passed tickets; after PRs merge,
  `bbs-ticket reset-base`;
- blocked/failed members with their triage reason and the missing input;
- `Next:` one `/bbs:create-pr` line per passed ticket (conductor never
  creates PRs).

Confirm clean state first: every member worktree committed, no debug
leftovers, `batch.md` + checkpoint current.

**Stop conditions**

- `NEEDS_CONTEXT`: no members resolvable, or a member needs human input
  `triage` classified as unrecoverable.
- `BLOCKED`: integration conflict that can't be resolved in any member
  worktree, or more than half the members failed terminally.

**Final status**

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: BUILT
SUMMARY: <n/m members passed; per-ticket branches; integration QA result>
NEXT: human review of the aggregate handoff, then /bbs:create-pr per ticket
```
