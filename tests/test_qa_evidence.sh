#!/usr/bin/env bash
# tests/test_qa_evidence.sh — coverage for `bbs-ticket qa-evidence`.
#
# Classifies the persisted qa verdict body against the coverage rubric it
# claims: a PASS/FIXED must show freshness=A, no C/D dimension, and evidence
# naming a real e2e run; a non-PASS status must name a blocker.
#   none | ok | contradiction:<d> | thin:<d> | unexplained

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"
[ -x "$BBS_TICKET_BIN" ] || { echo "FAIL: bbs-ticket not executable" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

GOOD_RUBRIC='RUBRIC: flow=A boundary=B regression=B data=A compat=B security=N/A a11y=B perf=B freshness=A'
GOOD_EVID='EVIDENCE: agent-browser journey signup→dashboard, observed row created; evidence/qa/signup.png'

# run_case <name> <expected> <body...>  — body passed as remaining args, newline-joined
run_case() {
  local name="$1" want="$2"; shift 2
  local T; T="$(mktemp -d)"
  local out rc
  out="$(
    export BABYSIT_PROJECT_HOME="$T/proj" BABYSIT_TICKET="bs-x"
    unset BBS_TICKET BABYSIT_HOME 2>/dev/null
    mkdir -p "$T/proj/tickets/bs-x/verdicts"
    if [ "$#" -gt 0 ]; then printf '%s\n' "$@" > "$T/proj/tickets/bs-x/verdicts/qa.md"; fi
    "$BBS_TICKET_BIN" qa-evidence 2>/dev/null
  )"; rc=$?
  rm -rf "$T"
  if [ "$rc" = 0 ] && [ "$out" = "$want" ]; then ok "$name"; else fail "$name" "want='$want' got='$out' rc=$rc"; fi
}

# no verdict file at all
run_case "none-no-file" "none"

# clean PASS: freshness=A, no C/D, real e2e evidence
run_case "ok-clean-pass" "ok" \
  "STATUS: DONE" "VERDICT: PASS" "SUMMARY: all flows green" "$GOOD_RUBRIC" "$GOOD_EVID"

# FIXED is treated like PASS
run_case "ok-fixed" "ok" \
  "STATUS: DONE" "VERDICT: FIXED(2)" "SUMMARY: fixed 2" "$GOOD_RUBRIC" "$GOOD_EVID"

# PASS but freshness not A → contradiction
run_case "contradiction-freshness" "contradiction:freshness=C" \
  "STATUS: DONE" "VERDICT: PASS" "SUMMARY: x" \
  "RUBRIC: flow=A boundary=B regression=B data=A compat=B security=N/A a11y=B perf=B freshness=C" \
  "$GOOD_EVID"

# PASS but a dimension graded D → contradiction
run_case "contradiction-dimension" "contradiction:flow=D" \
  "STATUS: DONE" "VERDICT: PASS" "SUMMARY: x" \
  "RUBRIC: flow=D boundary=B regression=B data=A compat=B security=N/A a11y=B perf=B freshness=A" \
  "$GOOD_EVID"

# PASS but no evidence line content → thin
run_case "thin-no-evidence" "thin:no-evidence" \
  "STATUS: DONE" "VERDICT: PASS" "SUMMARY: x" "$GOOD_RUBRIC" "EVIDENCE: none"

# PASS but evidence reads as code-only (no e2e keywords) → thin
run_case "thin-no-e2e" "thin:no-e2e" \
  "STATUS: DONE" "VERDICT: PASS" "SUMMARY: x" "$GOOD_RUBRIC" \
  "EVIDENCE: ran the unit suite and read the diff"

# honest FAIL is fine — the STATUS gate handles readiness
run_case "ok-honest-fail" "ok" \
  "STATUS: BLOCKED" "VERDICT: FAIL" "SUMMARY: export stuck at 99%" \
  "RUBRIC: flow=C boundary=D regression=C data=A compat=B security=N/A a11y=B perf=B freshness=C"

# concerns status with a named blocker explains itself → ok
run_case "ok-concerns-explained" "ok" \
  "STATUS: DONE_WITH_CONCERNS" "VERDICT: FAIL" "SUMMARY: no local target: missing DB creds"

# concerns status with nothing said → unexplained
run_case "unexplained-blank" "unexplained" \
  "STATUS: DONE_WITH_CONCERNS" "VERDICT: FAIL" "SUMMARY:" "EVIDENCE:"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
