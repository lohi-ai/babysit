#!/usr/bin/env bash
# Contract checks for the Orca-native autonomous foreman.

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
F="$ROOT/.claude/skills/foreman/SKILL.md"
WRAPPER="$ROOT/skills/foreman/SKILL.md"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; }

has_all() {
  local name="$1"; shift
  local needle
  for needle in "$@"; do
    grep -q -- "$needle" "$F" || { fail "$name"; return; }
  done
  ok "$name"
}

has_all "autonomous-project-scope" \
  'Autonomous Orca orchestrator' 'multiple tickets or dependent features' \
  'Project decomposition and DAG'

has_all "live-orchestration-contract" \
  '~/.claude/skills/orchestration/SKILL.md' 'skills get orchestration' \
  'worker-start' 'check --wait' 'worker-release' 'request recovery'

has_all "foreman-owns-topology" \
  'bbs ticket ensure --mode=worktree' 'git worktree list' \
  'git worktree remove' 'dependency-order finish'

has_all "agent-independent-design-gate" \
  'Two-phase ticket dispatch' '--stop-after=plan' 'Orca decision gate' \
  'approval self-resolve'

has_all "project-qa-gate" \
  'Integration QA Task' 'bbs ticket switch' 'parent QA lease' \
  'Integration QA is read-only'

has_all "durable-resume-and-finish" \
  'pointers.orca_run' 'Terminal handles are routing metadata' \
  'bbs ticket readiness --json' 'BBS_FINISH=review | land | pr'

if ! grep -q 'bbs foreman mailbox wait' "$F" \
   && ! grep -q 'sleep 20' "$F" \
   && ! grep -q 'Copy the block below' "$F"; then
  ok "no-pane-or-mailbox-coordination"
else
  fail "no-pane-or-mailbox-coordination"
fi

if grep -q 'Autonomous Orca orchestrator' "$WRAPPER" \
   && grep -q '../../.claude/skills/foreman/SKILL.md' "$WRAPPER"; then
  ok "codex-wrapper-matches"
else
  fail "codex-wrapper-matches"
fi

echo
echo "PASS: $PASS  FAIL: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAIL_NAMES[@]}"
  exit 1
fi
