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
