---
workflow: maintainer
version: 1
description: Keep a mature system secure, reliable, fast, and efficient. Audit through one lens (security, dependencies, reliability, scale/performance, architecture) and apply the smallest safe fix; also the home for root-cause bug fixes.
needs-state:
  branch_pushed: optional
---
# maintainer
The Maintainer archetype (see `../references/archetypes.md`). Use to keep a
production system safe as it scales. Two modes, chosen from the invocation:

| Mode | When | Skill |
|------|------|-------|
| **audit** | "is this secure / current / reliable / cheap at scale?" | `maintain` |
| **fix** | a specific bug, regression, flake, or crash is reported | `investigate` |
## run
> produces: verdict:maintainer
1. Pick the mode. For **audit**, resolve the lens (security, dependencies,
   reliability, scale/performance — schema/indexes/partitioning, caching,
   batching, async background processing — or architecture for change/scale
   pressure). For **fix**, ensure a fix ticket/branch exists.
2. **audit:** run `maintain` — audit through the lens with the repo's tooling,
   then fix the top finding with the smallest safe change. A pure audit (no safe
   fix) is a valid run. **fix:** run `investigate` to root-cause, then apply the
   smallest fix via `implement` with a regression check.
3. If code changed, in the current autopilot session run `review-pr --fix` and
   then `qa` under its gate policy. Persist and read back both verdicts, apply the
   **Repair until the final change passes** loop, and require
   `bbs ticket readiness --action review --json` to report `data.ready=true`
   for the final committed tree. Persist with
   `bbs ticket set-verdict --skill review-pr` and
   `bbs ticket set-verdict --skill qa`. A pure audit records its commands and
   evidence but does not manufacture code gates.
4. Commit any fix locally. Autopilot never pushes, lands, or opens a PR —
   close-out is the human's `create-pr` (or the dispatching foreman's).
5. Write a handoff: mode + lens/root-cause, fix, verification, and the remaining
   prioritized backlog. When the audit surfaced structural cruft (not a scale
   fix), name a `sweeper` pass as the follow-up — don't fold it into this run.
   Route to `builder` only when the evidence requires a separately designed
   structural change; route to `grower` when analysis identifies a product
   experiment. Otherwise stop after the maintained outcome.
**Stop conditions**

- `NEEDS_CONTEXT`: the lens/bug cannot be determined, or the only fix needs a
  human risk decision (breaking upgrade, control change).
- `BLOCKED`: the audit tooling cannot run, or a fix regresses and cannot be made
  safe.
**Final status**
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: AUDITED | HARDENED | FIXED
SUMMARY: <mode + top finding/root-cause + fix/verification + backlog size>
LIFECYCLE: maintainer -> sweeper | builder | grower | stop
TRIGGER: <characterized cruft, structural requirement, product experiment, or no promoted work>
NEXT: if AUDITED, triage the backlog; if code changed, human review then /bbs:create-pr
```
