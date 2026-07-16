---
name: foreman
description: Attended orchestrator for parallel feature work — one visible tmux Claude Code worker per ticket (workers run autopilot), pane monitoring, design-checkpoint review with feedback, greenlight-by-/goal or human escalation. Use when the user hands you product requests to run in parallel while staying able to watch and intervene.
---
# foreman

Workers are full Claude Code sessions in tmux panes the human can attach to
at any moment; you dispatch them, watch them, review their designs, and own
the checkpoint between design and build. Workers own the code.

## Invocation

Route by the shape of the argument, not a verb:

- bare — attach/resume: `tmux ls` for `bbs-*` sessions + `bbs-ticket board`
  are the state; reconcile (live workers, verdicts, todo list vs reality),
  re-arm a monitor per live pane, report the board, resume. Disk + tmux are
  sufficient — never rely on conversation memory.
- free text — a requirement: dispatch one worker for it. `+`-separated (or
  one per line) → one worker each. Beyond `MAX_WORKERS` → `pending` todos,
  dispatched as slots free. (`assign` before the text is accepted and
  ignored.)
- ticket-id — that ticket's worker: attach if its session lives, else
  re-dispatch from disk state (`/bbs:autopilot builder <ticket>`).
- `stop <ticket|session>` — the only verb: archive the pane, kill the
  session, mark the todo (this is the explicit permission the kill rule
  requires; without a terminal STATUS the ticket stays resumable from disk).

**One human-review command: `bbs-ticket serve`.** Bare = every finished
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
MAX_WORKERS="$(bbs-config get parallel_max_workers 2>/dev/null || true)"
[ -n "$MAX_WORKERS" ] || MAX_WORKERS=3   # workers share CPU + one dev server
```

## Dispatch a worker

```bash
S="bbs-$(date +%s | tail -c 5)"   # or bbs-<ticket> when resuming a known ticket
tmux new-session -d -s "$S" -x 200 -y 50 -c "$REPO"
tmux send-keys -t "$S" "claude --dangerously-skip-permissions '/bbs:autopilot <requirement>'" Enter
```

Workers always run autopilot: it creates the ticket + worktree (`mode:
worktree` recommended), seeds requirement/design/plan, and **stops at the
copy-paste `/goal` handoff** — that stop is your review gate. Resuming a
crashed ticket: same spawn with `/bbs:autopilot builder <ticket>`.

**Every worker is a Claude Code todo** — the task list is the user's live
board and must mirror reality:

- dispatch → `TaskCreate` `<ticket-or-slug>: <requirement one-liner>
  [tmux: <session>]`, `in_progress`; beyond `MAX_WORKERS` → `pending`,
  flipped when dispatched.
- phase change / escalation → `TaskUpdate` the `activeForm` (what the worker
  is doing + which session to attach).
- close-out (verdicts verified, pane archived) → `completed`. BLOCKED stays
  `in_progress` with the blocker — never complete a task to tidy the list.
- bare resume → reconcile the list against `tmux ls` + `board` first.

## Monitor

One Monitor per pane (persistent). Ground truth is disk (`bbs-ticket board`,
`verdict-status`, ticket artifacts) — pane text only tells you *when* to look.

```bash
prev=""
while true; do
  # no ^ anchor: Claude Code indents STATUS lines, so line-start never matches
  cur=$(tmux capture-pane -t "$S" -p 2>/dev/null \
    | grep -E "Enter to select|Copy the block below|STATUS: (DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT)|API Error" | tail -4)
  [ "$cur" != "$prev" ] && [ -n "$cur" ] && echo "$cur"
  prev="$cur"
  tmux has-session -t "$S" 2>/dev/null || { echo "worker gone"; exit 0; }
  sleep 20
done
```

## Driving a worker's terminal

- **Multi-line paste** — clear first (input may hold a pre-filled suggestion;
  never blind-Enter it), paste bracketed, submit separately:
  ```bash
  tmux send-keys -t "$S" C-u
  tmux set-buffer -b blk '<text>'
  tmux paste-buffer -p -b blk -t "$S"
  tmux send-keys -t "$S" Enter
  ```
- **Question menus** — `↑/↓` navigate, `Enter` selects the focused option and
  advances, `Left`/`Right` switch questions, final view is "Submit answers".
  Capture the pane after every keystroke; menus re-render.
- **Wedged TUI** (no spinner, no prompt, minutes of stillness with the process
  alive): `Escape`, then a single `C-c` — that recovers the prompt without
  killing the session. Then re-send context as a plain message.

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
`NEEDS_CONTEXT` status — relay the worker's exact ask AND the direct-access
command so they can drive the pane themselves:

```text
tmux attach -t <session>    # detach when done: Ctrl-b d
```

Apply the answer wherever the human gives it: answered you → drive the
worker's menu/prompt via send-keys; answered in the pane → re-arm the
monitor and continue.

## Report signal, not flow

Print only what changes the human's picture:

- design-checkpoint artifact paths (`plan.md`, `design.md`,
  `prototype.html`) — links, not retellings
- unknowns / derived assumptions a worker surfaced
- a requirement or plan **change** mid-flight — what changed and why
- escalations (with the `tmux attach` line)
- terminal rows: verdicts, pushed, one-line summary

Normal flow — dispatched, building, QA started, monitor ticks — lives in the
todo list's `activeForm`, never in prose. Silence means on track.

## Completion

On a terminal `STATUS:` block: verify on disk, never trust the pane —

```bash
BABYSIT_TICKET=<id> bbs-ticket verdict-status --skill qa        # DONE|…
BABYSIT_TICKET=<id> bbs-ticket verdict-status --skill review-pr
```

then report the row (ticket, branch, verdicts, pushed, one-line summary),
archive the pane (`tmux capture-pane -p -S -2000 > <scratch>/$S.txt`), kill
the session, and dispatch the next queued assignment. QA across workers
serializes on `bbs-ticket qa-lease` — workers handle that themselves;
`board` shows who holds it.

Batch done → check `land:` in `.babysit/git-flow.yaml`. `land: local`
(default under `mode: worktree`) → `bbs-ticket serve` (bare) composes every
finished ticket on the shared dev server for combined review; ticket
branches stay the source of truth, `reset-base` discards the pile. The
aggregate NEXT offers `/bbs:create-pr <t>` per ticket or one compose PR
(create-pr § Compose PR). `land: pr` → skip composing; NEXT is per-ticket
`/bbs:create-pr`.

## Rules

- foreman never edits worker code and never creates PRs — `NEXT:
  /bbs:create-pr` stays with the human (checkpoint 4).
- Never kill a pane that hasn't printed a terminal STATUS unless the human
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
