#!/usr/bin/env bash
# Contract checks for the Orca-native autonomous foreman.

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
F="$ROOT/.claude/skills/foreman/SKILL.md"
A="$ROOT/.claude/skills/autopilot/SKILL.md"
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

has_all "finish-cleans-worker-workspace" \
  'worker-terminal' 'verified-clean non-primary worktree' \
  'git worktree remove' 'keep its branch' 'Under `review`' \
  'keep the worktree'

has_all "bounded-worker-pool" \
  'bbs config get parallel_max_workers' 'MAX_WORKERS=16' \
  'positive integer' 'one writer per child worktree'

has_all "autopilot-worker-execution-envelope" \
  '`AGENT_ROLE=orca`' 'already spawned' \
  'skips any developer `/goal`' 'worker-start --agent' \
  'exactly one `worker_done`'

if grep -q 'authenticated, current Orca Dispatch preamble' "$A" \
   && grep -q 'use the injected lifecycle instead' "$A" \
   && grep -q 'developer `/goal` handoff' "$A"; then
  ok "autopilot-accepts-orca-dispatch-envelope"
else
  fail "autopilot-accepts-orca-dispatch-envelope"
fi

has_all "agent-independent-design-gate" \
  'Two-phase ticket dispatch' '--stop-after=plan' 'Orca decision gate' \
  'approval self-resolve'

has_all "project-qa-gate" \
  'Integration QA Task' 'bbs ticket switch' 'parent QA lease' \
  'Integration QA is read-only'

has_all "durable-resume-and-finish" \
  'pointers.orca_run' 'Terminal handles are routing metadata' \
  'bbs ticket readiness --json' 'BBS_FINISH=review | land | pr'

has_all "ensure-before-init-ordering" \
  'ensure --mode=worktree' '--from-input-file "$SEED_PATH"' \
  'never pre-create' 'fast-path no-op' 'origin-type sub_ticket' \
  'from inside its own worktree'

has_all "reset-base-before-land" \
  'reset-base' 'bbs-serving' 'BLOCKs' 'dependency order'

has_all "native-task-list-init" \
  'native task list at entry' 'parent, children, and DAG' \
  'rebuild it from ticket + Orca state'

P="$ROOT/.claude/skills/references/preamble.md"
if grep -q 'MUST mirror' "$P" \
   && grep -q 'update_plan' "$P" \
   && grep -q 'TaskCreate' "$P" \
   && grep -q '`todo`' "$P"; then
  ok "preamble-task-list-per-harness"
else
  fail "preamble-task-list-per-harness"
fi

has_all "single-writer-multi-foreman" \
  '--foreman-id <id>' 'bbs ticket claim' 'hard fence' \
  'different parents may run'

has_all "direct-skill-invocation-is-primary" \
  'Direct skill invocation' 'default entrypoint' \
  'bbs foreman adopt' '--agent claude' '--agent omp' '--agent codex' \
  'not a prerequisite' 're-adopt the current session'

has_all "live-change-intake" \
  'bbs foreman inbox' 'change-request' \
  "Do not rewrite a settled ticket" 'prior Integration QA'

has_all "goal-compaction-and-days" \
  'persistent goal proxy' 'Compaction is a cold-resume boundary' \
  'bbs foreman ensure <id>' 'cold-starts instead'

has_all "active-status-reconcile" \
  '## Status reconciliation' 'check --wait' \
  'never a liveness-only reply' 'Dispatch only newly ready work' \
  'active for the next bounded check'

has_all "status-wake-full-snapshot" \
  'status wake' 'always prints the full' \
  'every project Task and supervised worker' 'IN_PROGRESS'

has_all "terminal-done-heartbeat" \
  'bbs foreman heartbeat "$FOREMAN_ID" --status done' \
  'only completion signal' 'record never completes' \
  'paused, or cancelled project never writes `done`'

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
