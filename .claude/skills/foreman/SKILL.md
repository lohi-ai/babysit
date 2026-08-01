---
name: foreman
description: Attended orchestrator for parallel feature work — one visible Claude Code worker per ticket in its own cmux workspace (tmux session when cmux is absent), workers run autopilot, pane monitoring, design-checkpoint review with feedback, greenlight-by-/goal or human escalation. Use when the user hands you product requests to run in parallel while staying able to watch and intervene.
---
# foreman

Workers are full Claude Code sessions in cmux workspaces (or tmux panes) the human
can open at any moment; you dispatch them, watch them, review their designs,
and own the checkpoint between design and build. Workers own the code.

## Invocation

Route by the shape of the argument, not a verb:

- bare — attach/resume: live `bbs …` workers (`cmux workspace list`, or
  `tmux ls`) + `bbs ticket board` are the state; reconcile (live workers,
  verdicts, todo list vs reality), re-arm a monitor per live worker, report
  the board, resume. Disk + the terminal are sufficient — never rely on
  conversation memory.
- free text — a requirement: dispatch one worker for it. `+`-separated (or
  one per line) → one worker each. Beyond `MAX_WORKERS` → `pending` todos,
  dispatched as slots free. (`assign` before the text is accepted and
  ignored.)
- ticket-id — that ticket's worker: attach if its session lives, else
  re-dispatch from disk state (`/bbs:autopilot builder <ticket>`).
