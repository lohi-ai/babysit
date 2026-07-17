# babysit

English | [Tiếng Việt](README.vi.md)

**Hand it your feature requests. Workers build them in parallel while you watch. Review one combined result.**

```
/bbs:foreman product-wide search on the home page
/bbs:foreman rebuild the novel request flow
```

**`foreman` is the primary flow**: one visible tmux worker per request (each running autopilot end-to-end — plan, code, review, QA, push), a design review before any code is written, and every finished ticket merged onto your local base so you review the whole batch running in one browser — then you create the PRs.

For a **single ticket**, drive autopilot directly (this is also exactly what each worker runs):

```
/bbs:autopilot add a settings page with dark mode toggle
```

~40 minutes of autonomous work per ticket that finishes even though no single Claude session could hold it all at once. You review the branch, then open the PR when you're happy.

*babysit is what you do when you don't need a babysitter.* It prefers decisions Claude can make and verify alone over decisions that need a human in the loop — built for scheduled runs, orchestrated pipelines, and anything you want to walk away from.

## The easy way to taste it

If you're the main dev on a small team, this is the whole loop — it does the grind, you own the gate. Read it top to bottom like a program:

```bash
/bbs:setup-project                        # once per repo — branch + QA defaults
/bbs:autopilot "add dark-mode toggle"     # any change: it plans → codes → reviews → QAs
#   → read tickets/<id>/plan.md, then paste the printed /goal block and walk away
#   → autopilot writes the code, reviews it, runs QA, pushes the branch
#   → you review the evidence, then:
/bbs:create-pr                            # you open the PR — autopilot never does
```

Step by step:

- **`/bbs:setup-project`** once — teaches autopilot your branch naming + QA defaults, so everything downstream is deterministic.
- **`/bbs:autopilot "<one-line requirement>"`** for any multi-step work. It checkpoints to disk (survives crashes and context compaction), then plans → implements → reviews → QAs, and stops at the PR checkpoint so you review before anything merges.

**The human checkpoints — where you stay in control.** Autopilot only pauses at the moments that are actually yours to own; pick which one by flag:

- `--stop-after=plan` — approve the approach before any code is written.
- *default* — stops QA-ready, you review the evidence.
- **`/bbs:create-pr`** — you invoke it; autopilot never opens PRs itself.

**Add as you need it:**

- **`/bbs:review-pr`** (a.k.a. `/code-review`) — a gate before merge, since there's no second reviewer on a small team. Your safety net.
- **`/bbs:foreman`** — the parallel-batch flow this README leads with: one visible tmux worker per ticket, several independent tickets at once. Reach for it when you want that; overkill for solo, serial work.

## Why it works

