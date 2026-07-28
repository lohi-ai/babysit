# P6 — monthly billing-instant generator

## `billing.py`

```python
"""Monthly billing instants: anchor-stable dates, wall-clock-stable times."""

import calendar
import re
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

__all__ = ["billing_instants"]

_LOCAL_RE = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}")


def _parse_start(start_local: str) -> datetime:
    """Parse a strict "YYYY-MM-DDTHH:MM" naive wall-clock string."""
    if not _LOCAL_RE.fullmatch(start_local):
        raise ValueError(f"malformed start_local: {start_local!r}")
    # strptime also rejects impossible dates such as 2021-02-30.
    return datetime.strptime(start_local, "%Y-%m-%dT%H:%M")


def _occurrence_date(start: datetime, months: int) -> datetime:
    """Naive local datetime of occurrence `months` after `start`.

    The anchor day is always the start's day-of-month; it is clamped to the
    last day of a shorter month but never rewritten, so a Jan 31 anchor gives
    Feb 28 and then Mar 31 again.
    """
    total = start.year * 12 + (start.month - 1) + months
    year, month_index = divmod(total, 12)
    month = month_index + 1
    day = min(start.day, calendar.monthrange(year, month)[1])
    return datetime(year, month, day, start.hour, start.minute)


def _to_utc(naive: datetime, zone: ZoneInfo) -> datetime:
    """Resolve a naive local time in `zone` to an aware UTC datetime.

    Nonexistent wall times (spring-forward gap) shift forward by the gap
    length; ambiguous wall times (fall-back) take the earlier instant.
    """
    off_before = naive.replace(tzinfo=zone, fold=0).utcoffset()
    off_after = naive.replace(tzinfo=zone, fold=1).utcoffset()

    if off_before == off_after:
        # Wall time exists exactly once.
        return naive.replace(tzinfo=zone).astimezone(timezone.utc)

    if off_before > off_after:
        # Offset shrank across the transition: the clock went back and this
        # wall time happens twice. The earlier instant is the one still using
        # the larger pre-transition offset.
        return (naive - off_before).replace(tzinfo=timezone.utc)

    # Offset grew across the transition: the clock jumped forward and this
    # wall time never happens. Shift forward by the gap; the shifted time is
    # at or after the transition, so it uses the post-transition offset.
    gap = off_after - off_before
    return (naive + gap - off_after).replace(tzinfo=timezone.utc)


def _format_utc(dt: datetime) -> str:
    return (
        f"{dt.year:04d}-{dt.month:02d}-{dt.day:02d}"
        f"T{dt.hour:02d}:{dt.minute:02d}:{dt.second:02d}Z"
    )


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing instants as UTC "YYYY-MM-DDTHH:MM:SSZ"."""
    if count < 1:
        raise ValueError(f"count must be >= 1, got {count!r}")
    start = _parse_start(start_local)
    zone = ZoneInfo(tz)
    return [
        _format_utc(_to_utc(_occurrence_date(start, i), zone))
        for i in range(count)
    ]
```

## `test_billing.py`

