#!/usr/bin/env bash
# tests/test_bbs_ticket_git_flow_mode.sh — coverage for the git-flow mode
# (trunk|branch|worktree) in bin/bbs-ticket § ensure, and for reset-base.
#
# The mode decides where a new ticket's branch lives:
#   trunk     no cut; identity rides BABYSIT_TICKET
#   branch    safe-cut gate (covered by test_bbs_ticket_safe_cut.sh)
#   worktree  always divert — primary checkout stays pinned to base
# reset-base snaps the primary's base branch back to origin/<base> after
# tickets land upstream, refusing when real work would be lost.
#
# Scenarios:
#   worktree-mode-diverts-on-clean-base   mode: worktree + clean main → WORKTREE=,
#                                         checkout stays on main, no NEEDS_CONFIRM
#                                         even in developer role
#   trunk-mode-no-cut                     mode: trunk → no cut, export line
#   mode-flag-overrides-config            mode: trunk in config + --mode worktree → divert
#   legacy-ticket-branch-optional         ticket_branch: optional maps to trunk
#   invalid-config-mode                   mode: bogus → exit 2
#   reset-base-after-merge-base           land a ticket via merge-base (fast-forward),
#                                         reset-base → RESET=1, main == origin/main,
#                                         ticket branch + worktree intact
#   reset-base-refuses-stray-commit       direct commit on main, no branch holds it → BLOCKED
#   reset-base-refuses-dirty              dirty primary → BLOCKED

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Fresh single-repo fixture: main pushed to a bare origin, cwd = the clone.
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

# Commit a .babysit/git-flow.yaml with the given content and push main.
set_git_flow() {
  mkdir -p .babysit
  printf '%s\n' "$1" > .babysit/git-flow.yaml
  git add .babysit/git-flow.yaml
  git -c user.email=t@t -c user.name=t commit -q -m "git-flow config"
  git push -q origin main
}

# ── worktree-mode-diverts-on-clean-base ───────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  unset AGENT_ROLE GT_ROLE   # developer role: divert must not NEEDS_CONFIRM
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "mode: worktree"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-a --type feat 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "expected rc=0, got $rc: $(cat "$T/err")"; exit 1; }
  wt="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$wt" ] || { echo "expected WORKTREE= on clean base under mode: worktree; out: $out"; exit 1; }
  [ -d "$wt" ] || { echo "worktree dir missing: $wt"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "primary checkout moved off main: $(git branch --show-current)"; exit 1; }
  wt_branch="$(git -C "$wt" branch --show-current)"
  case "$wt_branch" in
    feat/bs-*_feat-a) : ;;
    *) echo "expected worktree on feat/bs-*_feat-a, got '$wt_branch'"; exit 1 ;;
  esac
  [ "$(git -C "$wt" rev-parse HEAD)" = "$(git rev-parse main)" ] \
    || { echo "worktree HEAD is not main"; exit 1; }
) && ok "worktree-mode-diverts-on-clean-base" || fail "worktree-mode-diverts-on-clean-base"
rm -rf "$T"

# ── trunk-mode-no-cut ─────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "mode: trunk"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-b --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    && { echo "unexpected WORKTREE= under mode: trunk; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q '^export BABYSIT_TICKET=' \
    || { echo "expected export BABYSIT_TICKET= line; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "trunk mode cut a branch: $(git branch --show-current)"; exit 1; }
) && ok "trunk-mode-no-cut" || fail "trunk-mode-no-cut"
rm -rf "$T"

# ── inline-comment-on-mode ────────────────────────────────────────────
# An inline `# ...` comment on the mode line must be stripped before the
# value is validated (otherwise mode resolves to garbage → exit 2).
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "mode: trunk   # trunk | branch | worktree — see references/git-flow.md"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-ic --type feat 2>"$T/err")" || {
    echo "ensure failed (inline comment not stripped?): $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    && { echo "unexpected WORKTREE= — mode: trunk not honored with inline comment; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "inline-comment mode cut a branch: $(git branch --show-current)"; exit 1; }
) && ok "inline-comment-on-mode" || fail "inline-comment-on-mode"
rm -rf "$T"

