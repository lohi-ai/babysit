"""Grader-only reference implementation for P6.

fold=0 arithmetic gives exactly the contract's two DST rules:
- gap: fold=0 applies the pre-transition offset, which lands the instant
  shifted forward by the gap length;
- ambiguity: fold=0 is the earlier instant.
Run as a script to print the baked probe expectations.
"""
import calendar
from datetime import datetime, timezone
from zoneinfo import ZoneInfo


def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
    if not isinstance(count, int) or count < 1:
        raise ValueError("count must be >= 1")
    naive = datetime.strptime(start_local, "%Y-%m-%dT%H:%M")
    zone = ZoneInfo(tz)
    anchor_day = naive.day
    out = []
    for i in range(count):
        year = naive.year + (naive.month - 1 + i) // 12
        month = (naive.month - 1 + i) % 12 + 1
        day = min(anchor_day, calendar.monthrange(year, month)[1])
        local = datetime(year, month, day, naive.hour, naive.minute,
                         tzinfo=zone, fold=0)
        out.append(local.astimezone(timezone.utc)
                        .strftime("%Y-%m-%dT%H:%M:%SZ"))
    return out


PROBES = [
    ("2025-01-31T10:00", "America/New_York", 4),
    ("2024-01-31T10:00", "America/New_York", 3),
    ("2025-01-09T02:30", "America/New_York", 3),
    ("2025-09-02T01:30", "America/New_York", 3),
    ("2025-08-05T02:15", "Australia/Lord_Howe", 3),
    ("2025-08-31T09:00", "Europe/London", 5),
]

if __name__ == "__main__":
    for args in PROBES:
        print(f"{args!r}: {billing_instants(*args)!r},")
