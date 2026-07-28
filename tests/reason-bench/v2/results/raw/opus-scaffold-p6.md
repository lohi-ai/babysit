# P6 — monthly billing-instant generator (opus, scaffold)

**Deliverable shape: construction-shaped.** Two source files. The moves run on
the *design decisions* (month arithmetic, local→UTC resolution, validation),
then execution is disciplined: checkable criteria before code, error paths as
part of the contract, and a final adversarial sweep beyond my own tests. The
test file is the enumeration-shaped sub-deliverable, so its case list is
written at full width.

---

## 1. Frame

Build `billing.py::billing_instants(start_local, tz, count) -> list[str]`,
stdlib-only, plus shipped `unittest` tests. I cannot execute anything, so
correctness must come from construction + hand-tracing.

**Checkable success criteria** (each is a concrete assertion, not "works well"):

- C1 `len(result) == count`; every element matches `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`.
- C2 `result[0]` is the start itself converted to UTC.
- C3 Anchor persistence: `billing_instants("2024-01-31T12:00","UTC",4)` ==
  `[Jan 31, Feb 29, Mar 31, Apr 30]` — Mar is 31, **not** 28/29.
- C4 60-min gap: a 02:30 occurrence on 2024-03-10 in `America/New_York` yields
  `2024-03-10T07:30:00Z` (i.e. local 03:30 EDT).
- C5 30-min gap: a 02:15 occurrence on 2024-10-06 in `Australia/Lord_Howe`
  yields `2024-10-05T15:45:00Z` (local 02:45 +11:00).
- C6 Ambiguous: a 01:30 occurrence on 2024-11-03 in `America/New_York` yields
  `2024-11-03T05:30:00Z` (the earlier, EDT instant — not 06:30Z).
- C7 `ValueError` for `count < 1` and for each malformed `start_local` shape
  (space separator, unpadded fields, seconds present, impossible date).
- C8 The output list is strictly increasing across DST transitions.

**Out of scope** (declared, not silently dropped): timezone *validity* errors
(`ZoneInfoNotFoundError` propagates — the contract names `ValueError` only for
`count` and `start_local`), sub-minute start precision, weekly/annual cadence,
proration, and any I/O or persistence.

**Two readings I refuse to pick silently:**

- **R1 — "i-th calendar month after the start month", anchored.** Occurrence
  `i` is computed independently from the start: month = start month + i, day =
  `min(anchor_day, days_in_month)`. Jan 31 → Feb 28 → **Mar 31**.
