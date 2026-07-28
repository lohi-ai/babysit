# reason-bench v2 — results (2026-07-29)

12 runs: {haiku, opus, fable} × {base, scaffold} × {p6, p7}. Scaffold =
`v2/scaffold.md` (post-overhaul extraction of `reason` SKILL.md). Grading:
p6 executed against `grading/p6_probe.py` (6 baked probes) + 2 test-coverage
points; p7 hand-graded against `grading/p7-rubric.md` (8 traps, "resolves,
not mentions"). Max 16 per condition.

## Score matrix

| run | P6 /8 | P7 /8 | total /16 |
|---|---|---|---|
| haiku-base | 8 | 5 | 13 |
| haiku-scaffold | 8 | 6 | 14 |
| opus-base | 8 | 8 | 16 |
| opus-scaffold | 8 | 8 | 16 |
| **fable-base (target line)** | **8** | **8** | **16** |
| fable-scaffold | 8 | 8 | 16 |

## The two deltas

**Scaffold − base, per tier:** haiku **+1** (P7 5→6), opus **0** (ceiling both
sides), fable **0** (ceiling both sides).

**Tier vs the fable-base target line (16):** haiku-scaffold **−2** — the
scaffold does *not* lift haiku to fable level. opus-base already sits on the
line without any scaffold.

## P6: at ceiling for every tier — does not discriminate

All six runs scored 8/8. Even haiku-base solved the Lord Howe half-hour DST
gap, per-month anchor clamping, and fold-ambiguity blind (no execution
allowed), and shipped a DST test with a correct concrete UTC value plus an
anchor-restore test. Execution-graded calendar/DST code at this difficulty is
solved knowledge for current tiers; a v3 coding problem needs a domain where
the ground truth is *not* memorizable (e.g. interacting stateful invariants,
adversarially chosen probe inputs, or a spec with a deliberately
underdetermined corner that must be detected rather than guessed).

Submission-quality blemish (unscored): haiku-scaffold-p6's own suite has 1
failing test (`test_error_malformed_start_local` — their code accepts an
input their test rejects). Probes and the two coverage points unaffected.

## P7: the discriminating problem

| trap | haiku-base | haiku-scaffold | opus-base | opus-scaffold | fable-base | fable-scaffold |
|---|---|---|---|---|---|---|
| T1 late×close + credit doc | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| T2 storage magnitude | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| T3 immutability mechanism | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| T4 reproducibility | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ |
| T5 dedup sized (~700M keys) | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ |
| T6 price-interval splits | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ |
| T7 tz close + day-31 clamp | ✗ | ✗ | ✓ | ✓ | ✓ | ✓ |
| T8 aggregation @ scale | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

### Judgment calls (recorded for regrade consistency)

- **haiku-base T2 granted** despite arithmetic sloppiness — the retention
  decision was visibly number-driven, which is what T2 tests.
- **haiku-base T4 granted** via per-invoice event-bundle pinning: a valid
  (if storage-heavy) reproducibility mechanism.
- **haiku-scaffold T4 denied**: no watermark/manifest/snapshot pinning; a
  re-run after late arrivals reads different inputs. Its Verify move
  *self-certified* reproducibility anyway — a false-positive self-check.
- **opus-scaffold P7 notes**: found the 15-minute rollup-bucket requirement
  from 45-minute-offset zones (Chatham straddling an hourly bucket) — beyond
  the rubric; no extra credit, but it's the strongest single insight in the
  12 runs alongside its `occurred_at`-partitioning move that converts global
  dedup into per-partition `DISTINCT` (dissolving T5's 28 GB seen-set rather
  than sizing a store for it — granted as resolving T5).

### What the scaffold did to haiku (the only tier with signal)

- **Gained T5 + T6** — both via Attack's *Quantify* bullet and the spec
  sweep: it computed the 700M-key dedup window and noticed price
  `effective_from` must split line items. These are exactly the two wins v1
  predicted (magnitude quantification, spec-sweep).
- **Lost T4** — the scaffold's Verify move produced a confident self-check
  that asserted reproducibility without a pinning mechanism. A structured
  self-verification step can *launder* a gap into a checked-off claim.
  This is the v2 analogue of v1's "scaffold crowds out breadth" harm:
  structure without capability yields false confidence, not correctness.
- **Never got T7** in either condition — timezone-anchored close with
  day-31 clamping appears to be a capability gap the scaffold can't paper
  over.

## Known bias (unchanged from README)

Problem author, rubric author, and grader are all fable — the same model as
the subject. Traps are ones the author finds natural, so fable scores are an
**upper bound** and cross-tier gaps a **lower bound** on real difficulty.
Fable's own scaffold delta (0 at ceiling) says nothing about whether the
scaffold helps fable on problems fable finds hard — no such problem exists
in this bench by construction.

## Verdict

**"Scaffold + smaller model reaches fable level" is refuted at the haiku
tier and unfalsifiable at the opus tier.** Haiku-scaffold lands at 14/16 vs
the 16/16 target: the scaffold reliably transfers the *mechanical* moves
(quantify, sweep the spec) but not the *judgment* moves (choosing a pinning
mechanism, timezone-boundary policy), and its Verify step can mint false
confidence where capability is missing. Opus needs no scaffold to hit the
target on this bench — but with both P6 and opus-P7 at ceiling, the bench
can no longer measure improvements; discriminating between opus and fable
requires harder problems than an author-is-grader design produced here.
