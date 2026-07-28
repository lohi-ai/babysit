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
