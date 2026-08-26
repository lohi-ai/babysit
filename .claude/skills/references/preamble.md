# Skill Preamble
Runtime bootstrap for every babysit skill: run the bash block first, follow
the status contract when reporting. Route every decision through the
[Auto-Decision Framework](auto-decision-framework.md) (Mechanical / Taste /
User Challenge). Trust your own judgment for everything these rules don't pin
down.
## Output style — terse by default
Drop filler, pleasantries, hedging. Route by consumer:

| Consumer | Mode | Rules |
|----------|------|-------|
| Machine — checkpoint, telemetry, status lines | **Full** | Drop articles too. Maximum terseness. |
| Downstream model — handoffs, plan.md, requirement.md | **Dense** | Complete sentences. Keep the why, constraints, gotchas — these files are the next step's only memory; never cut information. |
| Human — terminal, AskUserQuestion, NEEDS_CONTEXT | **Lite** | Full sentences, professional but tight. |
| Security/destructive/ambiguous | **Normal** | Full prose. Resume terse after. |
Skills with their own output format take precedence.
## One mode, four escalation channels
Skills always run autonomously — never prompt mid-flight for taste or
cosmetic choices. Escalate only when proceeding on a guess would land
incorrect work: ambiguous requirement with materially different readings,
irreversible/high-blast-radius action without durable authorization, or
missing config/credentials that can't be inferred from the repo. Anything
derivable from the codebase, look up; recoverable forks, try the likely path
and report `BLOCKED` on failure. A second `NEEDS_CONTEXT` in one run means
you're steering — stop and report.
`AGENT_ROLE` (fallback `GT_ROLE`) picks the delivery channel:
`developer` (default, unset) → render as a single `AskUserQuestion`;
`dashboard` → publish an approval record and wait (below);
`orca` → ask the coordinator over the message bus and block (below);
anything else (`mayor`, `general`, `scanner`, …) → print the structured block
verbatim (an orchestrator relays it; `AskUserQuestion` would hang the run).
Only the channel changes. The analysis, the artifacts, and the decision itself
are identical in all four.
### `AGENT_ROLE=dashboard`
The human is at the web dashboard, not at this terminal. A design checkpoint
publishes a record on the ticket and blocks on the answer:
```bash
bbs ticket approval publish --kind plan --note "<the one question, in one line>"
DECISION=$(bbs ticket approval await)   # approved | redirected | dropped
```
`await` polls the record every 10s and prints the outcome to stdout, the
human's note to stderr. Rules for anything that consumes it:
- **No hard timeout, ever.** Resuming on a timer means resuming on a *guess* at
  the decision — the exact failure this channel exists to prevent. It reminds
  the assigned foreman's workspace once at 30 minutes (`--reminder-min`) and
  then keeps waiting.
- **`dropped` ends the wait like any other answer.** Stop work on the ticket
  and report; a drop is a decision, not a failure.
- **`redirected` always carries a note** (the server rejects an empty one) —
  rework against it and publish again. The second publish is a new checkpoint,
  not a retry.
- Publish once per checkpoint: re-publishing over a `pending` record is a no-op,
  so a resumed run re-running its checkpoint step will not reset the clock on a
  decision the human is already reading.
Honored by `autopilot` (its `/goal` handoff), `foreman` (the design gate), and
any skill that would otherwise call `AskUserQuestion` at a design checkpoint.
### `AGENT_ROLE=orca`
Set only by a foreman that dispatched this worker over Orca's message bus, so
the bus is known to be there. The coordinator is another agent reading a
mailbox, not a human at this terminal:
```bash
ANSWER=$(orca orchestration ask --question "<the one question, in one line>" \
  --options "a,b" --timeout-ms 1800000 --json)
```
- `ask` blocks until the coordinator answers and returns a durable message id.
  A timeout leaves the question *pending*, not dropped — resume the same
  question with `--resume <message_id>` rather than asking it again, or the
  coordinator sees two questions and answers one.
- Never `AskUserQuestion` here: nobody is watching this pane, and it would hang
  the run in a way a batch cannot recover from.
- If `ask` fails outright (no bus, no coordinator), fall through to the
  structured block below. A dispatched worker that cannot reach its foreman is
  in the orchestrator case, and the block is what an orchestrator reads.

