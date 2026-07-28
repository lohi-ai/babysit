"""Monthly billing-instant generator.

Given a naive local start wall time, an IANA timezone and a count, produce the
UTC instants of `count` monthly occurrences.

Design notes
------------
* The anchor day is remembered from the *start* and re-applied to every month
  independently, so a Jan 31 start bills Feb 28/29 and then Mar 31 again.
  (Deriving each occurrence from the previous one would let the February clamp
  stick and yield Mar 28.)
* Local -> UTC resolution never relies on the meaning of ``fold``. ``fold`` is
  used only as an *oracle for the pair of candidate offsets* around a
  transition; each candidate instant is then validated by converting it back
  UTC -> local (a direction that is always unambiguous). That makes the
  classification correct regardless of which fold value maps to the
  pre-transition offset, and it also handles zones with negative DST.
"""

import calendar
import re
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo

__all__ = ["billing_instants"]

_LOCAL_RE = re.compile(r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})")


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    """Return `count` monthly billing instants as ``YYYY-MM-DDTHH:MM:SSZ``.

    Raises ValueError for ``count < 1`` or a malformed ``start_local``.
    An unknown ``tz`` propagates ``zoneinfo.ZoneInfoNotFoundError``.
    """
    if not isinstance(count, int) or count < 1:
        raise ValueError(f"count must be a positive integer, got {count!r}")

    start = _parse_local(start_local)
    zone = ZoneInfo(tz)
    anchor_day = start.day

    out = []
    for i in range(count):
        year, month = _add_months(start.year, start.month, i)
        day = min(anchor_day, calendar.monthrange(year, month)[1])
        naive = datetime(year, month, day, start.hour, start.minute)
        out.append(_format_utc(_to_utc(naive, zone)))
    return out


def _parse_local(start_local: str) -> datetime:
    if not isinstance(start_local, str):
        raise ValueError(
            f"start_local must be a string, got {type(start_local).__name__}"
        )
    match = _LOCAL_RE.fullmatch(start_local)
    if match is None:
        raise ValueError(
            f"malformed start_local: {start_local!r} (expected YYYY-MM-DDTHH:MM)"
        )
    year, month, day, hour, minute = (int(part) for part in match.groups())
    try:
        return datetime(year, month, day, hour, minute)
    except ValueError as exc:
        raise ValueError(f"malformed start_local: {start_local!r} ({exc})") from exc


def _add_months(year: int, month: int, delta: int) -> tuple[int, int]:
    total = year * 12 + (month - 1) + delta
    return total // 12, total % 12 + 1


def _to_utc(naive: datetime, zone: ZoneInfo) -> datetime:
    """Resolve a naive wall time in `zone` to a UTC instant.

    exists & unambiguous -> that instant
    ambiguous (fall-back) -> the earlier of the two instants
    non-existent (gap)    -> the wall time shifted forward by the gap length
    """
    instants = _instants_for(naive, zone)
    if instants:
        return min(instants)

    shifted = naive + _gap_length(naive, zone)
    instants = _instants_for(shifted, zone)
    if not instants:  # would need two transitions inside one gap
        raise ValueError(
            f"cannot resolve wall time {naive.isoformat()} in {zone.key}"
        )
    return min(instants)


def _candidate_offsets(naive: datetime, zone: ZoneInfo) -> tuple[timedelta, timedelta]:
    """The (fold=0, fold=1) offsets: both offsets around a nearby transition."""
    return (
        naive.replace(tzinfo=zone, fold=0).utcoffset(),
        naive.replace(tzinfo=zone, fold=1).utcoffset(),
    )


def _instants_for(naive: datetime, zone: ZoneInfo) -> list[datetime]:
    """UTC instants whose local time in `zone` is exactly `naive` (0, 1 or 2)."""
    found = []
    for offset in set(_candidate_offsets(naive, zone)):
        instant = (naive - offset).replace(tzinfo=timezone.utc)
        if instant.astimezone(zone).replace(tzinfo=None) == naive:
            found.append(instant)
    return found


def _gap_length(naive: datetime, zone: ZoneInfo) -> timedelta:
    before, after = _candidate_offsets(naive, zone)
    return abs(after - before)


def _format_utc(instant: datetime) -> str:
    return (
        f"{instant.year:04d}-{instant.month:02d}-{instant.day:02d}"
        f"T{instant.hour:02d}:{instant.minute:02d}:{instant.second:02d}Z"
    )
