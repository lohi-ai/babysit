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
## One mode, two escalation channels
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
anything else (`mayor`, `general`, `scanner`, …) → print the structured block
verbatim (an orchestrator relays it; `AskUserQuestion` would hang the run).
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

# ── Bin resolver ─────────────────────────────────────────────────
# BBS_<NAME>_BIN resolves once here: home shim (~/.claude/bbs-*) first,
# repo copy (~/.claude/skills/babysit/bin/bbs-*) as plugin fallback.
_bbs_resolve() {
  local shim="$HOME/.claude/$1" repo="$HOME/.claude/skills/babysit/bin/$1"
  if [ -x "$shim" ]; then echo "$shim"
  elif [ -x "$repo" ]; then echo "$repo"
  else echo "$1"; fi
}
BBS_SLUG_BIN=$(_bbs_resolve bbs-slug)
BBS_TICKET_BIN=$(_bbs_resolve bbs-ticket)
BBS_AUTOPILOT_BIN=$(_bbs_resolve bbs-autopilot)
BBS_CONFIG_BIN=$(_bbs_resolve bbs-config)
BBS_UPDATE_CHECK_BIN=$(_bbs_resolve bbs-update-check)
BBS_UPGRADE_BIN=$(_bbs_resolve bbs-upgrade)
BBS_TELEMETRY_LOG_BIN=$(_bbs_resolve bbs-telemetry-log)
BBS_DB_BIN=$(_bbs_resolve bbs-db)
BBS_ENV_BIN=$(_bbs_resolve bbs-env)
BBS_BUILDER_PROFILE_BIN=$(_bbs_resolve bbs-builder-profile)
BBS_GLOBAL_DISCOVER_BIN=$(_bbs_resolve bbs-global-discover)
BBS_LEARNINGS_LOG_BIN=$(_bbs_resolve bbs-learnings-log)
BBS_LEARNINGS_SEARCH_BIN=$(_bbs_resolve bbs-learnings-search)
export BBS_SLUG_BIN BBS_TICKET_BIN BBS_AUTOPILOT_BIN BBS_CONFIG_BIN \
       BBS_UPDATE_CHECK_BIN BBS_UPGRADE_BIN BBS_TELEMETRY_LOG_BIN \
       BBS_DB_BIN BBS_ENV_BIN BBS_BUILDER_PROFILE_BIN \
       BBS_GLOBAL_DISCOVER_BIN BBS_LEARNINGS_LOG_BIN BBS_LEARNINGS_SEARCH_BIN

# Auto-update check — cache-friendly, silent when up-to-date.
# Prints UPGRADE_AVAILABLE <old> <new> or JUST_UPGRADED <old> <new> to stderr.
_UPD=$("${BBS_UPDATE_CHECK_BIN:-$HOME/.claude/bbs-update-check}" 2>/dev/null || true)
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
_bbs_cfg() { "${BBS_CONFIG_BIN:-$HOME/.claude/bbs-config}" get "$1" 2>/dev/null || true; }
_PROACTIVE=$(_bbs_cfg proactive); _PROACTIVE=${_PROACTIVE:-true}
_TEL=$(_bbs_cfg telemetry);       _TEL=${_TEL:-local}
_BRANCH=$(git branch --show-current 2>/dev/null || echo "unknown")
_REPO=$(basename "$(git rev-parse --show-toplevel 2>/dev/null || echo "unknown")")
_INVOKER="${AGENT_ROLE:-${GT_ROLE:-developer}}"
[ -n "$OPENCLAW_SESSION" ] || [ -n "$BABYSIT_SPAWNED" ] && _SPAWNED="true" || _SPAWNED="false"

# Project scope — slug + ticket re-derived from git remote + branch on every
# preamble, never from conversation memory. Empty TICKET = branch encodes
# none (e.g. main) — the skill decides whether that's OK.
eval "$("${BBS_SLUG_BIN:-$HOME/.claude/bbs-slug}" env 2>/dev/null || true)"
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
  "${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" init 2>/dev/null || true
fi

# Context Recovery — print latest checkpoint + recent timeline for this
# ticket, so a cold agent knows where the prior one left off. Silent when
# no ticket.
if [ -n "$TICKET" ]; then
  "${BBS_AUTOPILOT_BIN:-$HOME/.claude/bbs-autopilot}" recover 2>/dev/null || true
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
3. **Timeline audit** — `bbs-autopilot` appends step boundaries to
   `timeline.jsonl`.
4. **Ticket system is the oracle** — `bbs-ticket get status` is ground truth
   for whether the ticket exists / is open.
Divergence (layers 1↔2 disagree):
```
STATUS: BLOCKED
VERDICT: —
SUMMARY: Branch/checkpoint divergence — cannot safely resume.
REASON: branch='<current>' but checkpoint.branch='<recorded>' for ticket <ticket>
ATTEMPTED: Derived ticket from branch, read checkpoint.json, compared branch fields
RECOMMENDATION: Human triages — checkout the recorded branch or clear state with `bbs-autopilot clear <ticket>`
```
**No-ticket scope** — empty `TICKET` is a valid shape: skip ticket-state
writes with a one-line note, take requirement/plan from conversation, do the
work. Branch shape and git-flow policy are the workflow layer's concern, not
a skill precondition. Never invent a ticket id; to attach identity without a
checkout, `export BABYSIT_TICKET=<id>` (wins the resolve ladder).
### Handling update-check output
- `UPGRADE_AVAILABLE <old> <new>` — mention once ("babysit upgrade available
  — run `bbs-upgrade`") and continue; never auto-run or block.
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
