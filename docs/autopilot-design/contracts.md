# Contracts

All commands and version-2 schemas in this document are proposals. Existing commands retain their current output and exit behavior until migrated explicitly. New flags must be discoverable via help/capability output; do not paste these examples into the current binary expecting support.

## CLI surface

Prefer additive forms on existing commands. Introduce a new command only for a distinct operation.

| Proposed operation | Contract |
|---|---|
| `bbs autopilot snapshot --json [--ticket ID]` | Read-only canonical state, route inputs, policy provenance, evidence validity, active attempt, and next obligations; does not initialize a missing ticket |
| `bbs autopilot context --json [--ticket ID] [--since CURSOR]` | Bounded view or delta of the snapshot and artifact index; no network access or model call |
| `bbs autopilot git-flow --json` | Typed effective policy and source per field; legacy shell output unchanged |
| `bbs autopilot recover --json` | Cold recovery using snapshot, active attempt and required artifact references; existing text output preserved |
| `bbs ticket readiness --json --action push\|land\|pr` | Evaluate fresh gates and policy for this exact operation; no mutation, no approval creation |
| `bbs autopilot attempt start --json-file FILE` | Persist an assignment with owner, scope, expected snapshot revision, and idempotency key before dispatch |
| `bbs autopilot attempt update --id ID --expect-revision N --json-file FILE` | Bind runtime handle or record waiting/result/failure; reject stale writers and forbidden transitions |
| `bbs autopilot attempt show --id ID --json` | Read durable lifecycle; a runtime adapter checks actual liveness separately |
| `bbs ticket set-evidence --kind verification --json-file FILE` | Accept version-2 gate evidence after schema and attempt checks; retain explicit legacy parsing |
| `bbs autopilot checkpoint ... --expect-revision N` | Record a milestone atomically; optional new concurrency argument, existing behavior retained for legacy runs |
| `bbs skill enter --name NAME --json` / `bbs skill exit --invocation ID --outcome STATUS` | Centralize local session/telemetry lifecycle; no update/install, network, git mutation, or pruning as a side effect |

New JSON envelopes use `schema_version`, `ok`, `data`, and `error`. Errors carry stable `code`, human `message`, `retryable`, and `details`. New commands: exit 0 for successful reads (including `ready:false`), 2 for usage/schema errors, 3 for identity/state conflicts, 4 for unavailable required runtime/tool capabilities, and 1 for other I/O failures. Callers must read `ready`; exit 0 is never release permission. Legacy commands keep historical exit codes.

Missing ticket is a valid snapshot (`ticket:null`, `mode:null`) for direct skills. Explicitly naming a nonexistent ticket is `TICKET_NOT_FOUND`. Conflicting environment/manifest/branch identity remains an error through the existing resolver. Required state cannot be read as “absent” when the actual cause is permission failure or malformed JSON.

## Snapshot and cursor

Logical shape (fields shown as type descriptions, not runnable JSON):

```text
schema_version: 2
snapshot_id: digest of relevant canonical observed state
state_revision: monotonic revision of this ticket's orchestration state
ticket: {id, project, canonical_repo, worktree, identity_source} | null
run: {id, workflow, workflow_digest, mode, control, stop_after} | null
git: {branch, head, tree_digest, base_ref, base_sha, dirty,
      upstream, remote_head, remote_observed_at, pushed_exactly}
policy: {effective, provenance, digest}
artifacts: [{role, path, digest, required_for, exists}]
gates: [{name, state, attempt_id, reason_codes, evidence_path}]
active_attempt: {id, gate, state, owner, runtime_handle} | null
obligations: [{kind, reason, required_artifacts, allowed_actions}]
```

Gate states: `missing`, `running`, `pass`, `fail`, `stale`, `limited`, `unknown`. `limited` describes insufficient scope/runtime access; it cannot become `pass` by dropping the limitation from prose. `pushed_exactly` compares recorded remote head and local head; remote-tracking refs have observation age and are not proof of a current network check. Fetch/remote verification remains an explicit authorized operation; finish rechecks the target.

Read ticket data under its lock, sample git and file digests, then confirm inputs have not changed. Retry one unstable sample; persistent churn returns `STATE_CHANGED`, not a coherent-looking mix. Do not hold ticket locks during network calls or tests. A later mutation checks its expected revision and recomputes safety-critical facts inside its appropriate lock/boundary.

`--since` uses an opaque versioned cursor scoped to ticket/run and contract version. Persist a small bounded cache of prior snapshot projections under the ticket cache; no unbounded history of packet contents. Unknown/expired cursors return a full packet with `reset_reason`. Cursors never authorize evidence reuse. Stable serialization and sorted entries make equivalent snapshots equal; wall-clock read time is excluded from the content digest.

## Durable layout

Extend the existing ticket directory rather than introducing a parallel database:

```text
tickets/<id>/
  index.json                         existing ticket metadata/pointers/control
  manifest.yaml                      existing repo/branch identity
  requirement.md, plan.md             complete human/model artifacts
  checkpoint.json                    additive version-2 orchestration fields
  handoffs/                          milestone findings, decisions, deviations
  attempts/<attempt-id>.json          new bounded assignment + lifecycle record
  evidence/verification/
    attempts/<attempt-id>.json        immutable completed typed gate evidence
    result.json                      existing compatibility/latest projection
  verdicts/<skill>.md                 existing human-readable status projection
  cache/context/                     disposable bounded snapshot views
  history.jsonl                      existing local audit stream
```

The checkpoint adds `schema_version`, `run_id`, `revision`, `active_attempt_id`, `last_completed_milestone`, and `workflow_digest`; preserve existing fields and unknown additive fields. This is a checkpoint of orchestration progress, not another copy of git or artifact truth. Keep decisions in existing handoff/analytics mechanisms; do not introduce a competing universal memory file.

