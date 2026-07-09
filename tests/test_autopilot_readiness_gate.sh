#!/usr/bin/env bash
# tests/test_autopilot_readiness_gate.sh — Phase-1 bootstrap gate.
#
# v1.47.0 replaced the SKILL.md §0.4 gate bash with prose in the builder
# workflow (step 1). v1.53.0 turned the stop into a bootstrap: an
# unconfigured repo seeds default git-flow.yaml and continues (non-tech
# invokers can't answer branch policy). This test pins all three halves:
#
#   1. `bbs-autopilot probe` emits state_repo_configured / state_landing_doc
#      derived from .babysit/git-flow.yaml + CLAUDE.md|AGENTS.md at git toplevel.
#   2. builder.md step 1 consumes them: unconfigured single-repo → seed
#      documented defaults + /bbs:setup-project recommendation for the QA
#      harness; missing landing doc is a warning, not a stop.
#   3. The seed bash block in builder.md actually produces a valid
#      git-flow.yaml when executed in an unconfigured repo.

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

# ── builder-consumes-bootstrap-gate ─────────────────────────────────
# The prose in builder.md step 1 is the only consumer of the probe's
# readiness signals. Pin its load-bearing pieces: the signal name, the
# seed-and-continue behavior (no NEEDS_CONTEXT stop), the setup-project
# recommendation for the QA harness, and landing-doc-as-warning.
gate="$(awk '/Bootstrap gate/{h=1} h{print} h&&/warning/{exit}' "$BUILDER_MD")"
(
  printf '%s' "$gate" | grep -q 'state_repo_configured=0'      || { echo "no state_repo_configured=0 trigger"; exit 1; }
  printf '%s' "$gate" | grep -q '\.babysit/git-flow\.yaml'     || { echo "no git-flow.yaml mention"; exit 1; }
  printf '%s' "$gate" | grep -qi 'seed'                        || { echo "gate does not seed defaults"; exit 1; }
  printf '%s' "$gate" | grep -q 'NEEDS_CONTEXT'                && { echo "gate regressed to a NEEDS_CONTEXT stop"; exit 1; }
  printf '%s' "$gate" | grep -q '/bbs:setup-project'           || { echo "no setup-project recommendation"; exit 1; }
  printf '%s' "$gate" | grep -q 'state_landing_doc=0'          || { echo "no landing-doc warning clause"; exit 1; }
) && ok "builder-consumes-bootstrap-gate" || fail "builder-consumes-bootstrap-gate"

# ── bootstrap-seed-block-executes ───────────────────────────────────
# Extract the ```bash block inside the gate and run it in an unconfigured
# repo with no remote: it must write a parseable git-flow.yaml with a
# non-empty base_branch, mode: branch, and push: false.
seed="$(printf '%s\n' "$gate" | awk '/```bash/{f=1;next} f&&/```/{exit} f')"
T="$(mktemp -d)"
(
  [ -n "$seed" ] || { echo "no bash seed block inside the gate"; exit 1; }
  git init -q "$T/repo"; cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  bash -eu -c "$seed" || { echo "seed block failed to execute"; exit 1; }
  cfg=".babysit/git-flow.yaml"
  [ -f "$cfg" ] || { echo "seed did not write $cfg"; exit 1; }
  grep -q '^base_branch: .' "$cfg" || { echo "empty base_branch"; exit 1; }
  grep -qx 'mode: branch' "$cfg"   || { echo "mode != branch"; exit 1; }
  grep -qx 'push: false' "$cfg"    || { echo "push != false without a remote"; exit 1; }
) && ok "bootstrap-seed-block-executes" || fail "bootstrap-seed-block-executes"
rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d checks\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m  %d/%d checks failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
