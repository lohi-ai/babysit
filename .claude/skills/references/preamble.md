# Skill Preamble

Runtime bootstrap for every babysit skill. Run the bash block first; follow
the rules below when producing output and reporting status.

This file contains the shared runtime and status contract.

## Decisions

Route every decision point through
[Auto-Decision Framework](auto-decision-framework.md) (Mechanical / Taste /
User Challenge, 6 principles, audit trail). Read it before deciding.

## Output style — terse by default

ACTIVE EVERY RESPONSE. No filler drift across turns.
Drop: filler (just/really/basically/actually/simply), pleasantries (sure/certainly/of course/happy to), hedging. Short synonyms (fix not "implement a solution for"). Fragments OK. Code blocks unchanged. Pattern: `[thing] [action] [reason]. [next step].`

Consumer routing (Mechanical — auto-decide, no audit entry):

| Consumer | Mode | Rules |
|----------|------|-------|
| Machine — checkpoint, telemetry, handoffs, structured artifacts | **Full** | Drop articles too. Maximum terseness. |
| Human — terminal, AskUserQuestion, NEEDS_CONTEXT | **Lite** | Keep articles + full sentences. Professional but tight. |
| Security/destructive/ambiguous | **Normal** | Full prose. Resume terse after. |

Skills with their own output format take precedence.
Full routing table in [Auto-Decision Framework](auto-decision-framework.md).

## One mode, two escalation channels

Skills always run autonomously. Decisions go through the Auto-Decision
Framework; skills **never** prompt mid-flight for taste or cosmetic choices.
Escalate only when proceeding on a guess would land incorrect work — and when
you do, emit a `NEEDS_CONTEXT` block.

The `AGENT_ROLE` env var (fallback `GT_ROLE`) picks how that block reaches a
human:

| `AGENT_ROLE` | Watcher | `NEEDS_CONTEXT` delivery |
|--------------|---------|--------------------------|
| `developer` (default, unset) | Human at Claude Code terminal | Render as single `AskUserQuestion` |
| `mayor`, `general`, `scanner`, any other | Orchestrator (babysit-office, gastown, cron) | Emit structured block; orchestrator relays via its channel |

`AskUserQuestion` in non-`developer` runs **hangs the run** — always check
`_INVOKER` (set from `AGENT_ROLE` in the preamble) before calling it.

**This file is loaded at skill-invocation time regardless of which repo's
`CLAUDE.md` is in context** — runtime rules live here, not in babysit's own
`CLAUDE.md`. The preamble bash block must run end-to-end without prompts.

### When to escalate

Only emit `NEEDS_CONTEXT` when:

- Ambiguous requirement with multiple plausible interpretations that lead to different code.
- Irreversible / high-blast-radius action (deploy, migration, delete, external send) with no durable authorization on file.
- Missing config, credentials, or scope that can't be inferred from the repo.

Don't escalate for:

- Style / naming / cosmetic — pick and log (Auto-Decision Framework: Mechanical / Taste).
- Anything derivable from codebase, git history, or config — look it up.
- "Is this OK?" checkpoints after self-verifiable work — verify and report.
- Recoverable forks — try the most likely path; on failure, report `BLOCKED` with what was tried.

A second `NEEDS_CONTEXT` in the same run means you're steering — stop, report
what you have, let the human triage.

### `NEEDS_CONTEXT` shape

`REASON` explains why you can't proceed. `RECOMMENDATION` is the exact question
with 2–4 labeled options. When `AGENT_ROLE=developer`, render as one
`AskUserQuestion`; otherwise print the block verbatim.

```
STATUS: NEEDS_CONTEXT
REASON: Requirement "handle duplicate invoices" could mean (a) reject with 409,
(b) merge and sum, or (c) keep newest. Existing code does none of these.
ATTEMPTED: Grepped invoices/*.ts for prior handling — only happy path present.
RECOMMENDATION: Ask the ticket owner which of A/B/C applies before implementing.
```

