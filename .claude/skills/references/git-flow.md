# Git Flow Reference
Just enough git policy to cut the right branch, know how rigorously to test
it, and know whether babysit may push after QA. Config lives in
`.babysit/git-flow.yaml` and is two keys:
```yaml
profile: startup      # pet | startup | enterprise — everything below derives
base_branch: main     # branch autopilot starts from and compares against
```
Read the derived set — never re-parse the yaml — with:
```bash
eval "$(bbs autopilot git-flow)"
# BBS_PROFILE BBS_BASE_BRANCH BBS_MODE BBS_LAND BBS_PUSH BBS_RIGOR BBS_REVIEW_EFFORT
```
## Profiles
`setup-project` asks one question — *what does a mistake cost in this repo?* —
because that, not team size or attendance, is what a git flow is really
answering. **This table is the source of truth.**

| | **pet** | **startup** | **enterprise** |
|---|---|---|---|
| who | solo, hobby project | solo freelance / small team | team, enterprise codebase |
| priority | ship now | release speed > code quality | code quality > release speed |
| `mode` | `trunk` (work lands on base) | `branch` | `branch` |
| `land` | `none` (no PR) | `pr` | `pr` |
| `push` | `true` | `true` | `true` |
| review happens | nowhere — the push is the release | **locally**, in the browser, author merges | **on GitHub**, someone else merges |
| `review-pr` effort | `low` | `medium` | `high` |
| QA rigor | `smoke` (3–5 cases) | `standard` (5–10) | `strict` (8–12) |
| inner loop (edit → browser) | **0 steps** | **0 steps** | **0 steps** |

Rigor scales *breadth* only: `PASS`/`FIXED` still require every applicable
rubric dimension at B or better and `freshness=A` in all three tiers. A pet
project runs fewer cases; it never runs zero, and it never passes on a
C-grade dimension. See `../qa/SKILL.md § Rigor tiers`.

**No profile pays the worktree tax.** `worktree` costs a commit +
`merge-base` per test iteration and buys only parallelism — it lets N tickets
share one dev server, it does not deepen testing. So all three profiles keep
`branch`/`trunk`'s 0-step inner loop, and `mode: worktree` is what `foreman`
requests per dispatch (`--mode=worktree`) or an explicit override.

Any explicit `mode:` / `land:` / `push:` key in the yaml wins over the
profile's preset. That is the escape hatch, not the normal shape — a knob
written out by hand stops tracking its profile.
**Switching**: re-run `/bbs:setup-project` (offers a profile switch when the
yaml exists). `mode:` is read at branch-cut time, so a switch affects new
tickets only. *Into* `worktree`: primary checkout must end clean on
`base_branch` first. *Out of* `worktree`: finish or park in-flight worktrees
(`bbs ticket board`), release any qa-lease.
Legacy: the four pre-profile names still resolve — `trunk`→`pet`,
`branch-pr`→`startup`, `worktree-pr`→`enterprise` + `mode: worktree`,
`worktree-review`→ that plus `land: local`; `ticket_branch` aliases `mode`
(`optional`≡`trunk`, `required`≡`branch`). `base_branch` fallback:
`BBS_BASE_BRANCH` → `branches.develop` → `bbs config get base_branch` →
`origin/HEAD` → `main`.
Under `pet` there is no ticket branch, so the ticket's identity — and with it
the push gate — rides `BABYSIT_TICKET`. `ensure` prints the `export` line;
without it in the environment the pre-push hook resolves no ticket and
defers, and the qa/review-pr verdicts stop gating anything.
## `mode:` — where tickets get their branch
One-shot override: `--mode <m>` on `ensure` or the autopilot invocation.
- **`trunk`** — never cuts; sessions ride the current branch, identity via
  `BABYSIT_TICKET` env + `manifest.yaml` (re-attach: `bbs ticket session
  attach`). Escape hatches: `--no-branch` / `--cut-branch`.
- **`branch`** (default) — cuts `feat/<id>_<slug>` in place when safe,
  diverts to a worktree when not.
- **`worktree`** — `ensure` always diverts; the primary checkout stays
  pinned to `base_branch` as the shared test surface (node_modules + dev
  server live only there); `merge-base` lands each ticket there for QA.
## Ticket branches: cut from and refresh against `origin/<base>`
Local `<base>` is a test surface, not a git base — merging it into a ticket
branch drags other in-flight tickets into the PR. Every ticket-branch write
references `origin/<base>`:
- **Cut** — `ensure` forks from `origin/<base>` (fetch-first; local base only
  when origin lacks `<base>`). In place only from a clean checkout of
  `base_branch`; otherwise diverts to
  `<repo>/.babysit/worktrees/<ticket>_<slug>/` and prints `WORKTREE=<path>` —
  cd there; `manifest.yaml` records it. Under cmux, open it instead of cd-ing:
  `cmux workspace create --name "bbs <ticket>" --cwd <worktree>` — one
  worktree, one sidebar workspace, with its own status pill and diff. Hotfix
  off production: `BBS_BASE_BRANCH=production`.
