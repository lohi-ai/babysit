#!/usr/bin/env bash
# tests/test_sub_ticket_branch_shape.sh — regression guard for sub-ticket
# branch construction in autopilot workflows.
#
# Sub-ticket branches must be `feat/<parent>/<NNN>_<child-ticket-id>_<slug>`.
# Both modes of the `builder` workflow construct it and must stay in sync:
#   - orchestrate mode (dispatch-side): builds the branch from $CHILD
#   - child mode       (worker-side):   re-derives it from $PARENT_ID + seed
#
# Both must:
#   1. Slash-namespace the child under the parent (`feat/<parent>/...`)
#   2. Embed the child ticket id between POS and SLUG
#   3. Keep POS as the 3-digit zero-padded seed index
#
# Strategy: extract each literal `CHILD_BRANCH=` line from builder.md, eval it
# in a controlled env, assert the result. The dispatch line references $CHILD;
# the worker line references $PARENT_ID.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BUILDER="$SCRIPT_DIR/.claude/skills/autopilot/workflows/builder.md"
[ -f "$BUILDER" ] || { echo "FAIL: $BUILDER not found" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Extract the CHILD_BRANCH= line matching a distinguishing pattern ($2).
extract_child_branch_line() {
  grep -E '^\s*CHILD_BRANCH=' "$1" | grep -F "$2" | head -1 | sed -E 's/^\s+//'
}

# ── orchestrate-line-present (dispatch side, uses $CHILD) ───────────────
ORCH_LINE="$(extract_child_branch_line "$BUILDER" '${CHILD}')"
if [ -n "$ORCH_LINE" ]; then ok "orchestrate-line-present"
else fail "orchestrate-line-present" "no dispatch-side CHILD_BRANCH= line found in builder.md"; fi

# ── sub-implement-line-present (worker side, uses $PARENT_ID) ───────────
SUB_LINE="$(extract_child_branch_line "$BUILDER" '${PARENT_ID}')"
if [ -n "$SUB_LINE" ]; then ok "sub-implement-line-present"
else fail "sub-implement-line-present" "no worker-side CHILD_BRANCH= line found in builder.md"; fi

# ── orchestrate-single-repo-shape ──────────────────────────────────────
(
  TICKET="bs-abc"
  CHILD="bs-abc-001"
  POS="001"
  SLUG="reminder-api"
  eval "$ORCH_LINE"
  [ "$CHILD_BRANCH" = "feat/bs-abc/001_bs-abc-001_reminder-api" ] || { echo "got: $CHILD_BRANCH"; exit 1; }
) && ok "orchestrate-single-repo-shape" || fail "orchestrate-single-repo-shape"

# ── orchestrate-short-child-id-shape ─────────────────────────────────────
(
  TICKET="bs-abc"
  CHILD="bs-abca"
  POS="002"
  SLUG="reminder-ui"
  eval "$ORCH_LINE"
  [ "$CHILD_BRANCH" = "feat/bs-abc/002_bs-abca_reminder-ui" ] || { echo "got: $CHILD_BRANCH"; exit 1; }
) && ok "orchestrate-short-child-id-shape" || fail "orchestrate-short-child-id-shape"

# ── orchestrate-foreign-prefix-shape (LIN-, APP-, KL-) ────────────────
(
  TICKET="LIN-1234"
  CHILD="LIN-1234-007"
  POS="007"
  SLUG="webhooks-retry"
  eval "$ORCH_LINE"
  [ "$CHILD_BRANCH" = "feat/LIN-1234/007_LIN-1234-007_webhooks-retry" ] || { echo "got: $CHILD_BRANCH"; exit 1; }
) && ok "orchestrate-foreign-prefix-shape" || fail "orchestrate-foreign-prefix-shape"

# ── sub-implement-single-repo-shape ────────────────────────────────────
(
  PARENT_ID="bs-abc"
  TICKET="bs-abc-001"
  POS="001"
  SLUG="reminder-api"
  eval "$SUB_LINE"
  [ "$CHILD_BRANCH" = "feat/bs-abc/001_bs-abc-001_reminder-api" ] || { echo "got: $CHILD_BRANCH"; exit 1; }
) && ok "sub-implement-single-repo-shape" || fail "sub-implement-single-repo-shape"

# ── sub-implement-short-child-id-shape ───────────────────────────────────
(
  PARENT_ID="bs-abc"
  TICKET="bs-abca"
  POS="002"
  SLUG="reminder-ui"
  eval "$SUB_LINE"
  [ "$CHILD_BRANCH" = "feat/bs-abc/002_bs-abca_reminder-ui" ] || { echo "got: $CHILD_BRANCH"; exit 1; }
) && ok "sub-implement-short-child-id-shape" || fail "sub-implement-short-child-id-shape"

# ── pos-stays-zero-padded ──────────────────────────────────────────────
# POS is derived from the seed filename `<NNN>-<slug>.md` (3-digit padded
# by `printf '%03d'` in bbs-ticket). The branch must preserve that padding.
(
  TICKET="bs-abc"
  CHILD="bs-abc-042"
  POS="042"
  SLUG="x"
  eval "$ORCH_LINE"
  case "$CHILD_BRANCH" in
    feat/bs-abc/042_*) ;;
    *) echo "got: $CHILD_BRANCH"; exit 1 ;;
  esac
) && ok "pos-stays-zero-padded" || fail "pos-stays-zero-padded"

# ── no-flat-legacy-shape ───────────────────────────────────────────────
# Catches an accidental revert to the pre-1.30.1 flat form
# `feat/<parent>-<N>-<slug>` (no slash, no embedded child id).
case "$ORCH_LINE" in
  *'feat/${TICKET}-'*) fail "no-flat-legacy-shape" "builder.md dispatch line uses flat form" ;;
  *) ok "no-flat-legacy-shape-orchestrate" ;;
esac
case "$SUB_LINE" in
  *'feat/${PARENT_ID}-'*) fail "no-flat-legacy-shape-sub-implement" "builder.md child line uses flat form" ;;
  *) ok "no-flat-legacy-shape-sub-implement" ;;
esac

# ── summary ────────────────────────────────────────────────────────────
echo
echo "PASS: $PASS  FAIL: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAIL_NAMES[@]}"
  exit 1
fi
exit 0
