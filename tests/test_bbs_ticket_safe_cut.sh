#!/usr/bin/env bash
# tests/test_bbs_ticket_safe_cut.sh — coverage for the safe-cut gate in
# bin/bbs-ticket § ensure.
#
# Cutting a ticket branch in place is only safe from a clean checkout of the
# base branch. Anywhere else (another ticket's branch, or a dirty tree) the
# new branch would fork from — and drag along — work-in-progress: the classic
# failure is "started feature B while sitting on feat/a, lost feat/a's
# uncommitted code". Unsafe cuts must divert to a worktree off base and leave
# the invoking checkout untouched.
#
# Scenarios:
#   in-place-on-clean-base       on main + clean → checkout -b, no WORKTREE=
#   worktree-on-feature-branch   on feat/a + dirty → WORKTREE= printed, feat/a
#                                and its dirty file untouched, worktree branch
#                                forks from main (not feat/a), manifest records
#                                the worktree path
#   worktree-on-dirty-base       on main + dirty → worktree divert
#   in-place-cuts-from-origin    local main ahead of origin (integration
#                                merge) → in-place cut forks from origin/main,
#                                not local main; local main untouched
#   worktree-cuts-from-origin    same, via the worktree divert: worktree HEAD
#                                = origin/main, carries no merged-ticket files
#   developer-no-confirm-for-worktree
#                                developer role, unsafe cut → no exit 3, the
#                                divert proceeds (it never moves the checkout)
#   developer-confirm-in-place   developer role, clean base → exit 3 NEEDS_CONFIRM

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Fresh single-repo fixture: main pushed to a bare origin, cwd = the clone.
# Prints the repo path; caller cd's there inside a subshell.
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

# ── in-place-on-clean-base ────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-a --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^CREATED=1$' \
    || { echo "expected CREATED=1; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    && { echo "unexpected WORKTREE= on a clean base cut; out: $out"; exit 1; }
  branch="$(git branch --show-current)"
  case "$branch" in
    feat/bs-*_feat-a) : ;;
    *) echo "expected in-place cut to feat/bs-*_feat-a, got '$branch'"; exit 1 ;;
  esac
) && ok "in-place-on-clean-base" || fail "in-place-on-clean-base"
rm -rf "$T"

# ── worktree-on-feature-branch ────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  # Simulate in-progress feature A: non-ticket branch + a committed marker +
  # uncommitted work.
  git checkout -q -b feat/a
  echo "feat-a work" > a.txt
  git add a.txt && git -c user.email=t@t -c user.name=t commit -q -m "feat a wip"
  echo "uncommitted" > dirty.txt

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-b --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  wt="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$wt" ] || { echo "expected WORKTREE= in output; out: $out"; exit 1; }
  [ -d "$wt" ] || { echo "worktree dir missing: $wt"; exit 1; }
  case "$wt" in
    */repo/.babysit/worktrees/bs-*_feat-b) : ;;
    *) echo "expected worktree under <repo>/.babysit/worktrees/<ticket>_<slug>, got '$wt'"; exit 1 ;;
  esac
  # The in-repo worktree must be git-excluded — otherwise it trips the
  # gate's dirty-check on the next ticket.
  git status --porcelain | grep -q '\.babysit/' \
    && { echo "worktree dir leaks into git status"; exit 1; }

  # Invoking checkout untouched: still on feat/a, dirty file intact.
  [ "$(git branch --show-current)" = "feat/a" ] \
    || { echo "checkout moved off feat/a: $(git branch --show-current)"; exit 1; }
  [ -f dirty.txt ] || { echo "dirty.txt lost from feat/a checkout"; exit 1; }

  # Worktree branch forks from main, not feat/a.
  wt_branch="$(git -C "$wt" branch --show-current)"
  case "$wt_branch" in
    feat/bs-*_feat-b) : ;;
    *) echo "expected worktree on feat/bs-*_feat-b, got '$wt_branch'"; exit 1 ;;
  esac
  [ "$(git -C "$wt" rev-parse HEAD)" = "$(git rev-parse main)" ] \
    || { echo "worktree HEAD is not main (forked from feat/a?)"; exit 1; }
  [ ! -e "$wt/a.txt" ] || { echo "worktree carries feat/a's a.txt"; exit 1; }

  # Manifest records the worktree path.
  ticket="$(printf '%s\n' "$out" | sed -n 's|^TICKET=||p')"
  th="$(printf '%s\n' "$out" | sed -n 's|^TICKET_HOME=||p')"
  grep -q "worktree: $wt" "$th/manifest.yaml" \
    || { echo "manifest.yaml missing worktree path for $ticket"; cat "$th/manifest.yaml"; exit 1; }
) && ok "worktree-on-feature-branch" || fail "worktree-on-feature-branch"
rm -rf "$T"

