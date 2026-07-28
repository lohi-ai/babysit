# P6 — monthly billing-instant generator (fable, scaffold run)

**Deliverable shape: Construction-shaped** — run the moves on the design
decisions (month arithmetic, DST resolution strategy, validation), then
execute with checkable criteria, error paths as part of the contract, and an
adversarial sweep beyond the happy path.

## Frame

Restated: given a valid, unambiguous naive local start `"YYYY-MM-DDTHH:MM"`
in IANA zone `tz`, emit `count` monthly occurrences. Occurrence *i* is in the
*i*-th calendar month after the start month, on the anchor day (start's
day-of-month) clamped to the month's last day — the anchor itself never
decays. Each occurrence is at the start's wall time in `tz`; nonexistent wall
times (spring-forward gap) shift forward by the gap length; ambiguous wall
times (fall-back) take the earlier instant. Output is UTC,
`"YYYY-MM-DDTHH:MM:SSZ"`. `ValueError` on `count < 1` or malformed
`start_local`.

Checkable success criteria:

1. `billing_instants("2023-01-31T09:00","UTC",4)` yields Jan 31, Feb 28,
   Mar 31, Apr 30 — the spec's own anchor example.
2. A 60-min gap turns 02:30 into 03:30 local; a 30-min gap turns 02:15 into
   02:45 local — verifiable as concrete UTC strings for known transitions.
3. An ambiguous fall-back time maps to the earlier UTC instant.
4. Output format is exactly `"YYYY-MM-DDTHH:MM:SSZ"`, length `count`.
5. `count=0`, `count=-3`, and each malformed-input class raise `ValueError`.

Out of scope: invalid `tz` handling (spec is silent — `ZoneInfo` will raise
its own error and I let it propagate, declared below), sub-minute start
precision, non-Gregorian calendars, timezone-database version pinning.

No materially different readings of the spec found; the anchor rule and both
DST rules are given with worked examples, which pins them.

## Gather

**Facts** (each derivable from the task or the Python 3.11 stdlib contract):

- F1. Spec: occurrence 0 is the start itself; occurrence *i* is in the *i*-th
  month after the start month (task, "Contract").
- F2. Spec: anchor day clamps per-month but never mutates (task, explicit
  Jan 31 → Feb 28 → Mar 31 example).
- F3. Spec: gap → shift forward by gap length; ambiguity → earlier instant
  (task, with 60-min and 30-min worked examples).
- F4. `calendar.monthrange(y, m)[1]` returns the number of days in month
  (stdlib docs).
- F5. PEP 495 / `zoneinfo`: for a nonexistent local time with `fold=0`, the
  offset in effect *before* the transition is used; for an ambiguous time
  with `fold=0`, likewise the pre-transition offset (the first occurrence)
  is used. `datetime.fold` defaults to 0.
- F6. Derivation from F5, gap case: UTC = wall − off_before. Converting that
  UTC instant back to local uses off_after, giving wall + (off_after −
  off_before) = wall + gap. So `fold=0` + `astimezone(utc)` is *exactly*
  "shift forward by the gap length" — for any gap size, including 30 min.
- F7. Derivation from F5, overlap case: fall-back means off_before >
  off_after, so UTC = wall − off_before is the *smaller* (earlier) instant.
  `fold=0` is exactly "earlier of the two".
- F8. `datetime.strptime(s, "%Y-%m-%dT%H:%M")` validates ranges (month 1–12,
  day valid for that month/year, hour 0–23, minute 0–59) and raises
  `ValueError` otherwise (stdlib docs).
- F9. `strptime` `%m`/`%d`/`%H` accept *unpadded* numbers ("2024-1-5T3:04"
  parses), so strptime alone is too lax for "malformed" — a shape check is
  needed on top (known stdlib behavior).
- F10. Known IANA transitions used as test oracles: America/New_York
  2024-03-10 02:00→03:00 (second Sunday in March) and 2024-11-03
  02:00→01:00 (first Sunday in November); Australia/Lord_Howe 2024-10-06
  02:00 +10:30 → 02:30 +11:00 (first Sunday in October, 30-min shift).

**Assumptions** (uncited):

- A1. Invalid `tz` may propagate `zoneinfo`'s own exception; the spec only
  mandates `ValueError` for `count` and `start_local`. Carried into the
  module docstring/notes rather than absorbed.
