---
name: maintain
description: Keep a mature system secure, reliable, and efficient at scale. Use for security and dependency audits, reliability hardening, db/query performance (schema, indexes, partitioning, caching, batching, async processing), and architecture reviews under change or scale pressure.
---

# maintain

The Maintainer archetype (see `../references/archetypes.md`). Own a system that
already ships and keep it safe as it scales — audit first, then apply the
smallest hardening fix.

## Flow

1. Pick the lens for this run: **security** (vulns, secrets, authz),
   **dependencies** (outdated, deprecated, advisory-flagged), **reliability**
   (error handling, retries, timeouts, idempotency), **scale/performance**
   (N+1s, unbounded queries, hot-path allocation; db schema, table indexes,
   partitioning, caching, batching, async/background processing for work that
   shouldn't block a request), or **architecture** (structure that can't
   absorb the coming change or scale — a sync path that needs a queue, a data
   model blocking growth, a boundary that every change cuts across).
2. Audit through that lens using the repo's own tooling (audit/lint/scanner,
   logs, slow queries). Report findings ranked by severity × likelihood.
3. Fix the top issue with the smallest safe change. Prefer the established
   pattern; never widen scope into a refactor.
4. Verify the fix closes the finding without regressing — re-run the audit,
   tests, and typecheck.
5. List the remaining findings as a prioritized backlog for the human.

## Rules

- Read-only audit is a valid, complete run — propose fixes, do not force them.
- When a finding is a symptom without a clear cause (intermittent failure,
  unexplained regression, a timeout you can't yet trace to a mechanism), get
  root cause before hardening — don't fix blind. Invoke the `investigate`
  skill via the Skill tool (skill: `investigate`, or `bbs:investigate` as
  listed under the plugin), passing the symptom and the
  audit evidence as args, then apply the smallest fix once the cause is known.
- Never weaken a control to make something pass (no disabling auth, no
  `--force`, no silencing a scanner). Surface the tradeoff instead.
- Bounded blast radius — no destructive migrations, no force-push, no dropping
  data. Reversible changes only.
- Do not invent CVEs, advisories, or metrics. Cite the tool/output, or mark a
  finding as unverified.
- A risky upgrade or breaking change is a `plan-draft` decision, not an
  in-place edit. Same for architecture findings: apply an architecture fix
  in-place only when it is small and reversible (extract a background job,
  add a cache layer behind the existing interface); a restructure lands as a
  designed proposal the human promotes via `builder`.
- Architecture is not Sweeper work: this lens changes structure to absorb
  scale/change (behavior/timing may shift, verified by `qa`); pure
  behavior-preserving cleanup routes to `sweep`.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: AUDITED | HARDENED
SUMMARY: <lens + top finding + fix + verification>
NEXT: triage backlog, review-pr, then human review and create-pr
```
