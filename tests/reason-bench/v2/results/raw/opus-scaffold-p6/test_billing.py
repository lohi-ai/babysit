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
