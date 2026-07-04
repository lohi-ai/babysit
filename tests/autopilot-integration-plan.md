# Autopilot orchestrator — integration test plan

> **Flow migration (v1.47.0).** The pack now runs **one workflow per archetype**
> — `prototyper`, `builder`, `sweeper`, `grower`, `maintainer`. The old
> `build` / `plan` / `implement` / `quality` / `orchestrate` / `sub-implement`
> workflows were folded into `builder`, which selects an internal **mode**
> (child / orchestrate / implement / build / verify) from ticket state. Because
> `builder` accepts all those states, its frontmatter `needs-state` is permissive
> and the **per-mode prerequisite gating moved from the Assign phase into
> `builder`'s runtime mode-selection** (the workflow's mode table + stop
> conditions). Rows below that previously read "Assign BLOCKS" now mean
> "`builder` BLOCKS at mode-selection"; the BLOCKED SUMMARY wording is preserved.
> Intent-driven archetypes (`prototyper`, `sweeper`, `grower`, `maintainer`) are
> invoked by name, never state-routed.

Tracks the scenarios that exercise the Parse → Probe → Assign → Dispatch →
Verify-post pipeline. Each row is a black-box invocation of `/bbs:autopilot`;
assertions are made against the pinned BLOCKED template, the dispatched workflow
(now always `builder` for state-routed work), and the decision log.

Runner: manual `/bbs:autopilot` invocation against a fixture ticket.
Decision log: `~/.babysit/analytics/decisions.jsonl`.

