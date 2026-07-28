"""Monthly billing-instant generator (stdlib only)."""

import re
from calendar import monthrange
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

_START_RE = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}")


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing occurrences as UTC strings.

    Occurrence 0 is the start itself. The anchor day is the start's
    day-of-month, clamped per-month to the last day of shorter months
    (the anchor itself never changes). Each occurrence is at the start's
    wall time in `tz`; a time inside a DST spring-forward gap is shifted
    forward by the gap length, and an ambiguous fall-back time resolves
    to the earlier instant.
    """
    if count < 1:
        raise ValueError(f"count must be >= 1, got {count}")
    if not _START_RE.fullmatch(start_local):
        raise ValueError(f"malformed start_local: {start_local!r}")
    try:
        start = datetime.strptime(start_local, "%Y-%m-%dT%H:%M")
    except ValueError as exc:
        raise ValueError(f"malformed start_local: {start_local!r}") from exc

    zone = ZoneInfo(tz)
    anchor_day = start.day
    out: list[str] = []
    for i in range(count):
        months = start.month - 1 + i
        year = start.year + months // 12
        month = months % 12 + 1
        day = min(anchor_day, monthrange(year, month)[1])
        local = datetime(year, month, day, start.hour, start.minute, tzinfo=zone)
        # fold=0 (the default) implements both spec rules via PEP 495:
        # - spring-forward gap: the pre-transition offset is used, so the
        #   resulting UTC instant equals the wall time shifted forward by
        #   exactly the gap length;
        # - fall-back overlap: the pre-transition (larger) offset is used,
        #   which is the earlier of the two instants.
        out.append(local.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    return out
