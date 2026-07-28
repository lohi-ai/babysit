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
