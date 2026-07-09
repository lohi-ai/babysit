---
name: analytics-review
description: Maintainer pass over babysit telemetry. Use to turn ~/.babysit/analytics (skill-usage.jsonl, decisions.jsonl) into a short ticket-ready report — which skills fire and fail, which Taste decisions repeat, where runs go BLOCKED.
---
# analytics-review
The Maintainer archetype pointed at babysit itself. Telemetry is the pack's
primary feedback channel; this skill closes the loop by reading it and emitting
tickets, not dashboards. Read-only — it never edits the pack in the same run.
## Flow
1. Locate the data: `A="${BABYSIT_ANALYTICS_DIR:-$HOME/.babysit/analytics}"`.
   Default window is the last 30 days; honor an explicit window from the
   invocation. Empty/missing files are a valid finding ("telemetry not
   flowing"), not an error.
2. Aggregate `skill-usage.jsonl`: runs / error rate / median duration per
   skill (`event:"end"` rows carry `outcome` + `duration_s`), plus the audit
   events (`skill-verdict-audit`, Hook C rows). Flag skills with error rate
   ≥25% or that never fire.
3. Aggregate `decisions.jsonl`. Classification vocabulary is dirty
   (`Mechanical`/`mechanical`/`Taste`…) — lowercase before grouping. Two
   signals matter:
   - a **Taste decision repeated ≥3×** with the same `decision` value → candidate
     for promotion to a Mechanical rule in the owning skill;
   - `kind:"resize"` rows → is `plan-draft` habitually over-sizing?
4. Find where runs die: `outcome:"error"` clusters, BLOCKED/NEEDS_CONTEXT in
   verdict audits, and tickets whose checkpoint stopped advancing (if ticket
   state is reachable). Also pair `start`/`end` rows by `(session, skill)` to
   find **orphaned runs** — a `start` with no matching `end` died without
   closing. Pair within the window only: session ids drifted in older
   history, so cross-history gross `starts − ends` is unreliable (it can even
   go negative at a window edge). A skill that emits `start` but *never* any
   `end` is an instrumentation gap, not a death — report it separately. This
   is the direct read on the pack's "it finishes" guarantee.
5. Emit the report: a ranked, ticket-ready list. Each item is one line of
   ticket title + one line of evidence (counts, skill names, sample ts). ≤7
   items; below that bar, say "no action needed" — an empty report is a valid
   result.
## Rules
- Evidence or it didn't happen: every item cites counts from the files, never
  impressions. Don't invent trends from <5 data points.
- Report, don't fix. Changing a skill because of a finding is the next ticket
  (usually `sweep` or `maintain`), not this run.
- Local-only data: never send telemetry contents to an external service.
## Output
```text
STATUS: DONE | NEEDS_CONTEXT | BLOCKED
VERDICT: AUDITED
REPORT:
1. <ticket-ready title> — <evidence: counts, skills, window>
...
NEXT: file the top item as a ticket, or "no action needed"
```
