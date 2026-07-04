#!/usr/bin/env python3
"""Run ticket-system e2e evaluation.

For each test case:
  1. Create a fresh sandbox (BABYSIT_HOME=tempdir, BBS_TICKET=bs-test-N)
  2. Run any pre-setup `bbs-ticket` commands (seed existing artifacts)
  3. Invoke `claude -p <prompt>` — a real skill run against the sandbox ticket
  4. Assert filesystem state on the ticket (paths present/absent/content regex)

Unlike run_eval.py (which measures trigger rate), this measures SIDE EFFECTS:
did the skill actually write the expected handoffs, verdicts, and pointers
through the bbs-ticket broker?
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import uuid
from concurrent.futures import ProcessPoolExecutor, as_completed
from pathlib import Path


FIXTURE_CACHE_ROOT = Path.home() / ".cache" / "babysit-eval-fixtures"
GIT_COMMIT_ENV = ("-c", "user.email=test@babysit", "-c", "user.name=Test",
                  "-c", "commit.gpgsign=false")


def find_project_root() -> Path:
    current = Path.cwd()
    for parent in [current, *current.parents]:
        if (parent / ".claude").is_dir():
            return parent
    return current


def find_bbs_ticket() -> str:
    """Locate bbs-ticket binary. Prefer project-local, fall back to ~/.claude."""
    root = find_project_root()
    local = root / "bin" / "bbs-ticket"
    if local.is_file() and os.access(local, os.X_OK):
        return str(local)
    user = Path.home() / ".claude" / "bbs-ticket"
    if user.is_file():
        return str(user)
    found = shutil.which("bbs-ticket")
    if found:
        return found
    raise FileNotFoundError("bbs-ticket not found in repo bin/, ~/.claude/, or PATH")


def find_bbs_autopilot() -> str:
    """Locate bbs-autopilot binary. Prefer project-local, fall back to ~/.claude."""
    root = find_project_root()
    local = root / "bin" / "bbs-autopilot"
    if local.is_file() and os.access(local, os.X_OK):
        return str(local)
    user = Path.home() / ".claude" / "bbs-autopilot"
    if user.is_file():
        return str(user)
    found = shutil.which("bbs-autopilot")
    if found:
        return found
    raise FileNotFoundError("bbs-autopilot not found in repo bin/, ~/.claude/, or PATH")


def run_pre_setup(commands: list[list[str]], env: dict, bbs_ticket: str,
                  cwd: str | None = None) -> None:
    """Run each pre-setup command. First arg is always implicitly bbs-ticket."""
    for cmd in commands:
        full = [bbs_ticket, *cmd]
        result = subprocess.run(full, env=env, capture_output=True, text=True,
                                cwd=cwd)
        if result.returncode != 0:
            raise RuntimeError(
                f"pre_setup failed ({full}): exit={result.returncode} "
                f"stderr={result.stderr!r}"
            )


def assert_ticket_state(
    ticket_home: Path,
    assert_paths: list[str],
    assert_paths_empty: list[str],
    assert_path_contents: dict[str, str],
) -> tuple[bool, list[str]]:
    """Return (passed, list of failure messages)."""
    failures = []

    for rel in assert_paths:
        p = ticket_home / rel
        if not p.exists():
            failures.append(f"expected path missing: {rel}")
        elif p.is_file() and p.stat().st_size == 0:
            failures.append(f"expected path empty: {rel}")

    for rel in assert_paths_empty:
        p = ticket_home / rel
        if p.exists():
            failures.append(f"expected path absent but present: {rel}")

    for rel, pattern in assert_path_contents.items():
        p = ticket_home / rel
        if not p.exists():
            failures.append(f"content check: path missing: {rel}")
            continue
        content = p.read_text(errors="replace")
        if not re.search(pattern, content):
            snippet = content[:200].replace("\n", "\\n")
            failures.append(
                f"content mismatch at {rel}: /{pattern}/ not found. "
                f"head={snippet!r}"
            )

    return (len(failures) == 0, failures)


def assert_decisions(
    decisions_path: Path,
    ticket: str,
    patterns: dict[str, str],
) -> tuple[bool, list[str]]:
    """Check that decisions.jsonl has a row matching each pattern.

    Args:
        decisions_path: Path to the isolated decisions.jsonl for this case.
        ticket: Filter rows where json['ticket'] == this value.
        patterns: Map of JSONL field → regex. Each pattern must match at
                  least one row in the filtered set. All patterns must match.

    Returns:
        (passed, failure_messages)
    """
    failures = []
    if not decisions_path.exists():
        # No decisions file at all — every pattern fails.
        for field, pattern in patterns.items():
            failures.append(f"decisions: file missing ({decisions_path})")
        return (False, failures)

    rows: list[dict] = []
    for line in decisions_path.read_text(errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            rows.append(json.loads(line))
        except json.JSONDecodeError:
            continue

    matching = [r for r in rows if r.get("ticket") == ticket]
    if not matching and ticket:
        # Fallback: try without ticket filter (some autopilot decisions
        # may log an empty or different ticket value).
        matching = rows

    for field, pattern in patterns.items():
        found = False
        for row in matching:
            val = str(row.get(field, ""))
            if re.search(pattern, val):
                found = True
                break
        if not found:
            sample = matching[:3] if matching else "no rows"
            failures.append(
                f"decisions: /{pattern}/ not found in field '{field}' "
                f"(ticket={ticket}, rows={len(matching)}, sample={sample})"
            )

    return (len(failures) == 0, failures)


def assert_output_text(
    text: str,
    patterns: dict[str, str],
    label: str,
) -> tuple[bool, list[str]]:
    """Check that text matches each regex pattern. Returns (passed, failures)."""
    failures = []
    for name, pattern in patterns.items():
        if not re.search(pattern, text):
            snippet = text[:300].replace("\n", "\\n")
            failures.append(
                f"{label}: /{pattern}/ ({name}) not found. head={snippet!r}"
            )
    return (len(failures) == 0, failures)


BBS_STUB = """#!/usr/bin/env bash
# Test harness stub for the external `bbs` CLI. Workflows like build/implement
# probe `bbs ticket --help` and call `ticket show|comment|create`. The real
# orchestrator (babysit-office) provides this; in tests we fake it so chain
# workflows don't short-circuit to BLOCKED.
case "$1" in
  --help|"") echo "bbs (test stub)"; exit 0 ;;
  ticket)
    shift
    case "$1" in
      --help) exit 0 ;;
      show)
        BR="${STUB_BRANCH:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null)}"
        BB="${STUB_BASE_BRANCH:-main}"
        cat <<EOF
