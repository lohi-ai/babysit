#!/usr/bin/env bash
# tests/test_bbs_ticket_control.sh — coverage for the control axis:
# `bbs ticket assign | pause | cancel | resume | restore`.
#
# The point of the feature is that control and status are separate axes:
# status is the derived lifecycle rung, control is a human override. So the
# assertions are mostly about what does NOT change — pausing must not move
# status, and reconcile must not move a paused ticket even when the filesystem
# says it should advance.
#
# Scenarios:
#   pause-keeps-status        pause records prior_status/actor/note, status intact
#   pause-idempotent          second pause exits 0 without rewriting the record
#   cross-verb-guarded        cancel-while-paused and restore-a-pause both rc 2
#   reversible-round-trip     resume and restore each clear control losslessly
#   clear-when-unset          resume on an uncontrolled ticket is a no-op, rc 0
#   reconcile-skips-control   plan.md would advance triage→planned; paused wins
#   assign-and-unassign       assignee set, then --none clears it to null

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS="$SCRIPT_DIR/bin/bbs-ticket"

command -v jq >/dev/null 2>&1 || { echo "SKIP: jq not installed"; exit 0; }
[ -x "$BBS" ] || { echo "FAIL: $BBS not built (go build -o bin/bbs ./cmd/bbs)"; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# sandbox <dir> — pin HOME + project home + ticket id so nothing touches the
# real ~/.babysit. No git repo needed: every verb here is pure index.json.
sandbox() {
  export HOME="$1/home"; mkdir -p "$HOME"
  export BABYSIT_PROJECT_HOME="$1/home/projects/slug"
  export BABYSIT_TICKET="bs-ctl0001"
  export AGENT_ROLE=mayor
  unset BBS_TICKET 2>/dev/null || true
  IDX="$BABYSIT_PROJECT_HOME/tickets/$BABYSIT_TICKET/index.json"
}
field() { jq -r "$1 // empty" "$IDX"; }

# ── pause-keeps-status ────────────────────────────────────────────────
T="$(mktemp -d)"
(
  sandbox "$T"
  "$BBS" set-status planned >/dev/null 2>&1 || { echo "set-status failed"; exit 1; }
  "$BBS" pause --note "waiting on design" >/dev/null 2>&1 || { echo "pause failed"; exit 1; }

  [ "$(field .status)" = "planned" ] || { echo "status moved: $(field .status)"; exit 1; }
  [ "$(field .control.state)" = "paused" ] || { echo "control.state: $(field .control.state)"; exit 1; }
  [ "$(field .control.prior_status)" = "planned" ] || { echo "prior_status: $(field .control.prior_status)"; exit 1; }
  [ "$(field .control.note)" = "waiting on design" ] || { echo "note: $(field .control.note)"; exit 1; }
  [ "$(field .control.actor)" = "mayor" ] || { echo "actor: $(field .control.actor)"; exit 1; }
  [ -n "$(field .control.at)" ] || { echo "control.at empty"; exit 1; }
  # control must be a real object, not a JSON string (Doc.Set would stringify).
  [ "$(jq -r '.control | type' "$IDX")" = "object" ] || { echo "control not an object"; exit 1; }
  grep -q '"event": *"control_set"' "$(dirname "$IDX")/history.jsonl" \
    || { echo "no control_set history row"; exit 1; }
) && ok "pause-keeps-status" || fail "pause-keeps-status"
rm -rf "$T"

# ── pause-idempotent ──────────────────────────────────────────────────
T="$(mktemp -d)"
(
  sandbox "$T"
  "$BBS" pause --note first >/dev/null 2>&1 || { echo "pause failed"; exit 1; }
  out="$("$BBS" pause --note second 2>&1)"; rc=$?
  [ "$rc" -eq 0 ] || { echo "second pause rc=$rc: $out"; exit 1; }
  printf '%s' "$out" | grep -q "already paused" || { echo "unexpected: $out"; exit 1; }
  [ "$(field .control.note)" = "first" ] || { echo "record was overwritten: $(field .control.note)"; exit 1; }
) && ok "pause-idempotent" || fail "pause-idempotent"
rm -rf "$T"

# ── cross-verb-guarded ────────────────────────────────────────────────
T="$(mktemp -d)"
(
  sandbox "$T"
  "$BBS" pause >/dev/null 2>&1 || { echo "pause failed"; exit 1; }

  out="$("$BBS" cancel 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "cancel-while-paused rc=$rc: $out"; exit 1; }
  printf '%s' "$out" | grep -q "resume" || { echo "error must name the fix: $out"; exit 1; }

  out="$("$BBS" restore 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "restore-a-pause rc=$rc: $out"; exit 1; }
  printf '%s' "$out" | grep -q "resume" || { echo "error must name the fix: $out"; exit 1; }
  [ "$(field .control.state)" = "paused" ] || { echo "state changed: $(field .control.state)"; exit 1; }
) && ok "cross-verb-guarded" || fail "cross-verb-guarded"
rm -rf "$T"

# ── reversible-round-trip ─────────────────────────────────────────────
T="$(mktemp -d)"
(
  sandbox "$T"
  "$BBS" set-status in_progress >/dev/null 2>&1
  before="$(jq -S 'del(.control, .updated_at)' "$IDX")"

  "$BBS" pause >/dev/null 2>&1   || { echo "pause failed"; exit 1; }
  "$BBS" resume >/dev/null 2>&1  || { echo "resume failed"; exit 1; }
  [ "$(field .control.state)" = "" ] || { echo "control not cleared: $(field .control.state)"; exit 1; }
  [ "$(jq -r '.control | type' "$IDX")" = "null" ] || { echo "control should be null"; exit 1; }

  "$BBS" cancel >/dev/null 2>&1  || { echo "cancel failed"; exit 1; }
  [ "$(field .control.state)" = "cancelled" ] || { echo "not cancelled"; exit 1; }
  [ "$(field .status)" = "in_progress" ] || { echo "cancel moved status: $(field .status)"; exit 1; }
  "$BBS" restore >/dev/null 2>&1 || { echo "restore failed"; exit 1; }

  after="$(jq -S 'del(.control, .updated_at)' "$IDX")"
  [ "$before" = "$after" ] || { echo "round trip lost state"; diff <(echo "$before") <(echo "$after"); exit 1; }
) && ok "reversible-round-trip" || fail "reversible-round-trip"
rm -rf "$T"

# ── clear-when-unset ──────────────────────────────────────────────────
T="$(mktemp -d)"
(
  sandbox "$T"
  "$BBS" set-status backlog >/dev/null 2>&1
  out="$("$BBS" resume 2>&1)"; rc=$?
  [ "$rc" -eq 0 ] || { echo "resume rc=$rc: $out"; exit 1; }
  printf '%s' "$out" | grep -q "nothing to clear" || { echo "unexpected: $out"; exit 1; }
) && ok "clear-when-unset" || fail "clear-when-unset"
rm -rf "$T"

# ── reconcile-skips-control ───────────────────────────────────────────
T="$(mktemp -d)"
(
  sandbox "$T"
  "$BBS" set-status triage >/dev/null 2>&1
  echo "# plan" > "$(dirname "$IDX")/plan.md"   # would derive target=planned

  "$BBS" pause >/dev/null 2>&1 || { echo "pause failed"; exit 1; }
  out="$("$BBS" reconcile --ticket "$BABYSIT_TICKET" 2>&1)"
  printf '%s' "$out" | grep -q "skip — paused" || { echo "unexpected: $out"; exit 1; }
  [ "$(field .status)" = "triage" ] || { echo "reconcile advanced a paused ticket: $(field .status)"; exit 1; }

  # …and the same ticket does advance once the pause is lifted.
  "$BBS" resume >/dev/null 2>&1
  "$BBS" reconcile --ticket "$BABYSIT_TICKET" >/dev/null 2>&1
  [ "$(field .status)" = "planned" ] || { echo "reconcile stuck after resume: $(field .status)"; exit 1; }
) && ok "reconcile-skips-control" || fail "reconcile-skips-control"
rm -rf "$T"

# ── assign-and-unassign ───────────────────────────────────────────────
T="$(mktemp -d)"
(
  sandbox "$T"
  "$BBS" assign fm-alpha >/dev/null 2>&1 || { echo "assign failed"; exit 1; }
  [ "$(field .assignee)" = "fm-alpha" ] || { echo "assignee: $(field .assignee)"; exit 1; }

  "$BBS" assign --none >/dev/null 2>&1 || { echo "unassign failed"; exit 1; }
  [ "$(jq -r '.assignee | type' "$IDX")" = "null" ] || { echo "assignee not cleared"; exit 1; }

  out="$("$BBS" assign 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "bare assign rc=$rc: $out"; exit 1; }
) && ok "assign-and-unassign" || fail "assign-and-unassign"
rm -rf "$T"

echo
echo "  $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || { printf '  failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; }
