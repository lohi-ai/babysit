"""Monthly billing instants: anchor-stable dates, wall-clock-stable times."""

import calendar
import re
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

__all__ = ["billing_instants"]

_LOCAL_RE = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}")


def _parse_start(start_local: str) -> datetime:
    """Parse a strict "YYYY-MM-DDTHH:MM" naive wall-clock string."""
    if not _LOCAL_RE.fullmatch(start_local):
        raise ValueError(f"malformed start_local: {start_local!r}")
    # strptime also rejects impossible dates such as 2021-02-30.
    return datetime.strptime(start_local, "%Y-%m-%dT%H:%M")


def _occurrence_date(start: datetime, months: int) -> datetime:
    """Naive local datetime of occurrence `months` after `start`.

    The anchor day is always the start's day-of-month; it is clamped to the
    last day of a shorter month but never rewritten, so a Jan 31 anchor gives
    Feb 28 and then Mar 31 again.
    """
    total = start.year * 12 + (start.month - 1) + months
    year, month_index = divmod(total, 12)
    month = month_index + 1
    day = min(start.day, calendar.monthrange(year, month)[1])
    return datetime(year, month, day, start.hour, start.minute)


def _to_utc(naive: datetime, zone: ZoneInfo) -> datetime:
    """Resolve a naive local time in `zone` to an aware UTC datetime.

    Nonexistent wall times (spring-forward gap) shift forward by the gap
    length; ambiguous wall times (fall-back) take the earlier instant.
    """
    off_before = naive.replace(tzinfo=zone, fold=0).utcoffset()
    off_after = naive.replace(tzinfo=zone, fold=1).utcoffset()

    if off_before == off_after:
        # Wall time exists exactly once.
        return naive.replace(tzinfo=zone).astimezone(timezone.utc)

    if off_before > off_after:
        # Offset shrank across the transition: the clock went back and this
        # wall time happens twice. The earlier instant is the one still using
        # the larger pre-transition offset.
        return (naive - off_before).replace(tzinfo=timezone.utc)

    # Offset grew across the transition: the clock jumped forward and this
    # wall time never happens. Shift forward by the gap; the shifted time is
    # at or after the transition, so it uses the post-transition offset.
    gap = off_after - off_before
    return (naive + gap - off_after).replace(tzinfo=timezone.utc)


def _format_utc(dt: datetime) -> str:
    return (
        f"{dt.year:04d}-{dt.month:02d}-{dt.day:02d}"
        f"T{dt.hour:02d}:{dt.minute:02d}:{dt.second:02d}Z"
    )


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing instants as UTC "YYYY-MM-DDTHH:MM:SSZ"."""
    if count < 1:
        raise ValueError(f"count must be >= 1, got {count!r}")
    start = _parse_start(start_local)
    zone = ZoneInfo(tz)
    return [
        _format_utc(_to_utc(_occurrence_date(start, i), zone))
        for i in range(count)
    ]
