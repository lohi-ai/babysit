# Delivery and migration

This is L implementation scope. AP identifiers below are proposed slices, not created tickets. Keep the parent plan at L: these slices comprise the requested change rather than deferred scope. Existing autopilot/builder working-tree edits must be reconciled by their owner before implementation; do not overwrite them from this design.

## Ordered slices

| Slice | Depends on | Deliverable and likely touchpoints | Independent acceptance |
|---|---|---|---|
| AP-01: baseline and policy decisions | — | Reconcile fixture expectations; baseline runner/results; document canonical identity/default-profile behavior. `tests/autopilot-integration-*`, git-flow and identity tests | Baseline identifies skipped/unavailable harness runs, reproduces named failures, records policy decisions; no runtime behavior change |
| AP-02: typed observation | AP-01 | Shared read-only snapshot, typed git-flow/provenance, common route facts. `internal/cmd/autopilot.go`, `gitflow.go`, `internal/identity`, `internal/ticket` | Snapshot causes no filesystem/git/network mutation; correct dirty/no-ticket/offline behavior; remote-exists differs from pushed-exactly |
| AP-03: evidence and readiness | AP-02 | Typed immutable attempt evidence, common readiness evaluator, legacy projections. `ticket_verdict.go`, `ticket_handoff.go`, `ticket_baseops.go`, hooks and PR gates | Stale code/base/requirements and contradictory evidence refuse readiness; land/PR/hooks agree; minor valid concerns retain intended semantics |
| AP-04: attempts and recovery | AP-03 | Durable assignments, lifecycle/CAS, native and process adapter reconciliation. `autopilot_spawn*.go`, `autopilot_reviewgate.go`, `internal/agent`, checkpoint and leases | Crash before/after dispatch or acceptance never creates duplicate writers or accepts an old verdict; pause/cancel and independent-verifier routing work |
| AP-05: bounded context and runtime entry | AP-02, AP-04 | Context/delta views, artifact digests, skill entry/exit, local usage fields, safe log excerpts | Cold recovery matches full-context obligations; warm unchanged output is bounded; mandatory instructions and critical constraints remain available |
| AP-06: skill/workflow conversion | AP-03, AP-04, AP-05 | Builder first, then all archetypes, preamble, ADF links, handoff and authoring references; preserve specialist reasoning | Direct skills accept conversation context/no ticket; composed skills use packet facts; no duplicate gates; every stop point and finish policy remains testable |
| AP-07: consumer and distribution parity | AP-06 | Foreman/dashboard readers, hook adapters, install/plugin packaging, operator docs | Same ticket has the same readiness across surfaces; old CLI/new plugin mismatch is explicit; cache paths/version support verified |
| AP-08: paired evaluation and default rollout | AP-07 | Full scenario results, quality/token report, documented activation and rollback | Meets [evaluation.md](evaluation.md) thresholds; no claimed savings when usage is unavailable; release only after relevant existing suites pass |

Implement serially through correctness boundaries. Independent test-fixture/documentation work may be split later if the user or workflow authorizes workers; this design does not dispatch them. AP-05 can begin its read-only formatting after AP-02, but cannot ship before attempt recovery is correct.

## Compatibility policy

1. Add explicit contract-version discovery before changing skill callers. Keep legacy shell output, Python-style pointer rendering, legacy status parsing, and existing command exit codes intact. New JSON contracts use native booleans and documented enums.
2. Add new readers and shadow evaluation first. Compare old and new readiness without letting the shadow verdict authorize or block real operations. Record semantic mismatches locally without logging sensitive bodies.
3. Activate version-2 behavior per newly started run through a documented experimental opt-in. Persist the contract version on the run; resume cannot switch it silently. The exact flag/config spelling is chosen in AP-02 and documented in help before use.
4. Legacy checkpoints remain readable. An explicit migration creates a backup, translates fields, preserves unknown fields, and marks historical evidence as lacking freshness. Reverify before version-2 release. No migration invents an attempt, SHA, approval, or measured token usage.
5. Do not allow legacy writers to overwrite a version-2 active run. A version guard returns an actionable compatibility error. Older binaries that cannot honor that guard must not own those runs; installer/operator guidance must make this limitation explicit.
6. After parity and evaluation, make the new contract the default for new runs. Keep compatibility readers for old history; remove duplicated runtime prose only after every supported consumer has moved.

