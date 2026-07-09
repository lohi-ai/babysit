---
name: investigate
description: Debug a failure before fixing it. Use when the user asks why something is broken, wants root cause analysis, or reports an error, regression, flaky test, crash, or unexpected behavior.
---
# investigate
Root cause first, fix second: reproduce or collect the failing evidence,
name the root cause in one sentence before editing, confirm it by toggling
(revert the suspect change, remove the trigger input, or isolate it), apply
the smallest fix, then re-run the reproducer plus one nearby regression
check. Check the pothole map first — the git-archaeology recipe in
`../references/finding-unknowns.md`: a prior fix commit in the failing area
often names this same root cause. Competing theories: list them, test the
cheapest — never guess silently. No symptom-papering (broad retries,
catches, sleeps, guards) unless the root cause demands it. Preserve
unrelated user changes. If the failure depends on external state you cannot
access, stop with the exact missing evidence.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
ROOT_CAUSE: <one sentence>
EVIDENCE: <what confirmed the cause>
FIX: <what changed, or "none">
VERIFICATION: <commands/checks run>
```
