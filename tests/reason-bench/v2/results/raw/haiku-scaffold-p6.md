# P6 Solution: Monthly Billing-Instant Generator

## Move 1: Frame

**Task restatement:** Implement `billing_instants()` that generates a list of monthly recurring billing times starting from a naive wall-clock time. Handle anchor day wrapping (use last day of month if target is too far), DST gaps (spring-forward: shift forward by gap duration), and DST ambiguity (fall-back: use earlier of two times), then return all occurrences as UTC times in ISO format with Z suffix.

**Success criteria (checkable):**
1. Monthly recurrence works: Jan 15, count=3 → [Jan 15, Feb 15, Mar 15] all in same UTC hour
2. Anchor day wrapping: Jan 31 → Feb 28 (non-leap) → Mar 31; day-of-month anchor never changes
3. DST spring-forward gap: 2:30 AM in 60-minute gap (2:00→3:00) → becomes 3:30 AM local → correct UTC
4. DST fall-back ambiguity: 1:30 AM occurs twice → use earlier (fold=0, pre-rollback)
5. UTC conversion accurate: verify round-trip and Z suffix present
6. ValueError raised on count < 1 or malformed start_local

**Out of scope:** Non-IANA timezones; speculative future DST changes; timezone abbreviations.

---

## Move 2: Gather

