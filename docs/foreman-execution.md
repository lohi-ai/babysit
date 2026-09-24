# Verified Foreman projects

Foreman now has a shared project evaluator behind its CLI and the dashboard's
**Project** tab. It checks the accepted scope, child verification, integrated
product, delivery and cleanup before issuing a completion receipt. A heartbeat
or a worker's `DONE` message cannot issue that receipt.

## Start and resume

Use `/bbs:foreman` for a project. The existing parent design checkpoint remains;
`--auto` delegates that review. Foreman writes `project.json` before approval,
binds each accepted seed to a child, and uses v2 Autopilot evidence for code work.
Changes to the accepted contract invalidate its approval and integrated evidence.

```sh
bbs foreman snapshot PARENT --json
bbs foreman readiness PARENT --action dispatch --json
bbs foreman readiness PARENT --action finish --json
```

Read `data.ready` and the reasons in the response. A zero exit status means the
read succeeded; it does not mean the project is ready. Snapshot reads do not
change ticket state. Open the parent ticket in `bbs dashboard --server` to see the
same criteria, blockers, worker progress and delivery observations.

The full contract, evidence and progress formats are in the
[execution reference](../.claude/skills/foreman/references/execution.md).
Workers use the [verification producer](../.claude/skills/autopilot/references/verification.md)
to capture the current revision before a check and archive its actual results.
Do not convert old verdict prose into typed evidence: migrate legacy child
checkpoints using the supported Autopilot migration command and rerun the gates.

## Delivery and recovery

- Seal each code child while its verified checkout still exists. The seal binds
  actual commits, policy, accepted review/QA attempts and archived logs. Removed
  worktrees do not erase that proof. Changed source refs or logs invalidate it.
- One code child owns one repository. Split cross-repository work into dependent
  children; final verification names every affected repository surface.
- Final integration runs against the delivered local base for `land`, or a
  retained project QA branch for `review`/`pr`. Product review uses a separate
  evaluator identity. Capture the tested runtime revision for preview links.
- `bbs foreman complete PARENT --foreman ID` checks readiness and cleanup again,
  then writes the receipt. Review-ready branches and open PRs remain `in_review`.
  Local landing and remote merge are reported separately.
- For documentation/research projects, explicitly approve `artifacts_only` and
  seal the child workflow's durable outputs. Production commits cannot use this
  shortcut.

Structured progress advances when recorded evidence changes. Wait reports need a
reason and an expiry within 15 minutes. Terminal output, a recent heartbeat and a
reported wait are separate observations; none proves that an agent is alive or
authorizes reclaiming its resources. A project deadline stops new starts while
allowing already verified work to finish.

## Evidence and limits

Regression fixtures exercise false completion, stale scope/revisions/logs,
missing coverage, failed checks, crash retries, cleaned worktrees, artifacts-only
delivery, watcher behavior and API parity. Browser QA covers the actual dashboard
with deterministic local ticket data, including failure/retry and mobile layout.

These checks establish the execution contract. They do not establish a live model
quality or cost improvement. The independent product evaluator is a Builder
pilot; compare representative projects before expanding it. Track accepted
completion, escaped defects, interventions, repeated work, recovery, time to the
first usable journey and provider usage coverage. Missing usage remains unknown.
Orca continues to own agent dispatch; no new scheduler or service is introduced.
