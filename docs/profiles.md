# Working with profiles

English | [Tiếng Việt](profiles.vi.md)

One knob in `.babysit/git-flow.yaml` decides where finished work goes and how
hard QA tests it:

```yaml
profile: startup      # pet | startup | enterprise
base_branch: main
```

`setup-project` asks one question — **what does a mistake cost in this repo?** —
and writes the answer. Everything else derives. Check what you have:

```bash
bbs autopilot git-flow      # prints BBS_PROFILE / BBS_MODE / BBS_LAND / BBS_RIGOR / …
```

| | `pet` | `startup` | `enterprise` |
|---|---|---|---|
| long-lived branches | `main` | `develop` + `main` | `develop` + `staging` + `main` |
| how work lands | push **is** the release | PR into `develop`, author merges | PR into `develop`, someone else merges |
| review happens | nowhere | locally, in the browser | locally **and** on GitHub |
| QA rigor | smoke, 3–5 cases | standard, 5–10 | strict, 8–12 |

Rigor scales *breadth only*. `PASS` means the same in all three: every
applicable rubric dimension at B or better and a fresh end-to-end run on the
final code. A pet project runs fewer cases — never zero.

**A repo with no `profile:` key resolves to `pet`.** Nobody configured it, so it
behaves like plain git. An explicit `mode:`/`land:`/`push:`/`ticket_branch:` key
still wins over the preset, so a config written before profiles existed keeps
the shape it spelled out.

---

## What no profile changes: where you work

**Babysit works on the branch you are standing on.** No profile cuts a branch,
and no profile moves you into a worktree — `bbs autopilot git-flow` prints
`BBS_MODE='trunk'` under all three. A tool that silently relocates your work is
a tool you cannot manage, so isolation is something a *run* asks for, never
something a config file turns on behind you:

```bash
/bbs:autopilot --mode=worktree "<requirement>"   # this ticket gets its own checkout
bbs ticket ensure --slug-hint thing --mode=branch  # …or a feat/… branch cut in place
/bbs:foreman                                     # a batch: one worktree per ticket
```

