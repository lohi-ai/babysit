# Autopilot v2 evaluation baseline

AP-01 baseline recorded on `fc6e700dc18d901a09bbdcd49816c4612ef8e51d` in
the evaluation worktree. This is an audit of the existing fixture suite, not
a claim about Autopilot v2 performance.

Run the reproducible local audit with:

```bash
python3 tests/ticket-system/run_autopilot_v2_eval.py \
  --eval-set tests/autopilot-integration-eval-set.json \
  --repo-root . --run-local
```

The evaluator reports the exact revision, fixture classifications, local test
result, legacy-oracle rows, and live-usage availability. It never starts a
harness, invokes a provider, or reports missing usage as zero.

## Fixture audit

The fixture set has 28 rows:

- 8 binary rows run locally through `bbs autopilot`.
- 19 prompt rows have deterministic wiring coverage without a live harness.
- `P14-verify-post-miss` has no deterministic execution path and is recorded
  as local coverage unavailable.
- 20 prompt rows are eligible for a paired live harness run, but none were run
  for AP-01. Provider usage and cost are therefore `null`, not zero.

The local baseline exposed one existing failure:
`P20-explain-branch-derived` resolves `branch: unknown` in the ticket fixture,
so its expected branch string is absent. The differential oracle suite still
passes all 18 checks. This is evidence for AP-02 identity/snapshot work, not a
runtime change in AP-01.

## Accepted-contract mapping

Do not use aggregate legacy-fixture passes as v2 rollout evidence until these
rows receive versioned assertions:

| Row | Historical assumption | Accepted design treatment |
| --- | --- | --- |
| P2 | A dirty tree blocks inline work. | Retain the current branch unless explicit isolation is requested; the batch protects the dirty primary through worktree isolation. |
| P4 | A planned builder invocation without a plan blocks. | Builder's build mode drafts a plan when a requirement exists. |
| P5 | A plan-draft verdict alone represents approval. | Plan existence/completion and the required approval record are separate facts. |

The baseline also records the documented default-profile discrepancy: legacy
builder bootstrap writes `startup`, whereas v2 uses the existing resolver's
`pet` default and must not rewrite configuration while reading state.

## AP-08 follow-up

After `core-contract-ready.md`, `core-ready.md`, and `integration-ready.md`
are present with verified commits, merge those commits only into this
worktree, rerun the command above plus the combined-tree suites, and append
the candidate revision, quality failures, usage coverage, and rollout
recommendation. A live paired benchmark remains unavailable until explicitly
authorized and supported harness usage is observable.
