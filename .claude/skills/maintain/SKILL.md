---
name: maintain
description: Keep a mature system secure, reliable, and efficient at scale. Use for security and dependency audits, reliability hardening, db/query performance (schema, indexes, partitioning, caching, batching, async processing), and architecture reviews under change or scale pressure.
---
# maintain
The Maintainer archetype (see `../references/archetypes.md`). Audit a
shipping system through one lens per run — **security**, **dependencies**,
**reliability**, **scale/performance** (db schema, indexes, partitioning,
caching, batching, async/background processing), or **architecture**
(structure that can't absorb the coming change or scale) — using the repo's
own tooling (audit/lint/scanner, logs, slow queries). Rank findings by
severity × likelihood, fix the top one with the smallest safe reversible
change, verify it closes the finding without regressing, and leave the rest
as a prioritized backlog. Read-only audit is a valid, complete run.
A symptom without a clear cause routes through the `investigate` skill (via
the Skill tool) before hardening — don't fix blind. Never weaken a control
to make something pass (no disabling auth, no `--force`, no silencing a
scanner) — surface the tradeoff. Never invent CVEs, advisories, or metrics:
cite the tool output or mark the finding unverified. A risky upgrade,
breaking change, or restructure lands as a designed proposal promoted via
`plan-draft`/`builder`, not an in-place edit; small reversible architecture
fixes (extract a background job, add a cache behind the existing interface)
are fine. Pure behavior-preserving cleanup routes to `sweep`.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: AUDITED | HARDENED
SUMMARY: <lens + top finding + fix + verification>
NEXT: triage backlog, review-pr, then human review and create-pr
```
