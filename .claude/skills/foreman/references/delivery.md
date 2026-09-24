## Eager per-ticket finish

A child whose finish prerequisites pass finishes at the tick that observes it.
PR/review-ready tickets and settled workers need not wait for final project
QA. Local lands wait for the last per-ticket surface mutation and pre-land
integration gate, because those operations can reset base.

An evidence-only child is eligible when its workflow verdict, acceptance
evidence, lifecycle signal, and clean worktree are current. Archive its
artifacts, run `BABYSIT_TICKET="$CHILD" bbs ticket set-status done`, release and
close its worker surfaces, and remove the verified-clean non-primary worktree;
keep the branch.
If any code changed, use the code-bearing path instead.

A code-bearing child is eligible when all of these hold:

- current `review-pr` + `qa` verdicts are DONE or DONE_WITH_CONCERNS;
- `bbs ticket readiness --action <land|pr|review> --json` allows that exact
  action — under `review` it still guards a dirty tree, an active attempt,
  and stale evidence, even though Foreman performs no merge or remote write;
- every prerequisite child has itself finished (landed, PRed, or — under
  `review` — gates passed); dependency order is preserved, never reordered;
- under `land`, every required code-bearing child has settled per-ticket
  review/QA, no worker can still compose the primary, and any Pre-land
  integration QA Task passed. `surface compose` resets local base to
  `origin/<base>` and would discard an early merge. Final Integration QA
  depends on these lands, so it must not be a prerequisite of the land handler.

Foreman runs the handler itself — a clean git operation is coordination, not
code — in dependency order, one child at a time:

- `land` — `bbs ticket land <child>` from the primary checkout. Revert any
   scratch composition first (`bbs ticket surface revert`); `land` BLOCKs on
   a nonempty `bbs-serving` marker. It merges locally and never pushes. After
   the finish receipt is persisted and the landed head is verified on base,
   run `BABYSIT_TICKET="$CHILD" bbs ticket set-status done`.
- `pr` — invoke the real `create-pr` skill for that child as soon as its
   gates pass; a PR does not mutate base, so final integration QA does not
   hold PR creation, but it still gates project completion. Read back the
   child's `pointers.pr` and `in_review` status persisted
   by `create-pr`; do not repeat those writes. It becomes `done`
   only after the PR is observed merged.
- `review` — no merge is authorized. Run Orca worktree close-out and keep the
   clean branch and Git worktree for human inspection. Run
   `BABYSIT_TICKET="$CHILD" bbs ticket set-status in_review`.

After a successful `review`, `land`, or `pr`: archive the settled worker's
output, `worker-release` it, release the resource lease, and run Orca worktree
close-out. After `land` or `pr`, also remove the verified-clean non-primary Git
worktree with `bbs ticket worktree-remove` (git worktree remove + bounded
retry for transient NTFS open handles); keep the branch. On any failure
or hold, close settled terminals but keep the Git worktree recoverable.

**Orca worktree close-out** — `worker-release` closes only the one agent
terminal its Dispatch owns. Before any bulk close, prove nothing supervised is
still live in that worktree: every Dispatch recorded on the ticket is settled,
and `orca orchestration worker-list --run <run_id> --terminal-state active
--include-remote --json` shows no worker placed at that path — a `reclaimable`
row there gets its own `worker-release` first. Then close every other terminal
and harness process owned by that exact ticket worktree:

```bash
orca terminal close --worktree path:<worktreePath> --all --json
orca tab list --worktree path:<worktreePath> --json        # then per row:
orca tab close --page <browserPageId> --json
orca emulator list --worktree path:<worktreePath> --json   # then per row:
orca emulator kill --emulator <id> --json
orca terminal list --worktree path:<worktreePath> --json   # verify: zero rows
orca worktree set --worktree path:<worktreePath> \
  --workspace-status <in-review|completed> --json          # in-review under
                                                          #   review/pr, completed under land
orca automations list --json                               # land/pr only: rows whose
                                                          #   runContext.path matches
orca automations edit <id> --disabled --json               #   disable, keep history
bbs ticket worktree-remove <worktreePath>                 # land/pr only; last —
                                                          # retries transient NTFS
                                                          # open-handle failures
```

The bulk terminal close is mandatory even under `review`: it stops setup
shells, agent harnesses, and configured terminal tabs instead of leaving zombie
processes beside a dormant checkout. Never run it while a Dispatch is active or
unverifiable. `selector_not_found` on a `path:` selector means Orca tracks
nothing there — the clean case, not an error. A surface that refuses to close
keeps the Git worktree recoverable like any other hold. Never substitute
`orca worktree rm`: it also tries to delete the checked-out local branch, which
the ticket keeps — so after `git worktree remove` the Orca worktree record
stays behind pointing at a deleted path. That stale card is expected; it is
not a reason to run `worktree rm`, which would delete the branch outright once
the checkout is gone.

Failure routing — never blind-retry an unchanged state:

- surface-lease contention → leave the child eligible; the next tick retries;
- stale or `ready:false` readiness → return the child to verification
  (re-run the affected gate in its worktree) before landing;
- merge conflict → a supervised repair Dispatch in the child's worktree
  resolves it (merge `origin/<base>` in, never local base); keep the
  worktree and do not retry the land until that Dispatch settles;
- a `land` BLOCK that is not a conflict (dirty primary, off-base checkout,
  scratch marker) → report it in the tick output and stop retrying until the
  primary state changes;
- a discarded merge — a `surface compose`/`revert` reset base after the
  land, so the branch is no longer an ancestor — → re-land at the next tick;
  if the worktree was already removed, recreate it from the recorded branch
  first (`land` evaluates readiness inside it);
- a `create-pr` failure → retry once at the next tick, then mark the child
  blocked with evidence.

On resume, recognize a finish receipt before evaluating worktree-bound
readiness. Persist each successful handler's action, verified branch/head and
dependency SHAs, gate evidence paths, and PR URL or landed revision in the
child handoff before removing its worktree. A `pointers.pr` link or
`git merge-base --is-ancestor` result is a recovery lead, not proof of current
acceptance. Verify the receipt still matches current scope and revisions; for
`pr`, read the PR's state and head (an open or merged PR's head must match the
verified revision; later unverified commits and closed-unmerged PRs are not a
successful finish). For `land`, verify the
recorded head remains in base. Reuse valid evidence without recreating a
worktree just to run readiness; missing/stale proof requires reconstruction
from the recorded branch and re-verification. For a changed PR head, first
fetch and inspect that actual head; do not re-verify an obsolete local branch
and call the remote change covered. Preserve any divergent local work and
dispatch reconciliation without force-pushing. Recover a lost receipt from
actual handler state and existing gate evidence, never by assuming success.
