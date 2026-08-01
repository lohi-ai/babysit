# Git Flow Reference
Just enough git policy to cut the right branch and know whether babysit may
push after QA. Config lives in `.babysit/git-flow.yaml`:
```yaml
base_branch: main     # branch autopilot starts from and compares against
branch_prefix: feat   # default branch type for new work
push: true            # may autopilot push the QA-checked branch
mode: branch          # trunk | branch | worktree — see below
land: local           # local | pr — how a finished batch reaches human review;
                      # default: local under mode: worktree, pr otherwise
```
## Profiles
`setup-project` asks one question — *how does babysit work in this repo?* —
and each answer presets the knobs (the yaml stays the source of truth). The
axis is attended vs unattended, not team size.

| Profile | `mode` | `land` | Shape |
|---------|--------|--------|-------|
| `trunk` — pair-programming, human watches every run | trunk | — | ride the current branch; no cuts, no PR ceremony |
| `branch-pr` — background worker, one ticket at a time | branch | pr | cut per ticket, straight PR |
| `worktree-review` — parallel tickets, composed local review | worktree | local | review the composed surface on local dev (`serve`), then `create-pr` |
| `worktree-pr` — parallel tickets, straight PRs | worktree | pr | per-ticket PRs; browser-test any PR locally via `serve <ticket>` |

`push: false` on a worktree profile = no PRs; deliver via `merge-base`, the
human lands manually. **Switching**: re-run `/bbs:setup-project` (offers a
profile switch when the yaml exists). `mode:` is read at branch-cut time, so
a switch affects new tickets only. *Into* `worktree`: primary checkout must
end clean on `base_branch` first. *Out of* `worktree`: finish or park
in-flight worktrees (`bbs ticket board`), release any qa-lease.
Legacy: `ticket_branch` aliases `mode` (`optional`≡`trunk`, `required`≡`branch`);
`base_branch` fallback: `BBS_BASE_BRANCH` → `branches.develop` →
`bbs config get base_branch` → `origin/HEAD` → `main`.
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
those tickets. Same lock as `merge-base`, opposite direction.
## QA lease — parallel tickets, one test surface
`bbs ticket qa-lease` keeps the surface stable for a whole QA session: while
a ticket holds it, `merge-base`/`switch`/`reset-base` from any other ticket
BLOCK naming the owner. Protocol:
1. `bbs ticket qa-lease acquire` (BLOCKs while another ticket QAs).
2. `bbs ticket switch <ticket>`; run `prepare:` when set.
3. QA; fixes commit in the worktree, re-`switch` after each.
4. `bbs ticket set-verdict --skill qa`; run `revert:` if migrations were
   added; `bbs ticket qa-lease release`.
Reentrant for the owner. Past ttl (60 min default, `--ttl-min`) a crashed
holder's lease is stale — the next `acquire` steals it, loudly. Per repo: a
cross-repo ticket acquires one lease per repo, releases them all. Solo runs
(no lease on disk) see zero behavior change.
## Attended parallel review — board / serve / fix-pr
1. `bbs ticket board` — read-only status: tickets, verdicts, PRs, lease,
   what the primary serves.
2. `bbs ticket serve` — take the surface for human review: long qa-lease
   (240 min) + `switch`, here **and** in each linked sibling repo
   (`siblings` × `RELATED_*_REPO`). Bare = all finished tickets (qa +
   review-pr DONE) composed; `serve <t…>` = exactly those; `--release` = done.
3. Review-fix loop: human reviews in browser → agent fixes **in the
   worktree**, commits → re-run `serve <ticket>` (reentrant) → refresh
   browser. Under cmux, keep app and diff side by side in one workspace:
   `cmux browser open <qa url>` + `cmux diff --branch --repo <worktree>
   --base origin/<base>` (`--last-turn` for just the session's latest edit).
4. Approved → `serve --release` → `create-pr` per repo → comments via
   `fix-pr`.
5. `board --pr` flags merged PRs; then `reset-base` and `set-status done`.
## `land:` — composed local review vs straight PRs
How a *finished batch* reaches the human (read by foreman and workflow
handoffs). `land: local` (default under `mode: worktree`): compose the
surface first — bare `serve` under the review lease — human reviews the
combined result on local dev, then per-ticket `create-pr` or one compose PR;
with `push: false` the human lands manually. Not trunk mode: every ticket
was built and QA'd in isolation before touching the surface. `land: pr`:
straight per-ticket `create-pr`. That skips the composed *checkpoint*, not
local review — for UI work the PR diff is not enough: `serve <ticket>` →
browser-test → `serve --release` before approving.
