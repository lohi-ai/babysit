"""Tests for billing.py monthly billing instant generator."""

import unittest
from billing import billing_instants


class TestBillingInstants(unittest.TestCase):
    """Test suite for billing_instants function."""

    def test_single_occurrence_basic(self):
        """Test a single billing occurrence at start date."""
        result = billing_instants("2024-01-15T14:30", "America/New_York", 1)
        self.assertEqual(len(result), 1)
        # Jan 15, 2024 14:30 EST (UTC-5) = 19:30 UTC
        self.assertEqual(result[0], "2024-01-15T19:30:00Z")

    def test_multiple_occurrences(self):
        """Test multiple monthly occurrences."""
        result = billing_instants("2024-01-15T14:30", "America/New_York", 3)
        self.assertEqual(len(result), 3)
        # Verify all are valid UTC strings
        for r in result:
            self.assertTrue(r.endswith("Z"))
            self.assertRegex(r, r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")

    def test_month_end_clamping_nonleap(self):
        """Test anchor day clamping for months with fewer days (non-leap year)."""
        # Jan 31 -> Feb 28 (non-leap) -> Mar 31
        result = billing_instants("2023-01-31T10:00", "America/New_York", 3)
        self.assertEqual(len(result), 3)
        # Verify the dates by checking the date part of the UTC string
        self.assertIn("01-31", result[0])
        self.assertIn("02-28", result[1])
        self.assertIn("03-31", result[2])

    def test_month_end_clamping_leap_year(self):
        """Test anchor day clamping with leap year February."""
        # Jan 31 in leap year -> Feb 29 -> Mar 31
        result = billing_instants("2024-01-31T10:00", "America/New_York", 3)
        self.assertEqual(len(result), 3)
        self.assertIn("01-31", result[0])
        self.assertIn("02-29", result[1])
        self.assertIn("03-31", result[2])

    def test_dst_spring_forward_gap(self):
        """Test DST spring-forward gap handling (wall time doesn't exist)."""
        # March 10, 2024: 2:00 AM EST -> 3:00 AM EDT (1-hour gap)
        # 2:30 AM doesn't exist, should shift to 3:30 AM EDT
        result = billing_instants("2024-03-10T02:30", "America/New_York", 1)
        self.assertEqual(len(result), 1)
        # 3:30 AM EDT (UTC-4) = 07:30 UTC
        self.assertEqual(result[0], "2024-03-10T07:30:00Z")

    def test_dst_fall_back_ambiguity(self):
        """Test DST fall-back ambiguity (use earlier instant)."""
        # November 3, 2024: 2:00 AM EDT -> 1:00 AM EST (1-hour fold-back)
        # 1:30 AM is ambiguous; should use earlier (EDT, UTC-4)
        result = billing_instants("2024-11-03T01:30", "America/New_York", 1)
        self.assertEqual(len(result), 1)
        # 1:30 AM EDT (UTC-4) = 05:30 UTC (1:30 + 4:00)
        self.assertEqual(result[0], "2024-11-03T05:30:00Z")

    def test_utc_timezone(self):
        """Test with UTC timezone (no DST)."""
        result = billing_instants("2024-01-15T14:30", "UTC", 1)
        self.assertEqual(result[0], "2024-01-15T14:30:00Z")

    def test_positive_offset_timezone(self):
        """Test with a positive UTC offset timezone."""
        # Asia/Tokyo is UTC+9
        result = billing_instants("2024-01-15T14:30", "Asia/Tokyo", 1)
        # 14:30 JST (UTC+9) = 05:30 UTC (14:30 - 9:00)
        self.assertEqual(result[0], "2024-01-15T05:30:00Z")

    def test_count_zero_raises_error(self):
        """Test that count < 1 raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2024-01-15T14:30", "America/New_York", 0)

    def test_count_negative_raises_error(self):
        """Test that negative count raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2024-01-15T14:30", "America/New_York", -1)

    def test_malformed_start_local_no_t(self):
        """Test that missing 'T' separator raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2024-01-15 14:30", "America/New_York", 1)

    def test_malformed_start_local_no_colon(self):
        """Test that missing ':' in time raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2024-01-15T1430", "America/New_York", 1)

    def test_malformed_start_local_with_seconds(self):
        """Test that input with seconds raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2024-01-15T14:30:00", "America/New_York", 1)

    def test_invalid_timezone_raises_error(self):
        """Test that invalid timezone raises ValueError."""
        with self.assertRaises(ValueError):
            billing_instants("2024-01-15T14:30", "Invalid/Timezone", 1)

    def test_year_boundary_crossing(self):
        """Test that month calculation correctly crosses year boundaries."""
        # Start in November, go forward 4 months -> should end in March of next year
        result = billing_instants("2023-11-15T10:00", "UTC", 5)
        self.assertEqual(len(result), 5)
        # Check that we cross from 2023 to 2024
        self.assertIn("2023-11-15", result[0])
        self.assertIn("2023-12-15", result[1])
        self.assertIn("2024-01-15", result[2])
        self.assertIn("2024-02-15", result[3])
        self.assertIn("2024-03-15", result[4])

    def test_early_spring_forward_time(self):
        """Test a time just before the spring-forward gap."""
        # 1:00 AM EST should work fine (gap starts at 2:00 AM)
        result = billing_instants("2024-03-10T01:00", "America/New_York", 1)
        self.assertEqual(len(result), 1)
        # 1:00 AM EST (UTC-5) = 06:00 UTC
        self.assertEqual(result[0], "2024-03-10T06:00:00Z")

    def test_late_spring_forward_time(self):
        """Test a time after the spring-forward gap."""
        # 4:00 AM EDT should work fine (gap ends at 3:00 AM)
        result = billing_instants("2024-03-10T04:00", "America/New_York", 1)
        self.assertEqual(len(result), 1)
        # 4:00 AM EDT (UTC-4) = 08:00 UTC
        self.assertEqual(result[0], "2024-03-10T08:00:00Z")

    def test_multiple_months_with_dst_transitions(self):
        """Test multiple occurrences spanning DST transitions."""
        # Start before spring forward, should handle DST properly
        result = billing_instants("2024-02-15T14:30", "America/New_York", 3)
        self.assertEqual(len(result), 3)
        # Feb 15 (EST), Mar 15 (EDT after transition), Apr 15 (EDT)
        # All should be valid UTC strings
        for r in result:
            self.assertTrue(r.endswith("Z"))

    def test_month_30_to_29_to_30(self):
        """Test month-end handling for 30-day and 31-day months."""
        # April 30 -> May 30 -> June 30
        result = billing_instants("2024-04-30T10:00", "UTC", 4)
        self.assertEqual(len(result), 4)
        self.assertIn("04-30", result[0])
        self.assertIn("05-30", result[1])
        self.assertIn("06-30", result[2])
        self.assertIn("07-30", result[3])


if __name__ == '__main__':
    unittest.main()