- A2. `count` is an `int` per the signature; no isinstance defense
  (impossible-scenario handling would violate simplicity).
- A3. Test-oracle dates in F10 are recalled correctly (rule-derived: US DST
  second-Sunday-March/first-Sunday-November; Lord Howe first-Sunday-October;
  2024-10-06 and 2024-11-03 are Sundays). Confidence high but I cannot
  execute to confirm — carried in ATTACK.

No gating unverified assumption: F6/F7 are *derived*, not recalled, so the
approach does not rest on a guess needing a spike.

## Branch

Three genuinely different shapes for the DST-resolution core:

- **A. Naive-datetime month arithmetic + `ZoneInfo` attach at `fold=0` +
  `astimezone(utc)`.** The PEP 495 fold-0 semantics implement *both* spec
  rules exactly (F6, F7) with zero manual offset math.
  - +Smallest code; both DST rules fall out of one documented mechanism.
  - +No transition-scanning logic to get wrong.
  - −Correctness rests on F6/F7 being right (they are derivations, and
    Attack re-checks them).
- **B. Explicit disambiguation: build the candidate local time, compare
  `utcoffset()` at `fold=0` vs `fold=1` to classify exists/gap/ambiguous,
  then apply the spec rules by hand (add the offset difference in the gap
  case, pick min-UTC in the ambiguous case).**
  - +Makes the spec rules visible in code; robust if fold semantics were
    misremembered.
  - −~3× the code; the hand-rolled gap arithmetic reproduces exactly what
    fold=0 already does, so every extra line is a new place to be wrong.
- **C. Instant-space iteration: convert occurrence 0 to a UTC timestamp and
  step forward by computed month lengths in seconds.**
  - +Trivially monotonic output.
  - −Fights the spec: the contract is wall-clock (anchor day + wall time),
    so each step still requires a local-calendar computation; the timestamp
    layer adds nothing and invites off-by-gap errors. Argued for honestly,
    rejected firmly.

**Pick: A** — one documented mechanism (PEP 495 fold) provably equals both
spec rules, minimizing code and failure surface. **Switch trigger:** if
Attack shows fold=0 does *not* reduce to "shift forward by gap length" for
any gap size, or does not give the earlier ambiguous instant, switch to B.

Validation design (same for all candidates): regex shape gate
`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$` (kills F9's unpadded/extra-seconds
laxness) then `strptime` for range validity (F8). `count < 1` checked first.
Month arithmetic: `months = start.month - 1 + i; year += months // 12;
month = months % 12 + 1` — pure integer math, no date-library rollover
semantics involved, so the anchor can never decay (F2 by construction:
`day = min(anchor_day, monthrange(...)[1])` reads `anchor_day` fresh every
iteration).

## Attack

- **Counterexample hunt on the pick's core claim (F6), real values.**
  NY 2024-03-10, wall 02:30, fold=0 → offset −05:00 (pre-gap EST) → UTC
  07:30. Real local at 07:30 UTC is 03:30 EDT = spec's "02:30 + 60-min gap".
  Matches. 30-min case: Lord Howe 2024-10-06 wall 02:15, fold=0 → +10:30 →
  UTC 2024-10-05 15:45; real local there is 02:45 +11:00 = spec's "02:15 →
  02:45". Matches. Ambiguity (F7): NY 2024-11-03 wall 01:30, fold=0 → −04:00
  (EDT) → UTC 05:30, the earlier of {05:30, 06:30}. Matches. The attack does
  not land; the pick stands.
- **Anchor-decay trap.** The classic bug is `date += one_month` where Feb 28
  becomes the new anchor. Concrete probe: "2023-01-31T09:00" must give
  Mar 31, not Mar 28. Design computes every occurrence from the immutable
  `anchor_day`, so occurrence 2 = min(31, 31) = 31. Survives; encoded as a
  test.
- **Quantify.** Work is O(count) with ~one `ZoneInfo` lookup per iteration;
  even a pathological `count = 12 000` (a millennium of billing) is
  thousands of cheap datetime ops — microseconds-to-milliseconds, no
  capacity risk. Output size 21 bytes × count ≈ 250 KB at that extreme.
  Nothing here changes the design.
