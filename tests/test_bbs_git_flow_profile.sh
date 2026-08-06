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
#   unconfigured-repo-derives-pet          no profile: key → pet, like plain git
#   profile-startup-derives-worktree       profile: startup → worktree/local/standard/medium
#   profile-enterprise-derives-strict      profile: enterprise → worktree/local/strict/high
#   legacy-profile-names-resolve           the four pre-profile names still map
#   explicit-key-overrides-profile         mode:/land:/push: beat the preset
#   invalid-profile-exits-2                profile: hobby → exit 2, named
#   branch-with-land-local-exits-2         lone mode: branch → land pr; the pair → exit 2
#   pet-ensure-rides-main                  profile: pet → no cut
#   enterprise-ensure-diverts-to-worktree  profile: enterprise → worktree, primary stays on base
#   dirty-tree-divert-warns-about-loop     branch mode + dirty → warns, doesn't cut silently
#   eval-of-the-resolver-is-injection-safe yaml values cannot execute under eval
#   skills-consume-the-derived-policy      qa/create-pr/review-pr/setup-project read it

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

# Expectations are written as bare KEY=value and compared against the emitted
# KEY='value' — the output is consumed by `eval`, so every value ships
# single-quoted (see printGitFlow). Building the quotes here rather than in each
# expectation means a regression that stopped quoting fails every scenario.
quoted() { printf "%s='%s'" "${1%%=*}" "${1#*=}"; }

# expect_gf <name> <yaml> <expected BBS_KEY=value>...
expect_gf() {
  local name="$1" yaml="$2"; shift 2
  local out; out="$(gf "$yaml")" || { fail "$name" "git-flow exited non-zero"; return; }
  local want
  for want in "$@"; do
    printf '%s\n' "$out" | grep -qx -- "$(quoted "$want")" || {
      fail "$name" "missing '$(quoted "$want")' in: $(printf '%s' "$out" | tr '\n' ' ')"; return; }
  done
  ok "$name"
}

expect_gf "profile-pet-derives-trunk-smoke" "profile: pet" \
  BBS_PROFILE=pet BBS_MODE=trunk BBS_LAND=none BBS_PUSH=true \
  BBS_RIGOR=smoke BBS_REVIEW_EFFORT=low BBS_BASE_BRANCH=main

expect_gf "unconfigured-repo-derives-pet" "base_branch: main" \
  BBS_PROFILE=pet BBS_MODE=trunk BBS_LAND=none BBS_PUSH=true \
  BBS_RIGOR=smoke BBS_REVIEW_EFFORT=low

expect_gf "profile-startup-derives-worktree" "profile: startup" \
  BBS_PROFILE=startup BBS_MODE=worktree BBS_LAND=local \
  BBS_RIGOR=standard BBS_REVIEW_EFFORT=medium

# auto_land is off in every preset and opt-in per repo: it is the one key that
# moves the local base with nobody watching.
expect_gf "auto-land-is-off-by-default" "profile: enterprise" \
  BBS_AUTO_LAND=false

expect_gf "auto-land-opt-in-is-exported" "profile: startup
auto_land: true" \
  BBS_PROFILE=startup BBS_LAND=local BBS_AUTO_LAND=true

expect_gf "profile-enterprise-derives-strict" "profile: enterprise" \
  BBS_PROFILE=enterprise BBS_MODE=worktree BBS_LAND=local \
  BBS_RIGOR=strict BBS_REVIEW_EFFORT=high

