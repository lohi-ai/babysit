# reason-bench results — 2026-07-29

3 models (haiku, sonnet, opus) × 2 conditions (base, scaffold = reason skill's
five-move structure) × 5 problems. Grader: Fable, per-trap binary against
`grading/p*-rubric.md` ("a trap counts only if the answer *resolves* it, not
merely mentions it"). P2 graded by execution: `grading/p2_probe.py` (5
behavioral probes) + check that the submission's own tests cover
cycle/failure-propagation. Raw outputs in `raw/`; per-trap detail in
`grading-notes.md`.

## Score matrix

| run | P1 plan /7 | P2 code /5 | P3 UI /7 | P4 arch /8 | P5 QA /8 | total /35 |
|-----|-----------|-----------|----------|------------|----------|-----------|
| haiku-base | 6 | 5 | 7 | 6 | 7 | **31** |
| sonnet-base | 6 | 5 | 7 | 6 | 8 | **32** |
| opus-base | 6 | 5 | 7 | 5 | 8 | **31** |
| haiku-scaffold | 7 | 5 | 7 | 6 | 7 | **32** |
| sonnet-scaffold | 6 | 5 | 7 | 7 | 6 | **31** |
| opus-scaffold | 6 | 5 | 7 | 7 | 7 | **32** |

## Key deltas

**Scaffold − base, within tier:** haiku +1, sonnet −1, opus +1. Net ≈ 0 at
the trap-score level; the real signal is per-domain (below).

**Small-tier+scaffold vs large-tier base:** haiku-scaffold (32) ≥ opus-base
(31). Consistent with the earlier reason-skill review finding: on tasks
where the small model can execute the moves, the scaffold closes the
discipline gap and tier stops mattering.

**Tier effect overall: nearly none.** 31–32/35 across all six runs. These
five problems, though built around commonly-missed traps, sit within reach
of every current tier — trap hit rates 83–91% everywhere.

## Per-domain findings

- **P2 coding, P3 UI: ceiling, no discrimination.** All six runs pass all
  execution probes (cycle, transitive skip, attempts=max_retries+1,
  concurrency cap incl. retries, cancel semantics) and all seven UI traps.
  Async DAG runners and Gmail-style bulk-edit UX are deeply represented
  patterns at every tier.
- **P4 architecture: scaffold helps (+1 tier-avg), via exactly the move
  designed for it.** Attack's magnitude check produced quantitative backlog
  math in all three scaffold runs (216k-event per-endpoint backlog & ~5/s
  serial drain ceiling; 100k deliveries/s peak forcing the hot path off
  Postgres; ~20k-event average-endpoint backlog vs whale-endpoint skew
  forcing Postgres spillover). Zero base runs quantified anything (T2 missed
  by all three).
- **P5 QA: scaffold hurts (−1 to −2).** All three base runs enumerated the
  Jan-31/month-end anchor trap; all three scaffold runs missed it. QA-plan
  quality here is enumeration-driven; scaffold runs spent depth on races and
  invariants (and were stronger on assumption honesty) but lost checklist
  breadth. Same pattern in P2: base runs wrote more tests (13/11/9) than
  scaffold runs (6/9/8), though all covered the critical cases.
- **P1 planning: near ceiling, qualitative scaffold edge.** All three base
  runs assumed a CRDT adoption directly; all three scaffold runs surfaced
  "is the current engine OT or CRDT?" as an unverified load-bearing
  assumption and made it a gating Phase-0 spike. Only haiku-scaffold
  resolved offline auth-token expiry (the T2 trap all others missed).
- **Universal miss:** P4's HMAC-signing trap (T5) — no run described payload
  signing; scaffold runs at least declared it out-of-scope explicitly, base
  runs silently carried an unused `secret` column.

## Bench-design lessons for a next iteration

1. Traps must be harder: current tiers resolve 83–91% of "commonly missed"
   edge cases unaided. Discriminating problems need either deeper
   composition (multi-constraint interactions) or execution surfaces where
   subtle bugs are checkable (P2's probe model, but with adversarial cases
   the models historically get wrong).
2. Word-capped deliverables interact with the scaffold: meta-reasoning
   crowds out enumeration breadth on checklist-shaped tasks. Either exempt
   the reasoning from the cap explicitly in the prompt (done here) AND
   instruct that the deliverable itself must maximize coverage, or score
   breadth and depth as separate axes.
3. Execution-based grading (P2) was the cheapest and most objective signal;
   prefer it wherever the domain allows.