Write the authoritative attempt/evidence record using temporary-file + atomic rename with checked errors. Update checkpoint and compatibility views under the ticket lock in a documented order. Since multiple file renames are not a transaction, publish immutable evidence first, then its accepted pointer/revision; recovery can rebuild derived Markdown/latest views. History/telemetry failures do not erase successful evidence. The evaluator ignores orphan evidence until its owning attempt has been accepted.

Version-2 readers preserve unknown optional fields, reject unsupported major versions for mutations, and fail closed on malformed required state. Validate IDs, relative paths, and symlink resolution. Evidence artifacts stay under the allowed ticket/worktree roots; external QA URLs are data, not file paths or commands.

## Evidence and readiness

Version-2 evidence extends the existing `verification` kind with a gate and provenance. Example:

```json
{
  "schema_version": 2,
  "ticket": "example-ticket",
  "run_id": "run-001",
  "attempt_id": "qa-002",
  "gate": "qa",
  "status": "DONE",
  "result": "PASS",
  "subject": {
    "repo_id": "canonical-repo-id",
    "base_sha": "FULL_BASE_SHA",
    "head_sha": "FULL_HEAD_SHA",
    "tree_digest": "sha256:CONTENT",
    "requirement_digest": "sha256:REQUIREMENT",
    "plan_digest": "sha256:PLAN",
    "policy_digest": "sha256:POLICY"
  },
  "producer": {
    "harness": "observed-harness",
    "model": null,
    "isolation": "fresh-native-context",
    "owner": "recorded-worker-handle"
  },
  "checks": [{
    "argv": ["go", "test", "./internal/cmd"],
    "cwd": "WORKTREE",
    "exit_code": 0,
    "log_path": "evidence/qa-002/go-test.log",
    "log_digest": "sha256:LOG",
    "acceptance_ids": ["AC-1"]
  }],
  "surface": {"kind": "local-test", "fingerprint": "sha256:INPUTS"},
  "unresolved_findings": [],
  "limitations": [],
  "started_at": "2026-09-10T00:00:00Z",
  "completed_at": "2026-09-10T00:01:00Z"
}
```

Uppercase digest placeholders make this illustrative, not valid fixture evidence. Production validates algorithms, complete SHAs, enums, timestamps, expected owner, and referenced artifacts. Optional plan absence is represented explicitly, not with a fake digest. Review checks may be structured findings/inspection artifacts instead of shell commands; the validator must not require a test command for a code-review finding. A QA fallback identifies its coverage and missing target; its admissibility is decided by the existing QA contract and effective policy, not merely by the existence of a log.

Readiness requires all action-specific gates to be accepted, fresh, and sufficiently evidenced; no unresolved material findings; valid current identity/control; and existing action authorization. `DONE_WITH_CONCERNS` is not automatically pass or fail: typed severity/limitations determine whether the gate meets policy. Preserve the existing handling of minor residuals; contradictory PASS plus failed checks is invalid. Legacy Markdown alone is historical information and cannot establish version-2 freshness.

Default invalidation is conservative: code tree, base, requirement, plan, relevant policy, or test-surface changes invalidate affected evidence. In the first release, invalidate both review and QA for any code change; selective reuse based on dependencies is deferred until a sound dependency map exists. A new commit with identical tree/base and unchanged other inputs may reuse evidence by content equality while retaining both SHAs in provenance. A changed base requires re-verification even if a file list looks similar.

The parent never changes a worker's failure into success. A repaired change creates a new attempt/evidence record. If review fixes code, QA runs on the result. If QA fixes code, review runs again, and QA must still cover the final resulting tree. Missing log/evidence, producer mismatch, stale generation, or unknown runtime surface prevents acceptance.

Hooks, `ticket land`, PR creation, and autopilot close-out consume this same evaluator. Hook adapters translate reasons into their native allow/ask/deny format. Direct CLI actions still enforce readiness when no hook is installed. Existing explicit approval mechanisms remain separate from evidence; an override must be durable, scoped, and visible, never inferred from a timed-out wait or tool exit 0.

## Attempts and crash recovery

States: `prepared → running → completed | failed | cancelled`; `running ↔ waiting` is allowed. Terminal records are immutable. `completed` means a worker produced a result, not that the gate passed; acceptance is a separate recorded outcome after validation.

The assignment records ticket/run/repo paths, skill path and digest, required artifact paths/digests, base and initial tree, acceptance IDs, edit scope, available check commands, requested capabilities, actual harness/model when known, owner, resource lease, and prohibited git/close-out operations. It passes facts and requirements, not the producer's self-assessment, to an independent reviewer.

Persist `prepared` before dispatch. Use the attempt ID as a dispatch idempotency key when the transport supports it; bind the returned handle before marking `running`. If dispatch acknowledgement is lost, reconcile by that key. When the transport cannot find or deduplicate it, report `dispatch_unknown` and inspect the process/checkout before dispatching again. Exactly-once execution cannot be promised across an arbitrary external spawn API.

On resume: inspect the recorded handle and lease, consume completed results, wait for a live worker, or mark a proven-dead worker failed and retry with a new ID. A missing native handle in a new session is unknown, not proof of death. Never let two mutating workers operate on the same checkout. PID alone is insufficient after reuse; pair it with process start identity or a transport session ID.

Respect pause/cancel before new dispatch and finish. Cancel through the actual adapter, confirm termination, then release the lease; do not release while a writer is still alive. Lease expiry requires liveness reconciliation before takeover. Retry a recoverable attempt with a changed hypothesis/capability at most twice; repeated identical failure becomes BLOCKED with evidence. Approval waits and legitimately running checks are exempt from failure counters.
