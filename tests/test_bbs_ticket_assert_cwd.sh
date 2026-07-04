#!/usr/bin/env bash
# tests/test_bbs_ticket_assert_cwd.sh — assert-cwd is a compatibility no-op.
#
# Product mode (the only mode that enforced a cwd) is removed; skills still
# call `bbs-ticket assert-cwd` at step 0, so the subcommand must keep
# exiting 0 from any cwd.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"
[ -x "$BBS_TICKET_BIN" ] || { echo "FAIL: $BBS_TICKET_BIN not executable" >&2; exit 1; }

PASS=0
FAIL=0
FAIL_NAMES=()

ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# ── noop-in-plain-git-repo ────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  git init -q "$T/repo"
  cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  "$BBS_TICKET_BIN" assert-cwd
) && ok "noop-in-plain-git-repo" || fail "noop-in-plain-git-repo"
rm -rf "$T"

# ── noop-outside-git ──────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  cd "$T"
  "$BBS_TICKET_BIN" assert-cwd
) && ok "noop-outside-git" || fail "noop-outside-git"
rm -rf "$T"

# ── noop-with-bypass-env ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  cd "$T"
  BBS_ALLOW_CANONICAL_CWD=1 "$BBS_TICKET_BIN" assert-cwd 2>/dev/null
) && ok "noop-with-bypass-env" || fail "noop-with-bypass-env"
rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
