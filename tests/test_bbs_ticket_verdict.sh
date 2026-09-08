#!/usr/bin/env bash
# tests/test_bbs_ticket_verdict.sh — coverage for `bbs ticket set-verdict`'s
# hollow-body guard.
#
# A verdict body with no `STATUS:` line reads as `none` at every gate — the same
# as never having run — while still looking done to a human reading the file.
# That is how bs-ho0yw43e stalled at the push gate on 2026-08-29 with a
# confident `VERDICT: PASS (with 1 finding, fixed)` sitting on disk. set-verdict
# refuses that body at the write, where the producer is still around to fix it.
#
# Scenarios:
#   hollow-body-refused      the live body: rc 2, stderr names STATUS:, nothing written
#   status-body-accepted     a real verdict still writes and reads back
#   body-file-guarded        --body-file takes the same check (long verdicts use it)
#   placeholder-allowed      an explicitly empty body still records "<no verdict>"
#   refusal-does-not-clobber set-verdict is last-writer-wins; a refused hollow
#                            write must not destroy the real verdict underneath

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS="$SCRIPT_DIR/bin/bbs-ticket"

[ -x "$BBS" ] || { echo "FAIL: $BBS not built (go build -o bin/bbs ./cmd/bbs)"; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

sandbox() {
  export HOME="$1/home"; mkdir -p "$HOME"
  export BABYSIT_PROJECT_HOME="$1/home/projects/slug"
  export BABYSIT_TICKET="bs-vd00001"
  export AGENT_ROLE=mayor
  unset BBS_TICKET 2>/dev/null || true
  VP="$BABYSIT_PROJECT_HOME/tickets/$BABYSIT_TICKET/verdicts/review-pr.md"
}

# Verbatim what the spawned verifier persisted for bs-ho0yw43e.
HOLLOW='# review-pr — bs-ho0yw43e

VERDICT: PASS (with 1 finding, fixed)
Effort: medium. Scope: `git diff HEAD~1`.'

# ── hollow-body-refused ───────────────────────────────────────────────
TMP=$(mktemp -d); sandbox "$TMP"
ERR=$("$BBS" set-verdict --skill review-pr --body "$HOLLOW" 2>&1 >/dev/null); RC=$?
if [ "$RC" -ne 2 ]; then
  fail hollow-body-refused "rc=$RC, want 2"
elif ! printf '%s' "$ERR" | grep -q 'STATUS:'; then
  fail hollow-body-refused "stderr does not name the missing line: $ERR"
elif [ -f "$VP" ]; then
  fail hollow-body-refused "refused write still created $VP"
elif [ "$("$BBS" verdict-status --skill review-pr)" != "none" ]; then
  fail hollow-body-refused "verdict-status moved off none"
else
  ok hollow-body-refused
fi
rm -rf "$TMP"

# ── status-body-accepted ──────────────────────────────────────────────
TMP=$(mktemp -d); sandbox "$TMP"
if ! "$BBS" set-verdict --skill review-pr --body "STATUS: DONE
$HOLLOW" >/dev/null 2>&1; then
  fail status-body-accepted "set-verdict rejected a body carrying a status line"
elif [ "$("$BBS" verdict-status --skill review-pr)" != "DONE" ]; then
  fail status-body-accepted "verdict-status = $("$BBS" verdict-status --skill review-pr), want DONE"
else
  ok status-body-accepted
fi
rm -rf "$TMP"

# ── body-file-guarded ─────────────────────────────────────────────────
TMP=$(mktemp -d); sandbox "$TMP"
printf '%s\n' "$HOLLOW" > "$TMP/v.md"
"$BBS" set-verdict --skill review-pr --body-file "$TMP/v.md" >/dev/null 2>&1; RC=$?
if [ "$RC" -ne 2 ]; then
  fail body-file-guarded "rc=$RC, want 2 — --body-file bypassed the guard"
elif [ -f "$VP" ]; then
  fail body-file-guarded "refused --body-file write still created $VP"
else
  ok body-file-guarded
fi
rm -rf "$TMP"

# ── placeholder-allowed ───────────────────────────────────────────────
# An empty body is a caller explicitly recording the absence of a verdict, not
# one fumbling the format of a real one. It stays legal.
TMP=$(mktemp -d); sandbox "$TMP"
if ! "$BBS" set-verdict --skill review-pr >/dev/null 2>&1; then
  fail placeholder-allowed "empty body no longer records the placeholder"
elif ! grep -q '<no verdict>' "$VP" 2>/dev/null; then
  fail placeholder-allowed "placeholder body not written to $VP"
else
  ok placeholder-allowed
fi
rm -rf "$TMP"

# ── refusal-does-not-clobber ──────────────────────────────────────────
TMP=$(mktemp -d); sandbox "$TMP"
"$BBS" set-verdict --skill review-pr --body "STATUS: DONE" >/dev/null 2>&1
"$BBS" set-verdict --skill review-pr --body "$HOLLOW" >/dev/null 2>&1
if [ "$("$BBS" verdict-status --skill review-pr)" != "DONE" ]; then
  fail refusal-does-not-clobber "a refused hollow write destroyed the real verdict"
else
  ok refusal-does-not-clobber
fi
rm -rf "$TMP"

echo
printf 'verdict guard: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then printf 'failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; fi
