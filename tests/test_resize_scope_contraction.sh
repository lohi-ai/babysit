#!/usr/bin/env bash
# tests/test_resize_scope_contraction.sh — resize-on-scope-contraction hooks.
#
# ticket_size is set by plan-draft against the *expected* footprint; when
# scope contracts mid-flight (plan defers ≥40%, or Files collapses to ≤3
# trivial entries) the pointer must be downgraded one tier with an audit-log
# line. The downgrade bash lives in
# .claude/skills/references/ticket-size-rubric.md ("Downgrade triggers");
# plan-draft and implement point at it. This test:
#   1. Extracts the rubric's downgrade block and runs it for real:
#      L→M downgrade persists the pointer + appends the resize audit line.
#   2. XS is a no-op: pointer unchanged, no audit line.
#   3. Pins that plan-draft and implement both reference the hook (the wiring
#      that was missing while the rubric documented it unconsumed).

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUBRIC_MD="$SCRIPT_DIR/.claude/skills/references/ticket-size-rubric.md"
PLAN_MD="$SCRIPT_DIR/.claude/skills/plan-draft/SKILL.md"
IMPL_MD="$SCRIPT_DIR/.claude/skills/implement/SKILL.md"
BBS_TICKET="$SCRIPT_DIR/bin/bbs-ticket"
[ -f "$RUBRIC_MD" ]  || { echo "FAIL: $RUBRIC_MD missing" >&2; exit 1; }
[ -x "$BBS_TICKET" ] || { echo "FAIL: $BBS_TICKET not executable" >&2; exit 1; }

PASS=0
FAIL=0
FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Extract the downgrade bash block (the fence under "## Downgrade triggers").
BLOCK="$(mktemp)"
awk '/^## Downgrade triggers/{h=1}
     h&&/^```bash$/{f=1;next}
     f&&/^```$/{exit}
     f{print}' "$RUBRIC_MD" > "$BLOCK"
grep -q 'set-pointer ticket_size' "$BLOCK" \
  || { echo "FAIL: could not extract downgrade block from rubric" >&2; exit 1; }

# run_downgrade <tempdir> <initial-size>
#   Fresh sandbox: temp HOME + analytics dir, a git repo on a feat branch,
#   ticket initialized with ticket_size=<initial-size>; then sources the
#   downgrade block with Hook A's trigger.
run_downgrade() {
  local t="$1" size="$2"
  export HOME="$t/home"; mkdir -p "$HOME"
  export BABYSIT_ANALYTICS_DIR="$t/analytics"
  export BBS_TICKET_BIN="$BBS_TICKET"
  export BBS_TICKET="bs-resize-1"
  git init -q "$t/repo"; cd "$t/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git checkout -q -b feat/bs-resize-1_scratch
  "$BBS_TICKET_BIN" init >/dev/null 2>&1
  "$BBS_TICKET_BIN" set-pointer ticket_size "$size" >/dev/null
  SKILL_NAME="plan-draft"; TICKET="bs-resize-1"; TRIGGER="deferral_ratio>=40%"
  set +u; . "$BLOCK"; set -u
}

# ── downgrade-L-to-M-with-audit-line ────────────────────────────────

T="$(mktemp -d)"
(
  run_downgrade "$T" L
  got="$("$BBS_TICKET_BIN" get pointers.ticket_size)"
  [ "$got" = "M" ] || { echo "pointer=$got, expected M"; exit 1; }
  log="$T/analytics/decisions.jsonl"
  [ -f "$log" ] || { echo "no decisions.jsonl written"; exit 1; }
  grep -q '"kind":"resize","from":"L","to":"M","trigger":"deferral_ratio>=40%"' "$log" \
    || { echo "audit line malformed: $(cat "$log")"; exit 1; }
) && ok "downgrade-L-to-M-with-audit-line" || fail "downgrade-L-to-M-with-audit-line"
rm -rf "$T"

# ── xs-is-noop-no-audit-line ────────────────────────────────────────

T="$(mktemp -d)"
(
  run_downgrade "$T" XS
  got="$("$BBS_TICKET_BIN" get pointers.ticket_size)"
  [ "$got" = "XS" ] || { echo "pointer=$got, expected XS"; exit 1; }
  [ ! -f "$T/analytics/decisions.jsonl" ] \
    || { echo "unexpected audit line: $(cat "$T/analytics/decisions.jsonl")"; exit 1; }
) && ok "xs-is-noop-no-audit-line" || fail "xs-is-noop-no-audit-line"
rm -rf "$T"

# ── skills-reference-the-hook ───────────────────────────────────────

(
  grep -q 'ticket-size-rubric' "$PLAN_MD" \
    && grep -q '40%' "$PLAN_MD" \
    && grep -q 'downgrade' "$PLAN_MD"
) && ok "plan-draft-references-hook" || fail "plan-draft-references-hook"

(
  grep -q 'ticket-size-rubric' "$IMPL_MD" \
    && grep -qi '≤3\|<=3' "$IMPL_MD" \
    && grep -q 'downgrade' "$IMPL_MD"
) && ok "implement-references-hook" || fail "implement-references-hook"

rm -f "$BLOCK"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d checks\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m  %d/%d checks failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