- **Sweep the spec.** `start_local` format → regex+strptime (consumed).
  `tz` → `ZoneInfo(tz)` (consumed; invalid-tz behavior declared out of
  scope, A1). `count` → range + `<1` guard (consumed). 0-indexing /
  occurrence 0 = start → `range(count)` starting at i=0 (consumed). Anchor
  rule incl. "never changes" → immutable `anchor_day` + per-month clamp
  (consumed). Gap rule with both worked examples → fold=0 (consumed, tested
  at 60 and 30 min). Ambiguity-earlier → fold=0 (consumed, tested).
  UTC output with `:SS` → `strftime("%Y-%m-%dT%H:%M:%SZ")` (consumed —
  seconds are emitted even in the theoretical historical-LMT case where an
  offset has a seconds component). `ValueError` cases → both guards
  (consumed). "Start guaranteed valid and unambiguous" → relied on: no
  special-casing of occurrence 0 (consumed). Nothing unused.
- **Steelman B.** B's real virtue is independence from fold-semantics
  recall. But F6/F7 were *derived* from the PEP 495 rule ("fold=0 uses the
  pre-transition offset"), which is the single load-bearing memory item —
  and B would need that same rule to classify gap vs overlap via the two
  folds' offsets. B does not actually remove the dependency; it only adds
  code. Rejection holds.
- **Strongest surviving objection:** the test oracles (A3) — exact 2024
  transition dates and the Lord Howe 30-minute rule — are recalled, and I
  cannot execute to confirm. If a date is off, a *test* fails against a
  correct implementation. Mitigated by deriving each date from the zone's
  standing rule + weekday cross-check (2024-11-03 and 2024-10-06 are four
  weeks apart, both Sundays), and by keeping the implementation itself free
  of any recalled constants.

## Verify

Check defined before finalizing: hand-trace the four riskiest expected
values, then re-read Frame.

- Trace 1 (criterion 1): "2023-01-31T09:00", UTC. i=0 → 2023-01-31 09:00Z.
  i=1 → months=1 → Feb, min(31,28)=28 → 2023-02-28 09:00Z. i=2 → Mar,
  min(31,31)=31 → 2023-03-31 09:00Z. i=3 → Apr, min(31,30)=30. Matches spec
  example exactly.
- Trace 2 (criterion 2, 60-min): start "2024-01-10T02:30" NY. i=2 →
  2024-03-10 02:30 fold=0 → EST −5 → **2024-03-10T07:30:00Z** (= 03:30 EDT).
  i=3 → 2024-04-10 02:30 EDT −4 → 06:30Z. Matches tests.
- Trace 3 (criterion 2, 30-min): "2024-09-06T02:15" Lord Howe. i=0 → +10:30
  → 2024-09-05T15:45:00Z. i=1 → 2024-10-06 02:15 in gap, fold=0 → +10:30 →
  **2024-10-05T15:45:00Z** (= 02:45 +11). Matches tests.
- Trace 4 (criterion 3): "2024-10-03T01:30" NY. i=1 → 2024-11-03 01:30
  fold=0 → EDT −4 → **05:30:00Z**, earlier than the EST reading 06:30Z.
  i=2 → Dec, EST → 06:30Z. Matches tests.
- Criterion 4: `strftime` emits zero-padded fields + literal `Z`;
  `range(count)` gives exactly `count` appends.
- Criterion 5: count 0/−3 hit the first guard; each malformed class is
  killed by regex (shape: unpadded, space, trailing seconds, empty,
  garbage) or strptime (ranges: month 13, Feb 30, hour 24, minute 60) —
  every class has a named subtest.
- Frame re-read: all five criteria met; out-of-scope items (invalid tz)
  declared, not silently handled. Adversarial inputs beyond my happy path:
  unpadded month, seconds suffix, hour 24, minute 60, Feb 30, empty string,
  30-minute-offset zone, year rollover, leap Feb 29, count=13 anniversary —
  all present in the shipped tests. No drift.

Self-check: load-bearing claims are F-cited or listed (A1–A3); Branch
candidates differ in shape (fold-delegation vs explicit classification vs
instant-space); Attack produced concrete counterexample attempts with real
UTC values and a magnitude estimate; spec fully swept; Verify defined before
finalizing and applied.

---

## Deliverable

### `billing.py`

```python
"""Monthly billing-instant generator (stdlib only)."""

import re
from calendar import monthrange
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

_START_RE = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}")


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing occurrences as UTC strings.

    Occurrence 0 is the start itself. The anchor day is the start's
    day-of-month, clamped per-month to the last day of shorter months
    (the anchor itself never changes). Each occurrence is at the start's
    wall time in `tz`; a time inside a DST spring-forward gap is shifted
    forward by the gap length, and an ambiguous fall-back time resolves
    to the earlier instant.
    """
    if count < 1:
        raise ValueError(f"count must be >= 1, got {count}")
    if not _START_RE.fullmatch(start_local):
        raise ValueError(f"malformed start_local: {start_local!r}")
    try:
        start = datetime.strptime(start_local, "%Y-%m-%dT%H:%M")
    except ValueError as exc:
        raise ValueError(f"malformed start_local: {start_local!r}") from exc

    zone = ZoneInfo(tz)
    anchor_day = start.day
    out: list[str] = []
    for i in range(count):
        months = start.month - 1 + i
        year = start.year + months // 12
        month = months % 12 + 1
        day = min(anchor_day, monthrange(year, month)[1])
        local = datetime(year, month, day, start.hour, start.minute, tzinfo=zone)
        # fold=0 (the default) implements both spec rules via PEP 495:
        # - spring-forward gap: the pre-transition offset is used, so the
        #   resulting UTC instant equals the wall time shifted forward by
        #   exactly the gap length;
        # - fall-back overlap: the pre-transition (larger) offset is used,
        #   which is the earlier of the two instants.
        out.append(local.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    return out
```

### `test_billing.py`

```python
import unittest

from billing import billing_instants


class TestBasics(unittest.TestCase):
    def test_utc_simple_series(self):
        self.assertEqual(
            billing_instants("2024-01-15T10:00", "UTC", 3),
            ["2024-01-15T10:00:00Z", "2024-02-15T10:00:00Z", "2024-03-15T10:00:00Z"],
        )

    def test_count_one_returns_start_only(self):
        self.assertEqual(
            billing_instants("2024-06-01T00:00", "UTC", 1),
            ["2024-06-01T00:00:00Z"],
        )

    def test_year_rollover(self):
        self.assertEqual(
            billing_instants("2023-11-15T08:00", "UTC", 4),
            [
                "2023-11-15T08:00:00Z",
                "2023-12-15T08:00:00Z",
                "2024-01-15T08:00:00Z",
                "2024-02-15T08:00:00Z",
            ],
        )

    def test_occurrence_12_is_next_year_anniversary(self):
        got = billing_instants("2024-05-20T12:00", "UTC", 13)
        self.assertEqual(len(got), 13)
        self.assertEqual(got[12], "2025-05-20T12:00:00Z")


class TestAnchorRule(unittest.TestCase):
    def test_jan31_clamps_feb_then_returns_to_31(self):
        # Spec example: Jan 31 -> Feb 28 -> Mar 31 (not Mar 28).
        self.assertEqual(
            billing_instants("2023-01-31T09:00", "UTC", 4),
            [
                "2023-01-31T09:00:00Z",
                "2023-02-28T09:00:00Z",
                "2023-03-31T09:00:00Z",
                "2023-04-30T09:00:00Z",
            ],
        )

    def test_leap_february_gets_day_29(self):
        self.assertEqual(
            billing_instants("2024-01-31T00:00", "UTC", 2),
            ["2024-01-31T00:00:00Z", "2024-02-29T00:00:00Z"],
        )

    def test_day30_clamps_only_in_february(self):
        self.assertEqual(
            billing_instants("2024-01-30T06:00", "UTC", 3),
            ["2024-01-30T06:00:00Z", "2024-02-29T06:00:00Z", "2024-03-30T06:00:00Z"],
        )


class TestTimezones(unittest.TestCase):
    def test_fixed_offset_zone(self):
        # Asia/Tokyo is UTC+9 year-round.
        self.assertEqual(
            billing_instants("2024-04-05T09:00", "Asia/Tokyo", 2),
            ["2024-04-05T00:00:00Z", "2024-05-05T00:00:00Z"],
        )

    def test_wall_time_held_across_dst_change(self):
        # New York: EST (-5) in Jan/Feb, EDT (-4) from 2024-03-10 on.
        # Wall time stays 12:00; the UTC instant moves.
        self.assertEqual(
            billing_instants("2024-01-20T12:00", "America/New_York", 4),
            [
                "2024-01-20T17:00:00Z",
                "2024-02-20T17:00:00Z",
                "2024-03-20T16:00:00Z",
                "2024-04-20T16:00:00Z",
            ],
        )

    def test_spring_forward_gap_shifts_forward_60min(self):
        # US spring-forward 2024-03-10: 02:00 -> 03:00 in America/New_York.
        # 02:30 does not exist that day; spec: becomes 03:30 EDT = 07:30 UTC.
        self.assertEqual(
            billing_instants("2024-01-10T02:30", "America/New_York", 4),
            [
                "2024-01-10T07:30:00Z",  # 02:30 EST
                "2024-02-10T07:30:00Z",  # 02:30 EST
                "2024-03-10T07:30:00Z",  # gap -> 03:30 EDT
                "2024-04-10T06:30:00Z",  # 02:30 EDT
            ],
        )

    def test_spring_forward_gap_30min_zone(self):
        # Australia/Lord_Howe: 2024-10-06 02:00 +10:30 -> 02:30 +11:00
        # (a 30-minute gap). 02:15 does not exist; spec: becomes 02:45 +11.
        self.assertEqual(
            billing_instants("2024-09-06T02:15", "Australia/Lord_Howe", 2),
            [
                "2024-09-05T15:45:00Z",  # 02:15 +10:30
                "2024-10-05T15:45:00Z",  # 02:45 +11:00
            ],
        )

    def test_fall_back_ambiguous_uses_earlier_instant(self):
        # US fall-back 2024-11-03: wall times 01:00-02:00 occur twice in
        # America/New_York. 01:30 ambiguous -> earlier (EDT, -4) = 05:30 UTC.
        self.assertEqual(
            billing_instants("2024-10-03T01:30", "America/New_York", 3),
            [
                "2024-10-03T05:30:00Z",  # 01:30 EDT
                "2024-11-03T05:30:00Z",  # ambiguous -> earlier occurrence (EDT)
                "2024-12-03T06:30:00Z",  # 01:30 EST
            ],
        )


class TestValidation(unittest.TestCase):
    def test_count_zero_raises(self):
        with self.assertRaises(ValueError):
            billing_instants("2024-01-01T00:00", "UTC", 0)

    def test_count_negative_raises(self):
        with self.assertRaises(ValueError):
            billing_instants("2024-01-01T00:00", "UTC", -3)

    def test_malformed_start_raises(self):
        bad = [
            "2024-1-31T09:00",      # unpadded month
            "2024-01-31 09:00",     # space instead of T
            "2024-01-31T09:00:00",  # seconds not part of the format
            "2024-13-01T09:00",     # month out of range
            "2024-02-30T09:00",     # day invalid for month
            "2024-01-31T24:00",     # hour out of range
            "2024-01-31T09:60",     # minute out of range
            "garbage",
            "",
        ]
        for s in bad:
            with self.subTest(s=s):
                with self.assertRaises(ValueError):
                    billing_instants(s, "UTC", 1)


if __name__ == "__main__":
    unittest.main()
```

### Notes

- The whole DST contract is delegated to PEP 495 `fold=0` semantics, which
  provably (see Gather F6/F7) equal the spec's two rules for any gap size —
  no transition scanning, no recalled offsets in the implementation.
- The regex shape-gate exists because `strptime` alone accepts unpadded
  fields; range validation (Feb 30, month 13, hour 24) is strptime's job.
- Invalid `tz` propagates `zoneinfo`'s own exception; the spec mandates
  `ValueError` only for `count` and `start_local`.
- Tests depend on the system tzdata (needs `America/New_York`,
  `Australia/Lord_Howe`, `Asia/Tokyo`; on Windows, `pip install tzdata`).

```text
STATUS: DONE
CONFIDENCE: high — running `python -m unittest` against a real tzdata would raise it to certain
ASSUMPTIONS: recalled 2024 transition oracles in the tests (NY Mar 10 / Nov 3, Lord Howe Oct 6, 30-min shift); invalid tz intentionally propagates zoneinfo's error rather than ValueError
ATTACK: the test oracles are recalled facts I cannot execute to confirm — but each is derived from the zone's standing rule with a weekday cross-check, and a wrong oracle would fail a test, not the implementation, whose logic carries no recalled constants
```
