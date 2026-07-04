#!/usr/bin/env bash
# tests/test_bbs_slug_env_identity.sh — regression guard for env-first identity.
#
# bbs-slug must let BABYSIT_TICKET (and the legacy BBS_TICKET alias) override
# the branch-derived ticket. When both are set and disagree, it must abort
# loudly so the caller can pick one — silent picking is a correctness bug.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_SLUG="$SCRIPT_DIR/bin/bbs-slug"
[ -x "$BBS_SLUG" ] || { echo "FAIL: $BBS_SLUG not executable" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

mk_repo() {
  local d="$1" branch="$2"
  git init -q "$d"
  git -C "$d" -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git -C "$d" checkout -q -b "$branch"
}

# ── branch-derived-ticket-when-no-env ──────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  mk_repo "$T/r" "feat/bs-abc123_my-slug"
  cd "$T/r"
  out="$("$BBS_SLUG" ticket)"
  [ "$out" = "bs-abc123" ] || { echo "got: $out"; exit 1; }
) && ok "branch-derived-ticket-when-no-env" || fail "branch-derived-ticket-when-no-env"
rm -rf "$T"

# ── BABYSIT_TICKET-overrides-branch ────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BBS_TICKET
  export BABYSIT_TICKET="bs-zzz999"
  mk_repo "$T/r" "feat/bs-abc123_my-slug"
  cd "$T/r"
  out="$("$BBS_SLUG" ticket)"
  [ "$out" = "bs-zzz999" ] || { echo "got: $out"; exit 1; }
) && ok "BABYSIT_TICKET-overrides-branch" || fail "BABYSIT_TICKET-overrides-branch"
rm -rf "$T"

# ── BBS_TICKET-legacy-alias-still-works ────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET
  export BBS_TICKET="bs-legacy"
  mk_repo "$T/r" "feat/bs-abc123_my-slug"
  cd "$T/r"
  out="$("$BBS_SLUG" ticket)"
  [ "$out" = "bs-legacy" ] || { echo "got: $out"; exit 1; }
) && ok "BBS_TICKET-legacy-alias-still-works" || fail "BBS_TICKET-legacy-alias-still-works"
rm -rf "$T"

# ── env-conflict-aborts-loudly ─────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  export BABYSIT_TICKET="bs-aaa"
  export BBS_TICKET="bs-bbb"
  mk_repo "$T/r" "feat/bs-abc123_my-slug"
  cd "$T/r"
  out="$("$BBS_SLUG" ticket 2>&1)"
  rc=$?
  [ "$rc" = "1" ] || { echo "expected rc=1, got rc=$rc"; exit 1; }
  printf '%s' "$out" | grep -q "BABYSIT_TICKET=bs-aaa" || { echo "missing BABYSIT_TICKET in error: $out"; exit 1; }
  printf '%s' "$out" | grep -q "BBS_TICKET=bs-bbb"     || { echo "missing BBS_TICKET in error: $out"; exit 1; }
) && ok "env-conflict-aborts-loudly" || fail "env-conflict-aborts-loudly"
rm -rf "$T"

# ── env-agreement-passes ───────────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  export BABYSIT_TICKET="bs-same"
  export BBS_TICKET="bs-same"
  mk_repo "$T/r" "feat/bs-abc123_my-slug"
  cd "$T/r"
  out="$("$BBS_SLUG" ticket)"
  [ "$out" = "bs-same" ] || { echo "got: $out"; exit 1; }
) && ok "env-agreement-passes" || fail "env-agreement-passes"
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