# ── mode-flag-overrides-config ────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "mode: trunk"

  out="$("$BBS_TICKET_BIN" ensure --mode worktree --slug-hint feat-c --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    || { echo "expected --mode worktree to override mode: trunk; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "primary checkout moved off main"; exit 1; }
) && ok "mode-flag-overrides-config" || fail "mode-flag-overrides-config"
rm -rf "$T"

# ── legacy-ticket-branch-optional ─────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "ticket_branch: optional"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-d --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^export BABYSIT_TICKET=' \
    || { echo "expected legacy ticket_branch: optional to behave as trunk; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "legacy optional cut a branch"; exit 1; }
) && ok "legacy-ticket-branch-optional" || fail "legacy-ticket-branch-optional"
rm -rf "$T"

# ── invalid-config-mode ───────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "mode: bogus"

  "$BBS_TICKET_BIN" ensure --slug-hint feat-e --type feat >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on mode: bogus, got $rc"; exit 1; }
  grep -q "invalid mode" "$T/err" || { echo "expected invalid-mode message: $(cat "$T/err")"; exit 1; }
) && ok "invalid-config-mode" || fail "invalid-config-mode"
rm -rf "$T"

# ── reset-base-after-merge-base ───────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  set_git_flow "mode: worktree"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-f --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  wt="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$wt" ] || { echo "expected WORKTREE=; out: $out"; exit 1; }
  (
    cd "$wt"
    echo "ticket work" > f.txt
    git add f.txt && git -c user.email=t@t -c user.name=t commit -q -m "ticket work"
    "$BBS_TICKET_BIN" merge-base >/dev/null 2>"$T/mb-err" \
      || { echo "merge-base failed: $(cat "$T/mb-err")"; exit 1; }
  ) || exit 1
  [ -f f.txt ] || { echo "merge-base did not land f.txt on the primary"; exit 1; }

  rb="$("$BBS_TICKET_BIN" reset-base 2>"$T/rb-err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "reset-base failed rc=$rc: $(cat "$T/rb-err")"; exit 1; }
  printf '%s\n' "$rb" | grep -q '^RESET=1$' \
    || { echo "expected RESET=1; out: $rb"; exit 1; }
  [ "$(git rev-parse main)" = "$(git rev-parse origin/main)" ] \
    || { echo "main != origin/main after reset-base"; exit 1; }
  [ ! -f f.txt ] || { echo "f.txt still on primary after reset"; exit 1; }
  # The ticket branch and worktree survive untouched.
  [ -f "$wt/f.txt" ] || { echo "worktree lost f.txt"; exit 1; }
) && ok "reset-base-after-merge-base" || fail "reset-base-after-merge-base"
rm -rf "$T"

# ── reset-base-refuses-stray-commit ───────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  echo "direct work" > direct.txt
  git add direct.txt && git -c user.email=t@t -c user.name=t commit -q -m "committed directly on main"

  "$BBS_TICKET_BIN" reset-base >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc"; exit 1; }
  grep -q "BLOCKED" "$T/err" || { echo "expected BLOCKED: $(cat "$T/err")"; exit 1; }
  git rev-parse --verify -q HEAD >/dev/null || { echo "history damaged"; exit 1; }
  [ -f direct.txt ] || { echo "direct.txt lost despite BLOCK"; exit 1; }
) && ok "reset-base-refuses-stray-commit" || fail "reset-base-refuses-stray-commit"
rm -rf "$T"

# ── reset-base-refuses-dirty ──────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  echo "uncommitted" > dirty.txt

  "$BBS_TICKET_BIN" reset-base >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on dirty primary, got $rc"; exit 1; }
  grep -q "uncommitted changes" "$T/err" || { echo "expected dirty-tree reason: $(cat "$T/err")"; exit 1; }
  [ -f dirty.txt ] || { echo "dirty.txt lost despite BLOCK"; exit 1; }
) && ok "reset-base-refuses-dirty" || fail "reset-base-refuses-dirty"
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
