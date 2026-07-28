#!/usr/bin/env python3
"""P6 execution grader. Usage: python3 p6_probe.py <submission-dir>

<submission-dir> must contain billing.py. Prints per-probe PASS/FAIL and a
score /6. Expected values baked from p6_reference.py (executed 2026-07-29,
hand-verified against the contract).
"""
import importlib.util
import sys

EXPECTED = {
    ("2025-01-31T10:00", "America/New_York", 4): [
        "2025-01-31T15:00:00Z", "2025-02-28T15:00:00Z",
        "2025-03-31T14:00:00Z", "2025-04-30T14:00:00Z"],
    ("2024-01-31T10:00", "America/New_York", 3): [
        "2024-01-31T15:00:00Z", "2024-02-29T15:00:00Z",
        "2024-03-31T14:00:00Z"],
    ("2025-01-09T02:30", "America/New_York", 3): [
        "2025-01-09T07:30:00Z", "2025-02-09T07:30:00Z",
        "2025-03-09T07:30:00Z"],
    ("2025-09-02T01:30", "America/New_York", 3): [
        "2025-09-02T05:30:00Z", "2025-10-02T05:30:00Z",
        "2025-11-02T05:30:00Z"],
    ("2025-08-05T02:15", "Australia/Lord_Howe", 3): [
        "2025-08-04T15:45:00Z", "2025-09-04T15:45:00Z",
        "2025-10-04T15:45:00Z"],
    ("2025-08-31T09:00", "Europe/London", 5): [
        "2025-08-31T08:00:00Z", "2025-09-30T08:00:00Z",
        "2025-10-31T09:00:00Z", "2025-11-30T09:00:00Z",
        "2025-12-31T09:00:00Z"],
}


def main(subdir: str) -> int:
    spec = importlib.util.spec_from_file_location(
        "billing", f"{subdir}/billing.py")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    score = 0
    for i, (args, want) in enumerate(EXPECTED.items(), 1):
        try:
            got = mod.billing_instants(*args)
        except Exception as e:  # noqa: BLE001 — grading, report anything
            print(f"probe {i} FAIL {args}: raised {type(e).__name__}: {e}")
            continue
        if got == want:
            print(f"probe {i} PASS {args}")
            score += 1
        else:
            print(f"probe {i} FAIL {args}:\n  want {want}\n  got  {got}")
    try:
        mod.billing_instants("2025-01-01T00:00", "UTC", 0)
        print("count=0: no ValueError (note only, unscored)")
    except ValueError:
        print("count=0: ValueError ok")
    except Exception as e:  # noqa: BLE001
        print(f"count=0: wrong exception {type(e).__name__} (note only)")
    print(f"SCORE {score}/6")
    return score


if __name__ == "__main__":
    main(sys.argv[1])
