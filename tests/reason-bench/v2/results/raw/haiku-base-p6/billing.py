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
