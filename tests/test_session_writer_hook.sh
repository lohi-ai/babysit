#!/usr/bin/env bash
# tests/test_session_writer_hook.sh — verifies the preamble session-writer
# hook persists ~/.babysit/sessions/<uuid>.yaml when BABYSIT_SESSION is set,
# and is a no-op when it isn't. Also pins started_at preservation across
# rewrites and atomic mtime bump.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PREAMBLE="$SCRIPT_DIR/.claude/skills/references/preamble.md"
[ -f "$PREAMBLE" ] || { echo "FAIL: $PREAMBLE missing" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Extract the session-writer block from preamble.md into an executable snippet.
# The block is uniquely bounded by `# Session-writer hook` comment and the
# next `# Config + repo state.` comment.
BLOCK="$(mktemp)"
awk '/^# Session-writer hook/,/^# Config \+ repo state/' "$PREAMBLE" \
  | sed '$d' > "$BLOCK"
[ -s "$BLOCK" ] || { echo "FAIL: could not extract session block" >&2; rm -f "$BLOCK"; exit 1; }

# ── no-op-when-BABYSIT_SESSION-unset ───────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"
  unset BABYSIT_SESSION
  bash -c ". '$BLOCK'" >/dev/null 2>&1
  count="$(find "$HOME/.babysit/sessions" -type f -name '*.yaml' | wc -l | tr -d ' ')"
  [ "$count" = "0" ] || { echo "expected 0 yaml files, got $count"; exit 1; }
) && ok "no-op-when-BABYSIT_SESSION-unset" || fail "no-op-when-BABYSIT_SESSION-unset"
rm -rf "$T"

# ── writes-yaml-with-correct-fields ────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"
  export BABYSIT_SESSION="sess-uuid-1"
  export BABYSIT_TICKET="bs-fields"
  bash -c ". '$BLOCK'" >/dev/null 2>&1
  F="$HOME/.babysit/sessions/sess-uuid-1.yaml"
  [ -f "$F" ] || { echo "no yaml at $F"; ls -R "$HOME/.babysit"; exit 1; }
  grep -qx "version: 1"            "$F" || { echo "bad version"; cat "$F"; exit 1; }
  grep -qx "session_id: sess-uuid-1" "$F" || { echo "bad session_id"; cat "$F"; exit 1; }
  grep -qx "ticket: bs-fields"     "$F" || { echo "bad ticket"; cat "$F"; exit 1; }
  grep -q  "^started_at: "         "$F" || { echo "missing started_at"; cat "$F"; exit 1; }
  grep -q  "^last_seen_at: "       "$F" || { echo "missing last_seen_at"; cat "$F"; exit 1; }
  grep -q  "^pid: "                "$F" || { echo "missing pid"; cat "$F"; exit 1; }
  grep -q  "^cwd: "                "$F" || { echo "missing cwd"; cat "$F"; exit 1; }
) && ok "writes-yaml-with-correct-fields" || fail "writes-yaml-with-correct-fields"
rm -rf "$T"

# ── preserves-started_at-on-rewrite ────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"
  export BABYSIT_SESSION="sess-keep" BABYSIT_TICKET="bs-k"
  bash -c ". '$BLOCK'" >/dev/null 2>&1
  F="$HOME/.babysit/sessions/sess-keep.yaml"
  ORIG_STARTED="$(awk '/^started_at:/ {print; exit}' "$F")"
  [ -n "$ORIG_STARTED" ] || { echo "no started_at after first write"; exit 1; }
  sleep 1
  bash -c ". '$BLOCK'" >/dev/null 2>&1
  NEW_STARTED="$(awk '/^started_at:/ {print; exit}' "$F")"
  [ "$ORIG_STARTED" = "$NEW_STARTED" ] || { echo "started_at changed: $ORIG_STARTED → $NEW_STARTED"; exit 1; }
  # Ensure exactly one started_at line in the file (no duplicate from awk fallback)
  count="$(grep -c '^started_at:' "$F")"
  [ "$count" = "1" ] || { echo "expected 1 started_at line, got $count"; cat "$F"; exit 1; }
) && ok "preserves-started_at-on-rewrite" || fail "preserves-started_at-on-rewrite"
rm -rf "$T"

# ── mtime-bumped-on-rewrite ────────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"
  export BABYSIT_SESSION="sess-mtime" BABYSIT_TICKET="bs-m"
  bash -c ". '$BLOCK'" >/dev/null 2>&1
  F="$HOME/.babysit/sessions/sess-mtime.yaml"
  T1="$(stat -f %m "$F" 2>/dev/null || stat -c %Y "$F" 2>/dev/null)"
  sleep 2
  bash -c ". '$BLOCK'" >/dev/null 2>&1
  T2="$(stat -f %m "$F" 2>/dev/null || stat -c %Y "$F" 2>/dev/null)"
  [ "$T2" -gt "$T1" ] || { echo "mtime not bumped: $T1 → $T2"; exit 1; }
) && ok "mtime-bumped-on-rewrite" || fail "mtime-bumped-on-rewrite"
rm -rf "$T"

rm -f "$BLOCK"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