### `NEEDS_CONTEXT` shape
```
STATUS: NEEDS_CONTEXT
REASON: Requirement "handle duplicate invoices" could mean (a) reject with 409,
(b) merge and sum, or (c) keep newest. Existing code does none of these.
ATTEMPTED: Grepped invoices/*.ts for prior handling — only happy path present.
RECOMMENDATION: Ask the ticket owner which of A/B/C applies before implementing.
```
## Native task list
Multi-step work mirrors into Claude Code's native task list
(TaskCreate/TaskUpdate): seed tasks from the skill's driving artifact —
`plan.md`, the QA flow matrix, workflow milestones — and mark each
in_progress on start, completed only when its check passes. The task list is
the visible progress view; disk artifacts stay the durable state — on cold
resume rebuild the list from them, never the reverse.
## Preamble (run first)
```bash
# ── Skill preamble ───────────────────────────────────────────────
_SKILL_NAME="SKILL_NAME"          # set before running
_SESSION_ID="$$-$(date +%s)"
_TEL_START=$(date +%s)

# ── Bin reachability ─────────────────────────────────────────────
# Install guarantees exactly one thing: the `bbs` multicall binary on PATH
# (bin/setup-skills links ~/.local/bin/bbs; `brew install bbs` installs it).
# Skills call it as `bbs <sub>` — never a hyphenated alias, which a brew-only
# install does not ship (Formula/bbs.rb aliases just two subcommands).
# Net for shells that don't inherit a login PATH (cron, tmux workers, spawned
# orchestrators): prepend the absolute install dirs when they exist.
# $CLAUDE_PLUGIN_ROOT covers a marketplace / skills-dir plugin install, whose
# root is not ~/.claude/skills/babysit; `:-/nonexistent` keeps it inert unset.
for _d in "$HOME/.local/bin" "$HOME/.claude" "${CLAUDE_PLUGIN_ROOT:-/nonexistent}/bin" "$HOME/.claude/skills/babysit/bin"; do
  case ":$PATH:" in *":$_d:"*) ;; *) [ -d "$_d" ] && PATH="$_d:$PATH" ;; esac
done
export PATH
# Capability probe, once. A binary built before a subcommand existed exits 1
# *silently* (internal/cmd/root.go sets SilenceErrors) — byte-identical to a
# legit "no ticket" exit 1 — so probe rather than trust. `bbs ticket --help` is
# the honest test: cobra-backed, exit 0 when served. Probe only this one: the
# hand-rolled subcommands exit 2 on `--help`, and `bbs upgrade --help` would
# run a real git pull. (`bbs help <sub>` is NOT usable: cobra exits 0 for
# unknown topics.)
bbs ticket --help >/dev/null 2>&1 || echo \
  "BBS_DEGRADED: no working \`bbs\` on PATH — run bin/setup-skills from a checkout, or \`brew install lohi-ai/babysit/bbs\` (a plugin install ships no compiled binary)" >&2

# Auto-update check — cache-friendly, silent when up-to-date.
# Prints UPGRADE_AVAILABLE <old> <new> or JUST_UPGRADED <old> <new> to stderr.
_UPD=$(bbs upgrade check 2>/dev/null || true)
[ -n "$_UPD" ] && echo "$_UPD" >&2 || true

# Session tracking — count concurrent babysit sessions, prune stale (>120 min).
mkdir -p ~/.babysit/sessions
touch ~/.babysit/sessions/"$PPID"
_SESSIONS=$(find ~/.babysit/sessions -mmin -120 -type f 2>/dev/null | wc -l | tr -d ' ')
find ~/.babysit/sessions -mmin +120 -type f -exec rm {} + 2>/dev/null || true

# Session-writer hook — persist (or refresh) ~/.babysit/sessions/<id>.yaml.
# Best-effort: the guaranteed path is the bin/hooks/session-writer plugin
# hook (SessionStart + PostToolUse); this block additionally records the
# ticket from $BABYSIT_TICKET when a skill runs it.
# $BABYSIT_SESSION defaults from Claude Code's own session id, so every real
# tab gets a yaml (feeds `session list`, `board`, dashboard); autopilot's
# explicit $BABYSIT_SESSION still wins. Atomic mktemp+mv so the file's mtime
# gets bumped (in-place edit on Linux preserves mtime — see docs/identity.md
# § Atomic writes). Skipped when neither id is available.
BABYSIT_SESSION="${BABYSIT_SESSION:-cc-${CLAUDE_CODE_SESSION_ID:-}}"
[ "$BABYSIT_SESSION" = "cc-" ] && BABYSIT_SESSION=""
if [ -n "${BABYSIT_SESSION:-}" ]; then
  _SF="$HOME/.babysit/sessions/${BABYSIT_SESSION}.yaml"
  _STMP="$(mktemp "$HOME/.babysit/sessions/.session.XXXXXX" 2>/dev/null)" || _STMP=""
  if [ -n "$_STMP" ]; then
    {
      echo "version: 1"
      echo "session_id: ${BABYSIT_SESSION}"
      echo "ticket: ${BABYSIT_TICKET:-}"
      if [ -f "$_SF" ]; then
        awk '/^started_at:/ { print; found=1 } END { if (!found) exit 1 }' "$_SF" 2>/dev/null \
          || echo "started_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
      else
        echo "started_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
      fi
      echo "last_seen_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
      echo "pid: $$"
      echo "cwd: $(pwd)"
    } > "$_STMP" 2>/dev/null && mv "$_STMP" "$_SF" 2>/dev/null \
      || rm -f "$_STMP" 2>/dev/null
  fi
fi

# Config + repo state.
_bbs_cfg() { bbs config get "$1" 2>/dev/null || true; }
_PROACTIVE=$(_bbs_cfg proactive); _PROACTIVE=${_PROACTIVE:-true}
_TEL=$(_bbs_cfg telemetry);       _TEL=${_TEL:-local}
_BRANCH=$(git branch --show-current 2>/dev/null || echo "unknown")
_REPO=$(basename "$(git rev-parse --show-toplevel 2>/dev/null || echo "unknown")")
_INVOKER="${AGENT_ROLE:-${GT_ROLE:-developer}}"
[ -n "$OPENCLAW_SESSION" ] || [ -n "$BABYSIT_SPAWNED" ] && _SPAWNED="true" || _SPAWNED="false"

# Project scope — slug + ticket re-derived from git remote + branch on every
# preamble, never from conversation memory. Empty TICKET = branch encodes
# none (e.g. main) — the skill decides whether that's OK.
eval "$(bbs ticket env 2>/dev/null || true)"
SLUG="${SLUG:-unknown}"
TICKET="${TICKET:-}"
BABYSIT_PROJECT_HOME="${BABYSIT_PROJECT_HOME:-$HOME/.babysit/projects/$SLUG}"

echo "SKILL: $_SKILL_NAME"
echo "SESSION_ID: $_SESSION_ID"
echo "SESSIONS_ACTIVE: $_SESSIONS"
echo "SLUG: $SLUG"
echo "BRANCH: $_BRANCH"
echo "REPO: $_REPO"
echo "INVOKER: $_INVOKER"
echo "TICKET: ${TICKET:-<none>}"
echo "PROJECT_HOME: $BABYSIT_PROJECT_HOME"
echo "PROACTIVE: $_PROACTIVE"
echo "TELEMETRY: $_TEL"
echo "SPAWNED: $_SPAWNED"

# Ticket folder — idempotent. Seeds index.json if missing; no-op otherwise.
# Layout C (see references/ticket-layout.md) stores all per-ticket state here.
if [ -n "$TICKET" ]; then
  bbs ticket init 2>/dev/null || true
fi

# Context Recovery — print latest checkpoint + recent timeline for this
# ticket, so a cold agent knows where the prior one left off. Silent when
# no ticket.
if [ -n "$TICKET" ]; then
  bbs autopilot recover 2>/dev/null || true
fi

# Record skill start as JSONL (local-only, unless telemetry=off).
if [ "$_TEL" != "off" ]; then
  mkdir -p ~/.babysit/analytics
  printf '{"ts":"%s","skill":"%s","event":"start","session":"%s","repo":"%s","branch":"%s","invoker":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$_SKILL_NAME" "$_SESSION_ID" "$_REPO" "$_BRANCH" "$_INVOKER" \
    >> ~/.babysit/analytics/skill-usage.jsonl 2>/dev/null || true
fi
```
Replace `SKILL_NAME` with the skill's `name:` from frontmatter.
### Interpreting the state echo
- **`INVOKER`** — picks the `NEEDS_CONTEXT` channel (above).
- **`PROACTIVE=false`** — don't auto-invoke other babysit skills; run only
  what the user typed. Skip silently, never ask.
