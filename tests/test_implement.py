#!/usr/bin/env python3
"""Tests for bbs:implement skill.

Two test classes:

  TestImplementUnit  — harness-wiring tests. Uses skip_claude=True cases so no
                       real Claude session is spawned. Verifies pre_setup +
                       seed_files land correctly in the ticket home. Always run.

  TestImplementE2E   — real-spawn end-to-end. Invokes `claude -p /bbs:implement`
                       against a real Next.js scaffold and verifies the skill
                       writes the expected ticket artifacts (verdict, handoff).
                       Skipped unless BBS_E2E=1 (calls real Claude API → costs
                       $$, ~10-20 min).

Run unit tests:  python3 -m pytest tests/test_implement.py -q
Run e2e too:     BBS_E2E=1 python3 -m pytest tests/test_implement.py -q
"""

import json
import os
import sys
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
EVAL_SET = REPO_ROOT / "tests" / "ticket-system" / "ticket_eval_set.json"
PROJECT_ROOT = str(REPO_ROOT)

sys.path.insert(0, str(REPO_ROOT / "tests" / "ticket-system"))
from run_ticket_eval import run_single_case, warm_scaffolds  # noqa: E402


def load_case(name: str) -> dict:
    data = json.loads(EVAL_SET.read_text())
    for case in data:
        if case["name"] == name:
            return case
    raise KeyError(f"eval case not found: {name!r}")


class TestImplementUnit(unittest.TestCase):
    """Broker-wiring tests — no Claude spawned, always runs."""

    def _run(self, case_name: str) -> dict:
        case = load_case(case_name)
        return run_single_case(case, PROJECT_ROOT, timeout=60, model=None,
                               case_idx=0, dry_run=True, keep_on_pass=False)

    def test_wiring_seeds_plan_and_requirement(self):
        """Harness correctly seeds plan.md + requirement.md into ticket home."""
        result = self._run("I1-implement-wiring")
        self.assertTrue(result["pass"], msg=f"failures={result['failures']}")


@unittest.skipUnless(
    os.environ.get("BBS_E2E") == "1",
    "Real spawn — set BBS_E2E=1 to run (calls real `claude` with sonnet, "
    "costs API $$, ~10-20 min)."
)
class TestImplementE2E(unittest.TestCase):
    """End-to-end: real `claude -p /bbs:implement` against a Next.js scaffold.

    The skill receives a seeded plan.md (S-scope footer insertion) and must:
      1. Read app/page.tsx from the scaffold.
      2. Insert the footer element.
      3. Commit the change.
      4. Write a verdict via `bbs-ticket set-verdict --skill implement`.

    Assertions verify the ticket system received the verdict — proving the full
    implement → bbs-ticket broker chain works end-to-end.
    """

    @classmethod
    def setUpClass(cls):
        data = json.loads(EVAL_SET.read_text())
        warm_scaffolds(data, verbose=True)

    def _run(self, case_name: str, timeout: int = 900) -> dict:
        case = load_case(case_name)
        return run_single_case(case, PROJECT_ROOT, timeout=timeout,
                               model="sonnet", case_idx=0, dry_run=False,
                               keep_on_pass=False)

    def test_implement_writes_verdict_on_nextjs_scaffold(self):
        """bbs:implement reads plan, modifies scaffold, writes verdict to ticket."""
        result = self._run("I2-implement-bbs-nextjs")
        self.assertTrue(
            result["pass"],
            msg=(f"failures={result['failures']}\n"
                 f"skill_exit={result['skill_exit']}\n"
                 f"ticket_home={result['ticket_home']}"),
        )


if __name__ == "__main__":
    unittest.main()
