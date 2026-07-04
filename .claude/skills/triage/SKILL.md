---
name: triage
description: Tier-1 triage for a stalled or BLOCKED autonomous run. Use when a worker returned BLOCKED/NEEDS_CONTEXT or a ticket's checkpoint stopped advancing — classify recoverable vs needs-human, post a structured handoff, optionally resume from the checkpoint.
---

# triage

The layer between mechanical retry and human escalation. A worker that stops
is not automatically a human's problem: many BLOCKED runs die on transient or
mechanical causes a fresh session can clear. Triage reads the wreckage,
classifies it, and either restarts the run or hands the human a precise ask.

## Flow

1. Resolve the ticket (`bbs-ticket resolve`, or the id from the invocation)
   and collect the evidence: `checkpoint.json` (status, step, note), the
   latest handoff, `bbs-ticket verdict-status --skill <blocking skill>`, the
   history tail, and the recent `clean-handoff-audit` rows for this ticket
   (`grep '"clean-handoff-audit"' ~/.babysit/analytics/skill-usage.jsonl |
   grep '"<ticket>"' | tail`) — a run of `clean:false` with the same `issues`
   is the dirty-exit signal Hook C emits but can't fix. No ticket state at
   all → `NEEDS_CONTEXT` asking which run to triage.
2. Name the proximate cause in one sentence, from evidence — never from the
   status label alone.
3. Classify:
   - **Recoverable** — transient environment (network, rate limit, crashed
     session), mechanical state (dirty tree, stale lock, missing install),
     or a regenerable artifact (checkpoint says `done_step`, next step never
     ran). A fresh dispatch from the checkpoint clears these. A
     `clean-handoff-audit clean:false` is recoverable by construction:
     `uncommitted changes` → re-dispatch instructing a commit-first pass;
     `checkpoint predates the last commit` → run `bbs-autopilot checkpoint
     --refresh` and the audit clears without a re-run.
   - **Needs-human** — missing input only the human has (credentials, an
     ambiguous requirement, a product decision), or a verdict that BLOCKED on
     a genuine finding. Retrying reproduces the block.
4. Post the triage handoff to the ticket
   (`bbs-ticket add-handoff --skill triage`): cause, classification, evidence
   pointers, and the action taken.
5. Act:
   - Recoverable → clear the mechanical cause if trivial (never `--force`,
     never delete work), then re-dispatch `/bbs:autopilot <workflow> <ticket>`
     — it resumes from the checkpoint on disk. **One retry per cause**: if a
     prior `triage` handoff names the same cause, escalate as needs-human
     instead of looping.
   - Needs-human → emit the structured `NEEDS_CONTEXT` block naming the exact
     missing input and the checkpoint it unblocks.

## Rules

- Triage is diagnosis + dispatch, not repair. A fix that needs code changes is
  an `investigate` or `implement` ticket, not a triage action.
- Bounded blast radius: no force-push, no deleting branches/worktrees, no
  dropping the checkpoint. The run being dead does not make its state disposable.
- Log the classification to the decision trail (it's a Taste call).

## Output

```text
STATUS: DONE | NEEDS_CONTEXT | BLOCKED
CLASSIFICATION: recoverable | needs-human
CAUSE: <one sentence>
ACTION: <re-dispatched from step X | escalated with NEEDS_CONTEXT | none>
NEXT: <what the human should expect or provide>
```
