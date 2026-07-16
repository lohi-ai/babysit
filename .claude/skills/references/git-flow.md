# Git Flow Reference
Babysit needs just enough git policy to create the right branch and know
whether it may push after QA. Config lives in `.babysit/git-flow.yaml`:
```yaml
base_branch: main     # branch autopilot starts from and compares against
branch_prefix: feat   # default branch type for new work
push: true            # may autopilot push the QA-checked branch
mode: branch          # trunk | branch | worktree — see below
land: local           # local | pr — how a finished batch reaches human review;
                      # default: local under mode: worktree, pr otherwise
```
(`ticket_branch` is a legacy alias for `mode`: `optional`≡`trunk`,
`required`≡`branch`; `mode:` wins.) `base_branch` fallback ladder:
`BBS_BASE_BRANCH` → legacy `branches.develop` → `bbs-config get base_branch`
→ `origin/HEAD` → `main`. Some old projects carry a nested
`branches:`/`pr:` shape — don't add it to new ones.
## `mode:` — where tickets get their branch
A property of the *project* (set once via `/bbs:setup-project`); a one-shot
`--mode <m>` on `ensure` or the autopilot invocation overrides one call.

- **`trunk`** — shared-branch flow. No branch is ever cut; N sessions ride
  the current branch, identity carried by `BABYSIT_TICKET` env +
  `manifest.yaml`. `--no-branch` / `--cut-branch` are the per-call escape
  hatches; fresh shells re-attach with `export BABYSIT_TICKET=<id>` or
  `bbs-ticket session attach`.
- **`branch`** (default) — solo flow. Cuts `feat/<id>_<slug>` in place when
  safe, diverts to a worktree when not (safe-cut gate below).
- **`worktree`** — parallel flow. `ensure` **always** diverts: the primary
  checkout stays pinned to `base_branch` as the shared integration/test
  surface (node_modules + dev server live only there), every ticket gets its
  own worktree + clean branch, and `merge-base` lands each ticket on the
  primary for QA. The local base branch is disposable; worktree branches are
  the source of truth.
## Safe-cut gate
`ensure` cuts in place (`git checkout -b`) **only from a clean checkout of
`base_branch`**. Otherwise it diverts to a worktree forked from base
(`<repo>/.babysit/worktrees/<ticket>_<slug>/`, auto-git-excluded) and prints
`WORKTREE=<path>`; callers `cd "$WORKTREE"` and work there —
`manifest.yaml` records the path so `bbs-ticket resolve` re-attaches. Hotfix
off production: `BBS_BASE_BRANCH=production`. Explicit escape hatch:
`--no-branch`.
## QA loop with a long-running local server
When the dev server runs in the base checkout, the worktree is the source of
truth and the base checkout is the test surface:
1. Implement + commit in the ticket worktree.
2. `bbs-ticket merge-base` from the worktree — merges the ticket branch into
   the base checkout (locks the shared git dir so parallel runs serialize;
   BLOCKs instead of guessing when the base checkout is dirty, off base, or
   the merge conflicts — on conflict, merge base into the ticket branch in
   the worktree, resolve, commit, re-run).
3. QA finds a problem → fix **in the worktree**, commit, re-run `merge-base`.
   Never fix in the base checkout, and never hand-apply a diff there even
   temporarily — `merge-base`/`switch` are the only ways code reaches the
   surface, so what QA tests is always a committed ticket state.
4. The ticket branch always holds the complete change; push it; `create-pr`
   targets `base_branch`.
5. After PRs merge upstream, `bbs-ticket reset-base` from the primary snaps
   the base checkout back to `origin/<base>` (refuses when it would lose real
   work); in-flight worktrees re-run `merge-base` afterwards.
## Server prep — install, migrate, revert
`qa.yaml` may declare `prepare:` (idempotent install + DB migrate → exported
as `QA_ENV_PREPARE`) and `revert:` (undo a ticket's DB migrations →
`QA_ENV_REVERT`). Both always run in the **primary checkout** — the only
tree with node_modules and the running server. By mode:
- **trunk / branch** — the checkout already serves the branch: no switch
  needed; run `prepare:` whenever it is set (it must be idempotent — cheap
  when nothing changed, so no diff judgment needed). Nothing to revert —
  code and schema move forward together.