- `stop <ticket|tab|session>` — the only verb: archive the pane, close the
  workspace/session, mark the todo (this is the explicit permission the kill rule
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

## Terminal backend — cmux first, tmux fallback

```bash
if command -v cmux >/dev/null 2>&1 && cmux ping >/dev/null 2>&1; then MUX=cmux; else MUX=tmux; fi
```

Under cmux each worker is its own **workspace** — a sidebar entry with its own
cwd (the ticket worktree), status pill and notification badge, not a
horizontal tab inside another workspace. The human switches workers by
clicking, no attach/detach. `$W` is the handle: a `workspace:<n>` ref under
cmux, the session name under tmux.

| op | cmux (`$W` = `workspace:<n>`) | tmux (`$W` = session) |
|---|---|---|
| read pane | `cmux capture-pane --workspace "$W" --lines 60` | `tmux capture-pane -t "$W" -p` |
| alive? | `cmux workspace list \| grep -q "$W "` | `tmux has-session -t "$W"` |
| one-line msg | `cmux send --workspace "$W" -- '<text>\n'` | `tmux send-keys -t "$W" '<text>' Enter` |
| key | `cmux send-key --workspace "$W" enter\|escape\|ctrl+c\|ctrl+u` | `tmux send-keys -t "$W" Enter` |
| human opens it | clicks it in the sidebar, or `cmux workspace select "$W"` | `tmux attach -t "$W"`  (detach: `Ctrl-b d`) |
| close | `cmux workspace close "$W"` | `tmux kill-session -t "$W"` |

Refs are per-window ids, stable while cmux runs but reassigned across an app
restart — on bare resume re-derive `$W` from the workspace **title** via
`cmux workspace list`, which is why every worker is titled `bbs <ticket-or-slug>`.

cmux-only affordances that make the board readable — use them, they are the
reason to prefer cmux:

```bash
cmux workspace rename "$W" --title "bbs <ticket>: <one-liner>"   # once autopilot mints the ticket
cmux set-status bbs "<phase>" --workspace "$W" --icon sparkle    # sidebar pill: designing / building / QA / BLOCKED
cmux notify --workspace "$W" --title "<ticket> needs you" --body "<the ask>"   # on escalation
```

Horizontal **tabs** are for a worker's satellites, never for workers: a dev
server, a log tail, a browser preview belong *inside* that worker's workspace
(`cmux new-surface --workspace "$W" --working-directory <worktree>`,
`cmux new-pane --type browser --workspace "$W" --url <url>`), so the whole
worker closes as one unit.

## Dispatch a worker

```bash
# cmux — new sidebar workspace, unfocused so the human's current one is not stolen
W=$(cmux workspace create --name "bbs <slug>" --cwd "$REPO" ${G:+--group "$G"} \
      --command "claude --dangerously-skip-permissions '/bbs:autopilot <requirement>'" \
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

```bash
# tmux fallback
W="bbs-$(date +%s | tail -c 5)"   # or bbs-<ticket> when resuming a known ticket
tmux new-session -d -s "$W" -x 200 -y 50 -c "$REPO"
tmux send-keys -t "$W" "claude --dangerously-skip-permissions '/bbs:autopilot <requirement>'" Enter
```

Workers always run autopilot: it creates the ticket + worktree (`mode:
worktree` recommended), seeds requirement/design/plan, and **stops at the
copy-paste `/goal` handoff** — that stop is your review gate. Resuming a
crashed ticket: same spawn with `/bbs:autopilot builder <ticket>`.

**Every worker is a Claude Code todo** — the task list is the user's live
board and must mirror reality:

- dispatch → `TaskCreate` `<ticket-or-slug>: <requirement one-liner>
  [<workspace title|tmux session>]`, `in_progress`; beyond `MAX_WORKERS` →
  `pending`, flipped when dispatched.
- phase change / escalation → `TaskUpdate` the `activeForm` (what the worker
  is doing + which workspace/session to open). Under cmux mirror the phase onto the
  sidebar pill with `set-status`.
- close-out (verdicts verified, pane archived) → `completed`. BLOCKED stays
  `in_progress` with the blocker — never complete a task to tidy the list.
- bare resume → reconcile the list against `cmux workspace list` (or
  `tmux ls`) + `board` first.

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

Under tmux the loop is identical with the read/alive lines swapped for
`tmux capture-pane -t "$W" -p` and `tmux has-session -t "$W"`.

## Driving a worker's terminal

- **Multi-line paste** (the `/goal` block) — clear first (input may hold a
  pre-filled suggestion; never blind-Enter it), paste **bracketed** so the
  newlines land in the box instead of submitting each line, then submit:
  ```bash
  # cmux — paste-buffer is NOT bracketed (it Enters between lines); wrap it yourself
  cmux send-key --workspace "$W" ctrl+u
  cmux send --workspace "$W" -- "$(printf '\033[200~%s\033[201~' "$BLK")"
  cmux send-key --workspace "$W" enter
  ```
  ```bash
  # tmux
  tmux send-keys -t "$W" C-u
  tmux set-buffer -b blk '<text>'
  tmux paste-buffer -p -b blk -t "$W"
  tmux send-keys -t "$W" Enter
  ```
- **Question menus** — `↑/↓` navigate, `Enter` selects the focused option and
  advances, `Left`/`Right` switch questions, final view is "Submit answers".
  Under cmux: `cmux send-key --workspace "$W" up|down|left|right|enter`.
  Capture the pane after every keystroke; menus re-render.
- **Wedged TUI** (no spinner, no prompt, minutes of stillness with the process
  alive): `Escape`, then a single `C-c` (`cmux send-key … escape` then
  `ctrl+c`) — that recovers the prompt without killing the worker. Then
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
  borrows from (siblings outrank global patterns). Any `NEW:` flag in
  `design.md` disqualifies auto-greenlight.
- **Reuse** — name the existing components used; a new component needs a
  stated reason.
- **Prototype inspected** — actually Read `prototype.html` (against
  `DESIGN.md` tokens when the repo has one); file existence is not evidence.
- **Scope** — nothing beyond the request wording.

Then route — **human review is the default; auto-greenlight is the narrow
exception**:

- **Rubric incomplete** → feedback into the pane as a plain message; the
  worker redesigns. At most 2 rounds — rounds fix artifact gaps, they never
  argue taste — then escalate with the gaps named.
- **Auto-greenlight only when ALL hold**: every rubric line filled with
  named evidence, AND the change extends an existing screen using existing
  components only — no `NEW:` flag, no new page/screen, no navigation/IA
  change, no removed or relocated surface, no money/auth/irreversible-data
  path, and the worker followed the request as stated. Then paste the
  worker's own `/goal` block verbatim and log the filled rubric to the
  decisions log.
- **Everything else → escalate** (the default): `AGENT_ROLE=developer` →
  one `AskUserQuestion` (options: greenlight / redirect with note / drop)
  with a one-paragraph design summary + artifact paths + the rubric as
  filled; other roles → emit the `NEEDS_CONTEXT` block naming ticket +
  paths. When in doubt, escalate — a wrong escalation costs the human a
  minute; a wrong greenlight costs a build.

Worker questions mid-flight (menus) follow the same split: answer Mechanical/
Taste from the requirement + framework via send-keys; escalate User
Challenges.

Whenever a worker needs a human — a question you can't answer, a `BLOCKED`/
`NEEDS_CONTEXT` status — relay the worker's exact ask AND how to reach the
worker directly:

```text
cmux: open the "bbs <ticket>" workspace in the sidebar   (or: cmux workspace select <ref>)
tmux: tmux attach -t <session>      # detach when done: Ctrl-b d
```

Under cmux also fire `cmux notify --workspace "$W"` and set the status pill
to the blocker, so the workspace announces itself in the sidebar.

Apply the answer wherever the human gives it: answered you → drive the
worker's menu/prompt via `send`/`send-key`; answered in the workspace → re-arm the
monitor and continue.

## Report signal, not flow

Print only what changes the human's picture:

- design-checkpoint artifact paths (`plan.md`, `design.md`,
  `prototype.html`) — links, not retellings
- unknowns / derived assumptions a worker surfaced
- a requirement or plan **change** mid-flight — what changed and why
- escalations (with the workspace title / `tmux attach` line)
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
2000 > <scratch>/$W.txt`, or `tmux capture-pane -p -S -2000`), close the
workspace/session, and dispatch the next queued assignment. QA across workers
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
