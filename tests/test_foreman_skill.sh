#!/usr/bin/env bash
# Contract checks for the Orca-native autonomous foreman.

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
F="$ROOT/.claude/skills/foreman/SKILL.md"
A="$ROOT/.claude/skills/autopilot/SKILL.md"
C="$ROOT/.claude/skills/create-pr/SKILL.md"
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

has_all "repository-autonomy-profiles" \
  '## Repository profile and autonomy' 'BBS_PROFILE=pet | startup | enterprise' \
  'maximum' 'safe ready wave' 'scales verification breadth' \
  'ticket evidence controls model routing'

has_all "live-orchestration-contract" \
  '~/.claude/skills/orchestration/SKILL.md' 'skills get orchestration' \
  'worker-start' 'check --wait' 'worker-release' 'request recovery'

has_all "foreman-owns-topology" \
  'bbs ticket ensure --mode=worktree' 'git worktree list' \
  'git worktree remove' 'dependency-order finish'

has_all "finish-cleans-worker-workspace" \
  'Dispatch-owned agent terminal' 'verified-clean non-primary Git' \
  'terminal close --worktree path:<worktreePath> --all' \
  'mandatory even under `review`' 'Git worktree for' \
  'human inspection' 'keep the branch'

has_all "bounded-worker-pool" \
  'bbs config get parallel_max_workers' 'MAX_WORKERS=4' \
  'positive integer' 'one writer per child worktree'

has_all "global-weighted-admission" \
  'machine-global weighted' 'bbs foreman resource reserve' \
  'parallel_global_units' 'ADMISSION' 'no time-based expiry' \
  'resource backpressure'


has_all "child-slice-sizing" \
  'independent, testable, releasable units' \
  'never split one coherent change across siblings' \
  'split the work into sub-tickets' 'explicit' 'phases inside its own ticket'

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

has_all "lifecycle-aware-dispatch" \
  'pointers.workflow' 'Never hard-code every' \
  'Non-builder Execution Task' 'LIFECYCLE' 'TRIGGER' \
  'evidence-only prototype, recommendation, or audit'

has_all "project-qa-gate" \
  'Integration QA Task' 'bbs ticket surface compose' 'parent surface lease' \
  'Integration QA is read-only'

has_all "durable-resume-and-finish" \
  'pointers.orca_run' 'Terminal handles are routing metadata' \
  'bbs ticket readiness --action <review|land|pr> --json' 'BBS_FINISH=review | land | pr'

has_all "ensure-before-init-ordering" \
  'ensure --mode=worktree' '--from-input-file "$SEED_PATH"' \
  'never pre-create' 'fast-path no-op' 'origin-type sub_ticket' \
  'from inside its own worktree'

has_all "revert-before-land" \
  'surface revert' 'bbs-serving' 'BLOCKs' 'dependency order'

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

has_all "dag-emission-contract" \
  '## The project DAG' 'bbs ticket dag "$PARENT" --mermaid' \
  'Read-only: it never writes ticket state' 'never re-type the edges' \
  'Topology built' 'Topology changed' 'Status wake' \
  're-emits the DAG' 'rides with the status it explains'

has_all "ticket-topology-cli-contract" \
  'BABYSIT_TICKET="$PARENT" bbs ticket add-child "$CHILD"' \
  'bbs ticket add-relation' 'blocked_by "$PREREQUISITE"' \
  'blocks "$DEPENDENT"' 'never edit' 'index.json' 'append history'

has_all "goal-compaction-and-days" \
  'persistent goal proxy' 'Compaction is a cold-resume boundary' \
  'bbs foreman ensure <id>' 'cold-starts instead'

has_all "active-status-reconcile" \
  '## Status reconciliation' 'check --wait' \
  'never a liveness-only reply' 'maximum admitted ready wave' \
  'active for the next bounded check'
has_all "eager-per-ticket-finish" \
  '## Eager per-ticket finish' 'settled workers need not wait' \
  'bbs ticket land <child>' 'create-pr' 'dependency order' \
  'Final Integration QA' 'resets local base' \
  'pointers.pr' 'merge-base --is-ancestor' \
  'supervised repair Dispatch' 'never blind-retry' \
  'under `land`' 'final integration QA does not' '`review`'