Rollback disables version-2 for new runs and retains all evidence/artifacts. A version-2 run must finish with a compatible binary or be explicitly exported to a legacy checkpoint with current verification invalidated. Never downgrade by deleting state or reinterpreting unknown evidence as PASS. If readiness enforcement is faulty, pause affected release operations while correcting it; bypassing the guard is not rollback.

## Explicit semantic choices

| Area | Proposed resolution | Why / migration impact |
|---|---|---|
| No `.babysit/git-flow.yaml` | Use the existing resolver's `pet` defaults; stop builder's implicit `startup` write | Restores one source of policy truth; removes an implicit behavioral change during bootstrap |
| Git isolation | Retain current branch unless explicit run isolation or supported legacy policy requests otherwise | Preserve `6a54185` behavior and existing aliases; no automatic branch reshaping |
| Finish | Retain `review` default and existing durable `land`/`pr` authorization | Token reduction does not grant publication authority |
| Approval semantics | Track plan existence/completion separately from required approval records | `probeState` currently derives plan approval from a verdict; held checkpoints require their actual decision record |
| Verification freshness | New runs require typed identity; old Markdown remains readable history | Prevent reusing stale PASS without pretending old records contain missing provenance |
| Model choice | Resolve available capabilities and explicit user preferences; measure suitable smaller workers | No assumed model names, prices, or mandatory vendor dependency |
| Preamble | Centralize mechanics; preserve ADF, no-ticket fallback and escalation delivery | Reduce repeated shell/context while retaining autonomy rules |
| Plan length | Short `plan.md` links to deep contracts when necessary | Avoid forcing architecture details into a tiny summary or loading all detail for every step |

Removing implicit `startup` seeding can change QA breadth for unconfigured repos. Call this out in release notes, test it explicitly, and recommend an explicit profile where users want startup rigor. Do not silently change existing configured repositories.

## Implementation verification

Use the repository's existing test entry points; below are real current commands to rerun after applicable slices, not checks executed by this planning change:

```bash
go test ./internal/cmd ./internal/identity ./internal/ticket ./internal/agent
bash tests/test_bbs_git_flow_profile.sh
bash tests/test_bbs_ticket_verdict.sh
bash tests/test_qa_evidence.sh
bash tests/test_autopilot_checkpoint_refresh.sh
bash tests/test_pre_tool_gate_resolve.sh
bash tests/test_bbs_ticket_land.sh
bash tests/test_bbs_ticket_qa_lease.sh
python -m pytest tests/test_autopilot_integration.py -q
python -m pytest tests/test_hooks_portability.py tests/test_codex_plugin.py -q
```

At integration, run `go test ./...` and the relevant CI shell suites from [.github/workflows/test.yml](../../.github/workflows/test.yml). Build the current checkout's binary for shell tests; do not accidentally test a globally installed older `bbs`. Isolate `BABYSIT_HOME` and test fixtures; tests that require an OS home must use their established sandbox guard and never modify actual trust/credential records.

Extend tests with the new cases in [evaluation.md](evaluation.md), including schema compatibility, races, stale evidence and counterexamples. Existing differential suites preserve legacy behavior; intentional changes use versioned tests and explicit migration expectations rather than blindly editing the oracle.

No version bump is needed for this design package. When implementation is released, synchronize `VERSION`, both marketplace version fields, and the Codex plugin version per repository policy.

## Handoff to implementation

Each slice should leave a short requirement with acceptance IDs, a linked plan, implemented evidence, and unresolved deviations. Parent integration must test the final combined tree, even when each slice has passed separately. AP-08 owns the final report: baseline versus candidate, quality failures, usage coverage, token distribution, recovery outcomes, and rollout recommendation.

Next: review this design and implement AP-01; do not start with a pack-wide prompt rewrite.