```python
import unittest
from zoneinfo import ZoneInfo

from billing import billing_instants


def require_zone(name):
    """Skip the test if the tz database lacks `name`."""
    try:
        ZoneInfo(name)
    except Exception as exc:  # ZoneInfoNotFoundError, or no tzdata at all
        raise unittest.SkipTest(f"timezone {name!r} unavailable: {exc}")
    return name


class BasicScheduleTests(unittest.TestCase):
    def test_single_occurrence_is_the_start(self):
        self.assertEqual(
            billing_instants("2021-06-10T09:15", "UTC", 1),
            ["2021-06-10T09:15:00Z"],
        )

    def test_plain_monthly_sequence(self):
        self.assertEqual(
            billing_instants("2021-01-15T09:00", "UTC", 3),
            [
                "2021-01-15T09:00:00Z",
                "2021-02-15T09:00:00Z",
                "2021-03-15T09:00:00Z",
            ],
        )

    def test_crosses_year_boundary(self):
        self.assertEqual(
            billing_instants("2021-11-30T08:00", "UTC", 4),
            [
                "2021-11-30T08:00:00Z",
                "2021-12-30T08:00:00Z",
                "2022-01-30T08:00:00Z",
                "2022-02-28T08:00:00Z",
            ],
        )

    def test_fixed_half_hour_offset_zone(self):
        require_zone("Asia/Kolkata")
        self.assertEqual(
            billing_instants("2021-06-10T09:15", "Asia/Kolkata", 2),
            ["2021-06-10T03:45:00Z", "2021-07-10T03:45:00Z"],
        )


class AnchorRuleTests(unittest.TestCase):
    def test_short_month_clamps_to_last_day(self):
        self.assertEqual(
            billing_instants("2021-01-31T12:00", "UTC", 4),
            [
                "2021-01-31T12:00:00Z",
                "2021-02-28T12:00:00Z",
                "2021-03-31T12:00:00Z",
                "2021-04-30T12:00:00Z",
            ],
        )

    def test_leap_february(self):
        self.assertEqual(
            billing_instants("2020-01-31T00:00", "UTC", 3),
            [
                "2020-01-31T00:00:00Z",
                "2020-02-29T00:00:00Z",
                "2020-03-31T00:00:00Z",
            ],
        )

    def test_anchor_survives_a_full_year_of_clamping(self):
        got = billing_instants("2020-01-31T10:00", "UTC", 14)
        self.assertEqual(len(got), 14)
        self.assertEqual(got[1], "2020-02-29T10:00:00Z")   # leap clamp
        self.assertEqual(got[3], "2020-04-30T10:00:00Z")   # 30-day clamp
        self.assertEqual(got[4], "2020-05-31T10:00:00Z")   # anchor restored
        self.assertEqual(got[12], "2021-01-31T10:00:00Z")
        self.assertEqual(got[13], "2021-02-28T10:00:00Z")  # non-leap clamp

    def test_day_30_anchor_clamps_only_in_february(self):
        got = billing_instants("2021-01-30T00:00", "UTC", 3)
        self.assertEqual(
            got,
            [
                "2021-01-30T00:00:00Z",
                "2021-02-28T00:00:00Z",
                "2021-03-30T00:00:00Z",
            ],
        )


class WallTimeTests(unittest.TestCase):
    def test_wall_time_is_preserved_across_a_dst_change(self):
        # 12:00 New York is 17:00Z under EST and 16:00Z under EDT.
        require_zone("America/New_York")
        self.assertEqual(
            billing_instants("2021-02-20T12:00", "America/New_York", 3),
            [
                "2021-02-20T17:00:00Z",
                "2021-03-20T16:00:00Z",
                "2021-04-20T16:00:00Z",
            ],
        )

    def test_spring_forward_gap_shifts_by_one_hour(self):
        # 2021-03-14 02:00 EST -> 03:00 EDT, so 02:30 does not exist and
        # becomes 03:30 EDT == 07:30Z, matching the earlier 02:30 EST rows.
        require_zone("America/New_York")
        self.assertEqual(
            billing_instants("2021-01-14T02:30", "America/New_York", 3),
            [
                "2021-01-14T07:30:00Z",
                "2021-02-14T07:30:00Z",
                "2021-03-14T07:30:00Z",
            ],
        )

    def test_fall_back_ambiguity_takes_the_earlier_instant(self):
        # 2021-11-07 02:00 EDT -> 01:00 EST, so 01:30 happens twice; the
        # earlier one is 01:30 EDT == 05:30Z (the later would be 06:30Z).
        require_zone("America/New_York")
        got = billing_instants("2021-09-07T01:30", "America/New_York", 3)
        self.assertEqual(
            got,
            [
                "2021-09-07T05:30:00Z",
                "2021-10-07T05:30:00Z",
                "2021-11-07T05:30:00Z",
            ],
        )
        self.assertNotEqual(got[2], "2021-11-07T06:30:00Z")

    def test_thirty_minute_gap_shifts_by_thirty_minutes(self):
        # Lord Howe moves +10:30 -> +11:00 at 02:00 on 2021-10-03, a 30 minute
        # gap, so 02:15 becomes 02:45 (+11:00) == 2021-10-02T15:45Z.
        require_zone("Australia/Lord_Howe")
        self.assertEqual(
            billing_instants("2021-08-03T02:15", "Australia/Lord_Howe", 3),
            [
                "2021-08-02T15:45:00Z",
                "2021-09-02T15:45:00Z",
                "2021-10-02T15:45:00Z",
            ],
        )


class ValidationTests(unittest.TestCase):
    def test_count_below_one(self):
        for count in (0, -1, -100):
            with self.subTest(count=count):
                with self.assertRaises(ValueError):
                    billing_instants("2021-01-15T09:00", "UTC", count)

    def test_malformed_start_local(self):
        bad = [
            "",
            "not-a-date",
            "2021-1-5T09:00",        # unpadded fields
            "2021-01-05 09:00",      # space instead of T
            "2021-01-05T09:00:00",   # seconds not accepted
            "2021-01-05T09:00Z",     # trailing designator
            "2021-01-05",            # no time
            "20210105T0900",         # no separators
            "2021-13-01T00:00",      # month out of range
            "2021-02-30T00:00",      # day out of range
            "2021-01-05T24:00",      # hour out of range
            "2021-01-05T09:60",      # minute out of range
        ]
        for value in bad:
            with self.subTest(start_local=value):
                with self.assertRaises(ValueError):
                    billing_instants(value, "UTC", 1)


if __name__ == "__main__":
    unittest.main()
```

