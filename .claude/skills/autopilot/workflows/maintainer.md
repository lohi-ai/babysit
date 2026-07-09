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
> produces: verdict:maintainer + qa:checked
1. Pick the mode. For **audit**, resolve the lens (security, dependencies,
   reliability, scale/performance — schema/indexes/partitioning, caching,
   batching, async background processing — or architecture for change/scale
   pressure). For **fix**, ensure a fix ticket/branch exists.
2. **audit:** run `maintain` — audit through the lens with the repo's tooling,
   then fix the top finding with the smallest safe change. A pure audit (no safe
   fix) is a valid run. **fix:** run `investigate` to root-cause, then apply the
   smallest fix via `implement` with a regression check.
3. If code changed, run `review-pr` and fix mechanical findings.
4. Run `qa` (or the strongest fallback) to confirm no regression. Persist the
   verdict with `bbs-ticket set-verdict --skill qa`.
5. Commit and push any fix when policy allows.
6. Write a handoff: mode + lens/root-cause, fix, verification, and the remaining
   prioritized backlog. When the audit surfaced structural cruft (not a scale
   fix), name a `sweeper` pass as the follow-up — don't fold it into this run.
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
NEXT: triage backlog; human review, then /bbs:create-pr
```