## Preamble (run first)

```bash
# ── Skill preamble ───────────────────────────────────────────────
_SKILL_NAME="SKILL_NAME"          # set before running
_SESSION_ID="$$-$(date +%s)"
_TEL_START=$(date +%s)

# ── Bin resolver ─────────────────────────────────────────────────
# Every babysit script is invoked via a BBS_<NAME>_BIN env var that resolves
# once here: prefer the home shim (`~/.claude/bbs-*`, created by setup-skills)
# then fall back to the repo copy (`~/.claude/skills/babysit/bin/bbs-*`, used
# when the pack is installed as a plugin without symlinks). Skill files then
# call `"${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" init` etc. instead of repeating the dual-path chain.
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

# Session-writer hook — when an autopilot run set $BABYSIT_SESSION, persist
# (or refresh) the matching ~/.babysit/sessions/<uuid>.yaml. Atomic
# mktemp+mv so the file's mtime gets bumped (in-place edit on Linux
# preserves mtime — see docs/identity.md § Atomic writes). Skipped when
# $BABYSIT_SESSION is unset — most preamble runs are not autopilot.
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

# Project scope — slug + ticket, derived from git remote + branch name. The
# ticket id is NEVER taken from conversation memory: it's re-derived on every
# preamble from the branch, so a compacted or freshly-spawned agent sees the
# same ticket the prior agent did. Empty TICKET means the branch doesn't
# encode one (e.g. main) — the skill decides whether that's OK.
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
# ticket. Single most important line for compaction survival: a cold agent
# reads this and knows where the prior agent left off. Silent when no ticket.
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

- **`INVOKER`** — who triggered this run. Picks the `NEEDS_CONTEXT` delivery
  channel (see routing table above). `developer` → `AskUserQuestion`;
  `mayor` / `general` / `scanner` / other → structured block.
- **`PROACTIVE`** — `"false"` means don't auto-invoke other babysit skills from
  conversation context; only run what the user explicitly typed. Skip silently
  — do **not** ask for confirmation. Babysit is non-interactive by design.
- **`TELEMETRY`** — `local` (default) writes JSONL to `~/.babysit/analytics/`;
  `off` disables all telemetry writes. Nothing ever leaves the machine.
- **`SPAWNED`** — `true` means an orchestrator started this session
  (`OPENCLAW_SESSION` or `BABYSIT_SPAWNED` set). Skip welcome text and optional
  summaries; the parent is capturing output programmatically.

### Ticket consistency — the four-layer invariant

The preamble prints `SLUG`, `BRANCH`, `TICKET`, `PROJECT_HOME`, and (if a
ticket is in scope) the Context Recovery block. Every skill must respect:

1. **Branch name is the machine-readable anchor.** `feat/<ticket>_<slug>`.
   Every wake-up re-derives `TICKET` from the current branch via `bbs-slug`.
   Conversation memory is never trusted.
2. **Checkpoint cross-check.** `~/.babysit/projects/<slug>/tickets/<ticket>/checkpoint.json`
   stores `branch` + `ticket` + `slug`. If `branch` field doesn't match
   current branch, state has diverged — stop and report.
3. **Timeline audit.** `bbs-autopilot` appends every step boundary to
   `~/.babysit/projects/<slug>/timeline.jsonl` keyed by ticket.
4. **Ticket system is the outer oracle.** `"${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" get status` is
   ground truth for whether the ticket exists / is still open (reads
   `index.json` from Layout C). Called at workflow load-context.

**Divergence BLOCKED shape** — use when layers 1 and 2 disagree:

```
STATUS: BLOCKED
VERDICT: —
SUMMARY: Branch/checkpoint divergence — cannot safely resume.
REASON: branch='<current>' but checkpoint.branch='<recorded>' for ticket <ticket>
ATTEMPTED: Derived ticket from branch, read checkpoint.json, compared branch fields
RECOMMENDATION: Human triages — checkout the recorded branch or clear state with `bbs-autopilot clear <ticket>`
```

**No-ticket scope** — when `TICKET` is empty (branch doesn't encode one):
- Accept standalone mode (no checkpoint, no ticket comments) for skills that
  genuinely work without one (`office-hours`, `setup-project`, `browse`), or
- Stop with `NEEDS_CONTEXT` naming which branch shape is expected.

Never invent a ticket id. If the user says "work on bs-ab123" but the branch
is `main`, ask them to check out the right branch (`developer`) or emit
`NEEDS_CONTEXT` (other invokers). Don't fabricate state on the wrong branch.

### Handling update-check output

`_UPD` prints one of:

- `UPGRADE_AVAILABLE <old> <new>` — mention once at top of response ("babysit
  upgrade available — run `bbs-upgrade`") and continue. Don't auto-run
  `bbs-upgrade` unless `auto_upgrade=true`; even then, prefer after the skill
  finishes.
- `JUST_UPGRADED <from> <to>` — user upgraded since last run. Plugin manifest
  in memory is stale until reload. Emit this exact line at top of response:

  > babysit upgraded v\<from\> → v\<to\>. Run `/plugin marketplace update babysit` then `/reload-plugins` to pick up the new skills (the shell upgrade can't do this for you).

- Nothing — up-to-date, snoozed, or offline. Proceed silently.

Never block on upgrade. A pending upgrade is information, not a gate.

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

Every skill must end with exactly one status code.

| Status | Meaning | When |
|--------|---------|------|
| **DONE** | All steps completed successfully | Normal completion, evidence provided |
| **DONE_WITH_CONCERNS** | Completed with caveats | Finished but found issues the caller should know about |
| **BLOCKED** | Cannot proceed | Missing access, broken tool, unresolvable error |
| **NEEDS_CONTEXT** | Missing info to continue | Ambiguous requirements, missing config, unclear scope |

Output format:

```
STATUS: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
VERDICT: <skill-specific verdict per handoff-contracts.md>
SUMMARY: <1-2 sentences of what happened>
```

For non-happy-path statuses, add:

```
REASON: [1-2 sentences]
ATTEMPTED: [what was tried]
RECOMMENDATION: [what should happen next]
```

### Escalation rules

Bad work is worse than no work. Escalate when uncertain.

- **3-attempt rule:** same step fails 3×, stop and report `BLOCKED`.
- **Security uncertainty:** security-sensitive change unclear, stop and report `BLOCKED`.
- **Scope exceeded:** work exceeds what you can verify, stop and report `NEEDS_CONTEXT`.

When in doubt, stop. Never guess silently. Full escalation rules +
`NEEDS_CONTEXT` format above in [§ One mode, two escalation channels](#one-mode-two-escalation-channels).

### Status ↔ verdict mapping

Status is separate from skill-specific verdicts in
[handoff-contracts.md](handoff-contracts.md). Both are reported:

| Skill | Verdict | Status |
|-------|---------|--------|
| requirements-check | `PASS` | `DONE` |
| requirements-check | `REVIEW(M)` | `DONE` (deferred to implement) |
| bug-scan | `CLEAN` | `DONE` |
| bug-scan | `FOUND(3 fixed, 1 deferred)` | `DONE_WITH_CONCERNS` |
| implement | `BUILT` | `DONE` |
| qa | `FAIL` | `BLOCKED` (never `DONE*` — the PR gate reads `DONE*` as ready) |
| review-pr | `FINDINGS(N)` unresolved material | `BLOCKED` (minor residuals → `DONE_WITH_CONCERNS`) |
| investigate | `FIXED` | `DONE` |
| browse | `CHECKED` | `DONE` |
| any skill | tool broke, can't proceed | `BLOCKED` |
| any skill | requirements ambiguous | `NEEDS_CONTEXT` |
