#!/usr/bin/env python3
"""Audit and locally evaluate the Autopilot v2 fixture baseline.

This intentionally does not start a harness or buy provider usage.  The
``--run-local`` option runs the deterministic pytest surface only; live paired
benchmark runs remain an explicit, externally authorized follow-up.
"""

import argparse
import json
import subprocess
from pathlib import Path


LIVE_ONLY_CASE = "P14-verify-post-miss"


def classify_cases(cases: list[dict]) -> dict:
    """Return coverage categories without mutating fixtures or starting agents."""
    binary = [case["name"] for case in cases if case.get("binary_test")]
    live = [case["name"] for case in cases if not case.get("binary_test")]
    wiring = [name for name in live if name != LIVE_ONLY_CASE]
    local_unavailable = [name for name in live if name == LIVE_ONLY_CASE]

    return {
        "fixture_count": len(cases),
        "local": {
            "binary": binary,
            "wiring": wiring,
            "unavailable": local_unavailable,
        },
        "live": {
            "eligible": live,
            "executed": [],
            "status": "unavailable",
            "reason": "no provider-run authorization or harness invocation",
            "provider_usage": None,
        },
        "contract_audit": {
            "legacy_oracle_rows": [
                "P2-inline-new-feature-dirty",
                "P4-implement-unplanned",
                "P5-implement-unapproved",
            ],
            "reason": (
                "These rows encode pre-v2 dirty-tree, plan-routing, or "
                "plan-verdict assumptions and require versioned assertions "
                "before they are used as v2 rollout evidence."
            ),
        },
    }


def run_local(repo_root: Path) -> dict:
    """Run only the fixture's deterministic pytest surface."""
    command = ["python3", "-m", "pytest", "tests/test_autopilot_integration.py", "-q"]
    result = subprocess.run(command, cwd=repo_root, capture_output=True, text=True)
    return {
        "command": command,
        "exit_code": result.returncode,
        "status": "pass" if result.returncode == 0 else "fail",
        "stdout": result.stdout,
        "stderr": result.stderr,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--eval-set", type=Path, required=True)
    parser.add_argument("--repo-root", type=Path, default=Path.cwd())
    parser.add_argument("--run-local", action="store_true")
    args = parser.parse_args()

    cases = json.loads(args.eval_set.read_text())
    report = classify_cases(cases)
    report["revision"] = subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=args.repo_root, text=True
    ).strip()
    if args.run_local:
        report["local"]["run"] = run_local(args.repo_root)

    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if report.get("local", {}).get("run", {}).get("exit_code", 0) == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
