# Worktrees — the opt-in parallel shape
Read this when a run is inside a ticket worktree — a `foreman` batch (foreman
creates it, then starts autopilot there) or a human who ran
`bbs ticket ensure --mode=worktree` to isolate one ticket. The default shape
is [git-flow.md](git-flow.md): work on the current checkout, none of this
applies.

`bbs ticket ensure --mode=worktree` always diverts: the ticket gets
`<repo>/.babysit/worktrees/<ticket>_<slug>/`, `ensure` prints `WORKTREE=<path>`
and `manifest.yaml` records it — cd there, and every later step runs there.

The primary checkout stays where the human left it, and if it is on
`base_branch` it doubles as the shared test surface: `node_modules` and the dev
server live only there (one heavy tree per repo), and each ticket lands into it
for QA. The cost is real — a commit + `surface compose` per test iteration
instead of edit-and-refresh — which is why nothing opts into it by default.

## QA loop (worktree → shared surface)
1. Implement + commit in the ticket worktree.
2. `bbs ticket surface compose` from the worktree — resets the base checkout
   to `origin/<base>` and merges exactly the ticket in scope (locked; BLOCKs
   on dirty/off-base/conflict — on conflict: `refresh`, resolve, commit,
   re-run).
3. QA finds a problem → fix **in the worktree**, commit, re-run
   `surface compose`. Never fix in the base checkout — QA must test a
   committed ticket state.
4. Push the ticket branch; `create-pr` targets `base_branch`.
5. After PRs merge: `bbs ticket surface revert` from the primary snaps base
   back to `origin/<base>` (refuses if it would lose real work); in-flight
   worktrees re-run `surface compose`.

Conflicts: merge `origin/<base>` in (what `refresh` does); if that merges
clean, the conflict is with another in-flight ticket — QA solo via
`surface compose`, land the PRs in sequence. Only `surface compose` /
`surface revert` / `serve` / `land` touch local `<base>`, and none writes to
a ticket branch.

## Server prep — install, migrate, revert
`qa.yaml` may declare `prepare:` (idempotent install + migrate →
`QA_ENV_PREPARE`) and `revert:` (undo the ticket's migrations →
`QA_ENV_REVERT`). Both always run in the **primary checkout**: `prepare:` after
landing, and a ticket that added migrations runs `revert:` before releasing
its lease — `surface revert` drops code, not schema.

`bbs ticket surface compose <ticket…>` hops the surface from the primary's
side: `surface revert` + merge the named tickets, so the server serves
exactly base + those tickets — one lease covers the reset and every merge,
so the surface is never observably half-built. Bare inside a ticket
worktree it composes the ticket in scope — the worktree-side direction.

## Surface lease — parallel tickets, one test surface
One lock guards the shared surface: the surface lease. `surface compose` /
`surface revert` take it for the length of their work; `bbs ticket surface
acquire` holds the same lease across a whole QA session. The one contention
rule is **wait out a surface op, BLOCK on a QA session** — a merge lands in
seconds, a QA session runs for as long as someone is testing, so the second
is named rather than waited on. Protocol:
1. `bbs ticket surface acquire` (BLOCKs while another ticket QAs). It waits
   out a merge already landing — a lease granted mid-merge would cover a
   surface about to change — so acquire can take seconds, and BLOCKs after
   30s.
2. `bbs ticket surface compose <ticket>`; run `prepare:` when set.
3. QA; fixes commit in the worktree, re-`compose` after each.
4. `bbs ticket set-verdict --skill qa`; run `revert:` if migrations were
   added; `bbs ticket surface release`.

Reentrant for the owner. Past ttl (60 min default, `--ttl-min`) a crashed
holder's lease is stale — the next `acquire` steals it, loudly; a surface op's
lease expires in 5 min, so a killed merge cannot wedge the repo. Per repo: a
cross-repo ticket acquires one lease per repo, releases them all. Solo runs
(no surface lease on disk) see zero behavior change.

## Attended parallel review — board / serve / fix-pr
1. `bbs ticket board` — read-only status: tickets, verdicts, PRs, lease, what
   the primary serves.
2. `bbs ticket serve` — take the surface for human review: long surface
   lease (240 min) + `surface compose`, here **and** in each linked sibling
   repo (`siblings` × the workspace registry, `RELATED_*_REPO` as fallback —
   see
   `bbs config workspace show`). Bare = all finished tickets (qa + review-pr
   DONE) composed; `serve <t…>` = exactly those; `--release` = done.
3. Review-fix loop: human reviews in browser → agent fixes **in the worktree**,
   commits → re-run `serve <ticket>` (reentrant) → refresh browser.
4. Approved → `serve --release` → `create-pr` per repo → comments via `fix-pr`.
5. `board --pr` flags merged PRs; then `surface revert` and `set-status done`.

`land: local` is the config key for that composed checkpoint: compose first
(bare `serve` under the review lease), human reviews the combined result on
local dev, then per-ticket `create-pr` or one compose PR; with `push: false`
the human lands manually. It only makes sense alongside worktrees — a branch
cut in place takes the primary off `base_branch` and every compose BLOCKs with
*"primary checkout is on 'feat/…', not base"*.

## `finish:` — who closes a verified ticket out
`review | land | pr`, default `review`. It is the one key that lets a run act
on its own verdicts, so it stays opt-in per repo: unset, foreman stops at
QA-ready and the human owns checkpoint 4. `bbs autopilot git-flow` derives it
as `BBS_FINISH`, and each value names a whole handler rather than a step:

| `BBS_FINISH` | handler | blast radius |
|---|---|---|
| `review` (default) | the human, after `bbs ticket serve` | none |
| `land` | `bbs ticket land <ticket>` | a merge commit on a LOCAL branch |
| `pr` | the `create-pr` skill, one PR per ticket | pushes, visible to the team |

`finish: pr` needs a PR venue: under `land: none` it is rejected when the
config is read, because `create-pr` BLOCKs there by design. `auto_land:` was
the boolean half of this key and is gone — a config still carrying it fails
with the one-line rewrite rather than quietly changing what the repo does.

With `finish: land`, foreman runs `bbs ticket land <ticket>` as each worker
finishes: a `--no-ff` merge into the LOCAL base that is **kept**, unlike
`surface compose`/`serve`, which reset first and treat the composition as
scratch.

`land` gates on qa + review-pr `DONE` per ticket with no override flag, takes
the surface lease, checks every ticket in the batch before merging any of them,
and never pushes. Re-running it is a no-op (`LANDED=0 … already on <base>`).
**`serve` after landing is wrong** — it re-composes from `origin/<base>`,
which discards the merges (the ticket branches keep the work, but the review
surface disappears);
under `finish: land` the base branch *is* the composed surface, so review it
directly and push.
