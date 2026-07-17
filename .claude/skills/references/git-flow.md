# Git Flow Reference
Just enough git policy to create the right branch and know whether babysit
may push after QA. Config lives in `.babysit/git-flow.yaml`:
```yaml
base_branch: main     # branch autopilot starts from and compares against
branch_prefix: feat   # default branch type for new work
push: true            # may autopilot push the QA-checked branch
mode: branch          # trunk | branch | worktree — see below
land: local           # local | pr — how a finished batch reaches human review;
                      # default: local under mode: worktree, pr otherwise
```
## Profiles — pick how babysit works here, not knobs
`setup-project` asks one question — *how does babysit work in this repo?* —
and each answer is a preset over the knobs above (the yaml stays the source
of truth — hand-edit anytime). The axis is attended vs unattended, not team
size: a solo dev running parallel foreman batches is a background worker.

| How babysit works here | Profile | `mode` | `land` | `push` | Flow |
|------------------------|---------|--------|--------|--------|------|
| Pair-programming assistant — a human watches every run (solo, own project) | `trunk` | trunk | — | true | ride the current branch; no cuts, no PR ceremony |
| Background worker — one ticket at a time (freelance / client repo) | `branch-pr` | branch | pr | true | cut per ticket, straight PR — the client-facing work trail |
| Background worker — parallel tickets, composed local review (small team, or solo + foreman) | `worktree-review` | worktree | local | true | parallel tickets; review the composed surface on local dev (`serve`), then `create-pr` |
| Background worker — parallel tickets, straight PRs (big team / enterprise) | `worktree-pr` | worktree | pr | true | parallel tickets, straight per-ticket PRs; review lives on GitHub, browser-test any PR locally via `serve <ticket>` |

Variant: `push: false` on either worktree profile = no PRs; deliver by
`merge-base` onto local base, the human lands manually.
**Switching later** — re-run `/bbs:setup-project`: with `git-flow.yaml`
already present it offers a profile switch instead of full setup. `mode:` is
read when a ticket's branch is cut, so a switch affects new tickets only —
existing manifests keep their recorded shape. Transition care: *into*
`worktree`, the primary checkout must end up clean on `base_branch` (commit
or land whatever rides it first); *out of* `worktree`, finish or park
in-flight worktrees (`bbs-ticket board`) and release any qa-lease first.
(`ticket_branch` is a legacy alias for `mode`: `optional`≡`trunk`,
`required`≡`branch`; `mode:` wins.) `base_branch` fallback ladder:
`BBS_BASE_BRANCH` → legacy `branches.develop` → `bbs-config get base_branch`
→ `origin/HEAD` → `main`. Don't add the old nested `branches:`/`pr:` shape
to new projects.
## `mode:` — where tickets get their branch
A project property (set via `/bbs:setup-project`); a one-shot `--mode <m>`
on `ensure` or the autopilot invocation overrides one call.
- **`trunk`** — no branch is ever cut; N sessions ride the current branch,
  identity carried by `BABYSIT_TICKET` env + `manifest.yaml`. Per-call
  escape hatches: `--no-branch` / `--cut-branch`; fresh shells re-attach via
  `bbs-ticket session attach`.
- **`branch`** (default) — cuts `feat/<id>_<slug>` in place when safe,
  diverts to a worktree when not (see Cut below).
- **`worktree`** — `ensure` always diverts: the primary checkout stays
  pinned to `base_branch` as the shared test surface (node_modules + dev
  server live only there); `merge-base` lands each ticket there for QA.
  Local base is disposable; worktree branches are the source of truth.
## Ticket branches: cut from and refresh against `origin/<base>`
Local `<base>` is a test surface, not a git base — under parallel flows it
piles up integration merges, and merging any of it into a ticket branch
drags other tickets into the PR's ancestry. Every ticket-branch write
references `origin/<base>`:
- **Cut** — `ensure` forks from `origin/<base>` (best-effort fetch; local
  base only when origin has no `<base>`). In place only from a clean
  checkout of `base_branch`; otherwise it diverts to a worktree
  (`<repo>/.babysit/worktrees/<ticket>_<slug>/`, auto-git-excluded) and
  prints `WORKTREE=<path>` — cd there; `manifest.yaml` records the path so
  `bbs-ticket resolve` re-attaches. Hotfix off production:
  `BBS_BASE_BRANCH=production`. Escape hatch: `--no-branch`.
- **Refresh with latest code** — `bbs-ticket refresh` from the ticket
  checkout: fetch + merge `origin/<base>` into the branch (merge, not
  rebase — the branch may be pushed). BLOCKs on dirty tree or conflict.
- **Conflicts** — resolve by merging `origin/<base>` in (what refresh
  does); if that merges clean, the conflict is with another in-flight
  ticket, not base — QA solo via `switch`, land the PRs in sequence.
- **PR** — from the ticket branch (or a compose branch cut from
  `origin/<base>`), targeting `base_branch`.
