# P6 — monthly billing-instant generator (fable-base)

## billing.py

```python
"""Monthly billing-instant generator (stdlib only)."""

import calendar
import re
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

_FORMAT = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}")


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing instants as UTC strings.

    `start_local` is "YYYY-MM-DDTHH:MM", a naive wall-clock time in the IANA
    timezone `tz`. Occurrence 0 is the start itself; occurrence i falls in the
    i-th calendar month after the start month, on the anchor day-of-month
    (clamped to the last day of shorter months, without changing the anchor),
    at the start's wall time resolved per DST rules.
    """
    if count < 1:
        raise ValueError(f"count must be >= 1, got {count}")
    if not _FORMAT.fullmatch(start_local):
        raise ValueError(f"malformed start_local: {start_local!r}")
    # Regex fixes the shape; strptime rejects out-of-range fields
    # (month 13, Feb 30, hour 25, ...) with ValueError.
    start = datetime.strptime(start_local, "%Y-%m-%dT%H:%M")

    zone = ZoneInfo(tz)
    anchor_day = start.day
    out: list[str] = []
    for i in range(count):
        months = start.month - 1 + i
        year = start.year + months // 12
        month = months % 12 + 1
        day = min(anchor_day, calendar.monthrange(year, month)[1])
        # fold=0 implements both wall-time rules (PEP 495 semantics):
        # - ambiguous (fall-back): fold=0 selects the earlier of the two
        #   instants (the pre-transition offset);
        # - nonexistent (spring-forward): fold=0 extrapolates the
        #   pre-transition offset, which maps to the instant exactly one
        #   gap-length after the requested wall time (a 60-minute gap turns
        #   02:30 into 03:30, a 30-minute gap turns 02:15 into 02:45).
        local = datetime(
            year, month, day, start.hour, start.minute, tzinfo=zone, fold=0
        )
        out.append(local.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    return out
```

## test_billing.py

```python
"""Tests for billing.billing_instants (python -m unittest)."""

import unittest

from billing import billing_instants


class TestBasic(unittest.TestCase):
    def test_utc_simple_monthly(self):
        self.assertEqual(
            billing_instants("2024-01-15T10:00", "UTC", 3),
            [
                "2024-01-15T10:00:00Z",
                "2024-02-15T10:00:00Z",
                "2024-03-15T10:00:00Z",
            ],
        )

    def test_occurrence_zero_is_start(self):
        self.assertEqual(
            billing_instants("2024-06-01T00:00", "UTC", 1),
            ["2024-06-01T00:00:00Z"],
        )

    def test_year_rollover(self):
        self.assertEqual(
            billing_instants("2025-11-15T00:00", "UTC", 3),
            [
                "2025-11-15T00:00:00Z",
                "2025-12-15T00:00:00Z",
                "2026-01-15T00:00:00Z",
            ],
        )

    def test_fixed_offset_conversion(self):
        # Asia/Tokyo is UTC+9 year-round, no DST.
        self.assertEqual(
            billing_instants("2025-03-10T08:30", "Asia/Tokyo", 2),
            ["2025-03-09T23:30:00Z", "2025-04-09T23:30:00Z"],
        )


class TestAnchorRule(unittest.TestCase):
    def test_clamp_to_short_month_then_restore_anchor(self):
        # Jan 31 -> Feb 28 (2025 not a leap year) -> Mar 31 (anchor kept,
        # not Mar 28) -> Apr 30.
        self.assertEqual(
            billing_instants("2025-01-31T12:00", "UTC", 4),
            [
                "2025-01-31T12:00:00Z",
                "2025-02-28T12:00:00Z",
                "2025-03-31T12:00:00Z",
                "2025-04-30T12:00:00Z",
            ],
        )

    def test_leap_february(self):
        self.assertEqual(
            billing_instants("2024-01-31T12:00", "UTC", 2),
            ["2024-01-31T12:00:00Z", "2024-02-29T12:00:00Z"],
        )

    def test_dec_31_across_year_boundary(self):
        # 2026 is not a leap year.
        self.assertEqual(
            billing_instants("2025-12-31T23:30", "UTC", 3),
            [
                "2025-12-31T23:30:00Z",
                "2026-01-31T23:30:00Z",
                "2026-02-28T23:30:00Z",
            ],
        )


class TestWallTimeRule(unittest.TestCase):
    def test_wall_time_kept_across_dst_change(self):
        # America/New_York: EST (-05:00) in February, EDT (-04:00) after
        # 2025-03-09. Wall time stays 12:00; UTC instant moves an hour.
        self.assertEqual(
            billing_instants("2025-02-15T12:00", "America/New_York", 2),
            ["2025-02-15T17:00:00Z", "2025-03-15T16:00:00Z"],
        )

    def test_spring_forward_gap_shifts_forward(self):
        # DST begins 2025-03-09 in New York: 02:00 EST jumps to 03:00 EDT,
        # so 02:30 does not exist and becomes 03:30 EDT (07:30 UTC).
        self.assertEqual(
            billing_instants("2025-01-09T02:30", "America/New_York", 3),
            [
                "2025-01-09T07:30:00Z",
                "2025-02-09T07:30:00Z",
                "2025-03-09T07:30:00Z",
            ],
        )

    def test_spring_forward_gap_southern_hemisphere(self):
        # DST begins 2025-10-05 in Sydney: 02:00 AEST (+10:00) jumps to
        # 03:00 AEDT (+11:00), so 02:30 becomes 03:30 AEDT (16:30 UTC Oct 4).
        self.assertEqual(
            billing_instants("2025-08-05T02:30", "Australia/Sydney", 3),
            [
                "2025-08-04T16:30:00Z",
                "2025-09-04T16:30:00Z",
                "2025-10-04T16:30:00Z",
            ],
        )

    def test_fall_back_ambiguity_uses_earlier_instant(self):
        # DST ends 2025-11-02 in New York: 01:30 occurs twice; the earlier
        # is 01:30 EDT (-04:00) = 05:30 UTC (later would be 06:30 UTC).
        self.assertEqual(
            billing_instants("2025-09-02T01:30", "America/New_York", 3),
            [
                "2025-09-02T05:30:00Z",
                "2025-10-02T05:30:00Z",
                "2025-11-02T05:30:00Z",
            ],
        )


class TestErrors(unittest.TestCase):
    def test_count_below_one(self):
        for bad in (0, -1):
            with self.assertRaises(ValueError):
                billing_instants("2024-01-15T10:00", "UTC", bad)

    def test_malformed_start_local(self):
        for bad in (
            "2024-01-15 10:00",     # space instead of T
            "2024-01-15T10:00:00",  # trailing seconds
            "2024-1-15T10:00",      # single-digit month
            "24-01-15T10:00",       # two-digit year
            "2024-13-01T10:00",     # month out of range
            "2024-02-30T10:00",     # day out of range
            "2024-01-15T25:00",     # hour out of range
            "garbage",
            "",
        ):
            with self.assertRaises(ValueError, msg=bad):
                billing_instants(bad, "UTC", 1)


if __name__ == "__main__":
    unittest.main()
```

