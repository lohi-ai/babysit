#!/usr/bin/env bash
# tests/test_bbs_ticket_switch.sh — coverage for bin/bbs-ticket § switch.
#
# switch <ticket>... = reset-base + merge each named ticket's branch into the
# primary checkout: the fast QA hop that points the shared test surface at
# exactly base + the named tickets, runnable from the primary (no cd into
# worktrees).
#
# Scenarios:
#   switch-single            switch A → primary has A's file, SERVING=A
#   switch-swaps-tickets     switch A then switch B → primary has B, not A;
#                            A's worktree + branch survive
#   switch-multiple          switch A B → both files on the primary
#   switch-unknown-ticket    unknown id → BLOCKED exit 2, primary untouched
#   switch-refuses-dirty-primary
#                            uncommitted changes on the test surface → BLOCKED
#                            before anything moves; dirty file survives
#   switch-refuses-off-base-primary
#                            primary sitting on feat/X (branch-mode in-place
#                            ticket) → BLOCKED, checkout untouched
#   switch-conflict          A and B touch the same file → BLOCKED naming the
#                            conflicting merge, no half-merged tree

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

# ── switch-single ─────────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" switch "$TK_A" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "switch failed rc=$rc: $(cat "$T/err")"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing on primary after switch A"; exit 1; }
  [ ! -f b.txt ] || { echo "b.txt unexpectedly on primary"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVING=$TK_A$" \
    || { echo "expected SERVING=$TK_A; out: $out"; exit 1; }
  [ "$(git branch --show-current)" = "main" ] \
    || { echo "primary moved off main"; exit 1; }
) && ok "switch-single" || fail "switch-single"
rm -rf "$T"

# ── switch-swaps-tickets ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  "$BBS_TICKET_BIN" switch "$TK_A" >/dev/null 2>&1 || { echo "switch A failed"; exit 1; }
  "$BBS_TICKET_BIN" switch "$TK_B" >/dev/null 2>"$T/err" || {
    echo "switch B failed: $(cat "$T/err")"; exit 1; }
  [ -f b.txt ] || { echo "b.txt missing after switch B"; exit 1; }
  [ ! -f a.txt ] || { echo "a.txt still on primary after switching to B"; exit 1; }
  # Ticket A survives the swap: worktree file + branch intact.
  [ -f "$WT_A/a.txt" ] || { echo "A's worktree lost a.txt"; exit 1; }
  git -C "$WT_A" rev-parse --verify -q "$(git -C "$WT_A" branch --show-current)" >/dev/null \
    || { echo "A's branch gone"; exit 1; }
) && ok "switch-swaps-tickets" || fail "switch-swaps-tickets"
rm -rf "$T"

# ── switch-multiple ───────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" switch "$TK_A" "$TK_B" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "switch A B failed rc=$rc: $(cat "$T/err")"; exit 1; }
  [ -f a.txt ] && [ -f b.txt ] || { echo "expected both a.txt and b.txt"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVING=$TK_A,$TK_B$" \
    || { echo "expected SERVING=$TK_A,$TK_B; out: $out"; exit 1; }
) && ok "switch-multiple" || fail "switch-multiple"
rm -rf "$T"

# ── switch-unknown-ticket ─────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  "$BBS_TICKET_BIN" switch "$TK_A" >/dev/null 2>&1 || { echo "switch A failed"; exit 1; }
  pre="$(git rev-parse HEAD)"

  "$BBS_TICKET_BIN" switch bs-nope0000 >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on unknown ticket, got $rc"; exit 1; }
  grep -q "no local branch matches" "$T/err" \
    || { echo "expected unknown-ticket reason: $(cat "$T/err")"; exit 1; }
  # Pre-validation means the surface was not touched: A still served.
  [ "$(git rev-parse HEAD)" = "$pre" ] || { echo "primary changed despite BLOCK"; exit 1; }
  [ -f a.txt ] || { echo "a.txt lost despite BLOCK"; exit 1; }
) && ok "switch-unknown-ticket" || fail "switch-unknown-ticket"
rm -rf "$T"