- **R2 — iterative "add one month to the previous occurrence".** Jan 31 → Feb 28
  → **Mar 28**.
  The spec settles it explicitly ("the anchor day itself never changes … not
  Mar 28"), so **R1**. R2 is named here because it is the natural
  implementation and the trap the spec is testing; the chosen code never
  reads `result[i-1]`.

---

## 2. Gather

**Facts** (from the task text or directly derivable from the stdlib/tz domain):

- F1 Signature, format strings and `ValueError` conditions — task contract.
- F2 Anchor rule with the Jan-31 worked example — task contract.
- F3 Gap rule = *shift forward by the length of the gap*, with both a 60-minute
  and a 30-minute worked example — task contract. The 30-minute example rules
  out "snap to the transition instant" and "add exactly one hour".
- F4 Ambiguous rule = **earlier** instant — task contract.
- F5 The start is guaranteed valid and unambiguous — task contract. Later
  occurrences carry no such guarantee.
- F6 `calendar.monthrange(y, m)[1]` gives the month length, leap-aware; stdlib.
- F7 `datetime(...)` raises `ValueError` for impossible dates (2023-02-29,
  month 13, hour 24) — stdlib.
- F8 A `datetime` carrying `tzinfo=ZoneInfo(...)` exposes two candidate offsets
  via `fold=0` / `fold=1`; near a transition these are the pre- and
  post-transition offsets — that is what `fold` exists for (PEP 495).
- F9 The **UTC→local** direction is always total and unambiguous: for any
  instant there is exactly one local wall time. (No `fold` semantics involved.)
- F10 Real 30-minute-DST zone: `Australia/Lord_Howe` (+10:30 / +11:00).
- F11 `strftime("%Y")` zero-padding for years < 1000 is platform-dependent;
  f-string `{:04d}` is not.
- F12 `re.match` with a trailing `$` also matches before a trailing newline;
  `re.fullmatch` does not.

**Assumptions** (uncited; each is either verified below or carried to the output):

- A1 *Which* fold value is the pre-transition offset in a gap. — **Neutralized
  in Attack**: the chosen design does not depend on it (it uses `fold` only to
  obtain the *set* of two offsets, then validates by round-trip). This was the
  one assumption capable of gating the whole approach, so it drove the design
  choice rather than being built on.
- A2 No tzdata zone contains two transitions within one gap length. Guarded by
  an explicit raise rather than silent wrong output. **medium-high confidence.**
- A3 Concrete transition dates used in tests: US DST 2024-03-10 / 2024-11-03,
  Sydney 2024-04-07, Lord Howe 2024-10-06 at 02:00. Derived from the standing
  rules (2nd Sun Mar / 1st Sun Nov / 1st Sun Apr / 1st Sun Oct) plus 2024
  weekday arithmetic — traced in Verify. **medium-high confidence**; they are
  data, not logic, so a wrong date fails a test, never ships a bug.
- A4 `count` is an `int`. Non-`int` is rejected with `ValueError` (the contract
  names no other exception type).

---

## 3. Branch

The month arithmetic is settled by Frame/R1. The real design choice is
**local wall time → UTC instant**. Three genuinely different shapes:

**Cand-A — `fold=0` one-liner.**
`naive.replace(tzinfo=zone, fold=0).astimezone(timezone.utc)`.
- One line. Provably correct *if* `fold=0` means the pre-transition offset:
  then in a gap the produced instant equals the gap-shifted local time
  (`dt − o_pre == (dt + gap) − o_post`), and in an ambiguity `fold=0` is the
  earlier instant. Argument in one sentence: PEP 495 makes the desired rules
  the default, so write nothing.
- Fails C4/C5/C6 *silently and invisibly* if A1 is backwards, and I cannot
  execute to check. It also encodes the spec's rules nowhere in the source.

**Cand-B — candidate offsets + round-trip validation (chosen).**
Take both offsets (`fold=0`, `fold=1`), build both candidate instants, keep
those whose **UTC→local** conversion reproduces the wall time exactly (F9).
0 valid → gap: shift by `abs(o₁ − o₀)` and resolve again. 1 valid → done.
2 valid → ambiguous: take `min` (earliest).
- Correct under either fold convention (only A1-free facts F8/F9 are used).
- Case analysis mirrors the spec's three bullets line-for-line.
- ~20 lines instead of 1; two extra conversions per occurrence.

**Cand-C — transition search.**
Probe `utcoffset()` at instants ±N days around the date and binary-search the
transition to recover its exact time and both offsets, then classify from
first principles.
- No `fold` at all; yields the transition instant itself, useful if the spec
  ever needed "snap to transition".
- ~60 lines, a window heuristic (how wide?), ~40 conversions per occurrence,
  and a new failure mode when two transitions fall inside the window. Buys
  nothing the spec asks for.

**Pick: Cand-B.** It is the only candidate whose correctness I can establish
*without executing anything*, because it never depends on the one fact I can't
check (A1).
**Switch trigger:** if a requirement ever needs the transition instant or gap
*direction* (rather than magnitude) — e.g. "bill at the transition" — go to
Cand-C. If A1 is ever confirmed by execution and the code needs slimming,
Cand-A is a safe collapse of B.

---

## 4. Attack

**Counterexample attempts with real values:**

1. **Fold inverted (kills Cand-A).** If `fold=0` were the *post*-transition
   offset, Cand-A returns `2024-03-10T06:30:00Z` for C4 — local 01:30 EST,
   i.e. *backwards* — and `06:30Z` for C6, the later instant. Cand-B returns
   the same answer under either convention because `_instants_for` rejects both
   candidates by round-trip (07:30Z→03:30 local ≠ 02:30; 06:30Z→01:30 ≠ 02:30)
   and only then applies the shift. **Attack lands on A, not on B** — this is
   the objection that sent me back to Branch and produced the pick.
2. **Negative-DST zones** (`Europe/Dublin`, where tzdata models winter as
   negative DST). Any classifier keyed on `dst() != 0` or on "the DST offset is
   the bigger one" misreads Dublin. Cand-B keys on wall-clock validity only, so
   it is unaffected. `abs()` in `_gap_length` keeps the sign question out.
3. **Iterative month drift** (R2). Concrete: Jan 31 → Feb 28 → Mar 28 → Apr 28…
   The code recomputes from `start` every iteration and stores `anchor_day`
   once, so occurrence 2 of a Jan-31 start is Mar 31. Asserted in C3.
4. **`strptime` leniency.** `datetime.strptime("2024-1-5T3:04", "%Y-%m-%dT%H:%M")`
   *succeeds* — it accepts unpadded fields — so a strptime-only parser would
   silently accept a malformed input the contract says to reject. Fixed by a
   `\d{2}`-anchored `fullmatch` **before** constructing the datetime (F12: and
   `fullmatch`, not `match`+`$`, so `"2024-01-15T09:00\n"` is rejected).
5. **Gap that crosses midnight.** A zone jumping 23:30→00:30 turns an occurrence
   on day D into day D+1. The spec says "shift forward by the length of the
   gap", so the date change is *correct*, not a bug; the code does plain
   `naive + gap` and never re-clamps to the anchor month. Declared, not
   suppressed.
6. **Shifted time still invalid** (A2). Requires two transitions inside one gap;
   no such zone is known. Rather than loop, the code raises a named
   `ValueError` — loud and local.
7. **Year overflow.** `billing_instants("9999-12-31T23:00","UTC",2)` raises
   `ValueError` from the `datetime` constructor. Accepted and declared: the
   contract's `ValueError` channel is the right one.

**Quantify** (the magnitudes that could have killed the design):

- Work per occurrence: ≤ 4 offset lookups + ≤ 2 UTC→local conversions ≈ a few
  µs. A 100-year subscription is `count = 1200` → ~1200 iterations, order
  **10 ms**, and `ZoneInfo` caches the parsed zone per key, so the tz file is
  read **once**, not 1200 times.
- Memory: 1200 × ~21-byte strings ≈ **25 KB** — a list return is fine; no
  generator/streaming needed. (This is what makes returning a `list` per the
  contract non-controversial.)
- Cand-C's cost: ~40 conversions/occurrence ≈ 10× B for zero spec benefit —
  the estimate is what demotes it, not taste.
- Drift: month arithmetic is O(1) from `start` (`total = y*12 + m-1 + i`), so
  error accumulation over 1200 occurrences is exactly **zero**, versus an
  iterative scheme where one clamp poisons every later occurrence.

**Spec sweep — every element consumed or declined:**

| Spec element | Where consumed |
|---|---|
| `start_local` `"YYYY-MM-DDTHH:MM"` | `_LOCAL_RE.fullmatch` + `datetime(...)` |
| naive wall clock in `tz` | `_to_utc`, never `astimezone` from naive |
| start guaranteed valid/unambiguous | relied on only as "no special case needed"; code handles it anyway |
| `tz` IANA | `ZoneInfo(tz)` |
| monthly, i-th calendar month, 0-indexed | `_add_months(y, m, i)` |
| anchor day / last-day clamp / anchor never changes | `min(anchor_day, monthrange(...)[1])`, `anchor_day` from `start` |
| wall time `HH:MM` each occurrence | `datetime(..., start.hour, start.minute)` |
| gap → shift forward by gap length | `_gap_length` + re-resolve (both 60- and 30-min examples) |
| ambiguous → earlier | `min(instants)` |
| return `count` strings, UTC, `...SSZ` | loop + `_format_utc` |
| `ValueError` on `count < 1` | first guard |
| `ValueError` on malformed `start_local` | `_parse_local` (both regex and calendar failure) |
| *declined:* invalid tz, non-int `count`, sub-minute offsets, year > 9999 | documented in the docstring/notes below |

**Steelman of the strongest rejected candidate (Cand-A):** if `fold` semantics
are as I believe, Cand-A is *provably* identical to Cand-B on all three cases
— including the gap, where `dt − o_pre` is exactly the instant of the
gap-shifted wall time — at 1/20th the code. My 19 extra lines buy insurance
against a single fact, not against a class of bugs. I still take the insurance:
the run cannot execute, and a silent one-hour-backwards error on every DST
boundary is the worst possible failure for a billing system.

**Strongest surviving objection:** the hardcoded transition dates in the tests
(A3) are tzdata-dependent; a system with unusual tzdata could fail a test for
reasons unrelated to the code. Mitigated by `skipUnless(_zone_available(...))`
for missing tzdata, and by the fact that the failure mode is a red test, not a
shipped bug.

---

## 5. Verify

**The check, defined before finalizing:** hand-trace the three transition cases
plus the anchor case with real values; if any trace disagrees with C3–C6, the
design goes back to Branch.

*Trace 1 — C4, 60-minute gap.* `("2024-02-10T02:30", "America/New_York", 2)`,
anchor 10.
- i=0: Feb has 29 days, `min(10,29)=10` → naive `2024-02-10 02:30`. Both folds
  give −05:00 → one candidate `07:30Z`; round-trip `07:30Z → 02:30 EST` ✓ →
  `2024-02-10T07:30:00Z`.
- i=1: `total = 2024*12 + 1 + 1 = 24290`; `24290 // 12 = 2024`,
  `24290 % 12 + 1 = 3` → March, day `min(10,31)=10` → naive `2024-03-10 02:30`.
  Offsets {−05:00, −04:00}. Candidate `07:30Z` → local 03:30 EDT ≠ 02:30 ✗;
  candidate `06:30Z` → local 01:30 EST ≠ 02:30 ✗. Empty ⇒ gap of
  `|−4 − (−5)| = 60 min` ⇒ shifted `03:30` → single offset −04:00 →
  `07:30Z`, round-trip 03:30 ✓ → **`2024-03-10T07:30:00Z`**. Matches C4. ✓

*Trace 2 — C6, ambiguity.* naive `2024-11-03 01:30`, offsets {−04:00, −05:00}.
Candidate `05:30Z` → local 01:30 EDT (the transition is 06:00Z) ✓;
candidate `06:30Z` → local 01:30 EST ✓. Two valid ⇒ `min` = **`05:30Z`**, the
earlier. Matches C6. ✓ (`min` on aware UTC datetimes compares instants.)

*Trace 3 — C5, 30-minute gap.* naive `2024-10-06 02:15` Lord Howe, offsets
{+10:30, +11:00}. `15:45Z(Oct 5)` → local 02:45 ≠ 02:15 ✗; `15:15Z` → local
01:45 ≠ 02:15 ✗. Empty ⇒ gap 30 min ⇒ shifted `02:45` at +11:00 →
`02:45 − 11:00` = **`2024-10-05T15:45:00Z`**. Matches C5, and confirms the
implementation reads the gap *length* rather than assuming an hour. ✓

*Trace 4 — C3, anchor.* `("2024-01-31T12:00","UTC",4)`, anchor 31:
`min(31,31)=31`, `min(31,29)=29`, `min(31,31)=31`, `min(31,30)=30` →
Jan 31, Feb 29, Mar 31, Apr 30. Mar is **31**. ✓

*Trace 5 — A3 weekday arithmetic.* 2024-03-01 = Friday ⇒ Sundays 3, 10 ⇒ 2nd
Sunday = **Mar 10** ✓. 2024-11-01 = Friday ⇒ Sundays 3, 10 ⇒ 1st = **Nov 3** ✓.
2024-04-01 = Monday ⇒ 1st Sunday = **Apr 7** ✓. 2024-10-01 = Tuesday ⇒ 1st
Sunday = **Oct 6** ✓.

**Breadth sweep of the risk nouns** (the tests are the enumeration deliverable):
dates/calendar (anchor 29/30/31, leap and common Februaries, leap-day start,
year rollover, Dec→Jan wrap, 25-month no-drift), timezones (UTC, +05:30
half-hour with date rollback, +09:00, CEST), DST (60-min gap, 30-min gap,
northern fall-back, southern fall-back, clamp × DST interaction, monotonicity
across two transitions), malformed input (16 shapes), count bounds (0, −1,
−100, 1, 37), output shape (type, length, format regex, identity of
occurrence 0). Money/permissions/offline/retries/concurrency: **not applicable
— this is a pure function with no I/O, no state and no clock read**, so those
nouns are declined rather than missing.

**Re-reading Frame last:** C1 covered by `test_returns_list_of_formatted_strings`
+ `test_length_matches_count`; C2 by `test_first_occurrence_is_the_start_itself`;
C3 by `test_anchor_31_clamps_then_restores_*`; C4/C5 by the gap tests; C6 by the
two fall-back tests; C7 by `TestValidation`; C8 by
`test_instants_are_strictly_increasing_across_transitions`. No criterion
drifted, none was quietly dropped.

---

## Deliverable

### `billing.py`

```python
"""Monthly billing-instant generator.

Given a naive local start wall time, an IANA timezone and a count, produce the
UTC instants of `count` monthly occurrences.

Design notes
------------
* The anchor day is remembered from the *start* and re-applied to every month
  independently, so a Jan 31 start bills Feb 28/29 and then Mar 31 again.
  (Deriving each occurrence from the previous one would let the February clamp
  stick and yield Mar 28.)
* Local -> UTC resolution never relies on the meaning of ``fold``. ``fold`` is
  used only as an *oracle for the pair of candidate offsets* around a
  transition; each candidate instant is then validated by converting it back
  UTC -> local (a direction that is always unambiguous). That makes the
  classification correct regardless of which fold value maps to the
  pre-transition offset, and it also handles zones with negative DST.
"""

import calendar
import re
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo

__all__ = ["billing_instants"]

_LOCAL_RE = re.compile(r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})")


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing instants as ``YYYY-MM-DDTHH:MM:SSZ``.

    Raises ValueError for ``count < 1`` or a malformed ``start_local``.
    An unknown ``tz`` propagates ``zoneinfo.ZoneInfoNotFoundError``.
    """
    if not isinstance(count, int) or count < 1:
        raise ValueError(f"count must be a positive integer, got {count!r}")

    start = _parse_local(start_local)
    zone = ZoneInfo(tz)
    anchor_day = start.day

    out = []
    for i in range(count):
        year, month = _add_months(start.year, start.month, i)
        day = min(anchor_day, calendar.monthrange(year, month)[1])
        naive = datetime(year, month, day, start.hour, start.minute)
        out.append(_format_utc(_to_utc(naive, zone)))
    return out


def _parse_local(start_local: str) -> datetime:
    if not isinstance(start_local, str):
        raise ValueError(
            f"start_local must be a string, got {type(start_local).__name__}"
        )
    match = _LOCAL_RE.fullmatch(start_local)
    if match is None:
        raise ValueError(
            f"malformed start_local: {start_local!r} (expected YYYY-MM-DDTHH:MM)"
        )
    year, month, day, hour, minute = (int(part) for part in match.groups())
    try:
        return datetime(year, month, day, hour, minute)
    except ValueError as exc:
        raise ValueError(f"malformed start_local: {start_local!r} ({exc})") from exc


def _add_months(year: int, month: int, delta: int) -> tuple[int, int]:
    total = year * 12 + (month - 1) + delta
    return total // 12, total % 12 + 1


def _to_utc(naive: datetime, zone: ZoneInfo) -> datetime:
    """Resolve a naive wall time in `zone` to a UTC instant.

    exists & unambiguous -> that instant
    ambiguous (fall-back) -> the earlier of the two instants
    non-existent (gap)    -> the wall time shifted forward by the gap length
    """
    instants = _instants_for(naive, zone)
    if instants:
        return min(instants)

    shifted = naive + _gap_length(naive, zone)
    instants = _instants_for(shifted, zone)
    if not instants:  # would need two transitions inside one gap
        raise ValueError(
            f"cannot resolve wall time {naive.isoformat()} in {zone.key}"
        )
    return min(instants)


def _candidate_offsets(naive: datetime, zone: ZoneInfo) -> tuple[timedelta, timedelta]:
    """The (fold=0, fold=1) offsets: both offsets around a nearby transition."""
    return (
        naive.replace(tzinfo=zone, fold=0).utcoffset(),
        naive.replace(tzinfo=zone, fold=1).utcoffset(),
    )


def _instants_for(naive: datetime, zone: ZoneInfo) -> list[datetime]:
    """UTC instants whose local time in `zone` is exactly `naive` (0, 1 or 2)."""
    found = []
    for offset in set(_candidate_offsets(naive, zone)):
        instant = (naive - offset).replace(tzinfo=timezone.utc)
        if instant.astimezone(zone).replace(tzinfo=None) == naive:
            found.append(instant)
    return found


def _gap_length(naive: datetime, zone: ZoneInfo) -> timedelta:
    before, after = _candidate_offsets(naive, zone)
    return abs(after - before)


def _format_utc(instant: datetime) -> str:
    return (
        f"{instant.year:04d}-{instant.month:02d}-{instant.day:02d}"
        f"T{instant.hour:02d}:{instant.minute:02d}:{instant.second:02d}Z"
    )
```

### `test_billing.py`

```python
"""Tests for billing.billing_instants — run with `python -m unittest`."""

import re
import unittest
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

from billing import billing_instants

_UTC_FORMAT = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z")


def _zone_available(key: str) -> bool:
    try:
        ZoneInfo(key)
    except ZoneInfoNotFoundError:
        return False
    return True


class TestAnchorRule(unittest.TestCase):
    """Calendar arithmetic: the anchor day never changes, only clamps."""

    def test_plain_monthly_run(self):
        self.assertEqual(
            billing_instants("2024-01-15T09:00", "UTC", 3),
            [
                "2024-01-15T09:00:00Z",
                "2024-02-15T09:00:00Z",
                "2024-03-15T09:00:00Z",
            ],
        )

    def test_anchor_31_clamps_then_restores_leap_year(self):
        self.assertEqual(
            billing_instants("2024-01-31T12:00", "UTC", 4),
            [
                "2024-01-31T12:00:00Z",
                "2024-02-29T12:00:00Z",  # clamped
                "2024-03-31T12:00:00Z",  # anchor restored, not Mar 28/29
                "2024-04-30T12:00:00Z",  # clamped again
            ],
        )

    def test_anchor_31_clamps_then_restores_common_year(self):
        self.assertEqual(
            billing_instants("2023-01-31T00:00", "UTC", 3),
            [
                "2023-01-31T00:00:00Z",
                "2023-02-28T00:00:00Z",
                "2023-03-31T00:00:00Z",
            ],
        )

    def test_anchor_30_clamps_in_february(self):
        self.assertEqual(
            billing_instants("2023-01-30T08:00", "UTC", 3),
            [
                "2023-01-30T08:00:00Z",
                "2023-02-28T08:00:00Z",
                "2023-03-30T08:00:00Z",
            ],
        )

    def test_anchor_29_clamps_only_in_common_february(self):
        self.assertEqual(
            billing_instants("2023-01-29T08:00", "UTC", 2),
            ["2023-01-29T08:00:00Z", "2023-02-28T08:00:00Z"],
        )
        self.assertEqual(
            billing_instants("2024-01-29T08:00", "UTC", 2),
            ["2024-01-29T08:00:00Z", "2024-02-29T08:00:00Z"],
        )

    def test_leap_day_start_anchor_29(self):
        got = billing_instants("2024-02-29T10:00", "UTC", 13)
        self.assertEqual(got[0], "2024-02-29T10:00:00Z")
        self.assertEqual(got[1], "2024-03-29T10:00:00Z")
        self.assertEqual(got[11], "2025-01-29T10:00:00Z")
        self.assertEqual(got[12], "2025-02-28T10:00:00Z")  # 2025 is not a leap year

    def test_year_rollover(self):
        self.assertEqual(
            billing_instants("2023-11-15T00:00", "UTC", 4),
            [
                "2023-11-15T00:00:00Z",
                "2023-12-15T00:00:00Z",
                "2024-01-15T00:00:00Z",
                "2024-02-15T00:00:00Z",
            ],
        )

    def test_december_start_wraps_to_january(self):
        self.assertEqual(
            billing_instants("2024-12-31T23:59", "UTC", 3),
            [
                "2024-12-31T23:59:00Z",
                "2025-01-31T23:59:00Z",
                "2025-02-28T23:59:00Z",
            ],
        )

    def test_no_cumulative_drift_over_long_horizon(self):
        got = billing_instants("2024-01-31T00:00", "UTC", 25)
        self.assertEqual(got[12], "2025-01-31T00:00:00Z")
        self.assertEqual(got[13], "2025-02-28T00:00:00Z")
        self.assertEqual(got[24], "2026-01-31T00:00:00Z")


class TestFixedOffsetZones(unittest.TestCase):
    """Zones without DST, including a half-hour offset and date rollback."""

    @unittest.skipUnless(_zone_available("Asia/Kolkata"), "tzdata unavailable")
    def test_half_hour_offset_rolls_date_back(self):
        self.assertEqual(
            billing_instants("2024-01-15T00:15", "Asia/Kolkata", 2),
            ["2024-01-14T18:45:00Z", "2024-02-14T18:45:00Z"],
        )

    @unittest.skipUnless(_zone_available("Asia/Tokyo"), "tzdata unavailable")
    def test_whole_hour_offset_no_dst(self):
        self.assertEqual(
            billing_instants("2024-06-10T09:00", "Asia/Tokyo", 2),
            ["2024-06-10T00:00:00Z", "2024-07-10T00:00:00Z"],
        )


class TestDstTransitions(unittest.TestCase):
    """Gap (spring forward) and ambiguity (fall back) handling."""

    @unittest.skipUnless(_zone_available("America/New_York"), "tzdata unavailable")
    def test_60_minute_gap_shifts_forward_by_one_hour(self):
        # 2024-03-10 02:30 does not exist in New York (02:00 EST -> 03:00 EDT).
        # It becomes 03:30 EDT, which is the same UTC instant as 02:30 EST.
        self.assertEqual(
            billing_instants("2024-02-10T02:30", "America/New_York", 2),
            ["2024-02-10T07:30:00Z", "2024-03-10T07:30:00Z"],
        )

    @unittest.skipUnless(_zone_available("America/New_York"), "tzdata unavailable")
    def test_ambiguous_time_uses_earlier_instant(self):
        # 2024-11-03 01:30 happens twice: 05:30Z (EDT) and 06:30Z (EST).
        self.assertEqual(
            billing_instants("2024-10-03T01:30", "America/New_York", 2),
            ["2024-10-03T05:30:00Z", "2024-11-03T05:30:00Z"],
        )

    @unittest.skipUnless(_zone_available("America/New_York"), "tzdata unavailable")
    def test_anchor_clamp_and_dst_change_together(self):
        # Jan 31 -> Feb 29 (still EST) -> Mar 31 (now EDT, so one hour earlier UTC).
        self.assertEqual(
            billing_instants("2024-01-31T02:30", "America/New_York", 3),
            [
                "2024-01-31T07:30:00Z",
                "2024-02-29T07:30:00Z",
                "2024-03-31T06:30:00Z",
            ],
        )

    @unittest.skipUnless(_zone_available("Australia/Lord_Howe"), "tzdata unavailable")
    def test_30_minute_gap_shifts_forward_by_thirty_minutes(self):
        # 2024-10-06 02:00 +10:30 -> 02:30 +11:00, so 02:15 does not exist
        # and becomes 02:45 +11:00.
        self.assertEqual(
            billing_instants("2024-09-06T02:15", "Australia/Lord_Howe", 2),
            ["2024-09-05T15:45:00Z", "2024-10-05T15:45:00Z"],
        )

    @unittest.skipUnless(_zone_available("Australia/Sydney"), "tzdata unavailable")
    def test_southern_hemisphere_fall_back_uses_earlier_instant(self):
        # 2024-04-07 02:30 happens twice in Sydney (+11 then +10); take +11.
        self.assertEqual(
            billing_instants("2024-03-07T02:30", "Australia/Sydney", 2),
            ["2024-03-06T15:30:00Z", "2024-04-06T15:30:00Z"],
        )

    @unittest.skipUnless(_zone_available("America/New_York"), "tzdata unavailable")
    def test_instants_are_strictly_increasing_across_transitions(self):
        got = billing_instants("2024-01-15T02:30", "America/New_York", 24)
        self.assertEqual(got, sorted(got))
        self.assertEqual(len(set(got)), len(got))


class TestValidation(unittest.TestCase):
    def test_count_must_be_positive(self):
        for bad in (0, -1, -100):
            with self.subTest(count=bad):
                with self.assertRaises(ValueError):
                    billing_instants("2024-01-15T09:00", "UTC", bad)

    def test_malformed_start_local(self):
        bad_inputs = [
            "",
            "not-a-date",
            "2024-01-15 09:00",       # space instead of T
            "2024-1-5T09:00",         # unpadded fields
            "2024-01-15T09:00:00",    # seconds not accepted
            "2024-01-15T09:00Z",      # trailing zone designator
            "2024-01-15T09:00\n",     # trailing newline
            "2024-01-15",             # no time
            "T09:00",                 # no date
            "2024-13-01T00:00",       # month out of range
            "2024-00-10T00:00",       # month zero
            "2024-01-32T00:00",       # day out of range
            "2023-02-29T00:00",       # not a leap year
            "2024-04-31T00:00",       # April has 30 days
            "2024-01-15T24:00",       # hour out of range
            "2024-01-15T09:60",       # minute out of range
        ]
        for bad in bad_inputs:
            with self.subTest(start_local=bad):
                with self.assertRaises(ValueError):
                    billing_instants(bad, "UTC", 1)

    def test_non_string_start_local(self):
        with self.assertRaises(ValueError):
            billing_instants(20240115, "UTC", 1)

    def test_count_validated_before_start_local(self):
        # Both are invalid; either way it must be a ValueError, never a crash.
        with self.assertRaises(ValueError):
            billing_instants("nope", "UTC", 0)


class TestOutputShape(unittest.TestCase):
    def test_length_matches_count(self):
        for count in (1, 2, 12, 37):
            with self.subTest(count=count):
                self.assertEqual(len(billing_instants("2024-01-31T06:00", "UTC", count)), count)

    def test_returns_list_of_formatted_strings(self):
        got = billing_instants("2024-01-15T09:00", "UTC", 5)
        self.assertIsInstance(got, list)
        for value in got:
            self.assertIsInstance(value, str)
            self.assertRegex(value, _UTC_FORMAT)

    @unittest.skipUnless(_zone_available("Europe/Berlin"), "tzdata unavailable")
    def test_first_occurrence_is_the_start_itself(self):
        self.assertEqual(
            billing_instants("2024-06-15T14:00", "Europe/Berlin", 1),
            ["2024-06-15T12:00:00Z"],  # CEST = UTC+2
        )


if __name__ == "__main__":
    unittest.main()
```

---

## Notes / declared behaviour outside the contract

- **Unknown timezone** propagates `zoneinfo.ZoneInfoNotFoundError` (a `KeyError`
  subclass). The contract enumerates `ValueError` only for `count` and
  `start_local`, so I did not widen it; if callers want one exception type, wrap
  at the boundary.
- **Non-`int` `count`** raises `ValueError` (A4) — chosen over `TypeError` to
  keep a single documented failure channel.
- **Sub-minute UTC seconds** can be non-zero for pre-1900 starts in zones whose
  historical LMT offset has a seconds component (e.g. Europe/Amsterdam
  +00:19:32). The formatter prints the real seconds rather than forcing `:00`.
- **Year > 9999** (a very large `count`) raises `ValueError` from the `datetime`
  constructor rather than being clamped.
- **Gaps that cross midnight** move the occurrence to the following calendar
  day; that is what "shift forward by the length of the gap" requires, and the
  anchor is not re-applied afterwards.
- **`min()` on aware UTC datetimes** compares instants, which is exactly the
  "earlier of the two" tiebreak the ambiguity rule asks for.
- Nothing was executed. Correctness rests on the Verify hand-traces; the first
  real run should confirm the six criteria C3–C8 in particular.

```text
STATUS: DONE
CONFIDENCE: medium — actually running `python -m unittest` (or just confirming the tzdata transition dates in A3) would raise it to high; the algorithm itself is execution-independent by construction.
ASSUMPTIONS: A2 no tzdata zone has two transitions inside one gap (guarded by an explicit raise, not silent output); A3 the hardcoded 2024 transition dates for New_York / Sydney / Lord_Howe and Lord Howe's 02:00 +10:30->+11:00 rule — data used only in tests, hand-traced from the standing DST rules and 2024 weekdays; A4 non-int `count` is reported as ValueError. A1 (which fold value is the pre-transition offset) was deliberately designed out and is no longer load-bearing.
ATTACK: the tests pin real tzdata transition dates, so an exotic or outdated tzdata could fail a test for reasons unrelated to the code. It does not kill the answer because the resolution logic derives the gap length and the ambiguity pair from the zone at runtime rather than from those constants — the dates are assertions about the world, not inputs to the algorithm, so a wrong one produces a red test rather than a wrong invoice.
```