Only `merge-base` / `switch` / `reset-base` / `serve` touch local `<base>`,
and none of them writes to a ticket branch.
## QA loop with a long-running local server
The dev server runs in the base checkout; the worktree is the source of
truth:
1. Implement + commit in the ticket worktree.
2. `bbs-ticket merge-base` from the worktree — merges the ticket branch
   into the base checkout (locked so parallel runs serialize; BLOCKs on
   dirty/off-base/conflict — on conflict: `bbs-ticket refresh`, resolve,
   commit, re-run).
3. QA finds a problem → fix **in the worktree**, commit, re-run
   `merge-base`. Never fix in the base checkout or hand-apply a diff there —
   what QA tests must be a committed ticket state.
4. The ticket branch holds the complete change; push it; `create-pr`
   targets `base_branch`.
5. After PRs merge upstream: `bbs-ticket reset-base` from the primary snaps
   the base checkout back to `origin/<base>` (refuses when it would lose
   real work); in-flight worktrees re-run `merge-base`.
## Server prep — install, migrate, revert
`qa.yaml` may declare `prepare:` (idempotent install + DB migrate →
`QA_ENV_PREPARE`) and `revert:` (undo a ticket's DB migrations →
`QA_ENV_REVERT`). Both always run in the **primary checkout** — the only
tree with node_modules and the running server. By mode:
- **trunk / branch** — the checkout already serves the branch; run
  `prepare:` whenever set (idempotent, cheap when nothing changed). Nothing
  to revert — code and schema move forward together.
- **worktree** — code reaches the server via `switch`/`merge-base`; run
  `prepare:` in the primary after landing. On leaving the surface (verdict
  set, lease about to release) a ticket that added migrations runs
  `revert:` first — `reset-base` drops the code but not the schema.
`bbs-ticket switch <ticket> [<ticket>…]` hops the test surface from the
primary's side: `reset-base` + merge the named tickets in, so the server
serves exactly base + those tickets (conflicts BLOCK naming the ticket to
fix). Complement of `merge-base` — same lock, opposite direction.
## QA lease — parallel tickets, one test surface
The merge-base lock serializes single git operations but can't keep the
surface stable for a whole QA session. `bbs-ticket qa-lease` does: while a
ticket holds it, `merge-base` / `switch` / `reset-base` from any *other*
ticket BLOCK naming the owner. Protocol when tickets run in parallel:
1. `bbs-ticket qa-lease acquire` (BLOCKs while another ticket QAs).
2. `bbs-ticket switch <ticket>` — surface = base + exactly this ticket. Run
   `prepare:` (§ Server prep) when set.
3. QA; fixes commit in the worktree, re-run `switch` after each fix.
4. `bbs-ticket set-verdict --skill qa`; if the ticket added DB migrations
   run `revert:`; then `bbs-ticket qa-lease release`.
Reentrant for the owner (`acquire` refreshes). A crashed holder can't wedge
the queue: past its ttl (default 60 min, `--ttl-min`) the lease is stale —
the next `acquire` steals it and guard sites clear it, both loudly. The
lease is per repo (it lives in that repo's shared git dir): a cross-repo
ticket acquires one lease in each repo and releases them all when QA ends.
Solo runs never notice any of this: no lease on disk means zero behavior
change.
## Attended parallel review — board / serve / fix-pr
Human review on the real site is the longest must-do step, and it holds the
test surface. The attended loop, per ticket:
1. `bbs-ticket board` — who's on what: status, verdicts, sessions, PR,
   lease holder, what the primary serves. Read-only.
2. `bbs-ticket serve` — take the surface for human review: a long qa-lease
   (240 min) + `switch`, here **and** in each linked sibling repo
   (`siblings` × `RELATED_*_REPO`), so an FE/BE pair serves the ticket
   together. Three shapes: bare = every finished ticket (qa + review-pr
   DONE) composed; `serve <t…>` = exactly those; `serve --release` = done.
3. Review-fix loop: human reviews in the browser → asks the ticket's agent →
   agent fixes **in the ticket worktree**, commits → re-run `serve <ticket>`
   (reentrant: refreshes the lease, re-switches every repo) → refresh
   browser. Other tickets keep working in their worktrees; only their QA
   waits on the lease.
4. Approved → `bbs-ticket serve --release` → `create-pr` per repo → review
   comments via `fix-pr`.
5. `board --pr` flags merged PRs; then `bbs-ticket reset-base` and
   `set-status done`.
## `land:` — composed local review vs straight PRs
How a *finished batch* reaches the human (read by foreman and workflow
handoffs). `land: local` (default under `mode: worktree`): compose the
surface first — bare `bbs-ticket serve` under the review lease — so the
human reviews the combined result on local dev, then lands via per-ticket
`create-pr` or one compose PR (create-pr § Compose PR); with `push: false`
the human lands manually, no PRs. Ticket branches stay the source of truth;
`reset-base` discards the pile — this is *not* trunk mode: every ticket was
built, reviewed, and QA'd in isolation before touching the surface.
`land: pr` (default under other modes): straight per-ticket `create-pr`. It
skips the composed *checkpoint*, not local review — the PR diff is not
enough for UI work; `serve <ticket>` → browser-test → `serve --release`
before approving, fixes via `fix-pr` or the worktree + re-`serve`, never
the base checkout.