A repo that always wants one of those can write `mode: worktree` (or
`mode: branch`) by hand — see [Running tickets in parallel](#running-tickets-in-parallel).
Everything else in this doc is about what the profile *does* decide.

### The catch: identity rides an environment variable

With no ticket branch, there is nothing in git to identify the ticket. Identity
moves to `BABYSIT_TICKET`, which `ensure` prints for you:

```
export BABYSIT_TICKET=bs-xxxxxxxx
```

Lose that variable — new shell, new pane, a session that crashed and came
back — and the pre-push hook **resolves no ticket and defers**. The qa and
review-pr verdicts stop gating anything, silently. Under `pet` that means an
ungated push, and the push is the release. Recover with:

```bash
bbs ticket session list
bbs ticket session attach <id>     # re-echoes the export line
```

---

## The branch topology each profile assumes

`base_branch` is the one thing the profile can't derive for you — it depends on
how you release. Set it to match the shape below.

### `pet` — one branch

```
main ────────●────●────●──────►   push is the release
```

```yaml
profile: pet
base_branch: main
```

`create-pr` deliberately BLOCKs here (`land: none`); there is no PR step, so
**the qa + review-pr verdicts are the only thing standing between a session and
your released code.**

### `startup` — `develop` integrates, `main` releases

```
feat/bs-a ──┐
feat/bs-b ──┼──► develop ───────────────►   PRs land here
feat/bs-c ──┘        │
                     └──► main ──►  tagged release
```

```yaml
profile: startup
base_branch: develop
```

Why not just `main`: with `base_branch: main`, merging the PR **is** shipping,
so your local QA is the only gate that ever runs. `develop` splits *integrated*
from *shipped* into two events — a bad merge is contained on an integration
branch, and `main` stays releasable.

If you deploy on every merge and have no separate release moment, `develop` is
ceremony — set `base_branch: main` and know that you've made local QA the last
gate.

### `enterprise` — a deployed integration environment in the middle

```
feat/bs-a ──┐
feat/bs-b ──┼──► develop ──► staging ──► main
feat/bs-c ──┘     (CI)      (deployed QA)  (tagged release)
```

```yaml
profile: enterprise
base_branch: develop
```

Two review venues answering different questions: your local browser check is
**product** QA (does it work?), the GitHub PR on `develop` is **code** review by
someone else. `staging` is where the integrated result gets QA'd as a deployment
rather than as a checkout. Strict QA also *releases* the surface before handing
off — evidence travels in the PR body, so don't expect the app to still be up
after a QA run (`smoke`/`standard` leave it running and hand you the URL).

### Promotion is never babysit's job

`create-pr` targets `base_branch` and stops — the same reason it never merges.
Every promotion above it is yours or CI's:

```bash
git switch main && git merge --ff-only develop && git tag v1.2.0 && git push --follow-tags
```

The one exception is a hotfix, which forks production directly instead of
waiting behind everything on `develop`:

```bash
BBS_BASE_BRANCH=main bbs ticket ensure --slug-hint hotfix-thing --type hotfix --mode=branch
# …fix, QA, PR into main, then back-merge main into develop
```

---

## Once branches exist: local base is a *test surface*, not a git base

This rule only bites once something is actually cut — a `--mode=branch` ticket,
or a worktree batch.

Every ticket-branch write references `origin/<base>`, never local `<base>`.
Merging local base into a ticket branch drags whatever else you have in flight
into that ticket's PR. To pull in upstream changes, use:

```bash
bbs ticket refresh          # fetch + merge origin/<base>; BLOCKs on dirty tree or conflict
```

Only four commands touch local base — `merge-base`, `switch`, `reset-base`,
`serve` — and none of them writes to a ticket branch.

`ensure` fetches `origin/<base>` before cutting, then forks from the
remote-tracking ref with `--no-track`. Two fallbacks, in this order:

1. **No `origin/<base>` ref at all** (no remote, or the remote lacks the branch)
   → forks from your local `<base>`.
2. **Fetch failed but the ref exists** → forks from the *last-known*
   `origin/<base>` and says so:
   `ensure: warning — fetch failed, using the last-known origin/<base>`.

`bbs ticket board` closes the footer with where your local base actually stands,
so drift is visible before it bites:

```
BASE: main — 3 ahead / 12 behind origin/main (last fetch 2h ago)
  ↑ 3 commit(s) on local 'main' are not on origin/main — mode: trunk cuts no branch, so tickets build on them; they are only unpushed
  ↓ origin/main moved on — bbs ticket refresh (in a ticket) or bbs ticket reset-base (on the primary)
```

It never blocks — a diverged base is normal mid-flight. Board doesn't fetch, so
the numbers are as fresh as the age in parentheses. The `↑` line words itself
per mode: with nothing cut, tickets build *on* those commits and the only thing
wrong is that they aren't pushed; once tickets are cut from `origin/<base>`,
they are invisible to every ticket cut after them.

---

## Running tickets in parallel

Several sessions in one checkout means all changes interleave on one branch: you
**cannot review one ticket in isolation, drop a bad one, or attribute a
regression**. Worktrees buy exactly that separation, and cost a commit +
`bbs ticket merge-base` per test iteration instead of edit-and-refresh. That
trade is why nothing turns them on for you.

### The one rule

> **The primary checkout stays on `base_branch`.** It is the shared test
> surface — `node_modules` and the dev server live there. If a ticket branch
> occupies it, nothing can be composed there.

`serve` enforces this rather than guessing:

```
STATUS: BLOCKED
REASON: primary checkout … is on 'feat/…', not base 'main'.
```

### Option A — `foreman` dispatches for you

Needs [Orca](https://www.onorca.dev) as a hard dependency. Hand it several independent
requests; it opens one visible worker per ticket, each in its own Orca terminal
running autopilot end to end, and requests `--mode=worktree` per dispatch — so
it works the same on any repo, configured or not:

```
/bbs:foreman
```

It also gates each design before any code is written, and can merge finished
tickets onto your local base so you review the batch together.

### Option B — dispatch them yourself

```bash
/bbs:autopilot --mode=worktree "<requirement>"     # once per ticket
bbs ticket ensure --slug-hint <slug> --mode=worktree   # …or just the worktree
```

A repo that works this way *every* time can stop typing the flag:

```yaml
profile: startup
base_branch: develop
mode: worktree        # every ticket gets its own checkout
land: local           # …and the finished batch is composed locally before any PR
```

`land: local` is what makes the composed human-QA checkpoint the default
handoff. It only makes sense next to `mode: worktree`: a branch cut in place
takes the primary off `base_branch`, and every compose then BLOCKs.

### Then: compose, review, ship

```bash
bbs ticket board                  # status: tickets, verdicts, PRs, who holds the lease
bbs ticket serve                  # compose ALL finished tickets (qa + review-pr DONE) onto local base
bbs ticket serve <t1> <t2>        # …or exactly the ones you name
bbs ticket serve --release        # hand the surface back
```

`serve <t1> <t2>` composes exactly the tickets you name — the payoff is that you
can leave a bad one out and each still lands as its own clean PR.

| command | what it does |
|---|---|
| `bbs ticket merge-base` | run **from a worktree** — lands that ticket on the shared surface for QA |
| `bbs ticket switch <t…>` | run **from the primary** — resets to base, then merges exactly the named tickets |
| `bbs ticket reset-base` | after PRs merge upstream, snap local base back to `origin/<base>` |
| `bbs ticket qa-lease` | one QA session at a time on the shared surface; others BLOCK naming the owner |
| `bbs ticket land <t…>` | merge finished tickets into local base and **keep** the merge (see below) |

All of them refuse loudly rather than losing work.

### The QA loop, when tickets live in worktrees

1. Implement and **commit in the worktree**.
2. `bbs ticket merge-base` from the worktree.
3. QA finds a problem → fix **in the worktree**, commit, re-run `merge-base`.
   Never fix in the base checkout — QA must test a committed ticket state.
4. Push the ticket branch; `create-pr` targets `base_branch`.
5. After PRs merge: `bbs ticket reset-base` from the primary; in-flight
   worktrees re-run `merge-base`.

### `finish` — let a verified ticket close itself out

`serve` is a *look*, not a landing: it resets base and re-composes from scratch
each time, so nothing it puts there survives the next `reset-base`. `land` is
the opposite — a `--no-ff` merge into local base that stays.

A repo can ask for that to happen by itself:

```yaml
profile: startup
finish: land          # review (default) | land | pr
```

Now foreman merges each ticket into local base as its worker finishes, instead
of leaving a pile of branches for you to compose. You review the base branch —
which is already running on the dev server — and push it.

`finish: pr` is the other half of the same key: foreman runs `create-pr` per
ticket as it finishes, so the batch ends with open PRs instead of a pile of
branches. It needs a PR venue — under `land: none` the config is rejected when
it is read, because `create-pr` BLOCKs there by design.

`finish` is `review` by default and opt-in per repo, because it is the one key
that lets a run act on its own verdicts while you are asleep — moving your base
branch, or pushing where your team can see it. Both values gate on the same
thing: **qa + review-pr must both be `DONE`**, per ticket, with no `--force`
flag to reach for. Unverified work never closes out.

The rest of the guards are `land`'s, and they are what make a merge you did not
watch survivable:

- **The whole batch is checked before any of it merges** — a blocked third
  ticket doesn't leave the first two on a base you never asked for.
- **It takes the QA lease**, so it can't move base under a running QA session.
- **It never pushes.** Base ends up ahead of origin and the push stays yours.
- **Re-running is a no-op** (`LANDED=0 … already on main`).

`finish: pr` is the one that does push — that is what opening a PR is — but it
opens a PR and nothing more: the review, and the merge, stay human.

Don't run `serve` after landing: `serve` calls `reset-base`, which snaps base to
origin and discards the merges — the ticket branches keep the work, but your
review surface vanishes.

---

## Switching profiles

Re-run `/bbs:setup-project` — on a configured repo it offers the switch and
rewrites only `profile:`. A profile switch changes your review venue and QA
rigor; it never relocates work in flight.

Changing `base_branch` to `develop` on a repo that doesn't have one: create and
push it from `main` **before** the switch, or the first cut finds no
`origin/develop` and forks from your local base instead.

```bash
git switch -c develop main && git push -u origin develop
```

Setting `mode:`, `land:`, `push:` or `finish:` by hand overrides the
profile's preset. That's the escape hatch, not the normal shape — a knob written
out by hand stops tracking its profile. `mode:` is read at branch-cut time, so
adding or removing it affects new tickets only; before moving *into* worktree
work the primary must end clean on `base_branch`, and before moving out of it,
finish or park in-flight worktrees (`bbs ticket board`) and release any
qa-lease.

Full schema and the derivation table: [`.claude/skills/references/git-flow.md`](../.claude/skills/references/git-flow.md);
the worktree machinery: [`references/worktrees.md`](../.claude/skills/references/worktrees.md).