- **worktree** — code reaches the server via `switch`/`merge-base`; run
  `prepare:` in the primary after landing. On leaving the surface (verdict
  set, lease about to release) a ticket that added migrations runs
  `revert:` first — `reset-base` drops the code but not the schema.
`bbs-ticket switch <ticket> [<ticket>…]` hops the test surface from the
primary's side: `reset-base` + merge the named tickets in, so the server
serves exactly base + those tickets (conflicts BLOCK naming the ticket to
fix). Complement of `merge-base` — same lock, opposite direction.
## QA lease — parallel tickets, one test surface
The merge-base lock serializes single git operations; it cannot keep the
surface stable for the minutes a QA session takes — a parallel ticket's
`merge-base` mid-QA would silently change what's being tested.
`bbs-ticket qa-lease` closes that gap: while a ticket holds it, `merge-base` /
`switch` / `reset-base` from any *other* ticket BLOCK naming the owner.
Protocol when tickets run in parallel:
1. `bbs-ticket qa-lease acquire` (BLOCKs while another ticket QAs).
2. `bbs-ticket switch <ticket>` — surface = base + exactly this ticket, not
   whatever merge-base piled up. Run `prepare:` (§ Server prep) when it is
   set.
3. QA; fixes commit in the worktree, re-run `switch` after each fix.
4. `bbs-ticket set-verdict --skill qa`; if the ticket added DB migrations
   run `revert:`; then `bbs-ticket qa-lease release`.
Reentrant for the owner (`acquire` refreshes). A crashed holder can't wedge
the queue: past its ttl (default 60 min, `--ttl-min`) the lease is stale —
the next `acquire` steals it and guard sites clear it, both loudly.
The lease is per repo (it lives in that repo's shared git dir): a cross-repo
ticket QAing on a pair of repos acquires one lease in each repo for the same
session and releases them all when QA ends.
Solo runs never notice any of this: no lease on disk means zero behavior
change.
## Attended parallel review — board / serve / fix-pr
Human review on the real site is the longest must-do step, and it holds the
test surface. The attended loop, per ticket:
1. `bbs-ticket board` — who's on what: status, verdicts, sessions, PR,
   lease holder, and what the primary currently serves. Read-only.
2. `bbs-ticket serve` — take the surface for human review: a long qa-lease
   (240 min — agent-length TTLs would let a parallel run steal the surface
   mid-review) + `switch`, here **and** in each linked sibling repo
   (`siblings` × `RELATED_*_REPO`), so an FE/BE pair serves the ticket
   together. One command, three shapes: bare = every finished ticket
   (qa + review-pr DONE) composed; `serve <t…>` = exactly those tickets;
   `serve --release` = done reviewing.
3. Review-fix loop: human reviews in the browser → asks the ticket's agent →
   agent fixes **in the ticket worktree**, commits → re-run `serve <ticket>`
   (reentrant: refreshes the lease, re-switches every repo) → refresh browser.
   Other tickets keep implementing/reviewing in their worktrees; only their
   QA waits on the lease.
4. Approved → `bbs-ticket serve --release` (frees leases, leaves the
   surface as-is) → `create-pr` per repo → review comments via `fix-pr`.
5. `board --pr` flags merged PRs; then `bbs-ticket reset-base` and
   `set-status done`.
## `land:` — composed local review vs straight PRs
How a *finished batch* reaches the human (read by foreman and workflow
handoffs). `land: local` (default under `mode: worktree`): compose the
surface first — `bbs-ticket serve` (bare: every finished ticket, under the
review lease) — so the human reviews the combined result on the local dev
server, then lands via per-ticket `create-pr` or one compose PR (create-pr
§ Compose PR). Ticket
branches stay the source of truth; `reset-base` discards the pile — this is
*not* trunk mode: every ticket was still built, reviewed, and QA'd in
isolation before it touched the surface. `land: pr` (default under other
modes): skip the composed step, go straight to per-ticket `create-pr`.