- **Refresh** — `bbs ticket refresh` from the ticket checkout: fetch + merge
  `origin/<base>` (merge, not rebase — the branch may be pushed). BLOCKs on
  dirty tree or conflict.
- **Conflicts** — merge `origin/<base>` in (what refresh does); if that
  merges clean, the conflict is with another in-flight ticket — QA solo via
  `switch`, land the PRs in sequence.
- **PR** — from the ticket branch, targeting `base_branch`.
Only `merge-base` / `switch` / `reset-base` / `serve` touch local `<base>`,
and none writes to a ticket branch.
## QA loop (worktree → shared server)
1. Implement + commit in the ticket worktree.
2. `bbs ticket merge-base` from the worktree — merges the ticket branch into
   the base checkout (locked; BLOCKs on dirty/off-base/conflict — on
   conflict: `refresh`, resolve, commit, re-run).
3. QA finds a problem → fix **in the worktree**, commit, re-run
   `merge-base`. Never fix in the base checkout — QA must test a committed
   ticket state.
4. Push the ticket branch; `create-pr` targets `base_branch`.
5. After PRs merge: `bbs ticket reset-base` from the primary snaps base back
   to `origin/<base>` (refuses if it would lose real work); in-flight
   worktrees re-run `merge-base`.
## Server prep — install, migrate, revert
`qa.yaml` may declare `prepare:` (idempotent install + migrate →
`QA_ENV_PREPARE`) and `revert:` (undo the ticket's migrations →
`QA_ENV_REVERT`). Both always run in the **primary checkout**. trunk/branch:
run `prepare:` whenever set; nothing to revert. worktree: run `prepare:`
after landing; a ticket that added migrations runs `revert:` before
releasing its lease — `reset-base` drops code, not schema.
`bbs ticket switch <ticket…>` hops the surface from the primary's side:
`reset-base` + merge the named tickets, so the server serves exactly base +
those tickets — one lease covers the reset and every merge, so the surface is
never observably half-built. Same lock as `merge-base`, opposite direction.
## QA lease — parallel tickets, one test surface
One lock guards the shared surface: the qa-lease. `merge-base` / `switch` /
`reset-base` take it for the length of their work; `bbs ticket qa-lease`
holds the same lease across a whole QA session. The one contention rule is
**wait out a surface op, BLOCK on a QA session** — a merge lands in seconds,
a QA session runs for as long as someone is testing, so the second is named
rather than waited on. Protocol:
1. `bbs ticket qa-lease acquire` (BLOCKs while another ticket QAs). It waits
   out a merge already landing — a lease granted mid-merge would cover a
   surface about to change — so acquire can take seconds, and BLOCKs after 30s.
2. `bbs ticket switch <ticket>`; run `prepare:` when set.
3. QA; fixes commit in the worktree, re-`switch` after each.
4. `bbs ticket set-verdict --skill qa`; run `revert:` if migrations were
   added; `bbs ticket qa-lease release`.
Reentrant for the owner. Past ttl (60 min default, `--ttl-min`) a crashed
holder's lease is stale — the next `acquire` steals it, loudly; a surface op's
lease expires in 5 min, so a killed merge cannot wedge the repo. Per repo: a
cross-repo ticket acquires one lease per repo, releases them all. Solo runs
(no QA lease on disk) see zero behavior change.
## Attended parallel review — board / serve / fix-pr
1. `bbs ticket board` — read-only status: tickets, verdicts, PRs, lease,
   what the primary serves.
2. `bbs ticket serve` — take the surface for human review: long qa-lease
   (240 min) + `switch`, here **and** in each linked sibling repo
   (`siblings` × the workspace registry, `RELATED_*_REPO` as fallback —
   see `bbs config workspace show`). Bare = all finished tickets (qa +
   review-pr DONE) composed; `serve <t…>` = exactly those; `--release` = done.
3. Review-fix loop: human reviews in browser → agent fixes **in the
   worktree**, commits → re-run `serve <ticket>` (reentrant) → refresh
   browser. Under cmux, keep app and diff side by side in one workspace:
   `cmux browser open <qa url>` + `cmux diff --branch --repo <worktree>
   --base origin/<base>` (`--last-turn` for just the session's latest edit).
4. Approved → `serve --release` → `create-pr` per repo → comments via
   `fix-pr`.
5. `board --pr` flags merged PRs; then `reset-base` and `set-status done`.
## `land:` — how finished work reaches the human
Read by foreman, `create-pr`, and the workflow handoffs.
`land: none` (the `pet` profile): there is no PR step — work lands on
`base_branch` and the push *is* the release, so the qa + review-pr verdicts
are the only gate before it. `create-pr` BLOCKs under this policy rather than
opening a PR nobody wanted.
`land: local` (default under `mode: worktree`): compose the
surface first — bare `serve` under the review lease — human reviews the
combined result on local dev, then per-ticket `create-pr` or one compose PR;
with `push: false` the human lands manually. Not trunk mode: every ticket
was built and QA'd in isolation before touching the surface. `land: pr`:
straight per-ticket `create-pr`. That skips the composed *checkpoint*, not
local review — for UI work the PR diff is not enough: `serve <ticket>` →
browser-test → `serve --release` before approving.