- **`TELEMETRY=off`** — disable all telemetry writes. Nothing ever leaves the
  machine either way.
- **`SPAWNED=true`** — an orchestrator started this session; skip welcome
  text and optional summaries.
### Ticket consistency — the four-layer invariant
1. **Branch name is the anchor** (`feat/<ticket>_<slug>`) — `TICKET` is
   re-derived from it every wake-up; conversation memory is never trusted.
2. **Checkpoint cross-check** — `checkpoint.json` records `branch`; if it
   doesn't match the current branch, stop and report (block below).
3. **Timeline audit** — `bbs autopilot` appends step boundaries to
   `timeline.jsonl`.
4. **Ticket system is the oracle** — `bbs ticket get status` is ground truth
   for whether the ticket exists / is open.
Divergence (layers 1↔2 disagree):
```
STATUS: BLOCKED
VERDICT: —
SUMMARY: Branch/checkpoint divergence — cannot safely resume.
REASON: branch='<current>' but checkpoint.branch='<recorded>' for ticket <ticket>
ATTEMPTED: Derived ticket from branch, read checkpoint.json, compared branch fields
RECOMMENDATION: Human triages — checkout the recorded branch or clear state with `bbs autopilot clear <ticket>`
```
**No-ticket scope** — empty `TICKET` is a valid shape: skip ticket-state
writes with a one-line note, take requirement/plan from conversation, do the
work. Branch shape and git-flow policy are the workflow layer's concern, not
a skill precondition. Never invent a ticket id; to attach identity without a
checkout, `export BABYSIT_TICKET=<id>` (wins the resolve ladder).
### Handling update-check output
- `UPGRADE_AVAILABLE <old> <new>` — mention once ("babysit upgrade available
  — run `bbs upgrade`") and continue; never auto-run or block.
- `JUST_UPGRADED <from> <to>` — emit this exact line at top of response:
  > babysit upgraded v\<from\> → v\<to\>. Run `/plugin marketplace update babysit` then `/reload-plugins` to pick up the new skills (the shell upgrade can't do this for you).
## Telemetry (run last)
After the skill completes (success, error, abort), append a completion row
correlated by `_SESSION_ID`.
```bash
_TEL_END=$(date +%s)
_TEL_DUR=$(( _TEL_END - _TEL_START ))
rm -f ~/.babysit/sessions/"$PPID" 2>/dev/null || true

if [ "$_TEL" != "off" ]; then
  mkdir -p ~/.babysit/analytics
  printf '{"ts":"%s","skill":"%s","event":"end","session":"%s","duration_s":%d,"outcome":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${_SKILL_NAME}" "${_SESSION_ID}" "${_TEL_DUR}" "OUTCOME" \
    >> ~/.babysit/analytics/skill-usage.jsonl 2>/dev/null || true
fi
```
Replace `OUTCOME` with one of: `success`, `error`, `abort`, `unknown`.
## Completion Status Protocol
Every skill ends with exactly one status code, printed last:
```
STATUS: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
VERDICT: <skill-specific verdict per handoff-contracts.md>
SUMMARY: <1-2 sentences of what happened>
```
`DONE` = completed with evidence; `DONE_WITH_CONCERNS` = completed, caller
should read the concerns; `BLOCKED` = cannot proceed (broken tool, missing
access, same step failed 3×, security uncertainty); `NEEDS_CONTEXT` = missing
info only a human has — including scope exceeded: the work outgrew what you
can self-verify, so stop and report rather than ship unverified. Non-happy-path
statuses add `REASON`, `ATTEMPTED`, `RECOMMENDATION` lines. Bad work is worse
than no work — when in doubt, stop; never guess silently.
Two verdict→status rules are hook-enforced, not judgment calls:
`qa` `FAIL` reports `BLOCKED`, never `DONE*` (the PR gate reads `DONE*` as
ready); `review-pr` with unresolved material findings reports `BLOCKED`
(minor residuals → `DONE_WITH_CONCERNS`).
