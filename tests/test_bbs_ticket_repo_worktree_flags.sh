#!/usr/bin/env bash
# tests/test_bbs_ticket_repo_worktree_flags.sh — coverage for
# `bbs-ticket init --repo --worktree` and `set-pointer repo|worktree`.
#
# Scenarios (per sub-ticket #2 acceptance criteria):
#   init-with-both-flags, set-pointer-repo, set-pointer-worktree.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET="$SCRIPT_DIR/bin/bbs-ticket"
BBS_SLUG="$SCRIPT_DIR/bin/bbs-slug"
[ -x "$BBS_TICKET" ] || { echo "FAIL: $BBS_TICKET not executable" >&2; exit 1; }

PASS=0
FAIL=0
FAIL_NAMES=()

ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Set up a disposable repo on a feat/<ticket>_* branch so bbs-slug derives a
# ticket and bbs-ticket writes under a sandboxed $HOME/.babysit.
setup_env() {
  local t="$1"
  export HOME="$t/home"
  export PATH="$SCRIPT_DIR/bin:$PATH"
  mkdir -p "$HOME"
  git init -q "$t/repo"
  cd "$t/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git checkout -q -b feat/bs-x123_harness
  git remote add origin "$t/remote.git" 2>/dev/null || true
  git init -q --bare "$t/remote.git"
}

# init-with-both-flags
T="$(mktemp -d)"
(
  setup_env "$T"
  F="$("$BBS_TICKET" init --repo fe --worktree /tmp/wt/fe)"
  [ -f "$F" ] || { echo "no index"; exit 1; }
  # Confirm both pointers landed. Use bbs-ticket get-pointer for both.
  r="$("$BBS_TICKET" get-pointer repo 2>/dev/null)"
  w="$("$BBS_TICKET" get-pointer worktree 2>/dev/null)"
  [ "$r" = "fe" ] || { echo "repo=$r"; exit 1; }
  [ "$w" = "/tmp/wt/fe" ] || { echo "worktree=$w"; exit 1; }
) && ok "init-with-both-flags" || fail "init-with-both-flags"
rm -rf "$T"

# set-pointer-repo (post-init update)
T="$(mktemp -d)"
(
  setup_env "$T"
  "$BBS_TICKET" init >/dev/null
  "$BBS_TICKET" set-pointer repo be
  r="$("$BBS_TICKET" get-pointer repo 2>/dev/null)"
  [ "$r" = "be" ]
) && ok "set-pointer-repo" || fail "set-pointer-repo"
rm -rf "$T"

# set-pointer-worktree (post-init update)
T="$(mktemp -d)"
(
  setup_env "$T"
  "$BBS_TICKET" init >/dev/null
  "$BBS_TICKET" set-pointer worktree /tmp/wt/be
  w="$("$BBS_TICKET" get-pointer worktree 2>/dev/null)"
  [ "$w" = "/tmp/wt/be" ]
) && ok "set-pointer-worktree" || fail "set-pointer-worktree"
rm -rf "$T"

# ── summary ───────────────────────────────────────────────────────────

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
