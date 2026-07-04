#!/usr/bin/env bash
# tests/test_concurrent_tickets_same_folder.sh — pins the multi-tenant claim:
# two shells in the SAME folder running DIFFERENT BABYSIT_TICKET values
# produce fully isolated per-ticket state.
#
# This is the post-1.18 capability the identity overhaul unlocked. Pre-1.18
# identity was branch-derived, so two shells in the same pwd resolved to the
# same ticket (or the same checkout — even worse). Now identity follows the
# shell's env, and per-ticket files live in per-ticket dirs.
#
# Asserts, with both shells anchored at the same cwd:
#   1. bbs-slug ticket returns each shell's own BABYSIT_TICKET
#   2. bbs-ticket init seeds two different tickets/<id>/index.json dirs
#   3. Per-ticket pointer writes never cross-contaminate
#   4. Two distinct sessions coexist for the two tickets in ~/.babysit/sessions/
#   5. session list shows both with the right ticket attribution

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_SLUG="$SCRIPT_DIR/bin/bbs-slug"
BBS_TICKET="$SCRIPT_DIR/bin/bbs-ticket"
PREAMBLE="$SCRIPT_DIR/.claude/skills/references/preamble.md"
[ -x "$BBS_SLUG" ] && [ -x "$BBS_TICKET" ] && [ -f "$PREAMBLE" ] \
  || { echo "FAIL: missing bins" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

mk_repo() {
  local d="$1" branch="$2"
  git init -q "$d"
  git -C "$d" -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git -C "$d" checkout -q -b "$branch"
}

# Extract the preamble session-writer hook so we can replay it with two
# different BABYSIT_TICKET values — same as test_concurrent_sessions.sh.
HOOK_BLOCK="$(mktemp)"
awk '/^# Session-writer hook/,/^# Config \+ repo state/' "$PREAMBLE" \
  | sed '$d' > "$HOOK_BLOCK"

# ── bbs-slug-resolves-per-shell-ticket ─────────────────────────────────
# Two subshells, same pwd, different BABYSIT_TICKET → each gets its own.
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  mk_repo "$T/r" "feat/bs-shared_demo"

  out_a="$( cd "$T/r" && BABYSIT_TICKET="bs-aaa" "$BBS_SLUG" ticket )"
  out_b="$( cd "$T/r" && BABYSIT_TICKET="bs-bbb" "$BBS_SLUG" ticket )"

  [ "$out_a" = "bs-aaa" ] || { echo "shell A got '$out_a', want bs-aaa"; exit 1; }
  [ "$out_b" = "bs-bbb" ] || { echo "shell B got '$out_b', want bs-bbb"; exit 1; }
) && ok "bbs-slug-resolves-per-shell-ticket" || fail "bbs-slug-resolves-per-shell-ticket"
rm -rf "$T"

# ── per-ticket-dirs-isolated ───────────────────────────────────────────
# Two shells init two tickets from the same cwd; each ticket gets its own
# index.json under ~/.babysit/projects/<slug>/tickets/<id>/.
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  mk_repo "$T/r" "feat/bs-shared_demo"
  cd "$T/r"

  # Shell A — ticket bs-aaa
  ( BABYSIT_TICKET="bs-aaa" "$BBS_TICKET" init >/dev/null 2>&1 ) \
    || { echo "init A failed"; exit 1; }
  TH_A="$( BABYSIT_TICKET="bs-aaa" "$BBS_SLUG" ticket-home )"

  # Shell B — ticket bs-bbb
  ( BABYSIT_TICKET="bs-bbb" "$BBS_TICKET" init >/dev/null 2>&1 ) \
    || { echo "init B failed"; exit 1; }
  TH_B="$( BABYSIT_TICKET="bs-bbb" "$BBS_SLUG" ticket-home )"

  [ "$TH_A" != "$TH_B" ] || { echo "ticket-homes equal: $TH_A"; exit 1; }
  [ -f "$TH_A/index.json" ] || { echo "no index.json at $TH_A"; exit 1; }
  [ -f "$TH_B/index.json" ] || { echo "no index.json at $TH_B"; exit 1; }
  printf '%s' "$TH_A" | grep -q "/bs-aaa$" || { echo "TH_A wrong: $TH_A"; exit 1; }
  printf '%s' "$TH_B" | grep -q "/bs-bbb$" || { echo "TH_B wrong: $TH_B"; exit 1; }
) && ok "per-ticket-dirs-isolated" || fail "per-ticket-dirs-isolated"
rm -rf "$T"

