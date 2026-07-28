# reason-bench

Differential benchmark for the `reason` skill (`.claude/skills/reason/SKILL.md`):
does the five-move scaffold lift model output on hard problems, and does the
lift persist across model tiers?

## Matrix

- **Models:** haiku, sonnet, opus
- **Conditions:** `base` (task only) vs `scaffold` (task + `scaffold.md`, the
  domain-neutral extraction of the skill's five moves)

Note: `scaffold.md` is the pre-overhaul snapshot actually tested on
2026-07-29. The skill was revised afterward from these results (deliverable
shape rule, quantify + spec-sweep in Attack) — re-extract before a re-run.
- **Problems:** 5 domains, one hard problem each — see `problems/`

30 runs total. Each problem is self-contained (answerable from model knowledge,
no repo/web/shell access) so runs are comparable across models and conditions.

## Layout

```
problems/p{1..5}-task.md    # what the model under test sees — NO traps here
grading/p{1..5}-rubric.md   # grader-only: hidden traps + scoring, 1 pt each
scaffold.md                 # the reasoning scaffold given in the scaffold condition
results/raw/                # <model>-<condition>-p<N>.md, one per run
results/results.md          # graded matrix + analysis
```

## Run protocol

For each cell, launch a fresh agent with the target model and this prompt shape:

> Read `problems/pN-task.md` and complete the task it contains, using only your
> own knowledge. You may use Read ONLY on the file(s) named here and Write ONLY
> to save your answer. No other tools, no shell, no web. Write your complete
> answer to `results/raw/<model>-<condition>-pN.md`.

Scaffold condition adds: read `scaffold.md` first and follow it, writing every
move as a visible section before the deliverable.

Models under test must never read `grading/`.

## Grading protocol

Single grader (frontier model), per-trap binary scoring against the rubric —
a trap counts only if the answer *resolves* it, not merely mentions it. P2
(coding) is additionally graded by executing the submission's tests plus the
grader's probe tests. Report per-cell scores and the two deltas that matter:
scaffold−base within each tier, and small-tier+scaffold vs large-tier base.
