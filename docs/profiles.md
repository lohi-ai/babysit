# Working with profiles

One knob in `.babysit/git-flow.yaml` decides branch mechanics *and* QA rigor:

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
| branch per ticket | no | `feat/<id>_<slug>`, in a worktree | `feat/<id>_<slug>`, in a worktree |
| how work lands | push **is** the release | PR into `develop`, author merges | PR into `develop`, someone else merges |
| human QA before the PR | compose on local `main` | compose on local `develop` | compose on local `develop` |
| review happens | nowhere | locally, in the browser | locally **and** on GitHub |
| QA rigor | smoke, 3–5 cases | standard, 5–10 | strict, 8–12 |

Rigor scales *breadth only*. `PASS` means the same in all three: every
applicable rubric dimension at B or better and a fresh end-to-end run on the
final code. A pet project runs fewer cases — never zero.

**The rest of this doc is about the part that isn't in the table: what each
profile expects *you* to do with your base branch.** Get that wrong and the
gates quietly stop working.

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
branch, and `main` stays releasable. That is the whole return on the extra hop.

If you deploy on every merge and have no separate release moment, `develop`
is ceremony — set `base_branch: main` and know that you've made local QA the
last gate.

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

Two review venues answering different questions: the local compose is
**product** QA (does it work?), the GitHub PR on `develop` is **code** review
by someone else. `staging` is where the integrated result gets QA'd as a
deployment rather than as a checkout.

### Promotion is never babysit's job

`create-pr` targets `base_branch` and stops — the same reason it never merges.
Every promotion above it is yours or CI's:

```bash
git switch main && git merge --ff-only develop && git tag v1.2.0 && git push --follow-tags
```

The one exception is a hotfix, which forks production directly instead of
waiting behind everything on `develop`:

```bash
BBS_BASE_BRANCH=main bbs ticket ensure --slug-hint hotfix-thing --type hotfix
# …fix, QA, PR into main, then back-merge main into develop
```

---

## The rule that outranks every profile

**Your local `main` (or `develop`) is a *test surface*, not a git base.**

Every ticket-branch write references `origin/<base>`, never local `<base>`.
Merging local base into a ticket branch drags whatever else you have in flight
into that ticket's PR. To pull in upstream changes, use:

```bash
bbs ticket refresh          # fetch + merge origin/<base>; BLOCKs on dirty tree or conflict
```

Only four commands touch local base — `merge-base`, `switch`, `reset-base`,
`serve` — and none of them writes to a ticket branch.

`bbs ticket board` closes the footer with where your local base actually
stands, so drift is visible before it bites:

```
BASE: main — 3 ahead / 12 behind origin/main (last fetch 2h ago)
  ↑ 3 commit(s) exist only on local 'main' — tickets are cut from origin/main, so new ones will not have them
  ↓ origin/main moved on — bbs ticket refresh (in a ticket) or bbs ticket reset-base (on the primary)
```

It never blocks — a diverged base is normal mid-flight. Board doesn't fetch, so
the numbers are as fresh as the age in parentheses.

Under `startup` and `enterprise` the `↑` line is the one worth acting on:
those commits are invisible to every ticket cut after them. Under `pet` it
means the opposite — nothing is cut, so tickets build *on* that work and the
only thing wrong is that it isn't pushed. The line words itself per mode.

### So what does a ticket branch get cut from?

**`origin/<base>`, never your local one** — and `pet` doesn't cut at all:

| profile | cut from |
|---|---|
| `pet` | **nothing is cut.** The ticket rides your current branch, in whatever state it's in |
| `startup` | `origin/<base>`, fetched first |
| `enterprise` | `origin/<base>`, fetched first |

`ensure` fetches `origin/<base>` before cutting, then forks from the
remote-tracking ref with `--no-track`. The worktree cut and the in-place cut
(the solo-loop opt-out) use the same source, so neither is second-class:

```
ensure: worktree ready at …/bs-xxxxxxxx_probe (branch feat/bs-xxxxxxxx_probe off origin/main)
```

Two fallbacks, in this order:

1. **No `origin/<base>` ref at all** (no remote, or the remote lacks the
   branch) → forks from your local `<base>`.
2. **Fetch failed but the ref exists** → forks from the *last-known*
   `origin/<base>` and says so:
   `ensure: warning — fetch failed, using the last-known origin/<base>`.
   Note it does **not** silently fall back to local base here — a stale remote
   ref is still cleaner than a polluted local one.

**Why this matters.** Local base accumulates other people's tickets every time
you run `merge-base` or `switch` — that's its whole job as a test surface. If
tickets were cut from it, each new branch would inherit whatever else happened
to be composed there at that moment, and that would ride into the PR. Cutting
from `origin/<base>` is what keeps parallel PRs independent.

