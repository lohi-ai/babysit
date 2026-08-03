---
name: foreman
description: Attended orchestrator for parallel feature work — one visible Claude Code worker per ticket in its own cmux workspace, workers run autopilot, pane monitoring, design-checkpoint review with feedback, greenlight-by-/goal or human escalation. Requires cmux. Use when the user hands you product requests to run in parallel while staying able to watch and intervene.
---
# foreman

Workers are full coding-agent sessions in cmux workspaces the human can open at
any moment; you dispatch them, watch them, review their designs, and own the
checkpoint between design and build. Workers own the code.

Which CLI a worker runs on is config, not your decision — ask `bbs foreman
worker-command` for the command line (see [Dispatch a worker](#dispatch-a-worker))
rather than writing `claude` yourself. Everything else in this skill is
agent-independent: the prompt is `/bbs:<skill>` on every agent, and workers are
driven through cmux and disk state either way.

## Invocation

Route by the shape of the argument, not a verb:

- bare — attach/resume: live `bbs …` workers (`cmux workspace list`)
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
- `stop <ticket|workspace>` — the only verb: archive the pane, close the
  workspace, mark the todo (this is the explicit permission the kill rule
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

## Terminal backend — cmux, required

cmux is a hard dependency. Preflight before dispatching anything; a foreman
that degrades to a weaker terminal silently loses the sidebar, the status
pills and the notifications the whole attended loop is built on, so fail here
instead:

```bash
command -v cmux >/dev/null 2>&1 && cmux ping >/dev/null 2>&1 || {
  echo "BLOCKED: foreman requires cmux — install it (https://github.com/manaflow-ai/cmux) and make sure the app is running" >&2
  exit 1
}
```

## Which agent runs the workers

Two independent config keys, in `<repo>/.babysit/config.yaml` (committed, the
team default) or `~/.babysit/config.yaml` (this machine, wins over the repo):

```yaml
worker_agent: grok    # the per-ticket workers      (default: claude)
foreman_agent: grok   # this session's own CLI      (default: claude)
```

They do not inherit from each other, deliberately: moving workers to another
agent is a throughput choice, and you audit their design gates and QA evidence —
`worker_agent: grok` alone leaves that audit on Claude Code. `BABYSIT_AGENT`
overrides both for one run; `--agent <name>` overrides everything.

Non-Claude agents need babysit's skills installed in *their* plugin store, not
just the binary on PATH — for grok, `grok plugin install
https://github.com/lohi-ai/babysit`. Without it a worker comes up fine and then
cannot resolve `/bbs:autopilot`. `worker-command` preflights the binary; the
plugin is on the human. If a worker's first pane output says the skill is
unavailable, that is this gap — report it, do not retry on another agent.

grok also gates on **directory trust**, separately from its permission mode: the
first run in a directory absent from `~/.grok/trusted_folders.toml` stops on "Do
you trust the contents of this directory?", and `--always-approve` does not
answer it. `worker-command` and `spawn` both preflight this and fail with the fix
named, so you get a `BLOCKED` rather than a pane parked on a question. The fix is
one-time per repo (workers share the repo cwd): `cd <repo> && grok`, answer,
quit. Do not answer it by sending keystrokes into a worker pane — a trust
decision that never reaches the file recurs on the next spawn.

Each worker is its own **workspace** — a sidebar entry with its own cwd (the
ticket worktree), status pill and notification badge, not a horizontal tab
inside another workspace. The human switches workers by clicking, no
attach/detach. `$W` is the handle, a `workspace:<n>` ref.

| op | command |
|---|---|
| read pane | `cmux capture-pane --workspace "$W" --lines 60` |
| alive? | `cmux workspace list \| grep -q "$W "` |
| one-line msg | `cmux send --workspace "$W" -- '<text>\n'` |
| key | `cmux send-key --workspace "$W" enter\|escape\|ctrl+c\|ctrl+u` |
| retitle | `cmux workspace rename "$W" --title "bbs <ticket>: <one-liner>"` |
| sidebar pill | `cmux set-status bbs "<lane>" --workspace "$W" --icon sparkle` |
| notify | `cmux notify --workspace "$W" --title "<ticket> needs you" --body "<the ask>"` |
| human opens it | clicks it in the sidebar, or `cmux workspace select "$W"` |
| close | `cmux workspace close "$W"` |

Refs are per-window ids, stable while cmux runs but reassigned across an app
restart — on bare resume re-derive `$W` from the workspace **title** via
`cmux workspace list`, which is why every worker is titled `bbs <ticket-or-slug>`.
The title is the durable handle; the ref is not.

`set-status` takes a fixed lane, not free text: `todo`, `working`,
`needs-attention`, `review`, `done`, `auto`. Map the phase onto the nearest
one (designing/building → `working`, escalation → `needs-attention`, design
gate or QA handoff → `review`) — an unknown lane is rejected, not shown.

**`cmux send` is byte-transparent, with three exceptions.** It interprets `\n`
and `\r` as Enter and `\t` as Tab; every other byte arrives at the worker's input
unchanged, so escape sequences land as literal characters rather than being
interpreted. Anything in bbs that sends multi-line text to a workspace — this
skill, `bbs dashboard`'s wake — sends it raw and never wraps or re-encodes it.

The practical consequence: **text with no trailing `\n` sits in the worker's
composer unsent.** The pane still looks busy, so it reads as a worker ignoring
you. Either end the message with `\n` or follow it with `send-key … enter`, then
capture the pane and confirm the composer is empty before believing it landed.

Satellites belong *inside* a worker's workspace, so the whole worker closes as
one unit:

```bash
cmux new-surface --workspace "$W" --working-directory <worktree>   # a shell for the dev server or a log tail
cmux new-pane --type browser --workspace "$W" --url <url>          # a live preview
```

## Dispatch a worker

```bash
# which CLI, with the right yolo flag and the requirement safely quoted.
# Resolution order: --agent > BABYSIT_AGENT > <repo>/.babysit/config.yaml
# (worker_agent:) > ~/.babysit/config.yaml > claude. It preflights, so BLOCKED
# means the agent is not installed — report it, do not fall back to another CLI.
CMD=$(bbs foreman worker-command --prompt "/bbs:autopilot <requirement>") || {
  echo "BLOCKED: $CMD" >&2; exit 1; }

# new sidebar workspace, unfocused so the human's current one is not stolen
W=$(cmux workspace create --name "bbs <slug>" --cwd "$REPO" ${G:+--group "$G"} \
      --command "$CMD" \
    | awk '/^OK/{print $2}')     # -> workspace:5

# batch of 2+: collapse them under one sidebar header. Run once, after the
# first worker exists; `create --from` mints its own anchor workspace, so the
# group survives closing any individual worker.
G=$(cmux workspace-group create --name "bbs foreman" --from "$W" | awk '/^OK/{print $2}')  # -> workspace_group:1
cmux workspace-group collapse "$G"    # optional; expand to see the workers again
```

On resume, re-derive `$G` by name from `cmux workspace-group list --json`, and
adopt a worker dispatched without it via
`cmux workspace-group add --group "$G" --workspace "$W"`.

Workers always run autopilot: it creates the ticket + worktree (`mode:
worktree` recommended), seeds requirement/design/plan, and **stops at the
copy-paste `/goal` handoff** — that stop is your review gate. Resuming a
crashed ticket: same spawn with `/bbs:autopilot builder <ticket>`.

**Every worker is a todo** — the task list is the user's live board and must
mirror reality. If you are running on an agent with no task tool, skip this
mirror silently and treat `bbs ticket board` as the board; it is the ground
truth either way:

- dispatch → `TaskCreate` `<ticket-or-slug>: <requirement one-liner>
  [<workspace title>]`, `in_progress`; beyond `MAX_WORKERS` →
  `pending`, flipped when dispatched.
- phase change / escalation → `TaskUpdate` the `activeForm` (what the worker
  is doing + which workspace to open), and mirror the phase onto the sidebar
  pill with `set-status`.
- close-out (verdicts verified, pane archived) → `completed`. BLOCKED stays
  `in_progress` with the blocker — never complete a task to tidy the list.
- bare resume → reconcile the list against `cmux workspace list` + `board`
  first.

## Monitor

One Monitor per worker (persistent). Ground truth is disk (`bbs ticket board`,
`verdict-status`, ticket artifacts) — pane text only tells you *when* to look.

```bash
prev=""
while true; do
  # no ^ anchor: Claude Code indents STATUS lines, so line-start never matches
  cur=$(cmux capture-pane --workspace "$W" --lines 60 2>/dev/null \
    | grep -E "Enter to select|Copy the block below|STATUS: (DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT)|API Error" | tail -4)
  [ "$cur" != "$prev" ] && [ -n "$cur" ] && echo "$cur"
  prev="$cur"
  cmux workspace list 2>/dev/null | grep -q "$W " || { echo "worker gone"; exit 0; }
  sleep 20
done
```

## Driving a worker's terminal

- **Multi-line paste** (the `/goal` block) — clear first (input may hold a
  pre-filled suggestion; never blind-Enter it), send the block as-is, then
  submit:
  ```bash
  cmux send-key --workspace "$W" ctrl+u
  cmux send --workspace "$W" -- "$BLK"
  cmux send-key --workspace "$W" enter
  ```
  `cmux send` is byte-transparent: embedded newlines arrive as real newlines
  and land in the input box without submitting each line. **Do not wrap the
  block in bracketed-paste markers** (`\033[200~` … `\033[201~`) — cmux
  forwards those bytes literally, so they show up as `[200~` and `[201~`
  inside the message the worker receives.
- **Question menus** — `↑/↓` navigate, `Enter` selects the focused option and
  advances, `Left`/`Right` switch questions, final view is "Submit answers":
  `cmux send-key --workspace "$W" up|down|left|right|enter`.
  Capture the pane after every keystroke; menus re-render.
- **Wedged TUI** (no spinner, no prompt, minutes of stillness with the process
  alive): `cmux send-key … escape`, then a single `ctrl+c` — that recovers the
  prompt without killing the worker. Then re-send context as a plain message.

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
Taste from the requirement + framework via send-keys; escalate User
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
open the "bbs <ticket>" workspace in the sidebar   (or: cmux workspace select <ref>)
```

Also fire `cmux notify --workspace "$W"` and set the status pill to
`needs-attention`, so the workspace announces itself in the sidebar.

Apply the answer wherever the human gives it: answered you → drive the
worker's menu/prompt via `send`/`send-key`; answered in the workspace → re-arm the
monitor and continue.

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
archive the pane (`cmux capture-pane --workspace "$W" --scrollback --lines
2000 > <scratch>/$W.txt`), close the workspace, and dispatch the next queued
assignment. QA across workers
serializes on `bbs ticket qa-lease` — workers handle that themselves;
`board` shows who holds it.

Batch done → check `land:` in `.babysit/git-flow.yaml`. `land: local`
(default under `mode: worktree`) → `bbs ticket serve` (bare) composes every
finished ticket on the shared dev server for combined review; ticket
branches stay the source of truth, `reset-base` discards the pile. The
aggregate NEXT offers `/bbs:create-pr <t>` per ticket or one compose PR
(create-pr § Compose PR). `land: pr` → skip composing; NEXT is per-ticket
`/bbs:create-pr`.

## Rules

- foreman never edits worker code and never creates PRs — `NEXT:
  /bbs:create-pr` stays with the human (checkpoint 4).
- `cmux workspace-group delete` closes **every** worker in the group — never
  use it. Retire workers one at a time with `cmux workspace close "$W"`; drop
  the header alone with `cmux workspace-group ungroup "$G"`.
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
NEXT: human review + /bbs:create-pr per ticket
```
