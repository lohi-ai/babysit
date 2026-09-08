#!/usr/bin/env bash
# tests/test_bbs_ticket_land.sh — coverage for `bbs ticket land`.
#
# land <ticket> is the opposite of switch: switch resets the base and treats the
# composition as scratch, land merges into the LOCAL base and KEEPS the merge.
# It is what `finish: land` calls when a worker finishes, so its guards are
# the whole feature — an unverified or half-landed batch is the failure that
# matters, not a missing convenience.
#
# Scenarios:
#   land-merges-and-keeps          qa + review-pr DONE → --no-ff merge on base,
#                                  file present, base ahead of origin, LANDED=1
#   land-idempotent                re-run → LANDED=0 … (already on main), no new
#                                  commit minted
#   land-blocks-unverified         missing qa verdict → rc 2 BLOCKED, base
#                                  untouched (no commit, no file)
#   land-blocks-batch-atomically   A finished, B not → rc 2 and NEITHER lands:
#                                  the whole batch is checked before any merge
#   land-blocks-off-base           primary on another branch → rc 2 BLOCKED
#   land-blocks-dirty-primary      uncommitted changes → rc 2 BLOCKED, preserved
#   land-blocks-on-foreign-lease   another ticket holds the qa-lease → rc 2
#   land-multi                     A + B both finished → both merges on base
#   land-survives-reset-base-guard reset-base after a land does not BLOCK on
#                                  stray commits (the ticket branches hold them)

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Fixture: bare origin + worktree-mode clone, two worktree tickets each with one
# committed file. Sets WT_A/WT_B, TK_A/TK_B. Leaves cwd in $1/repo.
build_two_tickets() {
  local t="$1"
  git init -q --bare "$t/remote.git"
  git init -q "$t/repo"
  (
    cd "$t/repo"
    git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
    git branch -M main
    git remote add origin "$t/remote.git"
    mkdir -p .babysit
    echo "mode: worktree" > .babysit/git-flow.yaml
    git add .babysit/git-flow.yaml
    git -c user.email=t@t -c user.name=t commit -q -m "git-flow config"
    git push -q origin main
  )
  cd "$t/repo"
  local out
  out="$("$BBS_TICKET_BIN" ensure --slug-hint tick-a --type feat 2>/dev/null)" || return 1
  TK_A="$(printf '%s\n' "$out" | sed -n 's|^TICKET=||p')"
  WT_A="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  out="$("$BBS_TICKET_BIN" ensure --slug-hint tick-b --type feat 2>/dev/null)" || return 1
  TK_B="$(printf '%s\n' "$out" | sed -n 's|^TICKET=||p')"
  WT_B="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$WT_A" ] && [ -n "$WT_B" ] || return 1
  ( cd "$WT_A" && echo "work a" > a.txt && git add a.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m "ticket a" ) || return 1
  ( cd "$WT_B" && echo "work b" > b.txt && git add b.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m "ticket b" ) || return 1
}

# finish <ticket> writes the two verdicts land gates on.
finish() {
  BABYSIT_TICKET="$1" "$BBS_TICKET_BIN" set-verdict --skill qa --body "STATUS: DONE" >/dev/null
  BABYSIT_TICKET="$1" "$BBS_TICKET_BIN" set-verdict --skill review-pr --body "STATUS: DONE" >/dev/null
}

# ── land-merges-and-keeps ─────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"

  before="$(git rev-parse HEAD)"
  out="$("$BBS_TICKET_BIN" land "$TK_A" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "land failed rc=$rc: $(cat "$T/err")"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing on base after land"; exit 1; }
  printf '%s\n' "$out" | grep -q "^LANDED=1 $TK_A " \
    || { echo "expected LANDED=1 row; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^LANDED=1 ALREADY=0$" \
    || { echo "expected summary LANDED=1 ALREADY=0; out: $out"; exit 1; }
  # The merge is kept and is a real merge commit (--no-ff), not a fast-forward.
  [ "$(git rev-parse HEAD)" != "$before" ] || { echo "base did not move"; exit 1; }
  [ "$(git rev-list --count --merges "$before"..HEAD)" -eq 1 ] \
    || { echo "expected exactly one merge commit"; exit 1; }
  # Ahead of origin: the whole point is that this outlives the serve/reset loop
  # only once pushed, and land says so on stderr.
  [ "$(git rev-list --count origin/main..main)" -gt 0 ] || { echo "base not ahead of origin"; exit 1; }
  grep -q "push it" "$T/err" || { echo "missing the push note; err: $(cat "$T/err")"; exit 1; }
) && ok "land-merges-and-keeps" || fail "land-merges-and-keeps"
rm -rf "$T"

# ── land-idempotent ───────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"
  "$BBS_TICKET_BIN" land "$TK_A" >/dev/null 2>&1 || { echo "first land failed"; exit 1; }
  after_first="$(git rev-parse HEAD)"

  out="$("$BBS_TICKET_BIN" land "$TK_A" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "re-land failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^LANDED=0 $TK_A .*already on main" \
    || { echo "expected already-landed row; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^LANDED=0 ALREADY=1$" \
    || { echo "expected summary LANDED=0 ALREADY=1; out: $out"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$after_first" ] || { echo "re-land minted a commit"; exit 1; }
) && ok "land-idempotent" || fail "land-idempotent"
rm -rf "$T"

