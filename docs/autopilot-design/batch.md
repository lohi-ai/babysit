# Implementation batch

Foreman: `fm-autopilot-v2`. User authorized implementation with Codex `gpt-5.6-sol` or `gpt-5.6-terra` workers. Source design: this directory. Preserve the dirty primary checkout, including existing autopilot and builder edits. No version bump, release, primary reset, or force push.

Orca run: `run_0f2b56fbc637`. Reconcile current handles by title after app restart.

| Lane | Ticket | Model | Initial terminal handle | Orca task |
|---|---|---|---|---|
| core | bs-hoh7dldj | gpt-5.6-sol | term_711d53ef-8371-47bb-a290-5a075dd61f80 | task_4ce7caf6c20b |
| evaluation | bs-eg9oyvdo | gpt-5.6-terra | term_9495ade0-636d-4393-ad76-04db88d7873e | task_e7c2d179c5e5 |
| integration | bs-zak7w7b3 | gpt-5.6-terra | term_2cc962fa-671c-4758-bf60-f5e2446f66c2 | task_dbcca70b3ab3 |

Review status: the user explicitly approved ALL three plans and requested fresh autopilot agents to implement. Approval is recorded on each ticket. Do not request the same plan approval again. Existing planning agents finished without implementation; replacement agents own execution. Each worktree fetched a newer remote base than the design baseline; evaluation records the actual full SHA.

Execution coordination: evaluation writes `handoffs/baseline-ready.md` in its ticket home with the AP-01 commit SHA as soon as it is verified; core reads that artifact and merges the baseline commit into its own branch. Core writes `handoffs/core-contract-ready.md` after AP-02 and `handoffs/core-ready.md` after AP-05, with tested SHAs and exact CLI contract. Integration uses these to start caller work and writes `handoffs/integration-ready.md` with the final verified integrated SHA. Evaluation then merges core and integration commits into its own branch for AP-08. Each milestone lists scope, commands, results, and unresolved concerns. These durable files permit progress without requiring a coordinator message for every dependency. Never treat absence as completion; wait with periodic liveness checks, and report a real blocker if an upstream worker fails.

## Worker ownership

- **core — Sol:** AP-02, AP-03, AP-04, AP-05. Own CLI/shared Go code, hooks/readiness enforcement, and focused Go tests. Deliver contracts incrementally; notify coordinator when the first contract is ready. Reuse existing stores/resolvers. New behavior opt-in; legacy behavior remains compatible.
- **evaluation — Terra:** AP-01 and AP-08. Own baseline fixture audit, standalone benchmark/evaluation tools and their tests, results documentation. Begin baseline now without waiting for core. After core/integration commits are available, integrate them in your worktree and run final combined evaluation. No fabricated live measurements or unapproved provider purchases.
- **integration — Terra:** AP-06 and AP-07. Own skill/workflow/preamble migration, packaging, dashboard/foreman consumers and operator documentation, plus their integration tests. Begin inventory and compatibility plan now; wait for the core contract before shipping callers. Bring the primary's existing autopilot/builder modifications into your worktree without dropping their behavior. Copy the accepted design package into the final branch so it is durable in git.

Each worker uses babysit `--mode=worktree` isolation and initially stops after a scoped plan for coordinator review. Local tests/QA run in the ticket worktree: the primary has unrelated dirty changes and must not be reset or composed onto. A missing runnable browser target uses the real skill's named local fallback. Each worker owns its commits, persisted review/QA verdicts, and checkpoints; follow the repository finish policy and leave primary landing to the human.

Dependencies: AP-01 establishes baseline independently; core may survey/plan concurrently and then implement. Integration starts caller edits after a published core contract. Final evaluation waits for both core and integration, verifies the combined tree, and reports unmeasured live-cost targets honestly. Workers exchange commit IDs through the coordinator and merge dependency commits only into their own worktrees.

Report ticket ID, absolute worktree and plan paths at the first checkpoint. Assign the ticket to `fm-autopilot-v2`. Do not recursively dispatch worker swarms; sequential native review/QA workers are permitted only with the user's requested models and actual available capabilities. Missing tools are reported, not guessed.

Next: coordinator reviews scoped plans, resolves the existing approval-record gate with named evidence, and resumes workers. Completion requires the final integrated branch and evidence, not three unrelated partial branches.
