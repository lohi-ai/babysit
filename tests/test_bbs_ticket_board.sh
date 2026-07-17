#!/usr/bin/env bash
# tests/test_bbs_ticket_board.sh — coverage for bin/bbs-ticket § board.
#
# board = read-only aggregated view: every ticket joined with its verdicts,
# branch, session, PR pointer, and siblings, plus a qa-lease + serving footer.
# Zero mutation — running board must never change any state.
#
# Scenarios:
#   board-rows-and-verdicts   both tickets listed; A's qa verdict DONE, B none;
#                             branch column from manifest; footer FREE/(base only)
#   board-lease-and-serving   lease held by A + switch A → footer shows owner
#                             and SERVING=A; switch B → SERVING=B;
#                             merge-base A then B (no reset) → SERVING=A,B
#   board-status-filter       done ticket hidden by default, shown with --all
#   board-sibling-unresolved  sibling with unset RELATED_* env → "path
#                             unresolved" sub-row, main row intact, rc 0

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Fixture: bare origin + clone with mode: worktree committed and pushed, then
# two worktree tickets A and B, each with one committed file. Sets globals
# WT_A/WT_B (worktree paths) and TK_A/TK_B (ticket ids). Runs in $1/repo.
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

# ── board-rows-and-verdicts ───────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-verdict --skill qa \
    --body "STATUS: DONE" >/dev/null 2>&1 || { echo "set-verdict failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" board 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "board failed rc=$rc: $(cat "$T/err")"; exit 1; }
  row_a="$(printf '%s\n' "$out" | grep "^$TK_A ")" || { echo "A row missing: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^$TK_B " || { echo "B row missing: $out"; exit 1; }
  printf '%s' "$row_a" | grep -q "DONE" || { echo "A's qa verdict not shown: $row_a"; exit 1; }
  printf '%s' "$row_a" | grep -q "feat/${TK_A}_tick-a" || { echo "A's branch not shown: $row_a"; exit 1; }
  # PUSHED renders the manifest bool through Python's repr — "False", not "false".
  [ "$(printf '%s' "$row_a" | awk '{print $5}')" = "False" ] \
    || { echo "A's PUSHED column should be False: $row_a"; exit 1; }
  printf '%s\n' "$out" | grep -q "^QA-LEASE: FREE$" || { echo "expected free lease: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVING: (base only)$" || { echo "expected base-only serving: $out"; exit 1; }
) && ok "board-rows-and-verdicts" || fail "board-rows-and-verdicts"
rm -rf "$T"

# ── board-lease-and-serving ───────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  "$BBS_TICKET_BIN" qa-lease acquire --ticket "$TK_A" >/dev/null 2>&1 || { echo "acquire failed"; exit 1; }
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" switch "$TK_A" >/dev/null 2>&1 || { echo "switch A failed"; exit 1; }
  out="$("$BBS_TICKET_BIN" board)"
  printf '%s\n' "$out" | grep -q "^QA-LEASE: $TK_A " || { echo "lease owner not shown: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVING: $TK_A$" || { echo "expected SERVING: $TK_A: $out"; exit 1; }

  # switch is set-semantics: B replaces A.
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" qa-lease release >/dev/null 2>&1
  "$BBS_TICKET_BIN" switch "$TK_B" >/dev/null 2>&1 || { echo "switch B failed"; exit 1; }
  "$BBS_TICKET_BIN" board | grep -q "^SERVING: $TK_B$" \
    || { echo "expected SERVING: $TK_B after switch B"; exit 1; }

  # merge-base appends: A lands on top of B without a reset.
  ( cd "$WT_A" && "$BBS_TICKET_BIN" merge-base >/dev/null 2>&1 ) || { echo "merge-base A failed"; exit 1; }
  "$BBS_TICKET_BIN" board | grep -q "^SERVING: $TK_B,$TK_A$" \
    || { echo "expected SERVING: $TK_B,$TK_A after merge-base"; exit 1; }
) && ok "board-lease-and-serving" || fail "board-lease-and-serving"
rm -rf "$T"

# ── board-status-filter ───────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-status done >/dev/null 2>&1 \
    || { echo "set-status failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" board)"
  printf '%s\n' "$out" | grep -q "^$TK_A " && { echo "done ticket shown by default: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^$TK_B " || { echo "B row missing: $out"; exit 1; }
  "$BBS_TICKET_BIN" board --all | grep -q "^$TK_A " \
    || { echo "done ticket missing under --all"; exit 1; }
) && ok "board-status-filter" || fail "board-status-filter"
rm -rf "$T"

# ── board-sibling-unresolved ──────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-sibling \
    --role be --repo ghost-api --ticket bs-ghost000 >/dev/null 2>&1 || { echo "set-sibling failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" board 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "board failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^$TK_A " || { echo "main row lost: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "be:bs-ghost000 (ghost-api) — path unresolved" \
    || { echo "expected unresolved sub-row: $out"; exit 1; }
) && ok "board-sibling-unresolved" || fail "board-sibling-unresolved"
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