## Notes

**Anchor stability.** The anchor day is read once from the start and re-clamped
against each target month independently (`min(anchor, monthrange(y, m)[1])`),
so clamping is never cumulative: Jan 31 → Feb 28 → Mar 31. Month arithmetic
goes through an absolute month index (`year*12 + month-1`) rather than repeated
date addition, which keeps occurrence `i` a pure function of `i`.

**Gap / fold resolution.** For a naive local time `t`, `t.replace(tzinfo=zone,
fold=0).utcoffset()` and the same with `fold=1` differ only at a transition
(PEP 495), and their *direction* classifies it:

| comparison | meaning | rule applied |
|---|---|---|
| `off0 == off1` | ordinary time | direct `astimezone(UTC)` |
| `off0 > off1` | offset shrank → clock went back → ambiguous | earlier instant = `t - off0` |
| `off0 < off1` | offset grew → clock jumped → nonexistent | `t + (off1 - off0)`, then `- off1` |

This is offset-direction based, not DST-flag based, so it also behaves
correctly in zones with negative DST (e.g. Europe/Dublin) and in zones whose
transitions are not one hour (Lord Howe's 30 minutes, and historical
transitions of other sizes). The gap branch derives the shifted wall time
explicitly rather than relying on the (true, but easy-to-misread) identity
that `fold=0` in a gap already yields the pre-transition offset — all three
branches happen to reduce to `t - off0`, but writing the rule out keeps the
spec visible in the code.

For the ambiguous case, "earlier instant" is the one with the *larger* offset
(UTC = local − offset), which is the pre-transition offset — i.e. `fold=0`.

**Validation.** `start_local` is checked with a strict fixed-width regex before
`strptime`, because `strptime`'s `%m`/`%d`/`%H` accept unpadded fields and
`fromisoformat` accepts basic-format and second/offset-bearing variants — both
would silently admit strings the contract calls malformed. Structural rejects
come from the regex; semantic ones (2021-02-30, month 13, hour 24) come from
`strptime`, and both raise `ValueError`.

**Formatting.** The output is built with explicit `f"{...:02d}"` fields instead
of `strftime`, since `strftime`'s zero-padding of years < 1000 is
platform-dependent. Seconds are emitted because the contract asks for them;
they are normally `00`, but they can be nonzero for pre-standard-time dates
whose LMT offsets carry seconds — the arithmetic path handles that without a
special case.

**Deliberately not handled.** An invalid `tz` propagates
`ZoneInfoNotFoundError` (a `KeyError` subclass) rather than being converted —
the contract only specifies `ValueError` for `count` and `start_local`. A
`count` large enough to pass year 9999 raises `ValueError` from `datetime`
construction, which is a reasonable outcome and needs no extra code.

**Untested by construction.** Nothing was executed. The riskiest expected
values are the tz-dependent ones; they rest on: US DST 2021 starting Sun
Mar 14 02:00 and ending Sun Nov 7 02:00, and Lord Howe starting Sun Oct 3
02:00 (first Sunday of October, +10:30 → +11:00). Each tz test skips rather
than fails if the zone is missing from the platform tz database.
