#!/usr/bin/env bash
# tests/test_lib_lock.sh — the shared mkdir-lock primitive (bin/lib/lock.sh).
#
# First module of the bin decomposition (docs/bin-decomposition-spike.md). Pins
# the primitive bbs-ticket builds its lock policies
# on: acquire creates the dir, a second acquire on the held dir times out, and
# release removes it (dir + any PID file under it).

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
LOCK_LIB="$SCRIPT_DIR/bin/lib/lock.sh"
[ -f "$LOCK_LIB" ] || { echo "FAIL: $LOCK_LIB missing" >&2; exit 1; }
# shellcheck source=../bin/lib/lock.sh
. "$LOCK_LIB"

PASS=0
FAIL=0
FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
LD="$T/x.lockdir"

# ── acquire creates the lock dir ────────────────────────────────────
bbs_lock_acquire "$LD" 5 && [ -d "$LD" ] && ok "acquire-creates-dir" || fail "acquire-creates-dir"

# ── second acquire on a held lock times out (non-zero) ──────────────
# max_tries=1 → ~0.1s, keeps the test fast.
if bbs_lock_acquire "$LD" 1; then fail "held-lock-times-out" "acquired an already-held lock"; else ok "held-lock-times-out"; fi

# ── release removes the dir and any PID file under it ───────────────
echo "$$" > "$LD/pid"
bbs_lock_release "$LD"
[ ! -e "$LD" ] && ok "release-removes-dir-and-pid" || fail "release-removes-dir-and-pid"

# ── acquire succeeds again after release ────────────────────────────
bbs_lock_acquire "$LD" 5 && [ -d "$LD" ] && ok "reacquire-after-release" || fail "reacquire-after-release"

# ── release of empty/unset path is a harmless no-op ─────────────────
( bbs_lock_release "" ) && ok "release-empty-noop" || fail "release-empty-noop"

rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d checks\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m  %d/%d checks failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
