"""Monthly billing-instant generator (stdlib only)."""

import calendar
import re
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

_FORMAT = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}")


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing instants as UTC strings.

    `start_local` is "YYYY-MM-DDTHH:MM", a naive wall-clock time in the IANA
    timezone `tz`. Occurrence 0 is the start itself; occurrence i falls in the
    i-th calendar month after the start month, on the anchor day-of-month
    (clamped to the last day of shorter months, without changing the anchor),
    at the start's wall time resolved per DST rules.
    """
    if count < 1:
        raise ValueError(f"count must be >= 1, got {count}")
    if not _FORMAT.fullmatch(start_local):
        raise ValueError(f"malformed start_local: {start_local!r}")
    # Regex fixes the shape; strptime rejects out-of-range fields
    # (month 13, Feb 30, hour 25, ...) with ValueError.
    start = datetime.strptime(start_local, "%Y-%m-%dT%H:%M")

    zone = ZoneInfo(tz)
    anchor_day = start.day
    out: list[str] = []
    for i in range(count):
        months = start.month - 1 + i
        year = start.year + months // 12
        month = months % 12 + 1
        day = min(anchor_day, calendar.monthrange(year, month)[1])
        # fold=0 implements both wall-time rules (PEP 495 semantics):
        # - ambiguous (fall-back): fold=0 selects the earlier of the two
        #   instants (the pre-transition offset);
        # - nonexistent (spring-forward): fold=0 extrapolates the
        #   pre-transition offset, which maps to the instant exactly one
        #   gap-length after the requested wall time (a 60-minute gap turns
        #   02:30 into 03:30, a 30-minute gap turns 02:15 into 02:45).
        local = datetime(
            year, month, day, start.hour, start.minute, tzinfo=zone, fold=0
        )
        out.append(local.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    return out
