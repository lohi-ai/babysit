#!/usr/bin/env bash
# tests/test_autopilot_checkpoint_refresh.sh — `bbs-autopilot checkpoint --refresh`.
#
# The clean-handoff audit (bin/hooks/clean-handoff-check) fires on every run
# whose last commit landed after the checkpoint was written — because commits
# happen past step boundaries (QA fix, review fix, final commit) while the
# checkpoint is stamped at the boundary. `--refresh` re-stamps the existing
# checkpoint so its mtime + head_sha move past the commit, WITHOUT counting as
# a new iteration. This test pins that contract:
#   1. mtime advances past the pre-refresh value (kills the staleness class);
#   2. head_sha updates to current HEAD;
#   3. step / status / workflow and iteration_count are preserved (not a step).

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_AUTOPILOT="$SCRIPT_DIR/bin/bbs-autopilot"
[ -x "$BBS_AUTOPILOT" ] || { echo "FAIL: $BBS_AUTOPILOT not executable" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "SKIP: jq not installed"; exit 0; }

PASS=0
FAIL=0
FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

mtime() { stat -f %m "$1" 2>/dev/null || stat -c %Y "$1" 2>/dev/null; }

# ── refresh re-stamps mtime + head_sha, preserves the step ──────────
# bbs-slug (which bbs-autopilot evals for its project home) keys off $HOME, so
# overriding HOME is what makes the state dir hermetic. Commits use `git -c`
# so the missing HOME gitconfig doesn't matter.
T="$(mktemp -d)"
(
  export HOME="$T"
  git init -q "$T/repo"; cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m c1
  SHA1="$(git rev-parse HEAD)"

  CP="$T/.babysit/projects/repo/tickets/tkt/checkpoint.json"

  "$BBS_AUTOPILOT" checkpoint --ticket tkt --workflow builder --step implement --status in_progress --note n1 >/dev/null
  [ -f "$CP" ] || { echo "checkpoint not written"; exit 1; }
  [ "$(jq -r .head_sha "$CP")" = "$SHA1" ] || { echo "initial head_sha != c1"; exit 1; }
  [ "$(jq -r .iteration_count "$CP")" = "1" ] || { echo "initial iteration != 1"; exit 1; }
  M0="$(mtime "$CP")"

  # A commit lands past the checkpoint (the exact case the audit flags).
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m c2
  SHA2="$(git rev-parse HEAD)"
  sleep 1   # guarantee a wall-clock tick so the mtime advance is observable

  "$BBS_AUTOPILOT" checkpoint --refresh --ticket tkt >/dev/null
  M1="$(mtime "$CP")"

  [ "$M1" -gt "$M0" ]                              || { echo "mtime did not advance ($M0 -> $M1)"; exit 1; }
  [ "$(jq -r .head_sha "$CP")" = "$SHA2" ]         || { echo "head_sha not updated to c2"; exit 1; }
  [ "$(jq -r .step "$CP")" = "implement" ]         || { echo "step not preserved"; exit 1; }
  [ "$(jq -r .status "$CP")" = "in_progress" ]     || { echo "status not preserved"; exit 1; }
  [ "$(jq -r .workflow "$CP")" = "builder" ]       || { echo "workflow not preserved"; exit 1; }
  [ "$(jq -r .note "$CP")" = "n1" ]                || { echo "note not preserved"; exit 1; }
  [ "$(jq -r .iteration_count "$CP")" = "1" ]      || { echo "iteration_count bumped (should stay 1)"; exit 1; }
) && ok "refresh-restamps-and-preserves-step" || fail "refresh-restamps-and-preserves-step"
rm -rf "$T"

# ── refresh with no ticket state fails loudly ───────────────────────
T="$(mktemp -d)"
(
  export HOME="$T"
  git init -q "$T/repo"; cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m c1
  "$BBS_AUTOPILOT" checkpoint --refresh --ticket ghost 2>/dev/null && { echo "refresh succeeded with no checkpoint"; exit 1; }
  exit 0
) && ok "refresh-no-checkpoint-errors" || fail "refresh-no-checkpoint-errors"
rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d checks\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m  %d/%d checks failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
