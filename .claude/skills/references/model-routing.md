# Model routing

The pack's canonical harness → model list. `autopilot` routes its planner and
QA models through it; `foreman` routes the CLI session it dispatches each child
ticket's worker on. Every model ID, effort, and price the pack reasons about is
written down here — a second copy inside a SKILL.md is a second thing to drift.

## Tiers

Classify from the requirement, the plan, and the acceptance commands. Weak or
ambiguous evidence stays `normal`; never classify up on a guess.

| Tier | Use for |
|---|---|
| `simple` | an obvious local docs/config edit, or a tiny isolated change with no new contract and no new state |
| `normal` *(default)* | ordinary implementation work — everything no other row names |
| `critical` / `hard` | security, auth, money, irreversible or live-data migration, distributed concurrency, a cross-system architecture decision |

## Ladders

Ranked by capacity, strongest first. Prices are published list rates in USD per
million tokens (input / output), checked 2026-09-14 against the provider price
pages — OpenAI `developers.openai.com/api/docs/pricing`, Anthropic
`platform.claude.com/docs/en/about-claude/pricing`, DeepSeek
`api-docs.deepseek.com/quick_start/pricing`, Devin `devin.ai/pricing`. Effective
cost is usually lower — cached input, prompt-cache reads, batch APIs — so read
them as a cost ordering, not an invoice. A model ID is usable only on the
harness whose ladder lists it.

### Codex

| # | Model | Effort | Capacity | Input | Output |
|---|-------|--------|----------|------:|-------:|
| 1 | `gpt-6-astra` | `high` | escalation rung — ~2.5x the workhorse, see below | 10.00 | 50.00 |
| 2 | `gpt-5.6-sol` | `high` | the workhorse rung — handles almost every ticket | 4.00 (promotional) | 20.00 |
| 3 | `gpt-5.6-terra` | `high` | cheapest capable rung; docs, config, tiny isolated changes | 2.00 | 12.00 |

### Claude Code

| # | Model | Effort | Capacity | Input | Output |
|---|-------|--------|----------|------:|-------:|
| 1 | Fable 5.1 (`fable`) | `high` | escalation rung — ~2x the workhorse, see below | 10.00 | 50.00 |
| 2 | Opus 5 (`opus`) | `high` | the workhorse rung — handles almost every ticket | 5.00 | 25.00 |
| 3 | Sonnet 5 (`sonnet`) | `high` | gate rung — the cheap capable option for review and QA fan-out | 3.00 | 15.00 |
| 4 | Haiku 4.5 (`haiku`) | — | mechanical work only | 1.00 | 5.00 |

### OMP — operator-configured roles

OMP addresses roles, not model IDs: an operator binds each role to a model, so
the ladder's capacity comes from the binding rather than from the name. The
convention is `@slow` > `@default` > `@smol`; read this machine's binding with
`omp config get modelRoles` and price the bound model from its own ladder.

| # | Role | Bound model here | Input | Output |
|---|------|------------------|------:|-------:|
| 1 | `@slow` | `openai-codex/gpt-5.6-sol:high` | 4.00 | 20.00 |
| 2 | `@default` | `opencode-go/deepseek-v4.1-flash:high` | 0.30 peak / 0.15 off-peak | 1.20 / 0.60 |
| 3 | `@smol` | `devin/swe-2:high` | subscription | subscription |

Roles carry their own names, so the ladder reads by binding, not by rank:
`@default` is the everyday rung, `@smol` takes easy work, and `@slow` is the
strongest role — the critical rung. Other roles (`@plan`, `@designer`,
`@advisor`, `@task`, `@commit`, `@tiny`) are bound by the operator for their own
jobs and are not tier inputs here. OMP has no rung above `@slow`: if an operator
binds it to a frontier model, the cost of that choice is theirs, not this
file's.

### Grok

Grok Build advertises two models (`grok models`): `grok-4.6`, its default, and
the older `grok-4.5`. All routing runs on the default, so every tier takes it
and no per-Dispatch launch preference is involved — Orca forwards
`--model`/`--effort` only for Claude, Codex, and Cursor. Grok's own config and
`--reasoning-effort` set its effort.

| # | Model | Effort | Capacity | Input | Output |
|---|-------|--------|----------|------:|-------:|
| 1 | `grok-4.6` | own config | the only routed rung — the harness default, all tiers | 2.00 | 6.00 |
| — | `grok-4.5` | — | advertised, not routed | — | — |

`grok-4.6` list rates double for a prompt at or above 200K tokens ($4.00 /
$12.00), so a long-context worker pays a different rate on the same rung.

## Tier → model

| Harness | `simple` | `normal` (default) | `critical` / `hard` |
|---|---|---|---|
| Codex | #3 `gpt-5.6-terra`, `high` | #2 `gpt-5.6-sol`, `high` | #2 `gpt-5.6-sol`, `high` |
| Claude Code | #2 `opus`, `high` | #2 `opus`, `high` | #2 `opus`, `high` |
| OMP | #3 `@smol`, `high` | #2 `@default`, `high` | #1 `@slow`, `high` |
| Grok | `grok-4.6` | `grok-4.6` | `grok-4.6` |

The routine rungs — `gpt-5.6-sol`, `opus`, `@default` — carry almost every
ticket, hard ones included. Codex and Claude repeat the routine rung in the
`critical` column on purpose: there `critical` buys escalation *eligibility*,
not a bigger model. OMP's `critical` takes `@slow`, the strongest role the
operator bound, which on this machine is the same model as Codex's routine rung
— no cost spike. Grok has a single routed rung, so its row is flat by fact
rather than by choice.

## Escalating to the top rung

`gpt-6-astra` ($10/$50) and Fable 5.1 ($10/$50) cost 2–2.5x the workhorse per
token, so they are an escalation, never a tier default, and never a hunch.
Escalate one Dispatch at a time, and only when one of these holds:

- the user named that model for this run, or
- the ticket is on the non-delegable floor — security, auth, money,
  irreversible or live-data migration — or is a cross-system architecture
  decision, **and** the workhorse already ran it and came back short: a
  `BLOCKED`, an inadequate plan, or a gate that failed on reasoning.

Name the trigger beside the model in the handoff, with the cost delta, and log
it as the Taste decision. "It looks hard" is not a trigger — when in doubt stay
on the workhorse and let the evidence promote the ticket. Run at most the
Dispatches that need it on the top rung; a ticket whose remaining work is
ordinary implementation drops back to the workhorse on a fresh worker.

## Rules

- Never invent a model ID, and never read an agent type name as a model name.
- A harness with no ladder here (cursor, pi, …) runs on its own
  configured default. Record that; do not guess an ID for it.
- An explicit model or effort the user named for this run wins over the tier,
  in every skill that reads this file.
- Prefer the cheapest rung that can carry the work. Cost is a reason to move
  down the ladder, never a reason to climb it: the top rungs need the
  escalation trigger above, and a `simple` ticket that turns out to need more
  moves up one rung, not to the top.
- Tier → model is a Taste decision: log the tier, harness, model, effort, and
  the evidence that classified it.
- What the harness advertises beats this file. When a live list disagrees with
  a row here, use the advertised value and fix the row.