# ── pointer-writes-do-not-cross-contaminate ────────────────────────────
# Writing pointers for ticket A must not appear in ticket B's index.json.
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  mk_repo "$T/r" "feat/bs-shared_demo"
  cd "$T/r"

  ( BABYSIT_TICKET="bs-aaa" "$BBS_TICKET" init >/dev/null 2>&1 )
  ( BABYSIT_TICKET="bs-bbb" "$BBS_TICKET" init >/dev/null 2>&1 )

  # Shell A writes a pointer; shell B writes a different one.
  ( BABYSIT_TICKET="bs-aaa" "$BBS_TICKET" set-pointer ticket_size "L" >/dev/null 2>&1 )
  ( BABYSIT_TICKET="bs-bbb" "$BBS_TICKET" set-pointer ticket_size "S" >/dev/null 2>&1 )

  size_a="$( BABYSIT_TICKET="bs-aaa" "$BBS_TICKET" get-pointer ticket_size 2>/dev/null )"
  size_b="$( BABYSIT_TICKET="bs-bbb" "$BBS_TICKET" get-pointer ticket_size 2>/dev/null )"

  [ "$size_a" = "L" ] || { echo "A's ticket_size: '$size_a', want L"; exit 1; }
  [ "$size_b" = "S" ] || { echo "B's ticket_size: '$size_b', want S"; exit 1; }
) && ok "pointer-writes-do-not-cross-contaminate" || fail "pointer-writes-do-not-cross-contaminate"
rm -rf "$T"

# ── two-tickets-two-sessions-coexist ───────────────────────────────────
# Each shell writes its own session file; both visible in `session list`.
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$HOME/.babysit/sessions"

  ( export BABYSIT_SESSION="sess-A" BABYSIT_TICKET="bs-aaa"
    bash -c ". '$HOOK_BLOCK'" >/dev/null 2>&1 )
  ( export BABYSIT_SESSION="sess-B" BABYSIT_TICKET="bs-bbb"
    bash -c ". '$HOOK_BLOCK'" >/dev/null 2>&1 )

  FA="$HOME/.babysit/sessions/sess-A.yaml"
  FB="$HOME/.babysit/sessions/sess-B.yaml"
  [ -f "$FA" ] && [ -f "$FB" ] || { echo "missing yaml files"; ls -la "$HOME/.babysit/sessions"; exit 1; }
  grep -qx "ticket: bs-aaa" "$FA" || { echo "FA: bad ticket"; cat "$FA"; exit 1; }
  grep -qx "ticket: bs-bbb" "$FB" || { echo "FB: bad ticket"; cat "$FB"; exit 1; }

  BABYSIT_HOME="$HOME/.babysit"; export BABYSIT_HOME
  out="$("$BBS_TICKET" session list)"
  printf '%s' "$out" | grep -q "sess-A" | : ; printf '%s' "$out" | grep -q "bs-aaa" \
    || { echo "list missing bs-aaa: $out"; exit 1; }
  printf '%s' "$out" | grep -q "bs-bbb" || { echo "list missing bs-bbb: $out"; exit 1; }
) && ok "two-tickets-two-sessions-coexist" || fail "two-tickets-two-sessions-coexist"
rm -rf "$T"

rm -f "$HOOK_BLOCK"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
