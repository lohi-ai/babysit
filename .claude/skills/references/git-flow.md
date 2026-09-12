# Git Flow Reference
Two keys in `.babysit/git-flow.yaml`. A repo with no config resolves to `pet`.
```yaml
profile: startup      # pet | startup | enterprise — everything below derives
base_branch: main     # what work compares against and PRs into
```
Read the derived set — never re-parse the yaml — with:
```bash
eval "$(bbs autopilot git-flow)"
# BBS_PROFILE BBS_BASE_BRANCH BBS_MODE BBS_LAND BBS_FINISH BBS_PUSH BBS_RIGOR BBS_REVIEW_EFFORT
```
## Profiles — `setup-project` asks *what does a mistake cost in this repo?*
| | **pet** (hobby) | **startup** (small team) | **enterprise** (team) |
|---|---|---|---|
| `base_branch` | `main` | `develop` | `develop` |
| `land` | `none` — the push is the release | `pr` | `pr` |
| `review-pr` effort | `low` | `medium` | `high` |
| QA rigor | `smoke` (3–5 cases) | `standard` (5–10) | `strict` (8–12) |
| Foreman finish (default) | `review` | `review` | `review` |

Rigor scales *breadth* only — `PASS` means the same thing in all three tiers
(`../qa/SKILL.md § Rigor tiers`). Under `land: none` `create-pr` BLOCKs: the
qa + review-pr verdicts are the only gate before the push. An explicit
`mode:`/`land:`/`push:`/`finish:` key always wins over the profile.

### Authorizing autonomous Foreman closeout

Profiles choose the review venue and rigor; they do not authorize a merge or
remote write. A repo that wants Foreman to finish and clean each successful
worker writes the action explicitly:

| Profile | Add to `.babysit/git-flow.yaml` | Successful result |
|---|---|---|
| `pet` | `finish: land` | Merge verified ticket branches into local `base_branch` (`main` by profile convention); never push. |
| `startup` | `finish: pr` | Push each verified ticket branch and create a PR targeting `base_branch`. |
| `enterprise` | `finish: pr` | Same remote PR closeout, with strict QA and high-effort review gates. |

After either action succeeds, Foreman archives the worker output, releases the
Orca worker (the terminal-close operation), and removes only that worker's
verified-clean non-primary worktree. Failed, blocked, held, or default
`finish: review` work stays intact for recovery. Branches are retained.

## Who owns git
- **Autopilot** works on the checkout it was started in. It commits its own
  work locally and nothing more — never branches, worktrees, pushes, lands,
  or opens a PR. `BBS_MODE` is `trunk` under every profile and autopilot
  never reads it.
- **Foreman** owns isolation and close-out. It creates the ticket worktree
  (`bbs ticket ensure --mode=worktree`), starts the worker inside it, and
  applies the repo's `finish:` policy (`review` | `land` | `pr`) once the
  verdicts read DONE. The machinery that shape needs — `merge-base`,
  qa-lease, `switch`/`serve`, `land` — lives in [worktrees.md](worktrees.md).
- **A human** can still ask for isolation directly:
  `bbs ticket ensure --mode=branch` cuts `feat/<id>_<slug>` in place
  (diverts to a worktree if the checkout is dirty or off base);
  `--mode=worktree` cuts into `.babysit/worktrees/<ticket>_<slug>/` with the
  primary checkout untouched. Nothing opts in implicitly — isolation is
  always an explicit ask.

## When something does get a branch
Ticket branches are cut and refreshed against `origin/<base>` (fetch-first) —
local base is a test surface, not a git base. `bbs ticket refresh` merges
`origin/<base>` back in; PRs target `base_branch`. Promotion (`develop` →
`staging` → `main`, tag, deploy) is never babysit's; hotfix off production
with a one-shot `BBS_BASE_BRANCH=production`. `base_branch` fallback:
`BBS_BASE_BRANCH` → `branches.develop` → `bbs config get base_branch` →
`origin/HEAD` → `main`. Legacy profile names resolve to a profile + the mode
their name promises: `trunk`→`pet`, `branch-pr`→`startup`+`branch`,
`worktree-pr`→`enterprise`+`worktree`, `worktree-review`→ that plus
`land: local`; `ticket_branch` aliases `mode` (`optional`≡`trunk`,
`required`≡`branch`).