# ── worktree-on-dirty-base ────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  echo "uncommitted" > dirty.txt

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-c --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  wt="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$wt" ] || { echo "expected WORKTREE= on dirty base; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "checkout moved off main"; exit 1; }
  [ -f dirty.txt ] || { echo "dirty.txt lost"; exit 1; }
) && ok "worktree-on-dirty-base" || fail "worktree-on-dirty-base"
rm -rf "$T"

# ── in-place-cuts-from-origin ─────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  # Simulate a landed ticket integrated into local main but not pushed:
  # local main is ahead of origin/main by one merge.
  git checkout -q -b feat/landed
  echo "landed" > landed.txt
  git add landed.txt && git -c user.email=t@t -c user.name=t commit -q -m "landed ticket"
  git checkout -q main
  git -c user.email=t@t -c user.name=t merge -q --no-ff feat/landed -m "integrate landed"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-f --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    && { echo "unexpected WORKTREE= on a clean base cut; out: $out"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] \
    || { echo "in-place cut forked from local main, not origin/main"; exit 1; }
  [ ! -e landed.txt ] || { echo "new branch carries the integrated ticket's landed.txt"; exit 1; }
  # No upstream: tracking origin/main would break plain `git push` later.
  git config "branch.$(git branch --show-current).merge" >/dev/null 2>&1 \
    && { echo "new branch has an upstream configured (should be --no-track)"; exit 1; }
  # Local main itself keeps its integration merge.
  [ "$(git rev-parse main)" != "$(git rev-parse origin/main)" ] \
    || { echo "local main lost its integration merge"; exit 1; }
) && ok "in-place-cuts-from-origin" || fail "in-place-cuts-from-origin"
rm -rf "$T"

# ── worktree-cuts-from-origin ─────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"
  cd "$T/repo"
  git checkout -q -b feat/landed
  echo "landed" > landed.txt
  git add landed.txt && git -c user.email=t@t -c user.name=t commit -q -m "landed ticket"
  git checkout -q main
  git -c user.email=t@t -c user.name=t merge -q --no-ff feat/landed -m "integrate landed"
  git checkout -q -b feat/next

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-g --type feat 2>"$T/err")" || {
    echo "ensure failed: $(cat "$T/err")"; exit 1; }
  wt="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$wt" ] || { echo "expected WORKTREE= in output; out: $out"; exit 1; }
  [ "$(git -C "$wt" rev-parse HEAD)" = "$(git rev-parse origin/main)" ] \
    || { echo "worktree forked from local main, not origin/main"; exit 1; }
  [ ! -e "$wt/landed.txt" ] || { echo "worktree carries the integrated ticket's landed.txt"; exit 1; }
) && ok "worktree-cuts-from-origin" || fail "worktree-cuts-from-origin"
rm -rf "$T"

# ── developer-no-confirm-for-worktree ─────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  unset AGENT_ROLE GT_ROLE
  build_repo "$T"
  cd "$T/repo"
  git checkout -q -b feat/a

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-d --type feat 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "expected rc=0 (worktree divert, no confirm), got $rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^WORKTREE=' \
    || { echo "expected WORKTREE=; out: $out"; exit 1; }
) && ok "developer-no-confirm-for-worktree" || fail "developer-no-confirm-for-worktree"
rm -rf "$T"

# ── developer-confirm-in-place ────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  unset AGENT_ROLE GT_ROLE
  build_repo "$T"
  cd "$T/repo"

  out="$("$BBS_TICKET_BIN" ensure --slug-hint feat-e --type feat 2>&1)"; rc=$?
  [ "$rc" -eq 3 ] || { echo "expected rc=3 NEEDS_CONFIRM, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q 'NEEDS_CONFIRM' \
    || { echo "expected NEEDS_CONFIRM; got: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "checkout moved despite NEEDS_CONFIRM"; exit 1; }
) && ok "developer-confirm-in-place" || fail "developer-confirm-in-place"
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
