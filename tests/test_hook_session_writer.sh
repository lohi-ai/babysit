#!/usr/bin/env bash
# tests/test_hook_session_writer.sh — verifies bin/hooks/session-writer, the
# guaranteed (hook-based) session-tracking path. The preamble block only runs
# when a skill executes it; this hook fires on SessionStart + PostToolUse(Bash)
# and mints ~/.babysit/sessions/cc-<session_id>.yaml with the ticket derived
# from the cwd's worktree dir or branch. Pins: minting, ticket derivation
# (worktree path, feat branch, sub-ticket branch, none), 60s throttle,
# started_at preservation, no-op without session_id.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
HOOK="$SCRIPT_DIR/bin/hooks/session-writer"
[ -x "$HOOK" ] || { echo "FAIL: $HOOK missing or not executable" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# ── no-op-without-session-id ───────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  printf '%s' '{"cwd":"/tmp"}' | "$HOOK"
  count="$(find "$HOME/.babysit/sessions" -type f 2>/dev/null | wc -l | tr -d ' ')"
  [ "${count:-0}" = "0" ] || { echo "expected no files, got $count"; exit 1; }
) && ok "no-op-without-session-id" || fail "no-op-without-session-id"
rm -rf "$T"

# ── mints-yaml-with-worktree-ticket ────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  mkdir -p "$T/repo/.babysit/worktrees/bs-wt1_some-slug"
  printf '%s' "{\"session_id\":\"s1\",\"cwd\":\"$T/repo/.babysit/worktrees/bs-wt1_some-slug\"}" | "$HOOK"
  F="$HOME/.babysit/sessions/cc-s1.yaml"
  [ -f "$F" ] || { echo "no yaml at $F"; exit 1; }
  grep -qx "session_id: cc-s1" "$F" || { echo "bad session_id"; cat "$F"; exit 1; }
  grep -qx "ticket: bs-wt1" "$F" || { echo "bad ticket"; cat "$F"; exit 1; }
) && ok "mints-yaml-with-worktree-ticket" || fail "mints-yaml-with-worktree-ticket"
rm -rf "$T"

# ── derives-ticket-from-feat-branch ────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  git -C "$T" init -q -b "feat/bs-br1_add-thing" repo
  printf '%s' "{\"session_id\":\"s2\",\"cwd\":\"$T/repo\"}" | "$HOOK"
  grep -qx "ticket: bs-br1" "$HOME/.babysit/sessions/cc-s2.yaml" \
    || { echo "bad ticket"; cat "$HOME/.babysit/sessions/cc-s2.yaml"; exit 1; }
) && ok "derives-ticket-from-feat-branch" || fail "derives-ticket-from-feat-branch"
rm -rf "$T"

# ── derives-ticket-from-sub-ticket-branch ──────────────────────────────
# Child branches are feat/<parent>/<pos>_<ticket>_<slug>.
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  git -C "$T" init -q -b "feat/bs-parent/003_bs-child_do-part" repo
  printf '%s' "{\"session_id\":\"s3\",\"cwd\":\"$T/repo\"}" | "$HOOK"
  grep -qx "ticket: bs-child" "$HOME/.babysit/sessions/cc-s3.yaml" \
    || { echo "bad ticket"; cat "$HOME/.babysit/sessions/cc-s3.yaml"; exit 1; }
) && ok "derives-ticket-from-sub-ticket-branch" || fail "derives-ticket-from-sub-ticket-branch"
rm -rf "$T"

# ── empty-ticket-on-base-branch ────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  git -C "$T" init -q -b main repo
  printf '%s' "{\"session_id\":\"s4\",\"cwd\":\"$T/repo\"}" | "$HOOK"
  grep -qx "ticket: " "$HOME/.babysit/sessions/cc-s4.yaml" \
    || { echo "expected empty ticket"; cat "$HOME/.babysit/sessions/cc-s4.yaml"; exit 1; }
) && ok "empty-ticket-on-base-branch" || fail "empty-ticket-on-base-branch"
rm -rf "$T"

# ── throttles-rewrites-within-60s ──────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  printf '%s' '{"session_id":"s5","cwd":"/tmp"}' | "$HOOK"
  F="$HOME/.babysit/sessions/cc-s5.yaml"
  grep -qx "cwd: /tmp" "$F" || { echo "first write missing"; exit 1; }
  printf '%s' '{"session_id":"s5","cwd":"/elsewhere"}' | "$HOOK"
  grep -qx "cwd: /tmp" "$F" || { echo "throttle failed: file rewritten within 60s"; cat "$F"; exit 1; }
) && ok "throttles-rewrites-within-60s" || fail "throttles-rewrites-within-60s"
rm -rf "$T"

# ── preserves-started-at-across-rewrites ───────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME
  printf '%s' '{"session_id":"s6","cwd":"/tmp"}' | "$HOOK"
  F="$HOME/.babysit/sessions/cc-s6.yaml"
  ORIG="$(grep '^started_at:' "$F")"
  # age the file past the throttle window, then rewrite
  touch -t 202001010000 "$F"
  printf '%s' '{"session_id":"s6","cwd":"/tmp"}' | "$HOOK"
  [ "$(grep '^started_at:' "$F")" = "$ORIG" ] \
    || { echo "started_at not preserved"; cat "$F"; exit 1; }
  grep -qx "session_id: cc-s6" "$F" || { echo "rewrite lost fields"; cat "$F"; exit 1; }
) && ok "preserves-started-at-across-rewrites" || fail "preserves-started-at-across-rewrites"
rm -rf "$T"

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || { printf 'failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; }
