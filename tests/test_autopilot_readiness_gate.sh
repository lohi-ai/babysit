#!/usr/bin/env bash
# tests/test_autopilot_readiness_gate.sh — Phase-1 bootstrap readiness gate.
#
# v1.47.0 replaced the SKILL.md §0.4 gate bash with prose in the builder
# workflow (step 1). This test pins both halves so neither can silently
# vanish again (the probe signals were emitted-but-unconsumed for a release):
#
#   1. `bbs-autopilot probe` emits state_repo_configured / state_landing_doc
#      derived from .babysit/git-flow.yaml + CLAUDE.md|AGENTS.md at git toplevel.
#   2. builder.md step 1 consumes them: unconfigured single-repo →
#      NEEDS_CONTEXT + /bbs:setup-project recommendation;
#      missing landing doc is a warning, not a stop.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_AUTOPILOT="$SCRIPT_DIR/bin/bbs-autopilot"
BUILDER_MD="$SCRIPT_DIR/.claude/skills/autopilot/workflows/builder.md"
[ -x "$BBS_AUTOPILOT" ] || { echo "FAIL: $BBS_AUTOPILOT not executable" >&2; exit 1; }
[ -f "$BUILDER_MD" ]    || { echo "FAIL: $BUILDER_MD missing"          >&2; exit 1; }

PASS=0
FAIL=0
FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# ── probe-emits-readiness-signals ───────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  git init -q "$T/repo"; cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init

  out="$("$BBS_AUTOPILOT" probe 2>/dev/null)"
  echo "$out" | grep -qx 'state_repo_configured=0' || { echo "unconfigured: repo_configured != 0"; exit 1; }
  echo "$out" | grep -qx 'state_landing_doc=0'     || { echo "unconfigured: landing_doc != 0"; exit 1; }

  mkdir -p .babysit; : > .babysit/git-flow.yaml; : > CLAUDE.md
  out="$("$BBS_AUTOPILOT" probe 2>/dev/null)"
  echo "$out" | grep -qx 'state_repo_configured=1' || { echo "configured: repo_configured != 1"; exit 1; }
  echo "$out" | grep -qx 'state_landing_doc=1'     || { echo "configured: landing_doc != 1"; exit 1; }
) && ok "probe-emits-readiness-signals" || fail "probe-emits-readiness-signals"
rm -rf "$T"

# ── landing-doc-signal-accepts-AGENTS-md ────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  git init -q "$T/repo"; cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  : > AGENTS.md
  out="$("$BBS_AUTOPILOT" probe 2>/dev/null)"
  echo "$out" | grep -qx 'state_landing_doc=1' || { echo "AGENTS.md: landing_doc != 1"; exit 1; }
) && ok "landing-doc-signal-accepts-AGENTS-md" || fail "landing-doc-signal-accepts-AGENTS-md"
rm -rf "$T"

# ── builder-consumes-readiness-gate ─────────────────────────────────
# The prose in builder.md step 1 is the only consumer of the probe's
# readiness signals. Pin its load-bearing pieces: the signal name, the
# NEEDS_CONTEXT stop, the setup-project recommendation, and
# landing-doc-as-warning.
gate="$(awk '/Readiness gate/{h=1} h{print} h&&/warning/{exit}' "$BUILDER_MD")"
(
  printf '%s' "$gate" | grep -q 'state_repo_configured=0'      || { echo "no state_repo_configured=0 trigger"; exit 1; }
  printf '%s' "$gate" | grep -q '\.babysit/git-flow\.yaml'     || { echo "no git-flow.yaml mention"; exit 1; }
  printf '%s' "$gate" | grep -q 'NEEDS_CONTEXT'                || { echo "gate does not stop with NEEDS_CONTEXT"; exit 1; }
  printf '%s' "$gate" | grep -q '/bbs:setup-project'           || { echo "no setup-project recommendation"; exit 1; }
  printf '%s' "$gate" | grep -q 'state_landing_doc=0'          || { echo "no landing-doc warning clause"; exit 1; }
) && ok "builder-consumes-readiness-gate" || fail "builder-consumes-readiness-gate"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d checks\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m  %d/%d checks failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