has_all "parent-design-gates-child-topology" \
  'Before step 2, pass' 'Project design checkpoint' \
  'Append `--auto` only when explicitly requested' 'read recorded `auto`' \
  'accepted parent plan/design/prototype'

has_all "final-qa-after-finish-before-done" \
  'After the handlers, run' 'Do not set parent completion' \
  'landed `<base>`' 'retained `qa/<parent>`' \
  'Independent tickets still need' 'Final Integration QA'

PROJECT_CONTRACT="$ROOT/.claude/skills/foreman/references/project-contract.md"
if python3 - "$PROJECT_CONTRACT" "$ROOT/.claude/skills/qa/SKILL.md" <<'PY'
from pathlib import Path
import sys
contract, qa = (Path(p).read_text() for p in sys.argv[1:])
for required in (
    'approval publish --kind project-plan',
    'Only current `approved` unlocks children',
    '`--auto` delegates the human design reviews',
    'auto: true', 'approval self-resolve',
    'bbs foreman report <parent>', 'atomically replace parent `report.md`',
    'PR_READY', 'LANDED_LOCAL', 'MERGED_REMOTE', 'UNKNOWN',
    'Never move `<base>`', 'No `-B`, force update or deletion',
    'Do not use\n   `surface revert` as restoration',
    'Changed inputs mean STALE',
):
    assert required in contract, required
assert 'Foreman final integration mode' in qa
assert "coordinator's lease" in qa
assert 'This explicit mode takes precedence' in qa
PY
then
  ok "project-approval-report-and-final-surface-contract"
else
  fail "project-approval-report-and-final-surface-contract"
fi

has_all "ticket-status-closeout" \
  'bbs ticket set-status done' 'bbs ticket set-status in_review' \
  'only after the PR is observed merged' \
  'parent ticket to `done`' 'parent to `in_review`'

if grep -q 'bbs ticket set-status in_review' "$C" \
   && grep -q 'status becomes `done` only after the PR is observed merged' "$C"; then
  ok "create-pr-persists-review-status"
else
  fail "create-pr-persists-review-status"
fi


has_all "status-wake-full-snapshot" \
  'status wake' 'always prints the full' \
  'every project Task and supervised worker' 'IN_PROGRESS'

has_all "terminal-done-heartbeat" \
  'bbs foreman heartbeat "$FOREMAN_ID" --status done' \
  'only completion signal' 'record never completes' \
  '`bbs foreman watch` closes the exact adopted Foreman terminal tab' \
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

has_all "shared-reconciliation-interval" \
  'bbs config get foreman_status_interval' 'default 3600' \
  'check --wait' 'missed-event/restart/stale-state' \
  'never let a bad value shrink the wait'

has_all "phase-specific-worker-model-routing" \
  '## Worker model and effort routing' '../references/model-routing.md' \
  'canonical harness' 'never invent a model ID' \
  'Classify the ticket once' 'Plan and design-feedback Dispatches' \
  'Build, review, and per-ticket QA' \
  '--model <model> --effort <effort>' 'launch.effective' 'grok-4.6' \
  'set-pointer planner_model' 'set-pointer planner_effort' \
  'set-pointer worker_model' 'set-pointer worker_effort' \
  'starts a fresh normal Build worker' 'Taste'

has_all "worker-model-cost-discipline" \
  'routine rung' 'top rung' 'came back' 'log the cost' \
  'Ordinary Build still returns'

REF="$ROOT/.claude/skills/references/model-routing.md"
if grep -q 'model-routing.md' "$F" \
   && ! grep -q 'model-routing.md' "$A" \
   && grep -q '^| Codex | #3 `gpt-5.6-terra`, `high` |' "$REF"; then
  ok "canonical-model-table-foreman-only"
else
  fail "canonical-model-table-foreman-only"
fi

echo
echo "PASS: $PASS  FAIL: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAIL_NAMES[@]}"
  exit 1
fi
