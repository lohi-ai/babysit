#!/usr/bin/env bash
# tests/test_session_rehydrate.sh — round-trip:
#   1. preamble session-writer hook persists ~/.babysit/sessions/<uuid>.yaml
#   2. `bbs-ticket session attach <uuid>` echoes the right exports
#   3. eval-ing those exports lets `bbs-ticket resolve` recover the ticket
#      with no branch context (cwd is /tmp).
#
# This is the "I crashed, where was I" recovery path.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PREAMBLE="$SCRIPT_DIR/.claude/skills/references/preamble.md"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"
[ -f "$PREAMBLE" ] && [ -x "$BBS_TICKET_BIN" ] \
  || { echo "FAIL: missing bins" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

BLOCK="$(mktemp)"
awk '/^# Session-writer hook/,/^# Config \+ repo state/' "$PREAMBLE" \
  | sed '$d' > "$BLOCK"

# ── attach-then-resolve-recovers-ticket ────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"

  # Session 1: persist via preamble hook
  ( export BABYSIT_SESSION="sess-rehydrate" BABYSIT_TICKET="bs-recovered"
    bash -c ". '$BLOCK'" >/dev/null 2>&1 )

  # Drop env entirely (simulating crash + new shell)
  unset BABYSIT_SESSION BABYSIT_TICKET BBS_TICKET

  # Session 2: attach
  BABYSIT_HOME="$HOME/.babysit"; export BABYSIT_HOME
  exports="$("$BBS_TICKET_BIN" session attach "sess-rehydrate")"
  printf '%s' "$exports" | grep -qx "export BABYSIT_TICKET=bs-recovered" \
    || { echo "missing TICKET export: $exports"; exit 1; }
  eval "$exports"
  [ "$BABYSIT_TICKET"  = "bs-recovered" ] || { echo "TICKET not set: $BABYSIT_TICKET"; exit 1; }

  # cd somewhere with no branch context — resolve must still recover via env (step 1).
  cd "$T"
  out="$("$BBS_TICKET_BIN" resolve)"
  [ "$out" = "bs-recovered" ] || { echo "resolve returned: $out"; exit 1; }
) && ok "attach-then-resolve-recovers-ticket" || fail "attach-then-resolve-recovers-ticket"
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
