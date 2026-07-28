# P6 Solution: Monthly Billing Instant Generator

## Implementation

### billing.py

```python
"""Monthly billing instant generator with DST handling."""

from datetime import datetime, timedelta
from zoneinfo import ZoneInfo


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Generate monthly billing instants from a local start time.

    Args:
        start_local: Wall-clock time "YYYY-MM-DDTHH:MM" in the given timezone
        tz: IANA timezone name (e.g., "America/New_York")
        count: Number of billing occurrences to generate

    Returns:
        List of UTC times formatted as "YYYY-MM-DDTHH:MM:SSZ"

    Raises:
        ValueError: If count < 1 or start_local is malformed
    """
    if count < 1:
        raise ValueError("count must be at least 1")

    # Parse and validate start_local format
    try:
        start_dt = datetime.fromisoformat(start_local)
    except ValueError:
        raise ValueError(f"Malformed start_local: {start_local}")

    # Validate format is exactly YYYY-MM-DDTHH:MM (no seconds)
    parts = start_local.split('T')
    if len(parts) != 2:
        raise ValueError(f"Malformed start_local: {start_local}")
    time_part = parts[1]
    if time_part.count(':') != 1:
        raise ValueError(f"Malformed start_local: {start_local}")

    start_year, start_month, start_day = start_dt.year, start_dt.month, start_dt.day
    start_hour, start_minute = start_dt.hour, start_dt.minute

    # Get timezone object
    try:
        tz_obj = ZoneInfo(tz)
    except Exception:
        raise ValueError(f"Invalid timezone: {tz}")

    result = []

    for i in range(count):
        # Calculate target month and year (add i months to start month)
        total_months = start_month + i - 1  # Convert to 0-indexed
        target_year = start_year + total_months // 12
        target_month = (total_months % 12) + 1

        # Calculate target day: anchor day, clamped to last day of month
        if target_month == 12:
            next_month_first = datetime(target_year + 1, 1, 1)
        else:
            next_month_first = datetime(target_year, target_month + 1, 1)
        days_in_month = (next_month_first - timedelta(days=1)).day
        target_day = min(start_day, days_in_month)

        # Create naive datetime for this billing date at the anchor time
        naive_dt = datetime(target_year, target_month, target_day, start_hour, start_minute)

        # Localize to timezone with fold=0 (earlier in case of fall-back ambiguity)
        localized_dt = naive_dt.replace(tzinfo=tz_obj, fold=0)

        # Check if we're in a spring-forward gap by round-tripping through UTC
        utc_dt = localized_dt.astimezone(ZoneInfo('UTC'))
        back_to_local = utc_dt.astimezone(tz_obj)

        if back_to_local.replace(tzinfo=None) != naive_dt:
            # We're in a DST gap; shift forward by the gap duration
            # Gap duration = difference between post-transition and pre-transition offset
            offset_before = localized_dt.utcoffset()  # Offset with fold=0
            offset_after = back_to_local.utcoffset()  # Offset after transition
            gap_shift_seconds = (offset_after - offset_before).total_seconds()

            # Shift the naive time forward
            naive_shifted = naive_dt + timedelta(seconds=gap_shift_seconds)
            localized_dt = naive_shifted.replace(tzinfo=tz_obj, fold=0)
            utc_dt = localized_dt.astimezone(ZoneInfo('UTC'))

        # Format as UTC: "YYYY-MM-DDTHH:MM:SSZ"
        result.append(utc_dt.strftime("%Y-%m-%dT%H:%M:%SZ"))

    return result
```

### test_billing.py

```python
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
```

## Notes

### Key Design Decisions

1. **DST Gap Handling via Round-Trip Detection**: Rather than trying to predict which dates will have gaps, the code creates a localized datetime with `fold=0`, converts to UTC, and converts back. If the round-trip doesn't return the original naive time, we know we're in a gap. The gap size is computed as the difference between the pre-gap and post-gap offset, which naturally handles both 1-hour and 30-minute gaps.

2. **fold=0 for Ambiguity**: The `fold` parameter disambiguates during fall-back (DST ending). `fold=0` selects the earlier instant, matching the spec requirement.

3. **Month-End Clamping**: Days are clamped to the last valid day of the target month. The anchor day itself never changes—only the occurrence date adjusts if the month is shorter.

4. **Year Boundary Handling**: Month arithmetic is 0-indexed internally to avoid off-by-one errors when crossing year boundaries.

5. **Input Validation**: Format is strictly validated to be "YYYY-MM-DDTHH:MM" with no seconds, as specified.

### Test Coverage

- Basic functionality (single/multiple occurrences, various timezones)
- Month-end clamping (leap years, 28/29/30/31 day months)
- DST spring-forward gaps (1-hour shift applied correctly)
- DST fall-back ambiguity (earlier instant selected)
- Error cases (count < 1, malformed input, invalid timezone)
- Edge cases (times just before/after gaps, year boundaries, varying offset timezones)
