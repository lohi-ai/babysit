---
name: growth-experiment
description: Propose, rank, and optionally scaffold a measurable product growth experiment. Use for A/B tests, activation, retention, acquisition, funnel, or ICE-ranking requests.
---

# growth-experiment

Turn a growth question into ranked experiments, and scaffold one only when requested.

## Flow

1. Read product positioning and any available funnel or analytics context. When the product runs, walk the target flow live (`browse`) — rank against the real funnel, not a guessed one.
2. Name the target metric and activation moment.
3. Propose 5-10 experiments ranked by ICE: impact, confidence, ease.
4. Recommend one winner with the assumption that most constrains confidence.
5. If asked to implement, scaffold the smallest flagged variant with exposure and conversion tracking.

## Rules

- Do not invent funnel numbers, traffic sources, or activation data.
- Separate ideas from implemented experiments.
- Prefer experiments that can be measured in one event path.
- Keep scaffolding reversible and behind an existing flagging pattern when available.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: RANKED | SCAFFOLDED
SUMMARY: <winner + metric + verification if implemented>
NEXT: approve, implement, or create-pr
```