Which is also the sharpest way to say what `pet` gives up: with no cut, your
work *does* start from local base, including anything else in flight there.
There is no separation by construction — not a weaker separation, none.

---

## `pet` — you work on the base branch

Mode `trunk`: nothing is ever cut. Sessions ride whatever branch you're on,
and work lands on `base_branch`. Git imposes nothing — edit anything, check out
anything, no ceremony. `create-pr` deliberately BLOCKs here (`land: none`);
there is no PR step, so **the qa + review-pr verdicts are the only thing
standing between a session and your released code.**

### ⚠️ The catch: identity rides an environment variable

With no ticket branch, there is nothing in git to identify the ticket. Identity
moves to `BABYSIT_TICKET`, which `ensure` prints for you:

```
export BABYSIT_TICKET=bs-xxxxxxxx
```

Lose that variable — new shell, new pane, a session that crashed and came
back — and the pre-push hook **resolves no ticket and defers**. The qa and
review-pr verdicts stop gating anything, silently. Under `pet` that means an
ungated push, and the push is the release.

This is not theoretical; it reproduces exactly. Recover with:

```bash
bbs ticket session list
bbs ticket session attach <id>     # re-echoes the export line
```

So the honest summary of `pet` is not "no limits" — it's **the discipline moved
out of git and into one environment variable you have to keep alive.** That
trade is fine for a hobby repo. It is the reason `pet` is the wrong answer for
anything with users.

### Parallel under `pet`

`pet` is the one profile that does **not** default to worktrees, and that is
the trade: several sessions in one folder, one dev server testing everything at
once, no ceremony — but all changes interleave on the base branch, so you
**cannot review one ticket in isolation, drop a bad one, or attribute a
regression**. It's all or nothing.

Wanting separable parallel tickets is usually the sign a repo has outgrown
`pet` — `startup` is then the better answer, and it's a one-line switch. If you
want them anyway, write **both** keys:

```yaml
profile: pet
base_branch: main
mode: worktree        # tickets get their own checkout
land: none            # …but there is still no PR step
```

Tickets fork `origin/main` into worktrees, `bbs ticket serve` composes the
finished ones onto local `main` for QA, and you push `main` yourself. Write
`mode: worktree` alone and you keep `land: none` — nothing silently promotes a
`pet` repo into a PR flow.

---

## `startup` — the primary checkout stays on base, forever

Mode `worktree`, land `local`. The rule is a single line:

> **The primary checkout never leaves `base_branch`. Every ticket lives in a
> worktree.**

Those two keys are one decision, not two. Tickets need somewhere to live that
isn't the primary; the primary needs to stay on base because that is the only
state in which a finished batch can be composed there for review. Take either
half away and the other stops working — which is why writing `mode: branch`
*next to* `land: local` is a resolve-time error rather than a config that reads
fine and BLOCKs hours later.

`ensure` says where the ticket went:

```
ensure: mode=worktree — cutting into a worktree (primary checkout stays on 'develop')
WORKTREE=/…/.babysit/worktrees/bs-xxxxxxxx_thing
```

### The daily loop

```bash
/bbs:foreman                      # or: /bbs:autopilot "<requirement>" per ticket
# …workers implement + QA in their own worktrees…

bbs ticket board                  # who's finished, who holds the lease
bbs ticket serve                  # compose every finished ticket onto local develop
# …browse the combined result: this is the human-QA checkpoint…
bbs ticket serve --release
/bbs:create-pr <ticket>           # one PR per ticket, targeting develop
```

`serve <t1> <t2>` composes exactly the tickets you name — the payoff over
`pet`-style parallel is that you can leave a bad one out and each still lands
as its own clean PR.

### ⚠️ What it costs

The inner loop is no longer 0-step. Every test iteration is **commit +
`bbs ticket merge-base`**, not edit-and-refresh, because the code you're
testing lives in a worktree and the dev server serves the primary.

If you genuinely run one ticket at a time, buy the fast loop back with one key:

```yaml
profile: startup
base_branch: develop
mode: branch          # cut feat/… in place when the tree is clean;
                      # derives land: pr — there is nothing to compose
```

Then the clean-base rule applies instead: be on `base_branch` with a clean tree
when a ticket starts, or `ensure` diverts to a worktree anyway and warns you
about the loop you just bought.

## `enterprise` — same git contract, one more environment

Branch mechanics are **identical to `startup`**: worktree per ticket, primary
pinned to `develop`, compose to review. Three things change:

