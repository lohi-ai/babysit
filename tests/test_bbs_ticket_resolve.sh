#!/usr/bin/env bash
# tests/test_bbs_ticket_resolve.sh — coverage for `bbs-ticket resolve`.
#
# Resolution ladder (docs/identity.md):
#   step 1: env BABYSIT_TICKET / BBS_TICKET (or conflict → exit 2)
#   step 2: manifest.yaml worktree cwd match (multi-match → exit 2)
#   step 3: branch regex
#   exit 1: no resolution

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"
BBS_SLUG_BIN="$SCRIPT_DIR/bin/bbs-slug"
[ -x "$BBS_TICKET_BIN" ] && [ -x "$BBS_SLUG_BIN" ] || { echo "FAIL: bins not executable" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

mk_repo() {
  local d="$1" branch="$2"
  git init -q "$d"
  git -C "$d" -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git -C "$d" checkout -q -b "$branch"
}

# ── step1-env-resolves ─────────────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BBS_TICKET; export BABYSIT_TICKET="bs-fromenv"
  mk_repo "$T/r" "main"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  out="$("$BBS_TICKET_BIN" resolve)"
  [ "$out" = "bs-fromenv" ] || { echo "got: $out"; exit 1; }
) && ok "step1-env-resolves" || fail "step1-env-resolves"
rm -rf "$T"

# ── step1-env-conflict-blocks ──────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  export BABYSIT_TICKET="bs-aaa" BBS_TICKET="bs-bbb"
  mk_repo "$T/r" "main"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  err="$("$BBS_TICKET_BIN" resolve 2>&1 1>/dev/null)"; rc=$?
  [ "$rc" = "2" ] || { echo "expected rc=2, got rc=$rc; err=$err"; exit 1; }
  printf '%s' "$err" | grep -q "STATUS: BLOCKED" || { echo "missing BLOCKED in: $err"; exit 1; }
) && ok "step1-env-conflict-blocks" || fail "step1-env-conflict-blocks"
rm -rf "$T"

# ── step3-branch-fallback ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  mk_repo "$T/r" "feat/bs-zzz_topic"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  out="$("$BBS_TICKET_BIN" resolve)"
  [ "$out" = "bs-zzz" ] || { echo "got: $out"; exit 1; }
) && ok "step3-branch-fallback" || fail "step3-branch-fallback"
rm -rf "$T"

# ── no-resolution-exits-1 ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  mk_repo "$T/r" "main"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  out="$("$BBS_TICKET_BIN" resolve 2>/dev/null)"; rc=$?
  [ "$rc" = "1" ] || { echo "expected rc=1, got rc=$rc; out=$out"; exit 1; }
  [ -z "$out" ]   || { echo "expected empty stdout, got: $out"; exit 1; }
) && ok "no-resolution-exits-1" || fail "no-resolution-exits-1"
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
