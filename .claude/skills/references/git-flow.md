# Git Flow Reference

Babysit only needs enough git policy to create the right branch and know whether
it may push after QA. Keep `.babysit/git-flow.yaml` small.

## Minimal `.babysit/git-flow.yaml`

```yaml
base_branch: main
branch_prefix: feat
push: true
mode: branch
```

| Field | Purpose |
|-------|---------|
| `base_branch` | Branch autopilot starts from and compares against. |
| `branch_prefix` | Default branch type for new work, usually `feat`. |
| `push` | Whether autopilot may push the QA-checked branch. |
| `mode` | `trunk`, `branch` (default), or `worktree`. See below. |
| `ticket_branch` | Legacy alias for `mode`: `optional`≡`trunk`, `required`≡`branch`. `mode:` wins when both are set. |

## `mode:` — where tickets get their branch

The mode is a property of the *project*, not the invocation — set it once
(`/bbs:setup-project` asks) and every `bbs-ticket ensure` honors it. A one-shot
`--mode trunk|branch|worktree` on `ensure` (or on the autopilot invocation,
which passes it through) overrides for a single call.

- **`trunk`** — hobby/shared-branch flow. No branch is ever cut: N sessions in
  one checkout all ride the current branch (e.g. `develop`), one dev server
  tests everything, and ticket identity is carried by `BABYSIT_TICKET` env +
  `manifest.yaml` instead of the branch name.
- **`branch`** — solo, one-ticket-at-a-time flow (the default; matches the
  historic behavior). Cuts `feat/<id>_<slug>` in place when it's safe (clean
  checkout of base), diverts to a worktree when it isn't — see the safe-cut
  gate below.
- **`worktree`** — enterprise/parallel flow. `ensure` **always** diverts, even
  from a clean base checkout: the primary checkout stays pinned to
  `base_branch` as the shared integration/test surface (node_modules + dev
  server live only there), every ticket gets its own lightweight worktree and
  clean branch, and `merge-base` lands each ticket on the primary for QA. PRs
  are cut from the worktree branch, so local integration merges never leak
  into them.

## Safe-cut gate — where `ensure` cuts

In `branch` mode, `bbs-ticket ensure`'s slow-path cuts in place (`git checkout
-b`) **only from a clean checkout of `base_branch`**. On any other branch, or
with uncommitted changes, cutting in place would fork from — and drag along —
another ticket's work-in-progress, so `ensure` diverts the cut to a worktree
forked from base and prints `WORKTREE=<path>`; the invoking checkout is never
touched. (`worktree` mode skips the safety check entirely and always diverts —
the primary checkout is the test surface and must stay on base.) The
worktree lands under `<repo>/.babysit/worktrees/<ticket>_<slug>/` (auto-added
to `.git/info/exclude` so it never dirties the checkout).

The base compared against — and forked from — is `base_branch` from
`.babysit/git-flow.yaml`, resolved via the [Fallbacks](#fallbacks) ladder.

Callers `cd "$WORKTREE"` and do all work there; `manifest.yaml` records the
path so `bbs-ticket resolve` re-attaches. For a hotfix off a production
branch, run with `BBS_BASE_BRANCH=production` — the gate compares against,
and forks from, that base. `--no-branch` stays the explicit escape hatch to
ride the current branch instead.

### QA loop with a long-running local server

When the dev server runs in the base checkout (e.g. the human sits on
`develop`), the worktree is the source of truth and the base checkout is the
test surface:

1. Implement + commit in the ticket worktree.
2. From the worktree: `bbs-ticket merge-base` — merges the ticket branch into
   the base checkout (the server picks it up). It locks the shared git dir so
   parallel ticket runs serialize, and BLOCKs instead of guessing when the
   base checkout is dirty, off base, or the merge conflicts (conflict → merge
   `base_branch` into the ticket branch in the worktree, resolve, commit,
   re-run).
3. QA finds a problem → fix **in the worktree**, commit, re-run
   `bbs-ticket merge-base`. Never fix in the base checkout directly: that
   leaves the ticket branch stale and not PR-ready.
4. The ticket branch always holds the complete change; push it and
   `create-pr` targets `base_branch`.
5. Once PRs merge upstream, the local base checkout is left carrying stale
   integration merges — run `bbs-ticket reset-base` from the primary to snap
   it back to `origin/<base>`. It refuses when it would lose real work (dirty
   tree, off-base, or non-merge commits origin doesn't have); in-flight
   worktrees just re-run `merge-base` afterwards. Under `mode: worktree` the
   local base branch is *disposable* — the worktree branches are the source
   of truth.

To hop the test surface between tickets from the primary's side (no cd into
worktrees), use `bbs-ticket switch <ticket> [<ticket>...]`: it runs
`reset-base`, then merges each named ticket's branch in, so the server serves
exactly `base_branch` + those tickets. `switch A` → test A alone; `switch A B`
→ test the combination; conflicts BLOCK naming the ticket to fix (resolve in
its worktree, re-run). It is the complement of `merge-base`: same lock, same
safety, opposite direction.

This is how many autopilot runs share one machine: each ticket lives in its
own lightweight worktree, while the single heavy tree (node_modules, dev
server) stays on the base checkout and integrates each ticket via
`merge-base` before its QA pass.

## `mode: trunk` — shared-branch flow

By default `bbs-ticket ensure` cuts `feat/<id>_<slug>` when the current branch
doesn't encode a ticket. Repos that develop multiple tickets on a shared branch
(e.g. `develop`) can opt out:

```yaml
mode: trunk        # legacy spelling: ticket_branch: optional
```

With this set, `ensure`'s slow-path creates the ticket **without** touching the
branch: the ticket dir and `manifest.yaml` record the current branch, and the
eval'd output includes `export BABYSIT_TICKET=<id>` so the shell carries
identity instead of the branch name. Fresh shells re-attach with
`export BABYSIT_TICKET=<id>` or `bbs-ticket session attach`.

One-shot overrides on `ensure` (no config needed): `--no-branch` skips the cut
for this call; `--cut-branch` forces the legacy cut. In developer mode
(`AGENT_ROLE` unset/`developer`) a slow-path cut without `--cut-branch` exits 3
with `NEEDS_CONFIRM` instead of silently moving the checkout — see
[ticket-layout.md § Bootstrap](ticket-layout.md#bootstrap-how-tickets-come-into-being).

This is enough for the default loop:

```text
/goal "STATUS: DONE or STATUS: BLOCKED appears" /bbs:autopilot "<task>"
```

Autopilot stops at a QA-checked branch handoff. A human runs `/bbs:create-pr`
after reviewing the result.

## Fallbacks

If `base_branch` is absent, `bbs-autopilot base-branch` falls back to:

1. `BBS_BASE_BRANCH`
2. legacy `.babysit/git-flow.yaml` `branches.develop`
3. `bbs-config get base_branch`
4. `origin/HEAD`
5. `main`

## Advanced Legacy Shape

Existing projects may still carry the older nested config:

```yaml
branches:
  develop: main
  production: main
pr:
  branch_format: "<type>/<ticket-id>_<short-description>"
  types: [feat, fix, chore, refactor, hotfix]
  merge_target:
    default: main
```

Do not add this shape for new projects unless the repo really needs multiple
base branches or hotfix-specific policy.
