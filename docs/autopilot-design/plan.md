# Plan
**Goal:** Improve autonomous completion and reduce total tokens per accepted change across babysit; measure both before changing defaults.
**Scope:** L, decomposed into eight implementation slices in [delivery.md](delivery.md); this change delivers the design only.
**Out of scope:** New hosted services, a database, automatic provider purchases, a dashboard redesign, and implementation in this planning change.
**Approach:** Keep the model's free-form work loop; move deterministic state collection, gate evaluation, evidence freshness, and recovery into reusable Go functions behind the existing CLI.
Expose bounded context packets and delta reads; retain complete source artifacts and explicit required reading. Extend existing ticket storage and verification evidence instead of introducing another state system.
Use shared contracts across git-flow, hooks, workflows, skills, native workers, process verifiers, foreman, and dashboard readers; stage rollout with old-command compatibility.
**Unknowns:** *Derived:* prose and CLI disagree on unconfigured profile/identity; preserve resolver behavior and reconcile consumers ([architecture.md](architecture.md#observed-baseline)).
*Derived:* current verdict status and QA rubric parsing do not prove revision freshness; migration must distinguish historical status from release readiness ([contracts.md](contracts.md#evidence-and-readiness)).
*Derived:* harness usage/capability coverage and real token baseline are unmeasured; do not make savings claims or mandatory model changes before [evaluation.md](evaluation.md).
**Verify:** Documentation links, examples, and source claims now; implementation acceptance commands and crash/concurrency scenarios are specified in [delivery.md](delivery.md) and [evaluation.md](evaluation.md).
**Next:** Review the technical design, then implement AP-01 through AP-08 in dependency order.
