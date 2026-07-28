"""Tests for the billing_instants module."""

import unittest
from datetime import datetime
from zoneinfo import ZoneInfo
from billing import billing_instants


class TestBillingInstants(unittest.TestCase):
    """Test cases for billing_instants function."""

    def test_basic_monthly_progression(self):
        """Test basic monthly recurrence without DST or month-end edge cases."""
        result = billing_instants("2023-01-15T10:00", "US/Eastern", 3)
        self.assertEqual(len(result), 3)

        # Parse results back to verify
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        # Jan 15, 10:00 AM EST = 3:00 PM UTC
        self.assertEqual(dates[0].month, 1)
        self.assertEqual(dates[0].day, 15)

        # Feb 15, 10:00 AM EST = 3:00 PM UTC
        self.assertEqual(dates[1].month, 2)
        self.assertEqual(dates[1].day, 15)

        # Mar 15, 10:00 AM EDT = 2:00 PM UTC (EDT is UTC-4)
        self.assertEqual(dates[2].month, 3)
        self.assertEqual(dates[2].day, 15)

    def test_month_end_anchor_wrapping(self):
        """Test that anchor day wraps to last day of shorter months."""
        # Jan 31 → Feb 28 → Mar 31
        result = billing_instants("2023-01-31T09:00", "US/Eastern", 3)
        self.assertEqual(len(result), 3)

        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        self.assertEqual(dates[0].day, 31)  # Jan 31
        self.assertEqual(dates[0].month, 1)

        self.assertEqual(dates[1].day, 28)  # Feb 28 (anchor was 31, month has 28)
        self.assertEqual(dates[1].month, 2)

        self.assertEqual(dates[2].day, 31)  # Mar 31
        self.assertEqual(dates[2].month, 3)

    def test_leap_year_feb_29(self):
        """Test that Feb 29 is used in leap years."""
        # 2024 is a leap year
        result = billing_instants("2024-01-29T12:00", "UTC", 3)
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        self.assertEqual(dates[0].day, 29)
        self.assertEqual(dates[0].month, 1)

        self.assertEqual(dates[1].day, 29)  # Feb 29 exists in 2024
        self.assertEqual(dates[1].month, 2)

        self.assertEqual(dates[2].day, 29)
        self.assertEqual(dates[2].month, 3)

    def test_non_leap_year_feb_wraps_to_28(self):
        """Test that Feb 28 is used in non-leap years."""
        # 2023 is not a leap year
        result = billing_instants("2023-01-29T12:00", "UTC", 3)
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        self.assertEqual(dates[0].day, 29)
        self.assertEqual(dates[0].month, 1)

        self.assertEqual(dates[1].day, 28)  # Feb 28 only, since 2023 is not a leap year
        self.assertEqual(dates[1].month, 2)

        self.assertEqual(dates[2].day, 29)
        self.assertEqual(dates[2].month, 3)

    def test_dst_spring_forward_gap(self):
        """Test handling of DST spring-forward gaps."""
        # On 2023-03-12, US/Eastern springs forward at 2:00 AM
        # 2:30 AM doesn't exist; should shift to 3:30 AM EDT
        result = billing_instants("2023-03-12T02:30", "US/Eastern", 1)

        self.assertEqual(len(result), 1)
        ts = result[0]

        # Parse and verify it's in a sensible range
        utc_dt = datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC"))

        # 3:30 AM EDT = 7:30 AM UTC
        self.assertEqual(utc_dt.hour, 7)
        self.assertEqual(utc_dt.minute, 30)
        self.assertEqual(utc_dt.day, 12)
        self.assertEqual(utc_dt.month, 3)

    def test_dst_spring_forward_multiple_months(self):
        """Test that DST gap handling works across multiple months."""
        # Start before DST, occurrence 2 is during DST
        result = billing_instants("2023-01-12T02:30", "US/Eastern", 3)

        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        # Jan 12: 2:30 AM EST = 7:30 AM UTC
        self.assertEqual(dates[0].month, 1)
        self.assertEqual(dates[0].hour, 7)
        self.assertEqual(dates[0].minute, 30)

        # Feb 12: 2:30 AM EST = 7:30 AM UTC (still EST)
        self.assertEqual(dates[1].month, 2)
        self.assertEqual(dates[1].hour, 7)
        self.assertEqual(dates[1].minute, 30)

        # Mar 12: 2:30 AM doesn't exist (gap), shifts to 3:30 AM EDT = 7:30 AM UTC
        self.assertEqual(dates[2].month, 3)
        self.assertEqual(dates[2].hour, 7)
        self.assertEqual(dates[2].minute, 30)

    def test_dst_fall_back_ambiguity(self):
        """Test handling of DST fall-back ambiguous times."""
        # On 2023-11-05, US/Eastern falls back at 2:00 AM EDT → 1:00 AM EST
        # 1:30 AM occurs twice; should use earlier (EDT)
        result = billing_instants("2023-11-05T01:30", "US/Eastern", 1)

        self.assertEqual(len(result), 1)
        ts = result[0]

        # Parse and verify
        utc_dt = datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC"))

        # 1:30 AM EDT (earlier) = 5:30 AM UTC
        self.assertEqual(utc_dt.hour, 5)
        self.assertEqual(utc_dt.minute, 30)

    def test_utc_timezone(self):
        """Test with UTC timezone (no DST)."""
        result = billing_instants("2023-06-15T14:30", "UTC", 3)
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        # All should be exactly as specified since UTC has no DST
        self.assertEqual(dates[0].hour, 14)
        self.assertEqual(dates[0].minute, 30)
        self.assertEqual(dates[1].hour, 14)
        self.assertEqual(dates[1].minute, 30)
        self.assertEqual(dates[2].hour, 14)
        self.assertEqual(dates[2].minute, 30)

    def test_different_timezone_europe_london(self):
        """Test with a different timezone (Europe/London)."""
        # Europe/London is UTC+0 in winter, UTC+1 in summer
        result = billing_instants("2023-01-15T12:00", "Europe/London", 2)
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        # Jan 15, 12:00 GMT = 12:00 UTC
        self.assertEqual(dates[0].hour, 12)
        self.assertEqual(dates[0].month, 1)

        # Feb 15, 12:00 GMT = 12:00 UTC
        self.assertEqual(dates[1].hour, 12)
        self.assertEqual(dates[1].month, 2)

    def test_output_format_has_z_suffix(self):
        """Test that output is formatted with Z suffix."""
        result = billing_instants("2023-06-15T10:00", "UTC", 1)
        self.assertTrue(result[0].endswith("Z"))
        self.assertRegex(result[0], r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")

    def test_output_includes_seconds(self):
        """Test that output includes :00 seconds."""
        result = billing_instants("2023-06-15T10:00", "UTC", 1)
        self.assertIn(":00Z", result[0])

    def test_single_occurrence(self):
        """Test with count=1 (just the start)."""
        result = billing_instants("2023-06-15T10:00", "UTC", 1)
        self.assertEqual(len(result), 1)

    def test_many_occurrences(self):
        """Test with count=12 (full year)."""
        result = billing_instants("2023-06-15T10:00", "UTC", 12)
        self.assertEqual(len(result), 12)

        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]
        # Should span from Jun 2023 to May 2024
        self.assertEqual(dates[0].month, 6)
        self.assertEqual(dates[0].year, 2023)
        self.assertEqual(dates[-1].month, 5)
        self.assertEqual(dates[-1].year, 2024)

    def test_error_count_zero(self):
        """Test that count=0 raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2023-06-15T10:00", "UTC", 0)

    def test_error_count_negative(self):
        """Test that negative count raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2023-06-15T10:00", "UTC", -1)

    def test_error_malformed_start_local(self):
        """Test that malformed start_local raises ValueError."""
        # Missing T
        with self.assertRaises(ValueError):
            billing_instants("2023-06-15 10:00", "UTC", 1)

        # Wrong format
        with self.assertRaises(ValueError):
            billing_instants("15/06/2023 10:00", "UTC", 1)

        # Missing minute
        with self.assertRaises(ValueError):
            billing_instants("2023-06-15T10", "UTC", 1)

    def test_error_invalid_timezone(self):
        """Test that invalid timezone raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2023-06-15T10:00", "Invalid/Timezone", 1)

        with self.assertRaises(ValueError):
            billing_instants("2023-06-15T10:00", "", 1)

    def test_crossing_year_boundary(self):
        """Test billing that crosses year boundary."""
        result = billing_instants("2023-11-15T10:00", "UTC", 3)
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        self.assertEqual(dates[0].year, 2023)
        self.assertEqual(dates[0].month, 11)

        self.assertEqual(dates[1].year, 2023)
        self.assertEqual(dates[1].month, 12)

        self.assertEqual(dates[2].year, 2024)
        self.assertEqual(dates[2].month, 1)

    def test_anchor_1st_of_month(self):
        """Test with anchor day = 1 (should never wrap)."""
        result = billing_instants("2023-01-01T00:00", "UTC", 3)
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        for dt in dates:
            self.assertEqual(dt.day, 1)

    def test_consistency_across_dst_transitions(self):
        """Test that the same wall time is used across DST transitions."""
        # Start at 10:00 AM, which is never in a gap
        result = billing_instants("2023-01-15T10:00", "US/Eastern", 12)
        dates = [datetime.fromisoformat(ts.rstrip("Z")).replace(tzinfo=ZoneInfo("UTC")) for ts in result]

        # All should have consistent UTC offset (within seasonal changes)
        # In winter: EST (UTC-5), summer: EDT (UTC-4)
        # So Jan-Feb should be at 15:00 UTC, Mar-Nov at 14:00 UTC, Dec at 15:00 UTC
        self.assertEqual(dates[0].hour, 15)  # Jan (EST)
        self.assertEqual(dates[1].hour, 15)  # Feb (EST)
        self.assertEqual(dates[2].hour, 14)  # Mar (EDT after spring forward)
        self.assertEqual(dates[11].hour, 15)  # Dec (EST)


if __name__ == "__main__":
    unittest.main()
