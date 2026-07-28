# P6 rubric (grader only — subjects must never see this)

8 points: 6 execution probes (`p6_probe.py`, expected outputs baked from
`p6_reference.py`) + 2 test-coverage points read from `test_billing.py`.

## Probes (1 pt each, exact list match)

| # | case | composed traps |
|---|------|----------------|
| 1 | 2025-01-31T10:00 America/New_York ×4 | anchor clamp Feb 28 + restore Mar 31 + EST→EDT offset change (15:00Z→14:00Z) |
| 2 | 2024-01-31T10:00 America/New_York ×3 | leap-year Feb 29 + restore Mar 31 |
| 3 | 2025-01-09T02:30 America/New_York ×3 | Mar 9 spring-forward gap → shift 60 min → 03:30 EDT |
| 4 | 2025-09-02T01:30 America/New_York ×3 | Nov 2 fall-back ambiguity → earlier instant (EDT) |
| 5 | 2025-08-05T02:15 Australia/Lord_Howe ×3 | Oct 5 **30-minute** gap → 02:45; :30/:00 offsets |
| 6 | 2025-08-31T09:00 Europe/London ×5 | double clamp-and-restore (Sep 30→Oct 31→Nov 30→Dec 31) + BST→GMT |

Predicted failure modes: adding months in UTC space (offset frozen at
start's, breaks probes 1/3/4/6 after a DST boundary); iterating from the
previous occurrence instead of the anchor (Feb 28→Mar 28, breaks 1/2/6);
naive `fold` handling (3/4); assuming all gaps are 60 min (5).

## Test-coverage points (1 pt each, "a test exists that would fail if the
behavior were wrong")

- **T7:** `test_billing.py` covers at least one DST case (gap shift or
  fold disambiguation) with a concrete expected UTC value.
- **T8:** covers anchor restoration — a 29/30/31-anchor series asserting
  the occurrence AFTER the short month returns to the anchor day.