- **It finishes.** `/bbs:autopilot` is a **goal proxy**: init seeds durable state — ticket, requirement, plan, checkpoint — then hands the work to [`/goal`](#3-run-it), Claude Code's session-scoped Stop hook that blocks the session from stopping until the QA and review verdicts are persisted. Inside the loop the model works free-form with full context, the way it would for a direct ask; checkpoints on disk let a fresh session resume where the last one stopped.
- **It doesn't hang.** Every decision routes through the [Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md). Claude decides and logs; if a human is genuinely required, it writes a `NEEDS_CONTEXT` block to the ticket instead of waiting on a pop-up.
- **It verifies itself.** QA is part of the default autopilot loop. PASS requires a locally running target or a named blocker, plus non-happy-path cases. No "it compiles, shipping it."
- **It's auditable.** JSONL telemetry to `~/.babysit/analytics/` plus `[WORK]` checkpoint comments. Read the tape after the fact — the primary feedback channel when no one is watching live.

## The five archetypes

As engineering, product, design, and data science melt into one kind of
product-builder, the useful unit of work is no longer the job title — it's the
*archetype* the work needs right now. Babysit is the product-building team:
it maps the Claude Code team's five archetypes onto skills and autopilot
workflows, so a single run can act as whichever teammate the task calls for.

A person spans 2–3 archetypes; so does a babysit run. Pick by the **shape of the
work**, not the file's "type." There is exactly **one autopilot workflow per
archetype**.

| Archetype | Mandate | Reach for it when | Workflow |
|-----------|---------|-------------------|----------|
| **Prototyper** | Churn brand-new ideas; most won't ship. Learn one thing fast. | No artifact exists yet, just a hunch — validate before you commit to building. | `prototyper` |
| **Builder** | Turn a prototype/idea into production-grade product and infra. | A validated idea or accepted plan exists. The default for new feature work. | `builder` |
| **Sweeper** | Clean up UI, simplify code and systems, unship, optimize. | The code carries weight it doesn't need — dead code, duplication, over-abstraction. Subtract; behavior must not change. | `sweeper` |
| **Grower** | Iterate on a shipped product to improve product-market fit. | The product ships but the funnel underperforms. Measure first, then run one reversible experiment. | `grower` |
| **Maintainer** | Keep a mature system secure, reliable, fast, and efficient at scale. | A mature system is under production/scale pressure — load, security, reliability, cost. | `maintainer` |

**Sweeper vs Maintainer** — the easy pair to confuse, since both touch
performance. Sweeper optimizes the *codebase* (subtract complexity, behavior
stays byte-identical; perf comes for free) and is triggered by accumulated
cruft. Maintainer optimizes the *system in production* (sustain it under real
load; caching/indexing/data-model changes may change timing) and is triggered by
scale, security, or cost. "A stable, widely-used feature" is a Maintainer
trigger; if it also needs structural cleanup, run Sweeper as a separate
behavior-preserving pass.

**Invariants every archetype keeps** — they differ only in *mandate and success
criterion*, never in rigor: decisions route through the
[Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md)
(taste decisions logged, never silently guessed); self-verification before
"done"; bounded blast radius (no force-push, no data loss, no external messages
without durable authorization); fail loud and local (`BLOCKED`/`NEEDS_CONTEXT`
over a wrong assumption).

Details and the skills each archetype composes:
[`.claude/skills/references/archetypes.md`](.claude/skills/references/archetypes.md).

## Quick start

Three steps. Install once globally, configure each repo once, then run.

### 1. Install the plugin

**Fastest — let Claude Code do it.** Paste this into Claude Code and it clones the repo, runs the installer, and then tells you the remaining steps one by one:

```
Install the babysit plugin for me: clone https://github.com/lohi-ai/babysit.git
into ~/.claude/skills/babysit, run ./bin/setup-skills --full, then list the exact next
steps I still need to run myself (the /plugin commands, configuring a repo, first run).
```

Claude handles the clone + `setup-skills`; the `/plugin` commands below are slash commands you run yourself, so it will hand those back to you as step-by-step instructions.

**Or do it by hand:**

```bash
git clone --single-branch --depth 1 https://github.com/lohi-ai/babysit.git ~/.claude/skills/babysit
cd ~/.claude/skills/babysit
./bin/setup-skills --full
```

Then inside Claude Code:

```
/plugin marketplace add ~/.claude/skills/babysit
/plugin install bbs@babysit
```

Requirements: Claude Code with plugin support, Git.

### 2. Configure your project

Inside any repo you want autopilot to ship from:

```
/bbs:setup-project
```

The wizard writes the smallest useful `.babysit/` config: `git-flow.yaml` with `base_branch`, `branch_prefix`, `push`, `mode`, and `qa.yaml` with `url`, `start`, `check`, `flows`. Re-running is idempotent.

#### Git flow: pick one of three modes

`mode:` in `.babysit/git-flow.yaml` decides where each ticket's branch lives — it's a property of the repo, set once:

| Mode | Shape | Best for |
|------|-------|----------|
| `trunk` | no branch cuts — tickets ride the shared branch (e.g. `develop`); identity travels as `BABYSIT_TICKET` | hobby repos: many sessions in one folder, one dev server tests everything at once |
| `branch` *(default)* | cut `feat/<id>_<slug>` in place when the checkout is a clean base; auto-divert to a worktree when it isn't | clean one-ticket PRs with mostly-serial work — cheapest QA, the server serves the ticket branch directly |
| `worktree` | every ticket gets its own worktree; the primary checkout stays pinned to base as the shared test surface | team/enterprise repos: parallel tickets, one clean PR each |

In `worktree` mode, QA lands a ticket on the shared surface with `bbs-ticket merge-base` (from the worktree), or hops the surface between tickets with `bbs-ticket switch <ticket>...` (from the primary — resets to base, then merges exactly the named tickets). After PRs merge upstream, `bbs-ticket reset-base` snaps local base back to origin. All three refuse loudly instead of losing work. The human-facing layer on top — `board`, `serve`, `/bbs:fix-pr` — is covered in [Working tickets in parallel](#working-tickets-in-parallel-worktree-mode). Details: [`references/git-flow.md`](.claude/skills/references/git-flow.md).

#### Solo dev or small team: trunk or worktree?

Head count is the wrong axis. What decides it is **how you're working right now — watching, or walking away:**

- **You're at the keyboard, course-correcting every run** (pair-programming on your own project): pick **`trunk`**. Tickets ride the current branch — no cut, no worktree, no PR ceremony. It's the fastest path precisely because a human is live to catch a wrong turn.
- **You want work to happen while you're not watching** — several tickets at once, or one you kick off and leave: pick **`worktree`**. Each ticket builds in its own worktree; the primary checkout stays pinned to base as the one dev server you review against. This is a *solo* flow too — one person running a batch in the background is a background worker, team or not.

That second case is exactly what **`/bbs:foreman`** turns on. Hand it a few requests; it spawns one visible worker per ticket, gates each design before any code is written, and — under `land: local` (the default in `worktree` mode) — merges every finished ticket onto your local base so you review the whole batch in one browser, then open the PRs yourself.

**Rule of thumb:** start on `trunk` while you're pairing. The day you want to hand off a batch and step away, re-run `/bbs:setup-project`, switch to `worktree`, and drive it with `/bbs:foreman`.

### 3. Run it

**Primary — `foreman`, the attended parallel flow:**

```
/bbs:foreman <one-line requirement>     # one worker per request; repeat for more
/bbs:foreman                                   # attach/resume: reconcile live workers + board
```

Foreman spawns a visible tmux worker per ticket (`tmux attach -t <session>` to watch or take over any of them), monitors the panes, and owns the checkpoint between design and build: when a worker stops at its plan/prototype handoff, foreman reviews the design, gives feedback, and either greenlights the build or escalates to you when your voice could change the outcome. It answers workers' mechanical questions itself, relays the ones that need you, verifies every QA/review verdict on disk, and — with `land: local` (the default in worktree mode) — merges all finished tickets onto your local base so you review the combined product on the dev server before deciding: per-ticket PRs or one compose PR.

**Secondary — `autopilot`, the single-ticket flow** (also what every foreman worker runs):

```
/bbs:autopilot "add a settings page with dark mode toggle"
```

Autopilot inits the ticket — requirement, plan, branch — then stops and prints a `/goal` block as its **last message**. That block is the one thing you do next: **copy it, paste it back into Claude Code, and walk away.** The goal session then writes the code, reviews it, runs QA, and pushes the branch. Open the PR yourself after review.

> **The handoff looks like this** — autopilot ends with a plain-language preamble, then the block to copy:
>
> ```
> Ready for bs-ab123. Before you paste, review what will be built:
>   plan:      tickets/bs-ab123/plan.md
>   prototype: tickets/bs-ab123/prototype.html
> Redirect the design now if it's wrong — otherwise you're one paste from done.
>
> 👉 Copy the block below and paste it into Claude Code to build it:
>
> /goal bs-ab123 is done: qa verdict PASS/FIXED persisted via bbs-ticket set-verdict,
> review-pr verdict persisted, branch pushed, handoff note written — or a
> NEEDS_CONTEXT / BLOCKED status block printed verbatim.
> Work it: /bbs:autopilot builder bs-ab123
> ```

#### Why `/goal` owns the work

`/goal <condition>` (built-in, Claude Code 2.1.139+) arms a session-scoped Stop hook: the model works free-form with full context — no step ceremony — and the hook blocks stopping until the condition holds. That's why the step is *paste the `/goal` block* rather than "run a command": pasting it is what arms the hook. Autopilot's printed block already encodes the babysit gates and the escape clause.

The escape clause means the loop terminates on escalation instead of grinding against a missing input. To bail mid-run: `/goal clear`, `Ctrl-C`, or touch `~/.babysit/projects/<slug>/tickets/<ticket>/STOP`.

Without `/goal`, re-invoking `/bbs:autopilot bs-ab123` still resumes from the checkpoint — you just nudge it past session boundaries by hand.

## How to use it

Babysit is a small assembly line for shipping a change. You drop an idea at one end and pick up a branch ready for review at the other. The line pauses at the four moments where you actually add value; everything between happens on its own.

### The four places it pauses

1. **"Is this the right thing to build?"** — `requirement.md` ready. You read and accept.
2. **"Is this the right way to build it?"** — `plan.md` ready. You read, tweak, accept.
3. **"Does it actually work?"** — code written, reviewed, QA checked, pushed.
4. **"Should this become a PR?"** — you review the handoff and run `/bbs:create-pr`; when reviewer comments land, `/bbs:fix-pr` works through them.

### Pick where it stops

| Stop at | How |
|---------|-----|
| pause 1 — `requirement.md` ready | `/bbs:autopilot "<idea>" --stop-after=requirement` |
| pause 2 — `plan.md` ready | `/bbs:autopilot "<idea>" --stop-after=plan` |
| pause 3 — QA-checked branch ready | `/bbs:autopilot "<idea>"` *(end-to-end, the default)* |
| pause 4 — PR handoff | run `/bbs:create-pr` after human review |

When a stage finishes, the ticket gets a `Next:` line — literally what to do next. Re-invoking `/bbs:autopilot bs-<id>` always picks the right next stage from probed state, so you never need to remember which workflow to call.

### Three input shapes

```
/bbs:autopilot "<one-line idea>"     # new feature — creates ticket + branch, runs end-to-end
/bbs:autopilot bs-ab123              # existing ticket — state-routes to the next stage
/bbs:autopilot                       # resume — picks up from the current branch's checkpoint
```

That's the whole surface. Flags (`--stop-after=`, `--replan`, `--dry-run`, `--workflow=<name> --force`) extend it; verb tokens don't exist.

### Working tickets in parallel (worktree mode)

`/bbs:foreman` runs this whole section for you — dispatch, design gates, verdict checks, and the composed final surface. The commands below are the layer underneath, for when you drive it by hand or want to see what foreman is doing.

One heavy checkout per repo runs the dev server; every ticket lives in its own lightweight worktree. That makes everything parallel *except* the moment someone needs to see a ticket actually running — and that moment gets three commands:

```bash
bbs-ticket board            # every ticket at a glance: status, verdicts, live session, PR, who holds the surface
bbs-ticket serve bs-ab123   # put this ticket on the running dev server for human review
bbs-ticket serve            # bare: compose every finished ticket (qa + review DONE) on the server
/bbs:fix-pr                 # after reviewer comments land: fetch unresolved threads, fix, reply, resolve
```

**The review loop.** Reviewing the running feature in the browser is the longest must-do step, so babysit makes it the cheapest to repeat:

1. A ticket reaches pause 3 — its handoff's `Next:` line hands you the exact command: `bbs-ticket serve bs-ab123`.
2. `serve` holds the test surface for 4 hours (agents' QA politely queues behind you) and switches the running server to base + exactly this ticket — in this repo **and** in its FE/BE sibling repo when the ticket spans both.
3. Review in the browser. Ask the ticket's session for changes; it commits in its own worktree; re-run `serve` (reentrant — refreshes the hold, re-cuts the surface) and refresh the browser. Repeat until happy.
4. Approved → `bbs-ticket serve --release`, then `/bbs:create-pr` per repo. Reviewer comments later → `/bbs:fix-pr`.
5. `bbs-ticket board --pr` flags merged PRs and prints the exact cleanup commands (`reset-base`, `set-status done`).

**One ticket, two repos** (a feature spanning frontend + backend): `/bbs:setup-project` records the sibling repos once; autopilot's builder crosses over on its own — creates the linked sibling ticket, implements and QAs both sides — and `serve` puts the whole pair in front of you with one command. Meanwhile other tickets' sessions keep implementing and reviewing in their own worktrees; `board` shows everyone who holds the surface and for how long. Full recipe: [`references/git-flow.md` § Attended parallel review](.claude/skills/references/git-flow.md).

## Going deeper

- **Routing internals & debugging** — Parse → Probe → Assign → Dispatch, `bbs-autopilot explain`, `--dry-run`, `--replan` / `--force` escape hatches: [`.claude/skills/autopilot/SKILL.md`](.claude/skills/autopilot/SKILL.md).
- **Config schemas** — [`.claude/skills/references/git-flow.md`](.claude/skills/references/git-flow.md) and [`docs/qa-config.md`](docs/qa-config.md) for hand-authoring `.babysit/`.

## Skill index

`/bbs:autopilot` composes the skills below into full workflows. Reach for one directly when you want just the piece — the greatest hits:

| I want to… | Skill |
|------------|-------|
| Run several feature requests in parallel while staying able to watch | `/bbs:foreman "<idea>"` |
| Stress-test an idea before I commit to building it | `/bbs:office-hours` |
| Design a feature inside the existing UI system | `/bbs:design-ui` |
| Ship a feature end-to-end from a one-line idea | `/bbs:autopilot "<idea>"` |
| Turn a requirement into `plan.md` (without coding it) | `/bbs:plan-draft` |
| Build from an already-accepted plan | `/bbs:implement` |
| Improve marketing copy or conversion | `/bbs:copy-rewrite`, `/bbs:conversion-fix` |
| Propose growth experiments or short-form scripts | `/bbs:growth-experiment`, `/bbs:social-content` |
| Check a URL or frontend flow in a browser | `/bbs:browse` |
| Run a full browser test/fix loop | `/bbs:qa` |
| Review a branch before landing | `/bbs:review-pr` |
| Root-cause a bug | `/bbs:investigate` |
| Configure this repo for autopilot | `/bbs:setup-project` |
| Create a reviewable pull request | `/bbs:create-pr` |
| Work through PR review comments (fix, reply, resolve) | `/bbs:fix-pr` |

Full skill table (with autonomous-ready / interactive-only classification) in [`docs/skills.md`](docs/skills.md).

## Companion CLI

`setup-skills` symlinks a handful of `bbs-*` bins into `~/.claude/` — `bbs-autopilot` (the runner), `bbs-slug` (branch-as-anchor resolver), plus helpers for env, config, db snapshots, and upgrade checks. Full table and purposes in [`docs/companion-cli.md`](docs/companion-cli.md). Run `<bin> --help` for usage on any of them.

## Operations

Day-2 config (`bbs-config`), telemetry (JSONL to `~/.babysit/analytics/`, local-only by default), and upgrade handling (`bbs-update-check` + `bbs-upgrade`) are covered in [`docs/operations.md`](docs/operations.md).

**Upgrade.** `cd ~/.claude/skills/babysit && git pull && ./bin/setup-skills`, then `/plugin marketplace update babysit` + `/reload-plugins` in Claude Code.

## Uninstall

```
/plugin uninstall bbs@babysit
/plugin marketplace remove babysit
```

```bash
./bin/setup-skills --uninstall
rm -rf ~/.claude/skills/babysit ~/.babysit
```

Manual cleanup if legacy symlinks remain from a pre-plugin install:

```bash
find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete
rm -f ~/.claude/babysit ~/.claude/bbs-*
```

## Troubleshooting

| Issue | Fix |
|-------|-----|
| Skills missing after upgrade | `cd ~/.claude/skills/babysit && git pull`, then `/reload-plugins` |
| `/bbs:*` not found | `/plugin marketplace add ~/.claude/skills/babysit` + `/plugin install bbs@babysit`; or `/reload-plugins` |
| Skills show without `bbs:` prefix | Legacy install — run `./bin/setup-skills`, then `/plugin install ~/.claude/skills/babysit` |
| `env resolve` returns empty | Check the right `.env.base` exists under `config/<app>/` |

## License

MIT.