# ── land-blocks-unverified ────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  # review-pr only: qa is the missing half.
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-verdict --skill review-pr --body "STATUS: DONE" >/dev/null

  before="$(git rev-parse HEAD)"
  "$BBS_TICKET_BIN" land "$TK_A" >"$T/out" 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc 2, got $rc"; exit 1; }
  grep -q "STATUS: BLOCKED" "$T/err" || { echo "no BLOCKED; err: $(cat "$T/err")"; exit 1; }
  grep -q "qa verdict is not DONE" "$T/err" || { echo "reason must name qa; err: $(cat "$T/err")"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$before" ] || { echo "base moved on a blocked land"; exit 1; }
  [ ! -f a.txt ] || { echo "a.txt landed despite the block"; exit 1; }
) && ok "land-blocks-unverified" || fail "land-blocks-unverified"
rm -rf "$T"

# ── land-blocks-batch-atomically ──────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"   # B is left unfinished

  before="$(git rev-parse HEAD)"
  "$BBS_TICKET_BIN" land "$TK_A" "$TK_B" >"$T/out" 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc 2, got $rc"; exit 1; }
  # The finished ticket must NOT have landed: every ticket is checked before any
  # merge, or a blocked batch leaves a base nobody asked for.
  [ "$(git rev-parse HEAD)" = "$before" ] || { echo "base moved despite the block"; exit 1; }
  [ ! -f a.txt ] || { echo "A landed before B was checked"; exit 1; }
) && ok "land-blocks-batch-atomically" || fail "land-blocks-batch-atomically"
rm -rf "$T"

# ── land-blocks-off-base ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"
  git checkout -q -b detour

  "$BBS_TICKET_BIN" land "$TK_A" >"$T/out" 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc 2, got $rc"; exit 1; }
  grep -q "is on 'detour', not base 'main'" "$T/err" \
    || { echo "reason must name the branch; err: $(cat "$T/err")"; exit 1; }
  [ ! -f a.txt ] || { echo "landed onto the wrong branch"; exit 1; }
) && ok "land-blocks-off-base" || fail "land-blocks-off-base"
rm -rf "$T"

# ── land-blocks-dirty-primary ─────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"
  echo "scratch" > dirty.txt

  "$BBS_TICKET_BIN" land "$TK_A" >"$T/out" 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc 2, got $rc"; exit 1; }
  grep -q "uncommitted changes" "$T/err" || { echo "err: $(cat "$T/err")"; exit 1; }
  [ -f dirty.txt ] || { echo "land destroyed uncommitted work"; exit 1; }
) && ok "land-blocks-dirty-primary" || fail "land-blocks-dirty-primary"
rm -rf "$T"

# ── land-blocks-on-foreign-lease ──────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"
  # B is mid-QA on the shared surface; landing A would move it under B.
  "$BBS_TICKET_BIN" qa-lease acquire --ticket "$TK_B" >/dev/null 2>&1 || { echo "seed lease failed"; exit 1; }

  before="$(git rev-parse HEAD)"
  "$BBS_TICKET_BIN" land "$TK_A" >"$T/out" 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc 2, got $rc"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$before" ] || { echo "landed through another ticket's lease"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_B$" \
    || { echo "lease owner changed"; exit 1; }
) && ok "land-blocks-on-foreign-lease" || fail "land-blocks-on-foreign-lease"
rm -rf "$T"

# ── land-multi ────────────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"; finish "$TK_B"

  out="$("$BBS_TICKET_BIN" land "$TK_A" "$TK_B" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "land failed rc=$rc: $(cat "$T/err")"; exit 1; }
  [ -f a.txt ] && [ -f b.txt ] || { echo "both tickets should be on base"; exit 1; }
  printf '%s\n' "$out" | grep -q "^LANDED=2 ALREADY=0$" \
    || { echo "expected LANDED=2; out: $out"; exit 1; }
) && ok "land-multi" || fail "land-multi"
rm -rf "$T"

# ── land-survives-reset-base-guard ────────────────────────────────────
# reset-base BLOCKs when base carries commits no other branch holds. A landed
# ticket must not trip it: the ticket branch still holds every commit, and the
# merge itself is excluded as a merge. Otherwise finish: land would wedge the
# serve loop for the rest of the batch.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  finish "$TK_A"
  "$BBS_TICKET_BIN" land "$TK_A" >/dev/null 2>&1 || { echo "land failed"; exit 1; }

  "$BBS_TICKET_BIN" reset-base >"$T/out" 2>"$T/err"; rc=$?
  [ "$rc" -eq 0 ] || { echo "reset-base blocked after a land rc=$rc: $(cat "$T/err")"; exit 1; }
  grep -q "^RESET=1$" "$T/out" || { echo "expected RESET=1; out: $(cat "$T/out")"; exit 1; }
  # And the work is not lost — the ticket branch still has it.
  git rev-parse --verify -q "refs/heads/feat/${TK_A}_tick-a" >/dev/null \
    || { echo "ticket branch gone"; exit 1; }
) && ok "land-survives-reset-base-guard" || fail "land-survives-reset-base-guard"
rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d scenarios\n' "$PASS"
  exit 0
fi
printf '\033[0;31mFAIL\033[0m  %d passed, %d failed: %s\n' "$PASS" "$FAIL" "${FAIL_NAMES[*]}"
exit 1
