# P6 — implement a monthly billing-instant generator

Implement, in Python 3.11+ **standard library only** (`zoneinfo` allowed), a
module `billing.py` exposing:

```python
def billing_instants(start_local: str, tz: str, count: int) -> list[str]:
```

## Contract

- `start_local` is `"YYYY-MM-DDTHH:MM"` — a naive wall-clock time in the IANA
  timezone `tz`. The start itself is guaranteed to be a valid, unambiguous
  local time.
- The subscription bills **monthly**. Occurrence `i` (0-indexed; occurrence 0
  is the start itself) falls in the i-th calendar month after the start month.
- **Anchor rule:** the anchor day is the start's day-of-month. In a month
  with fewer days, the occurrence falls on the **last day of that month**,
  but the anchor day itself never changes (a Jan 31 start bills Feb 28,
  then Mar 31 — not Mar 28).
- **Wall-time rule:** each occurrence is at the start's wall time (`HH:MM`)
  in `tz` on that date.
  - If that wall time **does not exist** on that date (DST spring-forward
    gap), shift forward by the length of the gap (a 60-minute gap turns
    02:30 into 03:30; a 30-minute gap turns 02:15 into 02:45).
  - If that wall time is **ambiguous** (DST fall-back), use the **earlier**
    of the two instants.
- **Return:** a list of `count` strings — the occurrences **converted to
  UTC**, formatted `"YYYY-MM-DDTHH:MM:SSZ"`.
- Raise `ValueError` for `count < 1` or a malformed `start_local`.

## Deliverables

Write to the directory named in your run instructions:

1. `billing.py` — the implementation.
2. `test_billing.py` — stdlib `unittest` tests you would ship with it,
   runnable as `python -m unittest`.

Also write your answer file (per run instructions) containing the same code
in fenced blocks plus any notes. You cannot execute anything — get it right
by construction.
