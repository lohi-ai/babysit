#!/usr/bin/env bash
# tests/test_bbs_git_flow_profile.sh — coverage for `bbs autopilot git-flow`,
# the one resolver that turns `.babysit/git-flow.yaml` into policy.
#
# The unit table (internal/cmd/gitflow_test.go) covers the derivation itself.
# This suite proves the *wired* behavior: what the CLI prints from a real repo,
# and that `ticket ensure` cuts the shape each profile implies.
#
# Scenarios:
#   profile-pet-derives-trunk-smoke        profile: pet → trunk/none/smoke/low
#   profile-startup-derives-branch         profile: startup → branch/pr/standard/medium
#   profile-enterprise-derives-strict      profile: enterprise → branch/pr/strict/high
#   legacy-profile-names-resolve           the four pre-profile names still map
#   explicit-key-overrides-profile         mode:/land:/push: beat the preset
#   invalid-profile-exits-2                profile: hobby → exit 2, named
#   pet-ensure-rides-main                  profile: pet → no cut
#   enterprise-ensure-cuts-in-place        profile: enterprise + clean base → in-place cut
#   dirty-tree-divert-warns-about-loop     branch mode + dirty → warns, doesn't cut silently

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_BIN="$SCRIPT_DIR/bin/bbs"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

build_repo() {
  local t="$1"
  git init -q --bare "$t/remote.git"
  git init -q "$t/repo"
  (
    cd "$t/repo"
    git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
    git branch -M main
    git remote add origin "$t/remote.git"
    git push -q origin main
  )
}

set_git_flow() {
  mkdir -p .babysit
  printf '%s\n' "$1" > .babysit/git-flow.yaml
  git add .babysit/git-flow.yaml
  git -c user.email=t@t -c user.name=t commit -q -m "git-flow config"
  git push -q origin main
}

# gf <yaml> → the BBS_* block for a fresh repo carrying that yaml.
gf() {
  local t; t="$(mktemp -d)"
  (
    export HOME="$t/home"; mkdir -p "$HOME"
    build_repo "$t"
    cd "$t/repo"
    set_git_flow "$1"
    "$BBS_BIN" autopilot git-flow
  )
  local rc=$?
  rm -rf "$t"
  return $rc
}

# expect_gf <name> <yaml> <expected BBS_* line>...
expect_gf() {
  local name="$1" yaml="$2"; shift 2
  local out; out="$(gf "$yaml")" || { fail "$name" "git-flow exited non-zero"; return; }
  local want
  for want in "$@"; do
    printf '%s\n' "$out" | grep -qx -- "$want" || {
      fail "$name" "missing '$want' in: $(printf '%s' "$out" | tr '\n' ' ')"; return; }
  done
  ok "$name"
}

expect_gf "profile-pet-derives-trunk-smoke" "profile: pet" \
  BBS_PROFILE=pet BBS_MODE=trunk BBS_LAND=none BBS_PUSH=true \
  BBS_RIGOR=smoke BBS_REVIEW_EFFORT=low BBS_BASE_BRANCH=main

expect_gf "profile-startup-derives-branch" "profile: startup" \
  BBS_PROFILE=startup BBS_MODE=branch BBS_LAND=pr \
  BBS_RIGOR=standard BBS_REVIEW_EFFORT=medium

expect_gf "profile-enterprise-derives-strict" "profile: enterprise" \
  BBS_PROFILE=enterprise BBS_MODE=branch BBS_LAND=pr \
  BBS_RIGOR=strict BBS_REVIEW_EFFORT=high

# ── legacy-profile-names-resolve ──────────────────────────────────────
legacy_ok=1
check_legacy() { # <yaml profile> <expected line>...
  local p="$1"; shift
  local out; out="$(gf "profile: $p")" || { legacy_ok=0; return; }
  local want
  for want in "$@"; do
    printf '%s\n' "$out" | grep -qx -- "$want" || {
      echo "        legacy '$p': missing '$want'"; legacy_ok=0; }
  done
}
check_legacy trunk           BBS_PROFILE=pet        BBS_MODE=trunk    BBS_RIGOR=smoke
check_legacy branch-pr       BBS_PROFILE=startup    BBS_MODE=branch   BBS_LAND=pr
check_legacy worktree-pr     BBS_PROFILE=enterprise BBS_MODE=worktree BBS_LAND=pr
check_legacy worktree-review BBS_PROFILE=enterprise BBS_MODE=worktree BBS_LAND=local
[ "$legacy_ok" -eq 1 ] && ok "legacy-profile-names-resolve" || fail "legacy-profile-names-resolve"

expect_gf "explicit-key-overrides-profile" \
  "$(printf 'profile: pet\nmode: worktree\nland: pr\npush: false\nbase_branch: main')" \
  BBS_PROFILE=pet BBS_MODE=worktree BBS_LAND=pr BBS_PUSH=false BBS_RIGOR=smoke

# ── invalid-profile-exits-2 ───────────────────────────────────────────
T="$(mktemp -d)"
(
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "profile: hobby"
  "$BBS_BIN" autopilot git-flow >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc"; exit 1; }
  grep -q "hobby" "$T/err" || { echo "error does not name the bad value: $(cat "$T/err")"; exit 1; }
) && ok "invalid-profile-exits-2" || fail "invalid-profile-exits-2"
rm -rf "$T"

# ── pet-ensure-rides-main ─────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "profile: pet"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint pet-a --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    && { echo "pet cut a worktree; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "pet cut a branch: $(git branch --show-current)"; exit 1; }
) && ok "pet-ensure-rides-main" || fail "pet-ensure-rides-main"
rm -rf "$T"

# ── enterprise-ensure-cuts-in-place ───────────────────────────────────
# The point of dropping worktree from the profiles: enterprise gets the same
# 0-step inner loop as everyone else.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "profile: enterprise"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint ent-a --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    && { echo "enterprise diverted to a worktree; out: $out"; exit 1; }
  case "$(git branch --show-current)" in
    feat/bs-*_ent-a) : ;;
    *) echo "expected an in-place cut, got '$(git branch --show-current)'"; exit 1 ;;
  esac
) && ok "enterprise-ensure-cuts-in-place" || fail "enterprise-ensure-cuts-in-place"
rm -rf "$T"

# ── dirty-tree-divert-warns-about-loop ────────────────────────────────
# The divert still happens (it is the safe thing), but it must say what it
# costs and how to get the fast loop back.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "profile: startup"
  echo "uncommitted" > dirty.txt

  out="$("$BBS_TICKET_BIN" ensure --slug-hint dirty-a --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    || { echo "expected a worktree divert on a dirty tree; out: $out"; exit 1; }
  grep -q "WARNING" "$T/err" || { echo "divert was silent: $(cat "$T/err")"; exit 1; }
  grep -q "commit or stash" "$T/err" \
    || { echo "no recovery hint: $(cat "$T/err")"; exit 1; }
) && ok "dirty-tree-divert-warns-about-loop" || fail "dirty-tree-divert-warns-about-loop"
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
