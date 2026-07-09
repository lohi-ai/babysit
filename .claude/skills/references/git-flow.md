# Git Flow Reference
Babysit needs just enough git policy to create the right branch and know
whether it may push after QA. Config lives in `.babysit/git-flow.yaml`:
```yaml
base_branch: main     # branch autopilot starts from and compares against
branch_prefix: feat   # default branch type for new work
push: true            # may autopilot push the QA-checked branch
mode: branch          # trunk | branch | worktree — see below
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
   Never fix in the base checkout.
4. The ticket branch always holds the complete change; push it; `create-pr`
   targets `base_branch`.
5. After PRs merge upstream, `bbs-ticket reset-base` from the primary snaps
   the base checkout back to `origin/<base>` (refuses when it would lose real
   work); in-flight worktrees re-run `merge-base` afterwards.
`bbs-ticket switch <ticket> [<ticket>…]` hops the test surface from the
primary's side: `reset-base` + merge the named tickets in, so the server
serves exactly base + those tickets (conflicts BLOCK naming the ticket to
fix). Complement of `merge-base` — same lock, opposite direction.