# ── switch-refuses-dirty-primary ──────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  echo "uncommitted surface work" > dirty.txt
  pre="$(git rev-parse HEAD)"

  "$BBS_TICKET_BIN" switch "$TK_A" >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on dirty surface, got $rc"; exit 1; }
  grep -q "uncommitted changes" "$T/err" \
    || { echo "expected dirty-tree reason: $(cat "$T/err")"; exit 1; }
  [ -f dirty.txt ] || { echo "dirty.txt destroyed despite BLOCK"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$pre" ] || { echo "HEAD moved despite BLOCK"; exit 1; }
  [ ! -f a.txt ] || { echo "ticket A merged despite BLOCK"; exit 1; }
) && ok "switch-refuses-dirty-primary" || fail "switch-refuses-dirty-primary"
rm -rf "$T"

# ── switch-refuses-off-base-primary ───────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  git checkout -q -b feat/manual-work

  "$BBS_TICKET_BIN" switch "$TK_A" >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 off base, got $rc"; exit 1; }
  grep -q "not base" "$T/err" || { echo "expected off-base reason: $(cat "$T/err")"; exit 1; }
  [ "$(git branch --show-current)" = "feat/manual-work" ] \
    || { echo "checkout moved despite BLOCK"; exit 1; }
) && ok "switch-refuses-off-base-primary" || fail "switch-refuses-off-base-primary"
rm -rf "$T"

# ── switch-conflict ───────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  # Make A and B collide on the same file.
  ( cd "$WT_A" && echo "A version" > clash.txt && git add clash.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m "a clash" )
  ( cd "$WT_B" && echo "B version" > clash.txt && git add clash.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m "b clash" )

  "$BBS_TICKET_BIN" switch "$TK_A" "$TK_B" >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on conflict, got $rc"; exit 1; }
  grep -q "merge conflict" "$T/err" || { echo "expected conflict reason: $(cat "$T/err")"; exit 1; }
  # No half-merged state left behind: tree clean, A landed, B's merge aborted.
  [ -z "$(git status --porcelain)" ] || { echo "primary left dirty after conflict"; exit 1; }
  [ -f a.txt ] || { echo "A's merge should have landed before the conflict"; exit 1; }
  [ ! -f b.txt ] || { echo "B's merge should have aborted"; exit 1; }
) && ok "switch-conflict" || fail "switch-conflict"
rm -rf "$T"

# ── switch-persists-serving ───────────────────────────────────────────
# switch writes <gitdir>/bbs-serving with set semantics: exactly the named
# tickets, replacing whatever was there (board reads this file).
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  GD="$(git rev-parse --absolute-git-dir)"

  "$BBS_TICKET_BIN" switch "$TK_A" >/dev/null 2>&1 || { echo "switch A failed"; exit 1; }
  [ "$(cat "$GD/bbs-serving")" = "$TK_A" ] || { echo "expected serving=$TK_A: $(cat "$GD/bbs-serving")"; exit 1; }
  "$BBS_TICKET_BIN" switch "$TK_B" >/dev/null 2>&1 || { echo "switch B failed"; exit 1; }
  [ "$(cat "$GD/bbs-serving")" = "$TK_B" ] || { echo "set semantics: A should be replaced"; exit 1; }
  "$BBS_TICKET_BIN" switch "$TK_A" "$TK_B" >/dev/null 2>&1 || { echo "switch A B failed"; exit 1; }
  [ "$(cat "$GD/bbs-serving")" = "$TK_A,$TK_B" ] || { echo "expected $TK_A,$TK_B: $(cat "$GD/bbs-serving")"; exit 1; }
  "$BBS_TICKET_BIN" reset-base >/dev/null 2>&1 || { echo "reset-base failed"; exit 1; }
  [ -z "$(cat "$GD/bbs-serving")" ] || { echo "reset-base should clear serving"; exit 1; }
) && ok "switch-persists-serving" || fail "switch-persists-serving"
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
