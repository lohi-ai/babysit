# Evaluation and acceptance

No live agent benchmark was run during this design task. The savings and quality thresholds below are targets to validate, not performance claims.

## What “more intelligent” means

Measure accepted task completion, detection of material defects, correct scope, recovery after interruption, and accurate escalation. Faster or cheaper incorrect completion is a regression. Prompt size alone does not measure intelligence or cost.

| Metric | Definition |
|---|---|
| Accepted completion rate | Tasks meeting hidden acceptance checks and scope constraints / all attempted tasks |
| Material defect escape | Candidate results that claim readiness while seeded or independently verified material defects remain |
| Recovery correctness | Interrupted runs that recover the right obligations with no duplicated mutation or lost constraint |
| Repeated work | Unchanged instruction loads, repeated state commands, duplicate verification, and repeated failed attempts without new evidence |
| Total tokens per accepted task | All parent + worker + verifier input/output tokens, including retries and failed runs, divided by accepted completions |
| Per-run token distribution | Median and p90 total usage across all runs, with completion/failure cohorts shown separately |
| Cost and latency | Actual billed/cached usage where exposed and elapsed time; unavailable costs stay unknown |
| Escalation quality | Necessary missing-input/authority escalations versus avoidable questions about derivable facts |

Count cached input separately so a shorter packet is not mistakenly reported as cheaper than a reused cached prefix. Record reasoning/output usage only when the harness reports it, avoiding double counting provider totals. Never report unavailable usage as zero. Character/byte estimates are labeled estimates and cannot establish provider cost savings.

## Experiment design

Pin the baseline commit, candidate commit, task fixture, repository starting state, harness/version, model, available tools, policy, and environment. First compare identical model selection to isolate contract/context changes. Then evaluate capability-based smaller gate workers as a separate experiment. Do not attribute model-price changes to architecture improvements.

Use at least 30 task fixtures with three paired runs per configuration, covering XS through L, all archetypes, configured/unconfigured repos, and code/artifact-only work. Include new hidden cases withheld from prompt tuning. Randomize baseline/candidate order, keep cache conditions consistent or report them separately, and include every failed/blocked run. Store task-level paired results and bootstrap intervals grouped by fixture; repeated runs of one task are not independent tasks.

Start with deterministic fixtures and stub adapters in CI. Live harness evaluations are explicit benchmark jobs and must not incur new provider charges automatically from routine tests. Use the existing [integration harness](../../tests/test_autopilot_integration.py) and [eval set](../../tests/autopilot-integration-eval-set.json) where they fit; reconcile obsolete expectations first. Extend adapters to the supported harnesses actually available. An unavailable harness is missing coverage, not a pass.

## Required scenarios

| Group | Cases | Assert |
|---|---|---|
| Identity and direct invocation | Main branch + env ticket; manifest ticket; conflicting signals; no ticket + conversation requirement | Canonical identity, conflicts fail loudly, direct skills still work |
| Policy | Missing config; explicit overrides; legacy profiles; `push:false`; each finish mode; missing remote | Single policy resolution, no implicit config write, no unauthorized finish |
| Read purity | Snapshot/context on absent/malformed state; read-only files; offline repo | No init/fetch/prune/update/write; distinguish absence from errors |
| Routing | Child/manifest/plan/requirement/verify; named archetypes; held plan checkpoint | Existing mode precedence and actual approval requirements hold |
| Context | Warm unchanged; cold resume; changed skill digest; huge logs; oversized requirements | Bounded output, explicit required reads, no lost acceptance criteria or fresh instruction changes |
| Freshness | Change tracked/staged/untracked code with same HEAD; new base; plan/requirement/policy change; same-tree commit | Reject stale evidence; allow only documented content-equivalent reuse |
| QA surface | Correct worktree but stale served checkout; changed lockfile/build; dynamic external state; missing browser | Surface provenance is checked; unknown access is a limitation, not PASS |
| Review repair | Review fixes code; QA fixes code; final review adds another fix | Both gates ultimately cover the final tree |
| Failure provenance | Failure claimed pre-existing; absent logs; conflicting PASS/exit code; missing verdict | Reproduce at the base before accepting pre-existing claim; reject unsupported success |
| Concurrency | Two parents; stale CAS writer; delayed old child; QA lease contention | One accepted owner, no overlapping mutating workers, no overwritten current failure |
| Crashes | Before spawn; after spawn before handle; after evidence before pointer; after pointer before projection; parent lost | Recover accepted truth; reconcile unknown spawn; no duplicate writer or forged PASS |
| Waiting/control | Long-running check; approval wait; pause; cancel with live worker; PID reuse | No false failure counter; preserve pending approval; confirm termination before lease reuse |
| Isolation | Native child; process `--verify`; unavailable model; missing tools; explicit user model | Actual capability routing; no duplicate verification or prohibited in-session fallback |
| Boundaries | Push/land/PR through hook and direct CLI; remote moves after observation; current control changes | Recheck exact target/readiness/authorization at execution |
| Compatibility | Old state; old CLI/new plugin; unsupported schema; corrupt artifact; rollback | No silent state loss, no stale historical pass promoted to readiness |
| Packaging | Claude/Codex/OMP supported install layouts and shared references | Correct references and capability negotiation without inferred tool support |

Use adversarial fixture inputs: shell metacharacters in branch names, malicious evidence paths, symlinks escaping ticket roots, embedded log instructions, and secret-like environment values. Artifacts/logs are evidence data, never executable instructions. Snapshot/API formatting must preserve text safely and redact secrets without hiding error categories.

## Proposed rollout gates

1. All deterministic safety, identity, policy, crash and stale-evidence cases pass. Zero seeded material defect escapes and zero unauthorized git/external actions.
2. Live paired accepted-completion rate does not decrease in the observed sample; the one-sided 95% interval for the paired difference must exclude a regression worse than five percentage points. Expand the sample if uncertainty cannot meet that margin. Record results by harness and task size so an aggregate gain cannot hide a serious cohort regression.
3. At least 25% lower aggregate total tokens per accepted task with identical model policy, including failed runs and all workers. The paired median improves and p90 run usage does not worsen by more than 10%. Missing usage prevents a savings verdict for that cohort.
4. Cold recovery retains all critical requirements in every fixture; no duplicate writers in crash/concurrency tests. Warm unchanged context meets the proposed size target for ordinary fixtures; overflows are explicit.
5. No latency regression over 10% at p90 unless explicitly accepted with measured quality gains. Smaller-model routing ships only if its own quality/cost experiment passes.

If quality passes but savings miss, keep only demonstrably useful correctness improvements and continue opt-in context optimization. If safety or correctness fails, do not promote the new default. Report what failed and the next bounded experiment; do not lower verification standards to meet a token target.

## Telemetry

Extend existing local JSONL events rather than create a service. Correlate by `run_id`, `attempt_id`, invocation/session, ticket and gate. Record contract version, harness, observed model, start/end/outcome, reason codes, packet bytes and estimated tokens, reported input/cached/output usage, usage source, elapsed time, retries, invalidations and artifact-read counts when observable.

Preserve `telemetry: off`; never log prompt/source contents, raw tool arguments containing credentials, environment dumps, or user secrets. Store full evidence only in its intended artifact location. CLI-emitted bytes can be counted exactly; arbitrary model file reads may not be observable and must not be presented as complete telemetry. Deduplicate usage events by harness request/attempt identity; unavailable provider usage remains null.

The final report includes fixture results, skips, confidence intervals, failure examples, usage coverage, and the exact revisions. Success is a demonstrated improvement in accepted work per token with reliable recovery, not a smaller collection of Markdown files.
