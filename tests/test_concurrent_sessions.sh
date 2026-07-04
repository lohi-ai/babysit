#!/usr/bin/env bash
# tests/test_concurrent_sessions.sh — two autopilot sessions for the same
# ticket coexist without colliding. Each gets its own session yaml; neither
# mutates the other's started_at; bbs-ticket session list shows both.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PREAMBLE="$SCRIPT_DIR/.claude/skills/references/preamble.md"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"
[ -f "$PREAMBLE" ] && [ -x "$BBS_TICKET_BIN" ] || { echo "FAIL: missing bins" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

BLOCK="$(mktemp)"
awk '/^# Session-writer hook/,/^# Config \+ repo state/' "$PREAMBLE" \
  | sed '$d' > "$BLOCK"

# ── two-sessions-same-ticket-coexist ───────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"

  # Session A
  ( export BABYSIT_SESSION="sess-A" BABYSIT_TICKET="bs-shared"
    bash -c ". '$BLOCK'" >/dev/null 2>&1 )
  # Session B (same ticket, different uuid)
  ( export BABYSIT_SESSION="sess-B" BABYSIT_TICKET="bs-shared"
    bash -c ". '$BLOCK'" >/dev/null 2>&1 )

  FA="$HOME/.babysit/sessions/sess-A.yaml"
  FB="$HOME/.babysit/sessions/sess-B.yaml"
  [ -f "$FA" ] && [ -f "$FB" ] || { echo "missing yaml files"; ls -la "$HOME/.babysit/sessions"; exit 1; }
  grep -qx "ticket: bs-shared" "$FA" || { echo "FA: bad ticket"; cat "$FA"; exit 1; }
  grep -qx "ticket: bs-shared" "$FB" || { echo "FB: bad ticket"; cat "$FB"; exit 1; }

  # Both sessions appear in list
  BABYSIT_HOME="$HOME/.babysit"; export BABYSIT_HOME
  out="$("$BBS_TICKET_BIN" session list)"
  printf '%s' "$out" | grep -q "sess-A" || { echo "list missing A: $out"; exit 1; }
  printf '%s' "$out" | grep -q "sess-B" || { echo "list missing B: $out"; exit 1; }
) && ok "two-sessions-same-ticket-coexist" || fail "two-sessions-same-ticket-coexist"
rm -rf "$T"

# ── A-rewrite-does-not-touch-B-started_at ──────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"
  ( export BABYSIT_SESSION="sess-A" BABYSIT_TICKET="bs-1"
    bash -c ". '$BLOCK'" >/dev/null 2>&1 )
  ( export BABYSIT_SESSION="sess-B" BABYSIT_TICKET="bs-1"
    bash -c ". '$BLOCK'" >/dev/null 2>&1 )

  FB="$HOME/.babysit/sessions/sess-B.yaml"
  B_BEFORE="$(awk '/^started_at:/ {print; exit}' "$FB")"
  sleep 1
  # Rewrite session A only
  ( export BABYSIT_SESSION="sess-A" BABYSIT_TICKET="bs-1"
    bash -c ". '$BLOCK'" >/dev/null 2>&1 )
  B_AFTER="$(awk '/^started_at:/ {print; exit}' "$FB")"
  [ "$B_BEFORE" = "$B_AFTER" ] || { echo "B started_at changed after A rewrite: $B_BEFORE → $B_AFTER"; exit 1; }
) && ok "A-rewrite-does-not-touch-B-started_at" || fail "A-rewrite-does-not-touch-B-started_at"
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
