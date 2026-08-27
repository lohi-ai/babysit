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

Rigor scales *breadth* only — `PASS` means the same thing in all three tiers
(`../qa/SKILL.md § Rigor tiers`). Under `land: none` `create-pr` BLOCKs: the
qa + review-pr verdicts are the only gate before the push. An explicit
`mode:`/`land:`/`push:`/`finish:` key always wins over the profile.

## Work rides the current branch
`BBS_MODE` is `trunk` under every profile: babysit never cuts a branch or
moves you to a worktree on its own. Identity then rides `BABYSIT_TICKET` —
`ensure` prints the `export` line, and without it the pre-push hook resolves
no ticket and the verdicts stop gating anything. Isolation is requested per
run, never inherited from a config file:
- `--mode=branch` on `ensure` / autopilot — cut `feat/<id>_<slug>` in place
  (diverts to a worktree if the checkout is dirty or off base).
- `--mode=worktree` — cut into `.babysit/worktrees/<ticket>_<slug>/`, primary
  checkout untouched. `foreman` passes this for every worker in a batch. The
  machinery that shape needs — `merge-base`, qa-lease, `switch`/`serve`,
  `land: local`, `finish` — lives in [worktrees.md](worktrees.md).

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
