---
name: investigate
description: Debug a failure before fixing it. Use when the user asks why something is broken, wants root cause analysis, or reports an error, regression, flaky test, crash, or unexpected behavior.
---

# investigate

Root cause first, fix second.

## Flow

1. Reproduce or collect the failing evidence: command output, logs, UI state, stack trace, or diff.
2. Identify what changed and why it broke now.
3. Name the root cause in one sentence before editing.
4. Apply the smallest fix that addresses that cause.
5. Re-run the reproducer and one nearby regression check.

## Guardrails

- Do not guess silently. If there are competing theories, list them and test the cheapest one.
- Do not paper over symptoms with broad retries, catches, sleeps, or guards unless the root cause demands it.
- Preserve unrelated user changes.
- If the failure depends on external state you cannot access, stop with the exact missing evidence.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
ROOT_CAUSE: <one sentence>
FIX: <what changed, or "none">
VERIFICATION: <commands/checks run>
```
