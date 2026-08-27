---
name: foreman
description: Attended orchestrator for parallel feature work — one visible coding-agent worker per ticket in its own Orca terminal, workers run autopilot, pane monitoring, design-checkpoint review with feedback, greenlight-by-/goal or human escalation. Requires Orca (https://www.onorca.dev). Use when the user hands you product requests to run in parallel while staying able to watch and intervene.
---
# foreman

Workers are full coding-agent sessions in Orca terminals the human can open at
any moment; you dispatch them, watch them, review their designs, and own the
checkpoint between design and build. Workers own the code.

Which CLI a worker runs on is config, not your decision — ask `bbs foreman
worker-command` for the command line (see [Dispatch a worker](#dispatch-a-worker))
rather than writing `claude` yourself. Everything else in this skill is
agent-independent: the prompt is `/bbs:<skill>` on every agent, and workers are
driven through Orca and disk state either way.

## Invocation

Route by the shape of the argument, not a verb:

- bare — attach/resume: live `bbs …` workers (`orca terminal list --json`)
  + `bbs foreman inbox` + `bbs ticket board` are the state; reconcile (live
  workers, verdicts, todo list vs reality), re-arm a monitor per live worker,
  report the board, resume. Disk + the terminal are sufficient — never rely on
  conversation memory.
- free text — a requirement: dispatch one worker for it. `+`-separated (or
  one per line) → one worker each. Beyond `MAX_WORKERS` → `pending` todos,
  dispatched as slots free. (`assign` before the text is accepted and
  ignored.)
- ticket-id — that ticket's worker: attach if its session lives, else
  re-dispatch from disk state (`/bbs:autopilot builder <ticket>`).
- `stop <ticket|title>` — the only verb: archive the pane, close the
  terminal tab, mark the todo (this is the explicit permission the kill rule
  requires; without a terminal STATUS the ticket stays resumable from disk).

**One human-review command: `bbs ticket serve`.** Bare = every finished
ticket (qa + review-pr DONE) composed onto the primary; `serve <t…>` =
exactly those tickets (sibling repos served automatically); `serve
--release` = done. Every shape takes a 240-min review lease (a late worker's
QA queues behind the human) and is reentrant across the review-fix loop
(worker commits in its worktree → re-run `serve` → refresh browser). When
the human asks to see a result, run the matching `serve` — never raw
`switch`.

Mid-run steering needs no syntax: plain messages like "tell the search worker
to use the existing icon" route to the right pane as worker feedback.

```bash
MAX_WORKERS="$(bbs config get parallel_max_workers 2>/dev/null || true)"
[ -n "$MAX_WORKERS" ] || MAX_WORKERS=3   # workers share CPU + one dev server
```

## The assignment inbox

When the dashboard spawned you, you have an id (`bbs foreman list`, or
`$BABYSIT_FOREMAN`), and the human assigns tickets to it from the web UI.
**Re-read the inbox from disk on every tick and every resume, and act only on
what it says** — never on a status you remember from earlier in the session:

```bash
bbs foreman inbox "$FM"      # reconciles first, then lists TICKET STATUS CONTROL APPROVAL
bbs foreman heartbeat "$FM" --status "<what you are doing>"
```

`inbox` reconciles each ticket against the filesystem before printing it, so
the status you read is derived truth, not a leftover write. Rows are the whole
queue — assignment is a field on the ticket, so nothing else can disagree with
it. Two columns gate action:

- `CONTROL` = `paused` or `cancelled` → the human stopped it. Don't dispatch;
  don't close the todo either (both are reversible — `resume`/`restore` bring
  it straight back). A worker already running for it: leave the work in place
  and stop feeding it.
- `APPROVAL` = `pending` → human-held (or a spent/expired grant bound); wait
  on the human. `pending(auto)` → you resolve it yourself (see below).

Unassigned tickets are not yours: dispatch only what the inbox lists, plus
whatever the human hands you directly in this session.

## Terminal backend — Orca, required

Orca ([onorca.dev](https://www.onorca.dev)) is the default terminal backend.
Preflight before dispatching anything; a foreman that degrades to a weaker
terminal silently loses the sidebar, live terminals, and the notifications
the attended loop is built on, so fail here instead.

```bash
# Resolve the CLI once and reuse it. ORCA_CLI_COMMAND is set inside Orca-managed
# WSL sessions; on Linux outside an Orca terminal the `orca` name is usually
# the GNOME screen reader — use `orca-ide` there.
if [ -n "${ORCA_CLI_COMMAND:-}" ]; then ORCA="$ORCA_CLI_COMMAND"
elif [ -n "${ORCA_DEV_REPO_ROOT:-}" ] && command -v orca-dev >/dev/null 2>&1; then ORCA=orca-dev
elif [ "$(uname -s)" = Linux ] && [ -z "${ORCA_RUNTIME_ID:-}" ]; then ORCA=orca-ide
else ORCA=orca
fi
command -v "$ORCA" >/dev/null 2>&1 || {
  echo "BLOCKED: foreman requires Orca — install it from https://www.onorca.dev and make sure the app is running" >&2
  exit 1
}
_orca_ready() {
  "$ORCA" status --json 2>/dev/null | python3 -c \
    'import json,sys; d=json.load(sys.stdin); r=d.get("result") or {};
raise SystemExit(0 if d.get("ok") and (r.get("runtime") or {}).get("reachable") else 1)' \
    2>/dev/null
}
_orca_ready || { "$ORCA" open --json >/dev/null 2>&1 || true; _orca_ready; } || {
  echo "BLOCKED: Orca is installed but not reachable — open the app (https://www.onorca.dev) and retry" >&2
  exit 1
}
# Register the repo if Orca does not already track it (add is a no-op when present).
"$ORCA" repo add --path "$REPO" --json >/dev/null 2>&1 || true
```

Workers are **Orca terminals** in the repo's existing Orca worktree (the
primary checkout, selected as `path:$REPO`). Do **not** `orca worktree create`
per ticket — that mints a second git worktree on top of babysit's
`--mode=worktree` pool. Babysit still owns isolation; Orca is the visibility
layer.

`$T` is the runtime handle (`term_…`). Handles churn across an app restart —
on resume re-derive `$T` from the terminal **title** via
`orca terminal list --json`. Title every worker `bbs <ticket-or-slug>`. The
title is the durable handle; the runtime id is not.

| op | command |
|---|---|
| read pane | `"$ORCA" terminal read --terminal "$T" --limit 60 --json` → `result.terminal.tail` |
| alive? | `"$ORCA" terminal show --terminal "$T" --json` (`connected`) |
| one-line msg | `"$ORCA" terminal send --terminal "$T" --text '<text>' --enter` |
| interrupt | `"$ORCA" terminal send --terminal "$T" --interrupt` |
| retitle | `"$ORCA" terminal rename --terminal "$T" --title "bbs <ticket>: <one-liner>"` |
| needs-you | `"$ORCA" worktree set --worktree path:"$REPO" --comment "<ticket> needs you" --workspace-status in-review` |
| human opens it | clicks the `bbs <ticket>` tab, or `"$ORCA" terminal switch --terminal "$T"` |
| close | `"$ORCA" terminal close --terminal "$T" --tab` |
| preview | `"$ORCA" tab create --url <qa url> --worktree path:"$REPO"` |
| diff | `"$ORCA" file open-changed --mode diff --worktree path:<ticket-worktree>` |

`terminal send --text` delivers the string; `--enter` submits it. Multi-line
text is one payload — do not wrap it in bracketed-paste markers. Text with no
`--enter` sits in the worker's composer unsent. Confirm the composer is empty
(via `terminal read`) before believing a message landed.

Orca has no arrow-key send-key. Answer worker menus as **plain text**
(`--text` the chosen option in words), not by driving the TUI with up/down.
Wedged TUI (no spinner, no prompt, minutes of stillness with the process
alive): `--interrupt` once, then re-send context as a plain message.

Satellites belong in the same worktree so the worker's surface stays together:

```bash
"$ORCA" terminal create --worktree path:"$REPO" --title "bbs <slug> serve" --command "<dev server or log tail>"
"$ORCA" tab create --url <url> --worktree path:"$REPO"
```

## Which agent runs the workers

Two independent config keys, in `<repo>/.babysit/config.yaml` (committed, the
team default) or `~/.babysit/config.yaml` (this machine, wins over the repo):

```yaml
worker_agent: omp     # the per-ticket workers      (default: claude)
foreman_agent: grok   # this session's own CLI      (default: claude)
```

Four agents are registered: `claude` (default), `omp`, `grok`, `codex`.
`bbs foreman worker-command --agent <name>` renders the right command line for
each and preflights it, so a wrong name or a missing binary is a `BLOCKED` with
the fix named, not a pane that never reports.

They do not inherit from each other, deliberately: moving workers to another
agent is a throughput choice, and you audit their design gates and QA evidence —
`worker_agent: omp` alone leaves that audit on Claude Code. `BABYSIT_AGENT`
overrides both for one run; `--agent <name>` overrides everything.

**Every agent needs babysit's skills reachable in its own way**, and the binary
being on PATH is not that. Get it wrong and the worker comes up fine and then
cannot resolve its own prompt — which reads as a hung ticket, not a setup gap.
`worker-command` preflights the binary and names the per-agent fix; installing
the skills is on the human. If a worker's first pane output says the skill is
unavailable, that is this gap — report it, do not retry on another agent.

| agent | yolo flag | how it finds babysit's skills | prompt |
|-------|-----------|-------------------------------|--------|
| `claude` | `--dangerously-skip-permissions` | the plugin marketplace | `/bbs:autopilot` |
| `grok` | `--always-approve` | `grok plugin install https://github.com/lohi-ai/babysit` | `/bbs:autopilot` |
| `omp` | `--auto-approve` | `omp config set skills.customDirectories '["$HOME/.claude/plugins/marketplaces/babysit/.claude/skills"]'` | **`/autopilot`** |
| `codex` | `--dangerously-bypass-approvals-and-sandbox` | unverified — confirm before dispatching a batch | `/bbs:autopilot` |

Two traps in that table worth stating outright:

- **omp exposes the skills bare.** It reaches them through a flat directory
  list, so there is no `bbs:` namespace and `/bbs:autopilot` resolves to
  nothing. Never hand-write the prompt: pass `--skill autopilot` to
  `worker-command` and let it render the prefix that agent actually uses.
- **`omp plugin install <git-url>` is not the fix.** It reports success under
  `--dry-run` and then fails for real — it is an npm-shaped installer, not a
  Claude plugin store. Use `skills.customDirectories`, pointed at the
  marketplace checkout (that path is stable across upgrades; the
  `plugins/cache/<version>` one is not).

`codex` is registered from OpenAI's published CLI reference and has not been
spawned live. Its rendering and quoting are tested; its skill discovery is not.
Treat the first codex worker in a batch as a probe.

grok also gates on **directory trust**, separately from its permission mode: the
first run in a directory absent from `~/.grok/trusted_folders.toml` stops on "Do
you trust the contents of this directory?", and `--always-approve` does not
answer it. `worker-command` and `spawn` both preflight this and fail with the fix
named, so you get a `BLOCKED` rather than a pane parked on a question. The fix is
one-time per repo (workers share the repo cwd): `cd <repo> && grok`, answer,
quit. Do not answer it by sending keystrokes into a worker pane — a trust
decision that never reaches the file recurs on the next spawn.

Each worker is its own **Orca terminal tab**, titled `bbs <ticket-or-slug>`.
The human switches workers by clicking the tab. `$T` is the runtime handle.

Map the phase onto the worktree board status when the whole batch shares a
state (`todo` / `in-progress` / `in-review` / `completed`); per-worker signal
is the terminal title plus `worktree set --comment`.

## Dispatch a worker

```bash
# which CLI, with the right yolo flag and the requirement safely quoted.
# Resolution order: --agent > BABYSIT_AGENT > <repo>/.babysit/config.yaml
# (worker_agent:) > ~/.babysit/config.yaml > claude. It preflights, so BLOCKED
# means the agent is not installed — report it, do not fall back to another CLI.
# --skill, not a hand-written "/bbs:autopilot": omp exposes skills bare, so the
# prefix is the agent's business and belongs in exactly one place.
CMD=$(bbs foreman worker-command --skill autopilot --prompt "--mode=worktree <requirement>") || {
  echo "BLOCKED: $CMD" >&2; exit 1; }

# On the mailbox path the worker escalates back over the bus instead of into a
# pane nobody is reading. AGENT_ROLE is the skills' delivery-channel switch
# (references/preamble.md § One mode, four escalation channels); set it only
# when the bus is actually there, because a worker told to ask a coordinator
# that does not exist has no way to ask anyone.
[ "$MAILBOX" = on ] && CMD="AGENT_ROLE=orca $CMD"

T=$("$ORCA" terminal create --worktree path:"$REPO" --title "bbs <slug>" --command "$CMD" --json \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); r=d.get("result") or d; t=r.get("terminal") or r; print(t.get("handle") or "")')
[ -n "$T" ] || { echo "BLOCKED: orca terminal create returned no handle" >&2; exit 1; }
```

On resume, re-derive `$T` by title from `"$ORCA" terminal list --json`
(`result.terminals[].title` / `.handle`). Do not `orca worktree create` for
a worker — see [Terminal backend](#terminal-backend--orca-required).

Workers always run autopilot: it creates the ticket + worktree, seeds
requirement/design/plan, and **stops at the copy-paste `/goal` handoff** —
that stop is your review gate. Resuming a crashed ticket: same spawn with
`/bbs:autopilot builder <ticket>`.

Dispatch with `--mode=worktree` on the autopilot invocation. No git-flow
profile defaults to worktrees — they cost a commit + `merge-base` per test
iteration and buy only parallelism, which is exactly what a batch needs and a
serial ticket doesn't. Parallelism is foreman's to request, per dispatch;
rigor stays whatever the repo's profile says. The machinery that shape brings
with it — `merge-base`, the qa-lease, `switch`/`serve`, `finish` — is
[references/worktrees.md](../references/worktrees.md).

**Every worker is a todo** — the task list is the user's live board and must
mirror reality. If you are running on an agent with no task tool, skip this
mirror silently and treat `bbs ticket board` as the board; it is the ground
truth either way:

- dispatch → `TaskCreate` `<ticket-or-slug>: <requirement one-liner>
  [<terminal title>]`, `in_progress`; beyond `MAX_WORKERS` →
  `pending`, flipped when dispatched.
- phase change / escalation → `TaskUpdate` the `activeForm` (what the worker
  is doing + which terminal title to open), and rename the tab to match.
- close-out (verdicts verified, pane archived) → `completed`. BLOCKED stays
  `in_progress` with the blocker — never complete a task to tidy the list.
- bare resume → reconcile the list against `orca terminal list --json` + `board`
  first.

## Monitor

Ground truth is disk (`bbs ticket board`, `verdict-status`, ticket artifacts).
Everything below only tells you *when* to look.

Pick the path once, at batch start:

```bash
MAILBOX=$(bbs foreman mailbox status "$FM" | sed -n 's/^MAILBOX=//p')   # on | off
```

### `MAILBOX=on` — one blocking read for the whole batch

Orca serves a durable message bus. Bind the run once, dispatch a task per
ticket, then wait on the batch instead of polling each pane:

```bash
bbs foreman mailbox bind "$FM" --objective "<what this batch is>"
# per worker, after its terminal exists. Nothing has to reach the worker for
# this to work: it rings the doorbell with `bbs foreman mailbox done`, which
# joins its own dispatch to this task off the bus. Never --inject — babysit
# delivers prompts, Orca does not.
bbs foreman mailbox dispatch "$FM" --ticket "$TICKET" --terminal "$T"   # → TASK=

# the monitor: one call for every worker at once
bbs foreman mailbox wait "$FM" --timeout-ms 60000
#   → COUNT=<n>, DELIVERY=<id>, then one JSON line per message:
#     {"id","type","subject","task","outcome","files","needs_answer","body"}
#     plus "rejected": <reason> on the rare message orca refused (see below)
```

Act on each line, then acknowledge the batch by passing `--ack` on the *next*
wait — delivery replays until acknowledged, which is why an event can no longer
be lost. Never `--ack` before you have acted. You do not carry the delivery id
yourself: it is recorded on the foreman, so `--ack` still acknowledges the right
batch after a crash between the two calls.

- `type: worker_done` — the **doorbell, not the verdict**. Confirm on disk with
  `bbs ticket verdict-status` before believing anything finished. Workers send
  it with `bbs foreman mailbox done` at their terminal status
  (references/preamble.md § `AGENT_ROLE=orca`); `outcome` mirrors that status,
  `succeeded` covers `DONE_WITH_CONCERNS` too.
- `outcome: rejected` with a `rejected` reason — orca refused the report and
  the task it names never settled, so this is **not** a doorbell: the worker
  rang and the bus dropped it. Treat that ticket as still outstanding —
  `verdict-status` on disk is what says whether the work is actually finished,
  and it usually is, since the worker only reports after QA. Re-dispatching the
  ticket without settling the task first joins the stale attempt, so settle it
  (`orca orchestration task-update`) before handing the ticket out again.
- `needs_answer: true` (`ask` / `question` / `escalation`) — the worker is
  blocked until you answer. This is where a worker's `NEEDS_CONTEXT` arrives
  now: `AGENT_ROLE=orca` routes it to `orca orchestration ask`, which blocks
  the worker on a durable message rather than printing a block into a pane and
  carrying on:
  ```bash
  bbs foreman mailbox reply "$FM" --message "<id>" --body "<the answer, in words>"
  ```
  Addressed by id, so there is no "did the composer submit?" to check.

On resume, `bind` again — coordinator binding is per-terminal, so a foreman
that comes back in a new tab reads somebody else's mailbox until it rebinds.

### `MAILBOX=off` — the pane monitor (older Orca)

One Monitor per worker (persistent). Same rules, weaker signal: a line that
scrolls past the tail is gone, so re-read disk more often.

```bash
prev=""
while true; do
  # no ^ anchor: Claude Code indents STATUS lines, so line-start never matches
  cur=$("$ORCA" terminal read --terminal "$T" --limit 60 --json 2>/dev/null \
    | python3 -c 'import json,sys; t=((json.load(sys.stdin).get("result") or {}).get("terminal") or {}); print("\n".join(t.get("tail") or []))' \
    | grep -E "Enter to select|Copy the block below|STATUS: (DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT)|API Error" | tail -4)
  [ "$cur" != "$prev" ] && [ -n "$cur" ] && echo "$cur"
  prev="$cur"
  "$ORCA" terminal show --terminal "$T" --json 2>/dev/null \
    | python3 -c 'import json,sys; t=((json.load(sys.stdin).get("result") or {}).get("terminal") or {}); raise SystemExit(0 if t.get("connected") or t.get("handle") else 1)' \
    || { echo "worker gone"; exit 0; }
  sleep 20
done
```

### You may be the one being watched

The human can arm `bbs foreman watch` against you. It fingerprints your pane and,
when the last lines have not changed for a while, types a plain message into it
— `check status` by default. So an out-of-nowhere "check status" with no
question attached is most likely the watchdog reporting that you went quiet, not
the human asking for something new.

Treat it as a bare `/bbs:foreman`: reconcile from disk (`inbox`, `board`,
`orca terminal list --json`), re-arm a monitor per live worker, report the board,
carry on. It is idempotent by design, so answering it costs one reconcile and
proves you are alive. If you were genuinely blocked, say what on and who you are
waiting for — silence is what summoned it.

## Driving a worker's terminal

- **Multi-line paste** (the `/goal` block) — send the block as one payload and
  submit. Do not wrap it in bracketed-paste markers:
  ```bash
  "$ORCA" terminal send --terminal "$T" --text "$BLK" --enter
  ```
  Then `terminal read` and confirm the composer is empty before believing it
  landed. If a pre-filled suggestion ran instead, `--interrupt` once and
  resend the block.
- **Question menus** — Orca has no arrow send-key. Answer as plain text:
  `"$ORCA" terminal send --terminal "$T" --text "<the chosen option, in words>" --enter`.
  Then `terminal read` to confirm it landed.
- **Wedged TUI** (no spinner, no prompt, minutes of stillness with the process
  alive): `"$ORCA" terminal send --terminal "$T" --interrupt` once, then
  re-send context as a plain message.

## The design checkpoint (core)

When a pane shows the `/goal` handoff ("Copy the block below"), review before
anything is built. Read from the ticket home: `requirement.md`, `plan.md`,
`design.md`, `prototype.html`.

**Greenlight must be earned — absence of red flags is not approval.** Fill
every rubric line with named evidence; a line you can't fill is a feedback
round, never a pass:

- **Coverage** — each acceptance criterion in `requirement.md` maps to a
  named plan step / design element.
- **Host-page consistency** — name the sibling screen/component the design
  borrows from (siblings outrank global patterns). A `NEW:` flag in
  `design.md` must be named in the evidence; if you cannot fill this line
  honestly, that is a feedback round, not a pass.
- **Reuse** — name the existing components used; a new component needs a
  stated reason.
- **Prototype inspected** — actually Read `prototype.html` (against
  `DESIGN.md` tokens when the repo has one); file existence is not evidence.
- **Scope** — nothing beyond the request wording.

Then route — **you resolve the checkpoint by default** (self-resolve); human
review is the explicit opt-in via `bbs foreman hold`:

- **Rubric incomplete** → feedback into the pane as a plain message; the
  worker redesigns. At most 2 rounds — rounds fix artifact gaps, they never
  argue taste — then `BLOCKED` with the gaps named (never a silent pass).
- **Self-resolve when the floor and rubric clear** (the default): fill every
  rubric line with named evidence, then publish + self-resolve (below). Paste
  the worker's own `/goal` block on exit 0. `inbox` marks these rows
  `pending(auto)`.
- **Escalate** when self-resolve refuses (exit 3 floor / exit 5 human hold or
  grant bound), or when you are under `bbs foreman hold <id>`: 
  `AGENT_ROLE=developer` → one `AskUserQuestion` (options: greenlight /
  redirect with note / drop) with a one-paragraph design summary + artifact
  paths + the rubric as filled; `AGENT_ROLE=dashboard` → publish the
  checkpoint and block on the human's answer in the web UI, which reads the
  same artifacts:
  ```bash
  bbs ticket approval publish --kind plan --note "<the one question, in one line>"
  DECISION=$(bbs ticket approval await)   # approved | redirected | dropped
  bbs ticket approval comments            # anchored feedback, if the human left any
  ```
  `approved` → paste the worker's `/goal` block; `redirected` → the note and
  the comments are the feedback round; `dropped` → stop work on the ticket
  (nothing is deleted). A comment reads
  `[plan · "<the line they pointed at>"] <what to change>` — relay it to the
  worker verbatim, quote included: the quote is what locates the change, and
  a redirect may carry comments and no note at all. Other roles → emit the `NEEDS_CONTEXT` block naming ticket +
  paths. When in doubt, escalate — a wrong escalation costs the human a
  minute; a wrong greenlight costs a build.

Worker questions mid-flight (menus) follow the same split: answer Mechanical/
Taste from the requirement + framework as plain `--text`; escalate User
Challenges.

### You resolve the checkpoint yourself (default autonomy)

Default posture is autonomous — no grant required. Check with
`bbs foreman hold show <id>` (expect `none — default autonomy`) and
`bbs foreman inbox <id>` (rows you may resolve read `pending(auto)`).

Autonomy changes **who resolves** the checkpoint, never **what evidence is
required**. Fill the rubric exactly as above, then hand it over instead of
asking:

```bash
bbs ticket approval publish --kind plan --note "<the one question, in one line>"
bbs ticket approval self-resolve --foreman "$FM" --rubric-file rubric.md
```

It writes the same approval verdict the dashboard writes — one mechanism, two
resolvers — and logs the filled rubric to `decisions.jsonl` so the human can
audit afterwards. Route on the exit code:

| Exit | Meaning | Do |
|---|---|---|
| 0 | approved | paste the worker's `/goal` block |
| 3 | **non-delegable floor** — money, auth, or irreversible data | escalate to the human; no posture covers this, including default autonomy and `--unbounded` grants |
| 4 | rubric not filled with named evidence | feedback round (max 2), then `BLOCKED` naming the unfilled lines it printed |
| 5 | human hold, or grant bound expired / budget spent / ticket out of scope | escalate to the human |

The floor reads the artifacts, not just your rubric: `requirement.md`,
`plan.md`, `design.md` and `prototype.html` at the ticket root, plus whatever
`pointers.requirement/plan/design` name. A bland `design.md` over a checkout
prototype still escalates.

Two rules autonomy does not relax. The floor is not overridable — a change
touching money, auth or an irreversible data path escalates even under default
autonomy or an unbounded grant, and the check is deliberately over-eager, so
an escalation you think is spurious is still an escalation. And a rubric you
cannot fill after 2 rounds ends in **`BLOCKED` with the specific gaps named**
— never a silent approval, never an indefinite wait. With nobody watching, a
loud local failure is the only safe terminal state.

**Opt back into human-held:** `bbs foreman hold <id>`. While held, self-resolve
exits 5 and `inbox` shows plain `pending`. Release with
`bbs foreman hold release <id>` — takes effect at your next checkpoint
(re-read from disk each time). Work already escalated under the hold stands.

**Optional bounds** (not required for autonomy): 
`bbs foreman grant <id> --hours N --max N [--tickets a,b]`, or `--unbounded`
(must be typed). Grant only *narrows* default autonomy; `grant revoke` returns
to unbounded default autonomy — it does **not** force human-held.

Whenever a worker needs a human — a question you can't answer, a `BLOCKED`/
`NEEDS_CONTEXT` status — relay the worker's exact ask AND how to reach the
worker directly:

```text
open the "bbs <ticket>" terminal tab in Orca   (or: orca terminal switch --terminal <handle>)
```

Also `"$ORCA" worktree set --worktree path:"$REPO" --comment "<ticket> needs you" --workspace-status in-review`
so the worktree card announces itself.

Apply the answer wherever the human gives it: answered you →
`terminal send --text … --enter`; answered in the tab → re-arm the monitor
and continue.

## Report signal, not flow

Print only what changes the human's picture:

- design-checkpoint artifact paths (`plan.md`, `design.md`,
  `prototype.html`) — links, not retellings
- unknowns / derived assumptions a worker surfaced
- a requirement or plan **change** mid-flight — what changed and why
- escalations (with the workspace title to open)
- terminal rows: verdicts, pushed, one-line summary

Normal flow — dispatched, building, QA started, monitor ticks — lives in the
todo list's `activeForm`, never in prose. Silence means on track.

## Completion

On a terminal `STATUS:` block: verify on disk, never trust the pane —

```bash
BABYSIT_TICKET=<id> bbs ticket verdict-status --skill qa        # DONE|…
BABYSIT_TICKET=<id> bbs ticket verdict-status --skill review-pr
```

then report the row (ticket, branch, verdicts, pushed, one-line summary),
archive the pane (`"$ORCA" terminal read --terminal "$T" --limit 2000 --json > <scratch>/$T.json`),
close the tab (`"$ORCA" terminal close --terminal "$T" --tab`), and dispatch
the next queued assignment. QA across workers
serializes on `bbs ticket qa-lease` — workers handle that themselves;
`board` shows who holds it.

**Close each ticket out the way the repo says.** One key decides it, and it
names a whole handler — never branch on `land`/`push`/profile yourself:

```bash
eval "$(bbs autopilot git-flow)"     # → BBS_FINISH: review | land | pr
```
`land` → `bbs ticket land "$TICKET"` (merge into local `$BBS_BASE_BRANCH`).
`pr` → run `create-pr` for that ticket — a Skill-tool invocation, not a shell
command (push + open the PR against base). `review` (default) → the human
closes it out.

Close out per ticket as it finishes, never batched at the end: a worker whose
branch is already on base frees the next one from merging around it, and a PR
opened early is one the reviewer sees early. Run the handler only on a ticket
whose `qa` + `review-pr` you just read as `DONE` — that read is the gate. Both
handlers re-check behind you (`land` refuses unverified work; the PR hook
denies a `BLOCKED` verdict), but a *missing* verdict makes the PR hook **ask**,
and an `ask` with nobody at the pane is a stalled worker, not a safe stop. A
refusal is a row to report, not a thing to work around: there is no override.

Each value also decides the batch's aggregate `NEXT:`:

- **`land`** — never pushes: base ends up ahead of origin and the human owns
  the push. Report `LANDED: <branch> → <base>`; a conflict leaves that ticket
  unlanded and the batch continues — relay it to its worker as feedback (merge
  `origin/<base>` in the worktree, resolve, commit), then re-run `land`. Base
  *is* the composed surface, so never `serve` after landing — `reset-base`
  discards every merge mid-look ([worktrees.md](../references/worktrees.md)).
  NEXT: review the running base, then push it.
- **`pr`** — the `create-pr` skill, one PR per ticket, which persists the
  pointer. Report `PR: <url>`; one PR for the whole batch stays a human ask,
  not a default (create-pr § Compose PR). NEXT: review on GitHub — for UI work
  offer `bbs ticket serve <t…>` → browser → `serve --release` first, so the
  human sees the combined product before the reviews land.
- **`review`** (default, and every unconfigured repo) — stop at QA-ready and
  leave checkpoint 4 to the human. `bbs ticket serve` (bare) composes every
  finished ticket on the shared dev server for combined review; ticket branches
  stay the source of truth, `reset-base` discards the pile. NEXT then depends
  on `$BBS_LAND`: `pr` (the `startup`/`enterprise` default) → `/bbs:create-pr
  <t>` per ticket, or one compose PR; `none` (a pet project) → there are no
  PRs, so land the branches on `$BBS_BASE_BRANCH` — say so plainly rather than
  pointing at `create-pr`, which BLOCKs under that policy.

## Rules

- foreman never edits worker code. Whether it closes a ticket out at all —
  `bbs ticket land`, or the `create-pr` skill — is the repo's `finish:` key,
  never your call. Unset (the default) means checkpoint 4 stays the human's
  and the NEXT is `/bbs:create-pr`.
- `orca terminal stop --worktree …` and `orca worktree rm` close **every**
  terminal in that worktree — never use them on the primary checkout. Retire
  workers one at a time with `orca terminal close --terminal "$T" --tab`.
- Never close a worker that hasn't printed a terminal STATUS unless the human
  says so; a wedged worker gets the recovery sequence, then a re-dispatch
  from disk state.
- Uncommitted repo config the workers depend on (e.g. `.babysit/git-flow.yaml`
  mode) can be destroyed by a worker's reset-base — commit it before
  dispatching at width.
- **Long sessions**: after a context compaction, or before a checkpoint
  decision (greenlight / escalate / kill / close-out) whose exact rule you
  can't recall, re-read this file (Glob `**/foreman/SKILL.md`) or re-run
  bare `/bbs:foreman` — resume is idempotent.

## Output (per assignment, and aggregate on resume)

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
TICKET: <id>  BRANCH: <branch>  QA: <verdict>  REVIEW: <verdict>  PUSHED: <bool>
SUMMARY: <one line per ticket>
NEXT: what the finish policy left for the human — review + /bbs:create-pr per
ticket (`review`), review the landed base + push (`land`), or review the open
PRs (`pr`)
```
