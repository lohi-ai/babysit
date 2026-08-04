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
| branch per ticket | no | `feat/<id>_<slug>` | `feat/<id>_<slug>` |
| how work lands | push **is** the release | PR, author merges | PR, someone else merges |
| review happens | nowhere | locally, in the browser | on GitHub |
| QA rigor | smoke, 3–5 cases | standard, 5–10 | strict, 8–12 |

Rigor scales *breadth only*. `PASS` means the same in all three: every
applicable rubric dimension at B or better and a fresh end-to-end run on the
final code. A pet project runs fewer cases — never zero.

**The rest of this doc is about the part that isn't in the table: what each
profile expects *you* to do with your base branch.** Get that wrong and the
gates quietly stop working.

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

It never blocks — a diverged base is normal mid-flight. The `↑` line is the
one worth acting on: those commits are invisible to every ticket cut after
them. Board doesn't fetch, so the numbers are as fresh as the age in
parentheses.

### So what does a ticket branch get cut from?

**`origin/<base>`, never your local one** — and `pet` doesn't cut at all:

| profile | cut from |
|---|---|
| `pet` | **nothing is cut.** The ticket rides your current branch, in whatever state it's in |
| `startup` | `origin/<base>`, fetched first |
| `enterprise` | `origin/<base>`, fetched first |

`ensure` fetches `origin/<base>` before cutting, then forks from the
remote-tracking ref with `--no-track`. Both the in-place cut and the worktree
divert use the same source, so a diverted ticket is not second-class:

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

Several sessions in one folder, one dev server testing everything at once.
That's the appeal, and it's also the limit: all changes interleave on the base
branch, so you **cannot review one ticket in isolation, drop a bad one, or open
per-ticket PRs**. It's all or nothing.

If you want separable parallel work, dispatch worktrees (see below) — the
profile doesn't prevent it. But at that point `startup` is the better fit.

---

## `startup` — start each ticket from a clean base

Mode `branch`. The rule is a single line:

> **Be on `base_branch`, with a clean tree, when a ticket starts.**

That is the exact condition for cutting `feat/<id>_<slug>` *in place*. If the
tree is dirty or you're on another branch, `ensure` doesn't fail — it
**diverts to a worktree** and tells you so:

```
ensure: 'main' has uncommitted changes — diverting the cut to a worktree
ensure: WARNING — the worktree loop costs a commit + 'bbs ticket merge-base' per test iteration.
ensure: to keep the 0-step loop: commit or stash on 'main' and re-run.
```

The divert is a safety net, not a failure — but it silently moves you onto the
expensive inner loop. If you wanted the fast one, commit or stash first.

After the cut you're on the ticket branch and your base is untouched. Land with
a PR per ticket (`land: pr`).

For UI work, review in a browser before approving — a PR diff doesn't show you
a rendered page. How you get there depends on where the ticket lives:

- **Cut in place** (the normal case): your dev server is *already* serving the
  ticket branch, because the primary checkout is on it. That's the 0-step inner
  loop — just look at it. Don't reach for `serve` here; it would BLOCK, since
  `serve` requires the primary to be on `base_branch`.
- **Diverted to a worktree**: the primary is still on base, so compose it:
  ```bash
  bbs ticket serve <ticket>
  # …look at it…
  bbs ticket serve --release
  ```

## `enterprise` — same git contract, different endgame

Branch mechanics are **identical to `startup`**: same clean-base rule, same
in-place cut, same divert. Two things change:

1. **Review moves to GitHub** and someone else merges. Your local surface stops
   being the review venue.
2. **Strict QA releases the surface before handing off** — evidence travels in
   the PR body instead. Under `smoke`/`standard`, QA *leaves the app running*
   and puts the URL in the handoff; under `strict` it does not. Don't expect to
   find the app still up after a QA run.

Everything in the parallel section below applies unchanged.

---

## Running tickets in parallel

**No profile turns worktrees on.** They cost a commit plus a `merge-base` per
test iteration and buy exactly one thing — letting N tickets share one dev
server. Parallelism is requested *per dispatch*, on top of whatever profile you
have. Your profile keeps deciding rigor.

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
per dispatch:

```
/bbs:foreman
```

It also gates each design before any code is written, and merges finished
tickets onto your local base so you review the batch together.

### Option B — dispatch them yourself

```bash
/bbs:autopilot --mode=worktree "<requirement>"     # once per ticket
```

or, to create the worktree without starting work:

```bash
bbs ticket ensure --mode=worktree --slug-hint <slug>
```

### Then: compose, review, ship

```bash
bbs ticket board                  # status: tickets, verdicts, PRs, who holds the lease
bbs ticket serve                  # compose ALL finished tickets (qa + review-pr DONE) onto local base
bbs ticket serve <t1> <t2>        # …or exactly the ones you name
bbs ticket serve --release        # hand the surface back
```

`serve <t1> <t2>` is the payoff over `pet`-style parallel: you compose exactly
the tickets you trust, leave a bad one out, and each still lands as its own
clean PR.

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

- **Into worktree work** — the primary must end clean on `base_branch` first.
- **Out of it** — finish or park in-flight worktrees (`bbs ticket board`) and
  release any qa-lease.

Setting `mode:`, `land:`, or `push:` by hand overrides the profile's preset.
That's the escape hatch, not the normal shape — a knob written out by hand
stops tracking its profile, and the combination may not be coherent (`mode:
trunk` with `land: pr` cuts no branch, so there is nothing for a PR to come
from).

Full schema and the derivation table: [`.claude/skills/references/git-flow.md`](../.claude/skills/references/git-flow.md).
