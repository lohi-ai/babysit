#!/usr/bin/env bash
# tests/test_bbs_ticket_session.sh — coverage for `bbs-ticket session list|attach|end`.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET="$SCRIPT_DIR/bin/bbs-ticket"
[ -x "$BBS_TICKET" ] || { echo "FAIL: bin not executable" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

write_session() {
  # write_session <dir> <id> <ticket> [age_min]
  local dir="$1" id="$2" tkt="$3" age="${4:-0}"
  local f="$dir/$id.yaml"
  cat > "$f" <<EOF
version: 1
session_id: $id
ticket: $tkt
started_at: 2026-04-30T00:00:00Z
last_seen_at: 2026-05-01T00:00:00Z
pid: 12345
cwd: /tmp/$id
EOF
  if [ "$age" -gt 0 ]; then
    # Backdate by N minutes for stale-session tests
    touch -t "$(date -u -v-${age}M +%Y%m%d%H%M.%S 2>/dev/null \
                 || date -u -d "$age min ago" +%Y%m%d%H%M.%S 2>/dev/null)" "$f"
  fi
}

# ── session-list-shows-recent ──────────────────────────────────────────
T="$(mktemp -d)"
(
  BABYSIT_HOME="$T/.babysit"; export BABYSIT_HOME
  mkdir -p "$BABYSIT_HOME/sessions"
  write_session "$BABYSIT_HOME/sessions" "abc-fresh" "bs-aaa"
  out="$("$BBS_TICKET" session list)"
  printf '%s' "$out" | grep -q "abc-fresh" || { echo "missing recent session: $out"; exit 1; }
  printf '%s' "$out" | grep -q "bs-aaa"    || { echo "missing ticket: $out"; exit 1; }
) && ok "session-list-shows-recent" || fail "session-list-shows-recent"
rm -rf "$T"

# ── session-list-skips-stale ───────────────────────────────────────────
T="$(mktemp -d)"
(
  BABYSIT_HOME="$T/.babysit"; export BABYSIT_HOME
  mkdir -p "$BABYSIT_HOME/sessions"
  write_session "$BABYSIT_HOME/sessions" "old-stale" "bs-old" 200
  write_session "$BABYSIT_HOME/sessions" "new-fresh" "bs-new" 0
  out="$("$BBS_TICKET" session list)"
  printf '%s' "$out" | grep -q "new-fresh" || { echo "missing fresh: $out"; exit 1; }
  if printf '%s' "$out" | grep -q "old-stale"; then
    echo "stale session leaked into list: $out"; exit 1
  fi
) && ok "session-list-skips-stale" || fail "session-list-skips-stale"
rm -rf "$T"

# ── session-attach-emits-exports ───────────────────────────────────────
T="$(mktemp -d)"
(
  BABYSIT_HOME="$T/.babysit"; export BABYSIT_HOME
  mkdir -p "$BABYSIT_HOME/sessions"
  write_session "$BABYSIT_HOME/sessions" "att-ok" "bs-att"
  out="$("$BBS_TICKET" session attach "att-ok")"
  printf '%s' "$out" | grep -qx "export BABYSIT_TICKET=bs-att"        || { echo "no TICKET export: $out"; exit 1; }
  printf '%s' "$out" | grep -qx "export BABYSIT_SESSION=att-ok"       || { echo "no SESSION export: $out"; exit 1; }
) && ok "session-attach-emits-exports" || fail "session-attach-emits-exports"
rm -rf "$T"

# ── session-attach-missing-errors ──────────────────────────────────────
T="$(mktemp -d)"
(
  BABYSIT_HOME="$T/.babysit"; export BABYSIT_HOME
  mkdir -p "$BABYSIT_HOME/sessions"
  err="$("$BBS_TICKET" session attach "no-such-id" 2>&1 1>/dev/null)"; rc=$?
  [ "$rc" -ne 0 ] || { echo "expected non-zero, got 0"; exit 1; }
  printf '%s' "$err" | grep -q "no session file" || { echo "missing diagnostic: $err"; exit 1; }
) && ok "session-attach-missing-errors" || fail "session-attach-missing-errors"
rm -rf "$T"

# ── session-end-removes-file ───────────────────────────────────────────
T="$(mktemp -d)"
(
  BABYSIT_HOME="$T/.babysit"; export BABYSIT_HOME
  mkdir -p "$BABYSIT_HOME/sessions"
  write_session "$BABYSIT_HOME/sessions" "end-me" "bs-end"
  [ -f "$BABYSIT_HOME/sessions/end-me.yaml" ] || { echo "setup wrong"; exit 1; }
  "$BBS_TICKET" session end "end-me" >/dev/null 2>&1
  [ ! -f "$BABYSIT_HOME/sessions/end-me.yaml" ] || { echo "end did not remove file"; exit 1; }
) && ok "session-end-removes-file" || fail "session-end-removes-file"
rm -rf "$T"

# ── session-bad-verb-usage-errors ──────────────────────────────────────
err="$("$BBS_TICKET" session bogus 2>&1 1>/dev/null)"; rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$err" | grep -q "usage:"; then
  ok "session-bad-verb-usage-errors"
else
  fail "session-bad-verb-usage-errors" "rc=$rc err=$err"
fi

# ── session-id-rejects-traversal ───────────────────────────────────────
# Path-traversal guard: session ids must be [A-Za-z0-9_-]; refuse anything else
# before the path is constructed.
T="$(mktemp -d)"
(
  BABYSIT_HOME="$T/.babysit"; export BABYSIT_HOME
  mkdir -p "$BABYSIT_HOME/sessions"
  # Plant a sentinel that traversal would otherwise reach.
  touch "$T/sentinel.yaml"
  for bad in "../sentinel" "./../sentinel" ".." "." "abc/def" "abc;rm" ""; do
    err="$("$BBS_TICKET" session end "$bad" 2>&1 1>/dev/null)"; rc=$?
    [ "$rc" -ne 0 ] || { echo "session end '$bad' rc=$rc; expected non-zero" >&2; exit 1; }
    err="$("$BBS_TICKET" session attach "$bad" 2>&1 1>/dev/null)"; rc=$?
    [ "$rc" -ne 0 ] || { echo "session attach '$bad' rc=$rc; expected non-zero" >&2; exit 1; }
  done
  [ -f "$T/sentinel.yaml" ] || { echo "sentinel was deleted — traversal not blocked" >&2; exit 1; }
) && ok "session-id-rejects-traversal" || fail "session-id-rejects-traversal"
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
