"""Autopilot integration tests — Parse/Probe/Assign/Dispatch routing surface.

Four test classes sharing a single eval-set JSON:
  TestAutopilotBinary       — calls bbs-autopilot subcommands directly (rows 15, 17, 20)
  TestAutopilotWiring       — skip_claude=True, validates harness machinery
  TestAutopilotE2E          — real claude -p "/bbs:autopilot ..."
  TestSingleRepoRegression  — pins §0.X byte-equality in single-repo mode

Run:
  pytest tests/test_autopilot_integration.py -q                       # wiring + binary + regression
  BBS_E2E=1 pytest tests/test_autopilot_integration.py -q             # + E2E
  BBS_E2E=1 pytest tests/test_autopilot_integration.py -q -k P6      # single fixture
"""

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

# Add ticket-system/ to import path
HERE = Path(__file__).resolve().parent
TICKET_SYSTEM = HERE / "ticket-system"
sys.path.insert(0, str(TICKET_SYSTEM))

from run_ticket_eval import find_project_root, run_single_case  # noqa: E402

EVAL_SET_PATH = HERE / "autopilot-integration-eval-set.json"
E2E = bool(os.environ.get("BBS_E2E"))
PROJECT_ROOT = str(find_project_root())


def _load_eval_set() -> list[dict]:
    return json.loads(EVAL_SET_PATH.read_text())


# ── Binary tests (bbs-autopilot subcommands, no Claude) ──────────────────


class TestAutopilotBinary:
    """Rows 15, 17, 20 — call bbs-autopilot directly."""

    BINARY_ROWS = {"P15", "P17", "P20", "P28", "P29", "P31", "P32"}

    @pytest.fixture(autouse=True)
    def _check_deps(self):
        if not (HERE / ".." / "bin" / "bbs-autopilot").resolve().exists():
            pytest.skip("bbs-autopilot binary not found")

    @pytest.mark.parametrize("case", _load_eval_set(), ids=lambda c: c["name"])
    def test_binary(self, case: dict):
        prefix = case["name"].split("-")[0]
        if prefix not in self.BINARY_ROWS:
            pytest.skip("not a binary test row")
        result = run_single_case(
            case, PROJECT_ROOT,
            timeout=30, model=None,
            case_idx=0, dry_run=False, keep_on_pass=False,
        )
        assert result["pass"], f"failures: {result['failures']}"


# ── Wiring tests (harness machinery, no Claude) ─────────────────────────


class TestAutopilotWiring:
    """Rows 1-14, 18, 19 — skip_claude, validate harness setup + assertions."""

    WIRING_SKIP = {"P15", "P17", "P20", "P28", "P29", "P31", "P32"}  # binary-only rows
    # E2E-only rows (verify-post needs real Claude, intake deferred)
    WIRING_ONLY_E2E = {"P14"}

    @pytest.mark.parametrize("case", _load_eval_set(), ids=lambda c: c["name"])
    def test_wiring(self, case: dict):
        prefix = case["name"].split("-")[0]
        if prefix in self.WIRING_SKIP:
            pytest.skip("binary-only row")
        if prefix in self.WIRING_ONLY_E2E:
            pytest.skip("E2E-only (verify-post requires real Claude)")

        # Wiring tests validate harness machinery: schema fields parsed,
        # pre_setup/autopilot_setup executed, no crashes. Strip ALL outcome
        # assertions — those are the autopilot's job (E2E tests).
        wiring_case = {
            k: v for k, v in case.items()
            if k not in (
                "assert_paths", "assert_paths_empty", "assert_path_contents",
                "assert_decisions", "assert_output",
                "assert_exit", "assert_stderr", "assert_stdout",
                "prompt",
            )
        }
        wiring_case["skip_claude"] = True

        result = run_single_case(
            wiring_case, PROJECT_ROOT,
            timeout=30, model=None,
            case_idx=0, dry_run=False, keep_on_pass=False,
        )
        # Harness error = hard failure. Empty failures list = wiring OK.
        harness_errors = [
            f for f in result["failures"]
            if "harness error" in f or "pre_setup failed" in f
              or "scaffold failed" in f or "timeout" in f
        ]
        assert not harness_errors, f"harness errors: {harness_errors}"


# ── E2E tests (real claude -p) ──────────────────────────────────────────


class TestAutopilotE2E:
    """Rows 1-13, 14, 18, 19 — real claude -p autopilot invocation."""

    E2E_SKIP = {"P15", "P17", "P20", "P28", "P29", "P31", "P32"}  # binary-only
    E2E_SKIP_DEFERRED = {"P16"}       # intake hydration (deferred)

    @pytest.mark.parametrize("case", _load_eval_set(), ids=lambda c: c["name"])
    def test_e2e(self, case: dict):
        if not E2E:
            pytest.skip("set BBS_E2E=1 to run autopilot E2E tests")

        prefix = case["name"].split("-")[0]
        if prefix in self.E2E_SKIP:
            pytest.skip("binary-only row")
        if prefix in self.E2E_SKIP_DEFERRED:
            pytest.skip("deferred (intake hydration needs multi-turn harness)")

        result = run_single_case(
            case, PROJECT_ROOT,
            timeout=300, model=None,
            case_idx=0, dry_run=False, keep_on_pass=False,
        )
        assert result["pass"], f"failures: {result['failures']}"


# ── Fixture drift guard ─────────────────────────────────────────────────


def _known_skills() -> set[str]:
    """Skill names = directories under .claude/skills/ that hold a SKILL.md."""
    skills_dir = HERE / ".." / ".claude" / "skills"
    return {
        d.name for d in skills_dir.iterdir()
        if d.is_dir() and (d / "SKILL.md").exists()
    }


def _skill_refs(case: dict):
    """Yield (context, skill_name) for every skill the fixture names.

    Two sources drift silently when a skill is renamed/removed:
      * `--skill <name>` inside pre_setup / autopilot_setup command arrays;
      * `assert_decisions.skill`.
    A stale name here is why v1.47.0 shipped a silent plan_approved routing
    regression — the fixture pointed at a skill that no longer existed and the
    harness happily ran the wrong path. This guard makes that a red test.
    """
    def scan_cmds(cmds):
        for cmd in cmds or []:
            if not isinstance(cmd, list):
                continue
            for i, tok in enumerate(cmd):
                if tok == "--skill" and i + 1 < len(cmd):
                    yield ("pre_setup", cmd[i + 1])

    yield from scan_cmds(case.get("pre_setup"))
    yield from scan_cmds(case.get("autopilot_setup"))
    dec = case.get("assert_decisions")
    if isinstance(dec, dict) and isinstance(dec.get("skill"), str):
        yield ("assert_decisions", dec["skill"])


class TestFixtureDrift:
    """Every skill named in the eval set must exist under .claude/skills/."""

    @pytest.mark.parametrize("case", _load_eval_set(), ids=lambda c: c["name"])
    def test_no_stale_skill_names(self, case: dict):
        known = _known_skills()
        stale = [
            f"{ctx}:{name}" for ctx, name in _skill_refs(case)
            if name not in known
        ]
        assert not stale, (
            f"{case['name']} references skills not in .claude/skills/: {stale}"
        )