ticket: $2
status: in_progress
description: Test stub ticket — chain workflow under test.
[WORK] build complete — next: quality. BRANCH=$BR BASE_BRANCH=$BB
EOF
        exit 0
        ;;
      comment) exit 0 ;;
      create) echo "bs-stub-$(date +%s)"; exit 0 ;;
      *) exit 0 ;;
    esac
    ;;
  daemon) exit 0 ;;
  pipeline) exit 0 ;;
  *) exit 0 ;;
esac
"""


def install_babysit_stub(stub_dir: Path) -> None:
    """Write the bbs shim into stub_dir and chmod +x."""
    stub_dir.mkdir(parents=True, exist_ok=True)
    stub = stub_dir / "bbs"
    stub.write_text(BBS_STUB)
    stub.chmod(0o755)


def scaffold_fixture(fixture: dict) -> Path:
    """Ensure the scaffold is cached under FIXTURE_CACHE_ROOT; return its path.

    The scaffold cmd runs once per cache_key per machine. The resulting tree
    is git-init'd with a single "scaffold" commit AND a fake origin remote
    (so `bbs-slug` derives a deterministic SLUG matching the harness's
    ticket-home derivation). Cases copy this cache dir; the original is
    never mutated. Sentinel lives OUTSIDE cache_dir so `copy_fixture`
    doesn't carry it into the working tree.
    """
    cache_key = fixture["cache_key"]
    cache_dir = FIXTURE_CACHE_ROOT / cache_key
    sentinel = FIXTURE_CACHE_ROOT / f"{cache_key}.ready"
    if sentinel.exists():
        return cache_dir

    if cache_dir.exists():
        shutil.rmtree(cache_dir)
    cache_dir.parent.mkdir(parents=True, exist_ok=True)

    cmd = [str(cache_dir) if c == "$DEST" else c for c in fixture["cmd"]]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        if cache_dir.exists():
            shutil.rmtree(cache_dir, ignore_errors=True)
        raise RuntimeError(
            f"scaffold failed: cache_key={cache_key} "
            f"exit={result.returncode} stderr={result.stderr[-500:]!r}"
        )

    if not (cache_dir / ".git").exists():
        subprocess.run(["git", "init", "-q"], cwd=cache_dir, check=True)
    subprocess.run(["git", "add", "-A"], cwd=cache_dir, check=True)
    subprocess.run(
        ["git", *GIT_COMMIT_ENV, "commit", "-q", "-m", "scaffold",
         "--allow-empty"],
        cwd=cache_dir, check=True,
    )
    # Fake remote so bbs-slug derives SLUG=fixtures-<cache_key> instead of
    # falling back to the (ephemeral, per-run) tempdir basename.
    fake_remote = f"https://babysit-eval.invalid/fixtures/{cache_key}.git"
    subprocess.run(
        ["git", "remote", "add", "origin", fake_remote],
        cwd=cache_dir, check=True,
    )
    sentinel.write_text("ok\n")
    return cache_dir


def copy_fixture(cache_dir: Path, dest: Path) -> None:
    """Copy cache_dir to dest. Small scaffolds; plain copytree is fine."""
    shutil.copytree(cache_dir, dest)


def apply_setup_commits(commits: list, dest: Path) -> None:
    """Write files and git-commit in sequence."""
    for idx, commit in enumerate(commits):
        for rel, content in commit.get("files", {}).items():
            target = dest / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)
        msg = commit.get("message", f"setup commit {idx + 1}")
        subprocess.run(["git", "add", "-A"], cwd=dest, check=True)
        subprocess.run(
            ["git", *GIT_COMMIT_ENV, "commit", "-q", "-m", msg],
            cwd=dest, check=True,
        )


def warm_scaffolds(eval_set: list, verbose: bool) -> None:
    """Pre-build all unique scaffolds before parallel workers start.

    Workers can't race on the same cache dir — serialize here.
    """
    seen = set()
    for case in eval_set:
        fixture = case.get("fixture")
        if not fixture or fixture.get("cache_key") in seen:
            continue
        key = fixture["cache_key"]
        seen.add(key)
        sentinel = FIXTURE_CACHE_ROOT / f"{key}.ready"
        if sentinel.exists():
            if verbose:
                print(f"  scaffold cached: {key}", file=sys.stderr)
            continue
        if verbose:
            print(f"  scaffolding: {key} (first run, ~30s)...", file=sys.stderr)
        scaffold_fixture(fixture)


def resolve_ticket_home(bbs_ticket: str, env: dict, cwd: str | None = None) -> Path:
    """Query bbs-ticket for the real ticket home under the current env.

    Pass `cwd` when a fixture is in play — `bbs-slug` derives SLUG from the
    git remote of the working directory, so the harness must resolve from
    the same spot the skill will run.
    """
    result = subprocess.run(
        [bbs_ticket, "env"], env=env, capture_output=True, text=True,
        check=True, cwd=cwd,
    )
    for line in result.stdout.splitlines():
        if line.startswith("TICKET_HOME="):
            return Path(line.split("=", 1)[1].strip())
    raise RuntimeError(f"bbs-ticket env did not emit TICKET_HOME: {result.stdout!r}")


def run_single_case(
    case: dict,
    project_root: str,
    timeout: int,
    model: str | None,
    case_idx: int,
    dry_run: bool,
    keep_on_pass: bool,
) -> dict:
    """Run one test case and return a result dict.

    Isolation strategy: each case uses a unique BBS_TICKET (suffixed with a
    short UUID) so multiple runs don't collide. Tickets are created under the
    real BABYSIT project home (not a tempdir) because `claude -p` can't see
    a redirected $HOME — it would lose its own auth/config. Cleanup deletes
    the ticket dir on pass; failures are preserved for postmortem.
    """
    name = case.get("name", f"case-{case_idx}")
    base_ticket = case.get("ticket", f"bs-test-{case_idx:03d}")
    ticket = f"{base_ticket}-{uuid.uuid4().hex[:6]}"
    # prompt_template takes precedence over prompt (supports $TICKET substitution)
    prompt_template = case.get("prompt_template")
    if prompt_template:
        prompt = prompt_template.replace("$TICKET", ticket)
    else:
        prompt = case.get("prompt", "")
    skip_claude = case.get("skip_claude", False) or not prompt
    pre_setup = case.get("pre_setup", [])
    seed_files = case.get("seed_files", {})
    assert_paths = case.get("assert_paths", [])
    assert_paths_empty = case.get("assert_paths_empty", [])
    assert_path_contents = case.get("assert_path_contents", {})
    assert_decision_patterns = case.get("assert_decisions", {})
    assert_output_patterns = case.get("assert_output", {})
    expect_ticket_create = case.get("expect_ticket_create", False)
    binary_test_cmd = case.get("binary_test")
    autopilot_setup_cmds = case.get("autopilot_setup", [])
    seed_working_tree_files = case.get("seed_working_tree_files", {})
    lint_workflow_content = case.get("lint_workflow_content")
    fixture = case.get("fixture")
    fixture_branch = case.get("fixture_branch")
    setup_commits = case.get("setup_commits", [])

    # Pre-set bin paths so skills can call bbs-ticket/bbs-slug even when the
    # preamble bash block is only partially executed in headless claude -p mode.
    _claude_dir = Path.home() / ".claude"
    _bbs_bins = {
        name: str(_claude_dir / name)
        for name in ("bbs-ticket", "bbs-slug", "bbs-autopilot",
                     "bbs-learnings-log", "bbs-telemetry-log")
        if (_claude_dir / name).exists()
    }

    env = {
        **{k: v for k, v in os.environ.items() if k != "CLAUDECODE"},
        "BBS_TICKET": ticket,
        "AGENT_ROLE": os.environ.get("AGENT_ROLE", os.environ.get("INVOKER", "general")),
        "BBS_TICKET_BIN": _bbs_bins.get("bbs-ticket", "bbs-ticket"),
        "BBS_SLUG_BIN": _bbs_bins.get("bbs-slug", "bbs-slug"),
        "BBS_AUTOPILOT_BIN": _bbs_bins.get("bbs-autopilot", "bbs-autopilot"),
        "BBS_LEARNINGS_LOG_BIN": _bbs_bins.get("bbs-learnings-log", "bbs-learnings-log"),
        "BBS_TELEMETRY_LOG_BIN": _bbs_bins.get("bbs-telemetry-log", "bbs-telemetry-log"),
    }

    bbs_ticket = find_bbs_ticket()

    # Decision-log isolation: each case gets its own analytics dir.
    decisions_dir = Path(tempfile.mkdtemp(prefix="bbs-decisions-"))
    env["BABYSIT_ANALYTICS_DIR"] = str(decisions_dir)
    env["BABYSIT_SKIP_COMMENT"] = "1"

    fixture_dir: Path | None = None
    ticket_home: Path | None = None
    result = {
        "name": name,
        "ticket": ticket,
        "prompt": prompt,
        "pass": False,
        "failures": [],
        "elapsed_s": 0.0,
        "skill_exit": None,
        "ticket_home": None,
        "fixture_dir": None,
    }

    start = time.time()
    try:
        # Fixture setup FIRST — ticket-home resolution depends on the git
        # remote in the working dir, so the harness must resolve from the
        # same cwd the skill will run in.
        if fixture:
            cache_dir = scaffold_fixture(fixture)
            fixture_dir = Path(tempfile.mkdtemp(prefix="bbs-eval-fixture-"))
            fixture_dir.rmdir()  # copytree requires dest not exist
            copy_fixture(cache_dir, fixture_dir)
            result["fixture_dir"] = str(fixture_dir)
            if fixture_branch:
                subprocess.run(
                    ["git", "checkout", "-q", "-b", fixture_branch],
                    cwd=fixture_dir, check=True,
                )
            if setup_commits:
                apply_setup_commits(setup_commits, fixture_dir)

        resolve_cwd = str(fixture_dir) if fixture_dir else None
        ticket_home = resolve_ticket_home(bbs_ticket, env, cwd=resolve_cwd)
        result["ticket_home"] = str(ticket_home)

        # Per-case stub for the external `bbs` CLI. Workflows probe this;
        # without it they emit BLOCKED at load-context and never invoke their
        # sub-skills.
        stub_dir = ticket_home / ".test-stubs"
        install_babysit_stub(stub_dir)
        env["PATH"] = f"{stub_dir}:{env.get('PATH', '')}"

        subprocess.run(
            [bbs_ticket, "init"], env=env, capture_output=True, text=True,
            check=False, cwd=resolve_cwd,
        )

        if pre_setup:
            run_pre_setup(pre_setup, env, bbs_ticket, cwd=resolve_cwd)

        for rel, content in seed_files.items():
            target = ticket_home / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)

        # Seed dirty working tree files (not committed — leaves tree dirty)
        if seed_working_tree_files and fixture_dir:
            for rel, content in seed_working_tree_files.items():
                target = fixture_dir / rel
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(content)

        # Run autopilot_setup commands (bbs-autopilot, not bbs-ticket)
        if autopilot_setup_cmds:
            bbs_autopilot = find_bbs_autopilot()
            for cmd in autopilot_setup_cmds:
                full_cmd = [bbs_autopilot] + [c.replace("$TICKET", ticket) for c in cmd]
                subprocess.run(full_cmd, env=env, capture_output=True, text=True,
                               check=False, cwd=resolve_cwd)

        # Write lint_workflow_content to a temp workflow file if provided
        lint_workflow_path = None
        if lint_workflow_content and fixture_dir:
            wf_dir = fixture_dir / ".claude" / "workflows"
            wf_dir.mkdir(parents=True, exist_ok=True)
            lint_workflow_path = wf_dir / "test-lint.md"
            lint_workflow_path.write_text(lint_workflow_content)

        claude_cwd = str(fixture_dir) if fixture_dir else project_root

        if dry_run or (skip_claude and not binary_test_cmd):
            # Skip claude -p; broker-only or wiring validation
            result["skill_exit"] = 0
        elif binary_test_cmd:
            # Binary test: run bbs-autopilot directly, no Claude.
            # Substitute $TICKET in binary_test args.
            bbs_autopilot = find_bbs_autopilot()
            bin_args = [a.replace("$TICKET", ticket) for a in binary_test_cmd]
            cmd = [bbs_autopilot, *bin_args]
            proc = subprocess.run(cmd, env=env, cwd=resolve_cwd,
                                  capture_output=True, text=True, timeout=30)
            result["skill_exit"] = proc.returncode
            # Write stdout/stderr to ticket_home if it exists, else fixture_dir
            out_dir = ticket_home if ticket_home and ticket_home.exists() else Path(resolve_cwd or claude_cwd)
            out_dir.mkdir(parents=True, exist_ok=True)
            (out_dir / "_stdout.log").write_text(proc.stdout or "")
            (out_dir / "_stderr.log").write_text(proc.stderr or "")
        else:
            cmd = [
                "claude",
                "-p", prompt,
                "--output-format", "stream-json",
                "--verbose",
                "--include-partial-messages",
                "--dangerously-skip-permissions",
            ]
            if model:
                cmd.extend(["--model", model])

            proc = subprocess.run(
                cmd,
                env=env,
                cwd=claude_cwd,
                capture_output=True,
                text=True,
                timeout=timeout,
            )
            result["skill_exit"] = proc.returncode
            # Save transcript into the ticket dir so post-mortem has the run
            (ticket_home / "_transcript.jsonl").write_text(proc.stdout or "")
            (ticket_home / "_stderr.log").write_text(proc.stderr or "")

        passed, failures = assert_ticket_state(
            ticket_home,
            assert_paths,
            assert_paths_empty,
            assert_path_contents,
        )

        # Re-resolve ticket_home if the skill may have created the ticket
        if expect_ticket_create and ticket_home:
            try:
                ticket_home = resolve_ticket_home(bbs_ticket, env, cwd=resolve_cwd)
                result["ticket_home"] = str(ticket_home)
                # Re-check assertions with updated ticket_home
                passed2, failures2 = assert_ticket_state(
                    ticket_home, assert_paths, assert_paths_empty,
                    assert_path_contents,
                )
                passed = passed and passed2
                failures.extend(failures2)
            except Exception as e:
                failures.append(f"expect_ticket_create: re-resolve failed: {e}")
                passed = False

        # Decision-log assertions
        if assert_decision_patterns:
            decisions_path = decisions_dir / "decisions.jsonl"
            d_passed, d_failures = assert_decisions(
                decisions_path, ticket, assert_decision_patterns,
            )
            passed = passed and d_passed
            failures.extend(d_failures)

        # Output assertions (transcript or binary stdout)
        if assert_output_patterns:
            output_text = ""
            if binary_test_cmd:
                out_dir = ticket_home if ticket_home and ticket_home.exists() else Path(resolve_cwd or claude_cwd)
                output_text = (out_dir / "_stdout.log").read_text(errors="replace")
            elif ticket_home:
                transcript = ticket_home / "_transcript.jsonl"
                if transcript.exists():
                    output_text = transcript.read_text(errors="replace")
            o_passed, o_failures = assert_output_text(
                output_text, assert_output_patterns, "output",
            )
            passed = passed and o_passed
            failures.extend(o_failures)

        # Binary-test exit/stdout/stderr assertions
        if binary_test_cmd:
            expected_exit = case.get("assert_exit")
            if expected_exit is not None and result["skill_exit"] != expected_exit:
                passed = False
                failures.append(
                    f"exit: expected {expected_exit}, got {result['skill_exit']}"
                )
            out_dir = ticket_home if ticket_home and ticket_home.exists() else Path(resolve_cwd or claude_cwd)
            stderr_patterns = case.get("assert_stderr", {})
            if stderr_patterns:
                stderr_text = (out_dir / "_stderr.log").read_text(errors="replace")
                se_passed, se_failures = assert_output_text(
                    stderr_text, stderr_patterns, "stderr",
                )
                passed = passed and se_passed
                failures.extend(se_failures)
            stdout_patterns = case.get("assert_stdout", {})
            if stdout_patterns:
                stdout_text = (out_dir / "_stdout.log").read_text(errors="replace")
                so_passed, so_failures = assert_output_text(
                    stdout_text, stdout_patterns, "stdout",
                )
                passed = passed and so_passed
                failures.extend(so_failures)

        result["pass"] = passed
        result["failures"] = failures

    except subprocess.TimeoutExpired:
        result["failures"] = [f"timeout after {timeout}s"]
    except Exception as e:
        result["failures"] = [f"harness error: {type(e).__name__}: {e}"]
    finally:
        result["elapsed_s"] = round(time.time() - start, 2)
        if result["pass"] and not keep_on_pass:
            if ticket_home is not None:
                shutil.rmtree(ticket_home, ignore_errors=True)
            if fixture_dir is not None:
                shutil.rmtree(fixture_dir, ignore_errors=True)
            shutil.rmtree(decisions_dir, ignore_errors=True)

    return result


def main():
    parser = argparse.ArgumentParser(
        description="Run ticket-system e2e evaluation against real claude -p"
    )
    parser.add_argument("--eval-set", required=True, help="Path to ticket eval set JSON")
    parser.add_argument("--num-workers", type=int, default=4)
    parser.add_argument("--timeout", type=int, default=300,
                        help="Per-case timeout in seconds (default 300)")
    parser.add_argument("--model", default="sonnet")
    parser.add_argument("--filter", default=None,
                        help="Only run cases whose name matches this regex")
    parser.add_argument("--dry-run", action="store_true",
                        help="Skip claude -p; validate pre_setup + assertions only")
    parser.add_argument("--keep-on-pass", action="store_true",
                        help="Don't delete ticket dir after a passing run (for inspection)")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    eval_set = json.loads(Path(args.eval_set).read_text())
    if args.filter:
        pat = re.compile(args.filter)
        eval_set = [c for c in eval_set if pat.search(c.get("name", ""))]

    project_root = str(find_project_root())

    if args.verbose:
        print(f"Running {len(eval_set)} cases against {project_root}", file=sys.stderr)

    warm_scaffolds(eval_set, args.verbose)

    results = []
    with ProcessPoolExecutor(max_workers=args.num_workers) as executor:
        futures = {
            executor.submit(
                run_single_case, case, project_root, args.timeout, args.model,
                idx, args.dry_run, args.keep_on_pass,
            ): case
            for idx, case in enumerate(eval_set, start=1)
        }
        for future in as_completed(futures):
            res = future.result()
            results.append(res)
            if args.verbose:
                status = "PASS" if res["pass"] else "FAIL"
                print(
                    f"  [{status}] ({res['elapsed_s']}s) {res['name']}"
                    + (f" — {res['failures'][0]}" if res["failures"] else ""),
                    file=sys.stderr,
                )

    passed = sum(1 for r in results if r["pass"])
    total = len(results)
    output = {
        "summary": {"total": total, "passed": passed, "failed": total - passed},
        "results": sorted(results, key=lambda r: r["name"]),
    }
    print(json.dumps(output, indent=2))
    sys.exit(0 if passed == total else 1)


if __name__ == "__main__":
    main()