1. **A second review venue.** The local compose is product QA; the PR on
   `develop` is code review, and someone else merges it.
2. **`staging` sits between `develop` and `main`** — the integrated result gets
   QA'd as a deployment, not just as a checkout.
3. **Strict QA releases the surface before handing off** — evidence travels in
   the PR body instead. Under `smoke`/`standard`, QA *leaves the app running*
   and puts the URL in the handoff; under `strict` it does not. Don't expect to
   find the app still up after a QA run.

---

## Running tickets in parallel

**Under `startup` and `enterprise` this is the default shape** — the profile
already turned worktrees on, so there is nothing to request. Under `pet` you
opt in with the two keys above, or dispatch `--mode=worktree` per ticket
without touching config.

### The one rule

> **The primary checkout stays on `base_branch`.** It is the shared test
> surface. If a ticket branch occupies it, nothing can be composed there.

`serve` enforces this rather than guessing:

```
STATUS: BLOCKED
REASON: primary checkout … is on 'feat/…', not base 'main'.
```

### Option A — `foreman` dispatches for you

Needs [cmux](https://cmux.com) as a hard dependency. Hand it several
independent requests; it opens one visible worker per ticket, each in its own
cmux workspace running autopilot end to end, and requests `--mode=worktree`
per dispatch — so it works the same way on a `pet` repo that never configured
it:

```
/bbs:foreman
```

It also gates each design before any code is written, and merges finished
tickets onto your local base so you review the batch together.

### Option B — dispatch them yourself

```bash
/bbs:autopilot "<requirement>"     # once per ticket
```

or, to create the worktree without starting work:

```bash
bbs ticket ensure --slug-hint <slug>
```

Add `--mode=worktree` to either only on a repo whose config doesn't already
say so (`pet`, or a solo-loop opt-out).

### Then: compose, review, ship

```bash
bbs ticket board                  # status: tickets, verdicts, PRs, who holds the lease
bbs ticket serve                  # compose ALL finished tickets (qa + review-pr DONE) onto local base
bbs ticket serve <t1> <t2>        # …or exactly the ones you name
bbs ticket serve --release        # hand the surface back
```

Supporting commands:

| command | what it does |
|---|---|
| `bbs ticket merge-base` | run **from a worktree** — lands that ticket on the shared surface for QA |
| `bbs ticket switch <t…>` | run **from the primary** — resets to base, then merges exactly the named tickets |
| `bbs ticket reset-base` | after PRs merge upstream, snap local base back to `origin/<base>` |
| `bbs ticket qa-lease` | one QA session at a time on the shared surface; others BLOCK naming the owner |

All of them refuse loudly rather than losing work.

### The QA loop, when tickets live in worktrees

1. Implement and **commit in the worktree**.
2. `bbs ticket merge-base` from the worktree.
3. QA finds a problem → fix **in the worktree**, commit, re-run `merge-base`.
   Never fix in the base checkout — QA must test a committed ticket state.
4. Push the ticket branch; `create-pr` targets `base_branch`.
5. After PRs merge: `bbs ticket reset-base` from the primary; in-flight
   worktrees re-run `merge-base`.

---

## Switching profiles

Re-run `/bbs:setup-project` — on a configured repo it offers the switch and
rewrites only `profile:`.

`mode:` is read at branch-cut time, so **a switch affects new tickets only**;
anything already in flight keeps the shape it was cut with.

- **Into worktree work** (`pet` → `startup`/`enterprise`, or the opt-out back to
  the default) — the primary must end clean on `base_branch` first.
- **Out of it** — finish or park in-flight worktrees (`bbs ticket board`) and
  release any qa-lease.

Changing `base_branch` to `develop` on a repo that doesn't have one: create and
push it from `main` **before** the switch, or the first `ensure` finds no
`origin/develop` and forks from your local base instead.

```bash
git switch -c develop main && git push -u origin develop
```

Setting `mode:`, `land:`, or `push:` by hand overrides the profile's preset.
That's the escape hatch, not the normal shape — a knob written out by hand
stops tracking its profile, and the combination may not be coherent. One
written-out pair is rejected outright:

```
git-flow: incoherent .babysit/git-flow.yaml: mode 'branch' with land 'local' —
a branch cut in place takes the primary checkout off 'develop', so nothing can
compose there. Use 'land: pr' for per-ticket PRs, or drop 'mode: branch' to
keep the profile's worktree default
```

Others are merely useless and are left alone (`mode: trunk` with `land: pr`
cuts no branch, so there is nothing for a PR to come from).

Full schema and the derivation table: [`.claude/skills/references/git-flow.md`](../.claude/skills/references/git-flow.md).
