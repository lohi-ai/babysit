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