| # | Scenario | Invocation | Ticket state | Expected phase outcome | Assertion |
|---|----------|------------|--------------|------------------------|-----------|
| 1 | Inline new feature, clean tree | `/bbs:autopilot add dark mode toggle` | no ticket | Parse=Inline, Assign ensures ticket, dispatch=`builder` | ticket cut, `requirement.md` seeded byte-for-byte, decision log `outcome=dispatch force=0` |
| 2 | Inline new feature, dirty tree | `/bbs:autopilot add dark mode toggle` | no ticket, `git status` dirty | Parse=Inline, Assign BLOCKS pre-`ensure` | BLOCKED SUMMARY mentions "uncommitted changes", no ticket created |
| 3 | `--workflow=builder` on ticket with no requirement | `/bbs:autopilot bs-ab123 --workflow=builder` | ticket present, `requirement.md` empty | Assign BLOCKS | pinned template: `you asked for build but requirement_md is absent (needs required). Suggest: /bbs:autopilot bs-ab123` |
| 4 | `--workflow=builder` on unplanned ticket | `/bbs:autopilot bs-ab123 --workflow=builder` | `plan.md` absent, `requirement.md` present | `builder` selects build mode (plan-then-implement) | dispatches; build mode drafts the plan first — no block when a requirement exists |
| 5 | `--workflow=builder` on planned but unapproved ticket | `/bbs:autopilot bs-ab123 --workflow=builder` | `plan.md` present, `verdicts/plan-draft.md` missing | Assign BLOCKS | SUMMARY: `plan_approved is absent (needs required)` |
| 6 | State-routed implement on approved ticket | `/bbs:autopilot bs-ab123` | `plan.md` present, plan-draft DONE | Dispatch=`builder` (mode) | Step execution enters `load-context`; no re-check of prereqs in step body |
| 7 | `--workflow=builder` with no commits | `/bbs:autopilot bs-ab123 --workflow=builder` | branch clean, 0 commits ahead | Assign BLOCKS | SUMMARY: `commits_ahead is 0 (needs 1+)` |
| 8 | `--workflow=builder` unpushed | `/bbs:autopilot bs-ab123 --workflow=builder` | 3 commits ahead, `origin/<branch>` missing | Assign BLOCKS | SUMMARY: `branch_pushed is absent (needs required)` |
| 9 | Ticket-only hint with workflow label | `/bbs:autopilot bs-ab123` | ticket labeled `workflow:plan` | Parse=TicketHinted, Dispatch=`builder` (mode) | decision log `mode=ticket-hinted` |
| 10 | Ticket-only hint with no label, state→build | `/bbs:autopilot bs-ab123` | requirement present, `plan.md` absent | Parse=TicketHinted, Assign routes to `builder` (build mode) | dispatched workflow is `builder`; its permissive `needs-state` matches probed state |
| 11 | `--replan` on a planned ticket | `/bbs:autopilot bs-ab123 --replan` | `plan.md` exists | Assign snapshots `plan.md` → `plan.md.bak`, dispatch=`builder` force=1 | `.bak` file present; decision log `force=1 prereq=bypassed` |
| 12 | `--workflow=<wf> --force` generic override | `/bbs:autopilot bs-ab123 --workflow=builder --force` | `plan.md` exists | Assign snapshots `plan.md` → `plan.md.bak`, dispatch=`builder` force=1 | equivalent behavior to row 11 via the generic flags; decision log records `replan=0 force=1` |
| 13 | Resume after crash | `/bbs:autopilot` (no args, after an earlier session died mid-workflow) | `checkpoint.json` present, status=done_step | Parse=Resume via `bbs-autopilot current`, dispatch=same workflow from next step | announces step ≠ last checkpoint's step |
| 14 | Verify-post miss — step checkpoints without producing declared artifact | Mid-workflow: simulate step `plan-draft` checkpointing `done_step` but `plan.md` empty | `> produces: file:plan.md + verdict:plan-draft` | Verify-post rewrites checkpoint to `status=blocked` | BLOCKED SUMMARY: `step \`plan-draft\` checkpointed done but did not produce \`file:plan.md\``; step does not auto-rerun |
| 15 | Authoring-time lint catches missing `needs-state:` | `bbs-autopilot lint-workflow workflows/foo.md` on a workflow missing the key | — | lint exits non-zero | stderr names missing key and workflow file; pre-commit blocks commit |
| 16 | Context-reference input → NEEDS_CONTEXT | `/bbs:autopilot the requirement above` (any conversation context) | no ticket | §0.0 context-reference guard detects the phrase → emits `NEEDS_CONTEXT` asking the user to paste the literal requirement; never dispatches | no ticket created; SUMMARY: "autopilot cannot resolve a context reference — paste the literal text"; RECOMMENDATION names the `/bbs:autopilot "<literal>"` invocation form |
| 17 | `bbs-autopilot explain` on a planned but unapproved ticket | `bbs-autopilot explain bs-ab123` | `plan.md` present, `verdicts/plan-draft.md` missing | state table prints `plan_md=1 plan_approved=0`; recommended-workflow line reads `builder — ticket has plan.md → builder (implement mode)` | output recommends `builder` and surfaces `plan_approved=0` so the human knows the plan is unapproved |
| 18 | `--dry-run` parity with live dispatch — match case | `/bbs:autopilot bs-ab123 --dry-run` | `plan.md` present, plan-draft DONE | `STATUS: DONE(dry-run)`, `VERDICT: DRY_RUN(builder)`, SUMMARY says would-dispatch; decision log row present with `outcome=dry-run`; identical `rationale` to live run | no `checkpoint.json` written, no `ensure` called, no branch cut |
| 19 | `--dry-run` parity — block case | `/bbs:autopilot bs-ab123 --workflow=builder --dry-run` | `plan.md` absent | prints pinned BLOCKED template exactly as the live run would (SUMMARY: `plan_md is absent (needs present)` or equivalent) | no state mutation; re-running without `--dry-run` produces identical BLOCKED verdict |
| 20 | `bbs-autopilot explain` (no arg, branch-derived ticket) | `bbs-autopilot explain` on a feat/ branch | any | uses `bbs-slug env` to derive ticket; state + workflow table print against derived ticket | output header shows derived ticket id; probe values match a direct `bbs-autopilot explain bs-xxx` call |
| 21 | Sub-ticket `origin.type` surfaced to explain | `bbs-autopilot explain bs-ab123` | `bbs-ticket init --origin-type sub_ticket --parent <p> --seed <s> --plan <p>` seeded | state section prints `origin_type:     sub_ticket`; recommended-workflow line reads `builder — ticket origin is sub_ticket → builder (child mode)` | regression guard — any rename of the `origin.type` JSON path or `state_origin_type` probe var breaks this row |
| 22 | Manifest presence surfaced to explain | `bbs-autopilot explain bs-ab123` | `manifest.md` seeded at ticket root + `pointers.manifest` set | state section prints `manifest_md:     1`; recommended-workflow line reads `builder — manifest.md exists → builder (orchestrate mode)` | regression guard — decomposed parents must route to `builder` orchestrate mode, not a plain implement |

## Scoring rubric

- **Mechanical (pass/fail):** rows 1–13, 16, 17, 20, 21, 22 — each is a single
  invocation with a deterministic expected dispatch or BLOCKED shape. Any
  deviation from the pinned template (SUMMARY wording, REASON content,
  RECOMMENDATION invocation form) counts as a fail.
- **Invariant (pass/fail):** rows 14, 15, 18, 19 — Verify-post miss produces
  exactly the documented BLOCKED template and does not auto-rerun; lint
  catches exactly the missing-frontmatter case and no false positives on
  compliant workflows; `--dry-run` output is byte-equal to the live run's
  rationale in both match and block cases.

## Running the plan

The eval harness (`tests/ticket-system/run_ticket_eval.py`) fixtures can be
extended with these 22 cases. For now, this document is the authoritative
list — add fixture ids here as they land.