## Notes

- **The whole wall-time rule collapses to `fold=0`.** Under PEP 495, a naive
  wall time attached to a zone with `fold=0`:
  - if **ambiguous** (fall-back), resolves to the pre-transition offset, i.e.
    the **earlier** of the two instants — exactly the spec;
  - if **nonexistent** (spring-forward), extrapolates the pre-transition
    offset, which as a UTC instant lands exactly one gap-length past the
    requested wall time (NY 02:30 fold=0 → 07:30 UTC = 03:30 EDT; a 30-minute
    gap likewise yields wall time + 30 min). Also exactly the spec.

  So no explicit gap/ambiguity detection is needed; the comment in the code
  documents why.
- **Month arithmetic** is done on a month index (`start.month - 1 + i`) with
  `divmod`-style splitting, then the day is `min(anchor_day, days_in_month)`
  computed fresh each occurrence — the anchor never mutates, so Jan 31 →
  Feb 28 → **Mar 31**.
- **Validation**: a `fullmatch` regex pins the exact `YYYY-MM-DDTHH:MM` shape
  (rejecting seconds, single-digit fields, stray whitespace), then `strptime`
  rejects out-of-range field values with `ValueError`. `count < 1` raises
  `ValueError` before any parsing side effects matter. An unknown `tz`
  propagates `ZoneInfoNotFoundError`, which the contract does not cover.
- **Test data cross-checks** (done by hand, since nothing could be executed):
  2025-03-09 and 2025-11-02 are the second Sunday of March / first Sunday of
  November (Jan 1 2025 was a Wednesday → Mar 1 and Nov 1 fall on Saturdays);
  2025-10-05 is the first Sunday of October (Oct 1 2025 is a Wednesday), the
  Australia/Sydney DST start. 2024 is a leap year; 2025 and 2026 are not.
- Tests need real IANA tzdata (present on macOS/Linux; on bare Windows,
  `pip install tzdata` — allowed since it only backfills the stdlib database).