# ── legacy-profile-names-resolve ──────────────────────────────────────
legacy_ok=1
check_legacy() { # <yaml profile> <expected line>...
  local p="$1"; shift
  local out; out="$(gf "profile: $p")" || { legacy_ok=0; return; }
  local want
  for want in "$@"; do
    printf '%s\n' "$out" | grep -qx -- "$(quoted "$want")" || {
      echo "        legacy '$p': missing '$(quoted "$want")'"; legacy_ok=0; }
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

# ── branch-with-land-local-exits-2 ────────────────────────────────────
# `mode: branch` + `land: local` reads as configured and can never run: the
# cut takes the primary off base, so nothing composes there. Written by hand it
# has to fail at resolve time, not at the first compose hours later — but a
# lone `mode: branch` (the solo opt-out, and every pre-preset repo) is not that
# mistake and must still resolve, to the PR landing branch mode implies.
T="$(mktemp -d)"
(
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"

  set_git_flow "$(printf 'profile: startup\nmode: branch')"
  eval "$("$BBS_BIN" autopilot git-flow)" || { echo "lone mode: branch failed to resolve"; exit 1; }
  [ "$BBS_MODE" = branch ] && [ "$BBS_LAND" = pr ] \
    || { echo "lone mode: branch derived $BBS_MODE/$BBS_LAND, want branch/pr"; exit 1; }

  set_git_flow "$(printf 'profile: startup\nmode: branch\nland: local')"
  "$BBS_BIN" autopilot git-flow >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc"; exit 1; }
  grep -q "land: pr" "$T/err" || { echo "error does not name the fix: $(cat "$T/err")"; exit 1; }
) && ok "branch-with-land-local-exits-2" || fail "branch-with-land-local-exits-2"
rm -rf "$T"

# ── auto-land-with-land-pr-exits-2 ────────────────────────────────────
# A PR is the landing venue. Auto-merging into local base behind it would land
# work the profile said a PR must gate, so the pair has to fail at resolve time
# rather than at the first worker that finishes overnight.
T="$(mktemp -d)"
(
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"

  set_git_flow "$(printf 'profile: startup\nland: pr\nauto_land: true')"
  "$BBS_BIN" autopilot git-flow >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc"; exit 1; }
  grep -q "auto_land" "$T/err" || { echo "error does not name the key: $(cat "$T/err")"; exit 1; }

  # …and the same pair reached through a legacy alias, which pins land: pr.
  set_git_flow "$(printf 'profile: worktree-pr\nauto_land: true')"
  "$BBS_BIN" autopilot git-flow >/dev/null 2>&1; rc=$?
  [ "$rc" -eq 2 ] || { echo "alias path: expected rc=2, got $rc"; exit 1; }
) && ok "auto-land-with-land-pr-exits-2" || fail "auto-land-with-land-pr-exits-2"
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

# ── enterprise-ensure-diverts-to-worktree ─────────────────────────────
# The whole point of the worktree preset: the primary checkout never leaves
# base_branch, because that is the only state in which a batch can be composed
# there for review.
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
    || { echo "expected a worktree cut; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "primary left base: $(git branch --show-current)"; exit 1; }
) && ok "enterprise-ensure-diverts-to-worktree" || fail "enterprise-ensure-diverts-to-worktree"
rm -rf "$T"

# ── dirty-tree-divert-warns-about-loop ────────────────────────────────
# Only reachable from the solo-loop opt-out (`mode: branch` + `land: pr`),
# which is the one shape that still cuts in place. The divert still happens
# (it is the safe thing), but it must say what it costs and how to get the
# fast loop back — a repo under the worktree preset chose that tax knowingly
# and gets an informational line instead.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "$(printf 'profile: startup\nmode: branch\nland: pr')"
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

# ── eval-of-the-resolver-is-injection-safe ────────────────────────────
# Every skill now reads policy with `eval "$(bbs autopilot git-flow)"`, and
# git-flow.yaml is a committed file in a repo babysit clones. An unquoted value
# is therefore arbitrary shell running as the user.
T="$(mktemp -d)"
(
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "profile: startup
base_branch: main; touch $T/PWNED-SEMICOLON"
  eval "$("$BBS_BIN" autopilot git-flow)" 2>/dev/null
  [ -e "$T/PWNED-SEMICOLON" ] && { echo "a ';' in base_branch executed under eval"; exit 1; }
  [ "$BBS_BASE_BRANCH" = "main; touch $T/PWNED-SEMICOLON" ] \
    || { echo "value did not survive quoting: '$BBS_BASE_BRANCH'"; exit 1; }

  set_git_flow "profile: startup
base_branch: \$(touch $T/PWNED-SUBST)main"
  eval "$("$BBS_BIN" autopilot git-flow)" 2>/dev/null
  [ -e "$T/PWNED-SUBST" ] && { echo "a \$( ) in base_branch executed under eval"; exit 1; }

  # a literal single quote must not break the eval either
  set_git_flow "profile: startup
base_branch: it's-main"
  eval "$("$BBS_BIN" autopilot git-flow)" 2>/dev/null \
    || { echo "a quote in the value broke the eval"; exit 1; }
  [ "$BBS_BASE_BRANCH" = "it's-main" ] || { echo "quote mangled: '$BBS_BASE_BRANCH'"; exit 1; }
  exit 0
) && ok "eval-of-the-resolver-is-injection-safe" || fail "eval-of-the-resolver-is-injection-safe"
rm -rf "$T"

# ── skills-consume-the-derived-policy ─────────────────────────────────
# The resolver is only half the feature: a profile that nothing reads changes
# nothing. Pin the consumer side of each skill the way
# test_autopilot_readiness_gate.sh pins builder.md's gate prose — the
# derivation above keeps passing even after a consumer silently drops it.
SK="$SCRIPT_DIR/.claude/skills"
(
  qa="$SK/qa/SKILL.md"
  grep -q 'bbs autopilot git-flow' "$qa" || { echo "qa does not read the derived policy"; exit 1; }
  grep -q 'BBS_RIGOR' "$qa"              || { echo "qa does not consume \$BBS_RIGOR"; exit 1; }
  for tier in smoke standard strict; do
    grep -q -- "- \*\*$tier\*\*" "$qa" || { echo "qa has no $tier tier definition"; exit 1; }
  done
  # the load-bearing invariant: rigor scales breadth, never the honesty floor
  grep -q 'identical in all three rigor tiers' "$qa" \
    || { echo "qa no longer pins the floor as tier-independent"; exit 1; }

  pr="$SK/create-pr/SKILL.md"
  grep -q 'bbs autopilot git-flow' "$pr" || { echo "create-pr does not read the derived policy"; exit 1; }
  awk '/land: none/{f=1} f&&/STATUS: BLOCKED/{found=1} END{exit !found}' "$pr" \
    || { echo "create-pr does not BLOCK under land: none"; exit 1; }

  rv="$SK/review-pr/SKILL.md"
  grep -q 'BBS_REVIEW_EFFORT' "$rv" || { echo "review-pr does not fall back to the repo's effort"; exit 1; }

  sp="$SK/setup-project/SKILL.md"
  grep -q 'profile' "$sp" || { echo "setup-project does not write a profile"; exit 1; }
  grep -q 'never ask about `mode`/`land`/`push`/rigor directly' "$sp" \
    || { echo "setup-project no longer asks exactly one question"; exit 1; }
) && ok "skills-consume-the-derived-policy" || fail "skills-consume-the-derived-policy"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