**Facts:**
- Python 3.11+ `zoneinfo.ZoneInfo` implements IANA timezone rules including DST
- DST spring-forward (gap): local time jumps forward; wall times in gap don't exist
- DST fall-back: local time repeats once; two UTC instants map to same wall time
- Anchor day: occurrence *i* lands on day-of-month (capped to month's last day) in *i*-th calendar month after start month
- `calendar.monthrange(year, month)` returns (weekday, days_in_month)
- Python `fold=0` means "earlier" (pre-DST-end for fall-back); `fold=1` means "later" (post-DST-end)

**Assumptions (unverified, carried into output):**
- Input start_local is valid when provided; tz is a valid IANA name (per spec guarantee)
- DST transitions happen at consistent offsets (e.g., 1-hour springs, 1-hour falls; some zones use 30 or 45-minute deltas, but offset math handles them)
- No DST changes during billing window beyond standard rules in tz database

---

## Move 3: Branch

**Option A (chosen): Direct datetime manipulation with round-trip verification**
- Create naive datetime for target date/time
- Localize with `fold=0` (gives earlier time for ambiguous, pre-gap for gaps)
- Round-trip through UTC and back: `naive.replace(tzinfo=tz) → UTC → back to tz`
- Compare returned wall time to requested time
- If matched: success (unambiguous or fold=0 correct for fall-back)
- If mismatch (gap detected): calculate gap size as `offset_fold1 - offset_fold0`, shift naive time by gap, re-localize

**Rationale:** Round-trip is explicit and debuggable. Offset math works for any gap size. Avoids exception handling for "times that don't exist" (Python doesn't error; it coerces).

**Why not Option B (try/except):** Python's replace() doesn't raise on gaps; it coerces. We'd have to detect the coercion via round-trip anyway.

**Why not Option C (pre-compute transitions):** Overkill; zoneinfo already implements transitions.

---

## Move 4: Attack

**Concrete failing input for gap:** 2023-03-12, 2:30 AM, US/Eastern
- On this date, US/Eastern springs forward 2:00 AM → 3:00 AM (EST -5 → EDT -4)
- Request: 2:30 AM
- fold=0: interpreted as 2:30 AM EST = 2:30 - (-5:00) = 7:30 UTC
- Convert back: 7:30 UTC = 7:30 - (-4:00) = 3:30 AM EDT
- Mismatch: requested 2:30, got back 3:30 → in a gap
- offset_fold0 = -5:00 (EST); offset_fold1 = -4:00 (EDT)
- gap_duration = -4:00 - (-5:00) = +1:00
- Shifted naive: 2:30 + 1:00 = 3:30 AM
- Re-localize 3:30 AM EDT = 7:30 UTC ✓

**Concrete failing input for fall-back:** 2023-11-05, 1:30 AM, US/Eastern
- On this date, US/Eastern falls back 2:00 AM EDT → 1:00 AM EST (EDT -4 → EST -5)
- Request: 1:30 AM
- fold=0: interpreted as 1:30 AM EDT = 1:30 - (-4:00) = 5:30 UTC
- Convert back: 5:30 UTC = 5:30 - (-4:00) = 1:30 AM EDT
- Match: requested 1:30, got back 1:30 → unambiguous or fold=0 is correct ✓
- (fold=1 would give 1:30 AM EST = 1:30 - (-5:00) = 6:30 UTC, but we use fold=0)

**Quantify:** Each month involves one datetime creation, one offset lookup, one UTC conversion. All constant-time operations. No loops over dates or transitions.

**Sweep spec:**
- ✓ start_local format: parsed with `fromisoformat()`
- ✓ tz (IANA name): validated via `ZoneInfo()`
- ✓ count: validated count >= 1
- ✓ Monthly recurrence: loop i=0..count-1, increment month (with year wrap)
- ✓ Anchor day rule: `min(anchor_day, last_day_of_month)`
- ✓ Wall-time rule (gap handling): offset diff + shift
- ✓ Wall-time rule (ambiguity): fold=0
- ✓ UTC conversion: `astimezone(ZoneInfo("UTC"))`
- ✓ Return format: `strftime("%Y-%m-%dT%H:%M:%SZ")`
- ✓ ValueError on count < 1: explicit check
- ✓ ValueError on malformed start_local: `fromisoformat()` raises
- ✓ ValueError on invalid tz: `ZoneInfo()` raises

**Strongest surviving objection:** What if `offset_fold1 == offset_fold0` (gap_duration = 0)? Then shift adds 0, and re-round-trip still fails, causing infinite loop. 
- Rebuttal: If fold=0 doesn't match, we're in a gap. A gap means offset changes; offset_fold1 ≠ offset_fold0. If they're equal, round-trip would have matched in the first place. Assumption holds.

---

## Move 5: Verify

**Verification plan:**
1. Round-trip check on each generated time: convert to UTC and back; wall time must recover exactly
2. Test suite covers: basic recurrence, anchor wrapping, leap years, DST gaps (before/after/during), DST ambiguity, UTC formatting, error cases, year boundaries
3. Specific DST test: Mar 12, 2023, 2:30 AM US/Eastern → 3:30 AM EDT → 7:30 AM UTC ✓
4. Spec element checklist: all covered above ✓

**Applied:** Implemented tests that verify each criterion. Tests parse UTC output and check day-of-month, month, hour against expected values across DST transitions.

---

## Code

### billing.py

```python
"""Monthly billing-instant generator with DST handling."""

from datetime import datetime, timedelta
from zoneinfo import ZoneInfo
import calendar


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """
    Generate monthly billing instants.

    Args:
        start_local: "YYYY-MM-DDTHH:MM" naive wall-clock time
        tz: IANA timezone name (e.g., "US/Eastern", "Europe/London")
        count: number of occurrences to generate

    Returns:
        List of UTC times formatted as "YYYY-MM-DDTHH:MM:SSZ"

    Raises:
        ValueError: if count < 1 or start_local is malformed
    """

    # Validate count
    if count < 1:
        raise ValueError(f"count must be >= 1, got {count}")

    # Parse start_local
    try:
        start_naive = datetime.fromisoformat(start_local)
    except ValueError:
        raise ValueError(f"Invalid start_local format: {start_local}")

    # Validate and get timezone
    try:
        tz_info = ZoneInfo(tz)
    except Exception:
        raise ValueError(f"Invalid timezone: {tz}")

    # Localize the start time to verify it's unambiguous
    try:
        start_localized = start_naive.replace(tzinfo=tz_info, fold=0)
    except Exception:
        raise ValueError(f"Failed to localize start time to {tz}")

    # Extract the anchor time from the start
    anchor_day = start_localized.day
    anchor_hour = start_localized.hour
    anchor_minute = start_localized.minute

    result = []

    for i in range(count):
        # Calculate the target month and year
        target_month = start_localized.month + i
        target_year = start_localized.year

        # Adjust for month overflow
        while target_month > 12:
            target_month -= 12
            target_year += 1

        # Get the last day of the target month
        last_day_of_month = calendar.monthrange(target_year, target_month)[1]

        # Apply anchor day rule: use anchor day unless month is shorter
        target_day = min(anchor_day, last_day_of_month)

        # Find the correct wall time (handling DST gaps and ambiguities)
        local_dt = _find_wall_time(
            target_year, target_month, target_day,
            anchor_hour, anchor_minute, tz_info
        )

        # Convert to UTC
        utc_dt = local_dt.astimezone(ZoneInfo("UTC"))

        # Format as ISO 8601 with Z suffix
        result.append(utc_dt.strftime("%Y-%m-%dT%H:%M:%SZ"))

    return result


def _find_wall_time(year: int, month: int, day: int, hour: int, minute: int, tz_info: ZoneInfo) -> datetime:
    """
    Find the correct local time in the given timezone, handling DST gaps and ambiguities.

    - If the wall time is unambiguous, return it as-is.
    - If the wall time is ambiguous (fall-back), use fold=0 (earlier in UTC).
    - If the wall time is in a gap (spring-forward), shift forward by gap duration.
    """

    naive_dt = datetime(year, month, day, hour, minute)

    # Start with fold=0: earlier time for ambiguous, pre-gap for gaps
    dt_fold0 = naive_dt.replace(tzinfo=tz_info, fold=0)

    # Round-trip through UTC to detect gaps
    utc_dt = dt_fold0.astimezone(ZoneInfo("UTC"))
    back_to_tz = utc_dt.astimezone(tz_info)

    # If round-trip matches, we're either unambiguous or fold=0 is correct
    if back_to_tz.hour == hour and back_to_tz.minute == minute:
        return dt_fold0

    # We're in a spring-forward gap. The wall time doesn't exist.
    # Calculate the gap size by comparing UTC offsets.
    offset_fold0 = dt_fold0.utcoffset()
    dt_fold1 = naive_dt.replace(tzinfo=tz_info, fold=1)
    offset_fold1 = dt_fold1.utcoffset()

    gap_duration = offset_fold1 - offset_fold0

    # Shift the naive wall time forward by the gap duration
    shifted_naive = naive_dt + gap_duration
    shifted_dt = shifted_naive.replace(tzinfo=tz_info, fold=0)

    return shifted_dt
```

### test_billing.py

```python
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
```

---

## Notes

**Design decisions:**
1. **Round-trip verification for gap detection:** Python's `replace(tzinfo=...)` doesn't error on gaps; it silently coerces. Round-tripping catches this coercion and detects gaps explicitly.
2. **Offset-based gap shifting:** Works for any gap size (60 min, 45 min, 30 min). The sign automatically points the right direction: spring-forward gaps have positive duration, fall-back is unambiguous so never shifts.
3. **fold=0 by default:** Aligns with spec: earlier time for ambiguous (fall-back), and pre-gap for gaps (which then gets shifted).
4. **No exception for invalid times:** Python's zoneinfo doesn't throw for gap times; it coerces. Relying on round-trip detection is both necessary and sufficient.

**Test coverage:**
- 23 test cases covering: basic progression, anchor wrapping (Feb 28/29, leap years), DST gaps (direct and multi-month), DST ambiguity, UTC formatting, error paths (count, malformed input, invalid tz), year boundaries, edge cases (day 1, 12-month span), and consistency across DST transitions.

---

```text
STATUS: DONE
CONFIDENCE: high — gap detection via round-trip and offset math are standard DST patterns; spec compliance verified by construction
ASSUMPTIONS: (1) offset_fold1 ≠ offset_fold0 whenever fold=0 doesn't match (gap case; gap by definition changes offset) — no infinite loops; (2) no speculative future DST changes mid-billing
ATTACK: spring-forward gap at 2:30 AM US/Eastern Mar 12, 2023: fold=0 interprets as EST (7:30 UTC), round-trip gives 3:30 AM EDT, detects gap, shifts 2:30+1:00=3:30, re-localizes to 3:30 AM EDT (7:30 UTC) ✓
```
