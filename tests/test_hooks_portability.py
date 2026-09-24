"""Exercise the shipped launch commands and native agent payloads, offline.

The hooks are compiled into the bbs binary (`bbs hooks <name>`), so the
manifest commands carry no shell syntax at all — the portability claim this
suite guards. Decisions are driven by real ticket state (verdict files under
BABYSIT_PROJECT_HOME), not stubs: the gate resolves identity in-process now,
so a fake bbs-ticket shim could never intercept it.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


def build_bbs(target: Path):
    result = subprocess.run(
        ["go", "build", "-o", str(target), "./cmd/bbs"],
        cwd=ROOT, capture_output=True, text=True, timeout=300,
    )
    if result.returncode != 0:
        raise RuntimeError(f"go build failed: {result.stderr}")


def git(repo: Path, *args: str):
    subprocess.run(["git", *args], cwd=repo, check=True,
                   capture_output=True, text=True)


class Hooks(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.cls_tmp = tempfile.TemporaryDirectory(prefix="bbs hooks build ")
        cls.addClassCleanup(cls.cls_tmp.cleanup)
        cls.bbs = Path(cls.cls_tmp.name) / "bbs"
        build_bbs(cls.bbs)

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="bbs hooks ")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.state = self.root / "state"
        self.project = self.state / "projects" / "testslug"
        self.ticket_home = self.project / "tickets" / "bs-test"
        (self.ticket_home / "verdicts").mkdir(parents=True)
        # The gate's readiness probe needs a git repo at the payload workdir.
        self.repo = self.root / "repo"
        self.repo.mkdir()
        git(self.repo, "init", "-q", "-b", "main")
        git(self.repo, "config", "user.email", "t@t")
        git(self.repo, "config", "user.name", "t")
        (self.repo / "f").write_text("x")
        git(self.repo, "add", "f")
        git(self.repo, "commit", "-qm", "init")
        self.env = {
            "HOME": str(self.home),
            "PATH": f"{self.bbs.parent}{os.pathsep}{os.environ['PATH']}",
            "BABYSIT_HOME": str(self.state),
            "BABYSIT_PROJECT_HOME": str(self.project),
            "BABYSIT_TICKET": "bs-test",
        }
        for k in ("BBS_TICKET", "CODEX_SESSION_ID", "CODEX_THREAD_ID",
                  "GROK_SESSION_ID", "GROK_HOOK_EVENT", "CLAUDE_CODE_SESSION_ID"):
            self.env.pop(k, None)
        self.hooks = json.loads((ROOT / "hooks/hooks.json").read_text())["hooks"]

    # ── helpers ──────────────────────────────────────────────────
    def verdict(self, skill: str, body: str):
        (self.ticket_home / "verdicts" / f"{skill}.md").write_text(body)

    def clear_verdicts(self):
        for f in (self.ticket_home / "verdicts").glob("*.md"):
            f.unlink()

    def run_hook(self, event="PreToolUse", payload=None, env=None):
        command = self.hooks[event][0]["hooks"][0]["command"]
        return subprocess.run(
            ["/bin/sh", "-c", command],
            input=json.dumps(payload or {
                "tool_input": {"command": "git push origin HEAD"},
                "cwd": str(self.repo),
            }),
            text=True, capture_output=True, env=env or self.env, timeout=15,
        )

    def decision(self, command="git push origin HEAD", **changes):
        payload = {"tool_input": {"command": command, "workdir": str(self.repo)},
                   "cwd": str(self.repo)}
        result = self.run_hook(payload=payload, env={**self.env, **changes})
        self.assertEqual(result.returncode, 0, result.stderr)
        if not result.stdout.strip():
            return "pass"
        return json.loads(result.stdout)["hookSpecificOutput"]["permissionDecision"]

    # ── manifest shape ───────────────────────────────────────────
    def test_commands_are_bbs_hooks_with_no_shell_syntax(self):
        # The whole point of the compiled seam: the manifest command must be a
        # bare `bbs hooks <name>` — no $VARS, conditionals, or quoting that a
        # non-POSIX shell could misread.
        self.assertEqual(set(self.hooks), {"PreToolUse", "PostToolUse", "SessionStart"})
        commands = {
            "PreToolUse": "bbs hooks pre-tool-gate",
            "PostToolUse": "bbs hooks session-writer",
            "SessionStart": "bbs hooks session-writer",
        }
        for event, want in commands.items():
            got = self.hooks[event][0]["hooks"][0]["command"]
            self.assertEqual(got, want)
            self.assertNotRegex(got, r"[$`;&|<>]")

    def test_single_gate_and_no_audit_hooks(self):
        self.assertEqual(len(self.hooks["PreToolUse"][0]["hooks"]), 1)

    # ── release decisions ────────────────────────────────────────
    def test_release_decisions(self):
        cases = [
            # (review, qa, command, expected)
            ("none", "none", "git push origin HEAD", "ask"),
            ("BLOCKED", "none", "git push origin HEAD", "deny"),
            ("DONE", "none", "git push origin HEAD", "pass"),
            ("DONE", "none", "gh pr create --fill", "ask"),
            ("DONE", "BLOCKED", "gh pr create --fill", "deny"),
            ("DONE", "DONE", "gh pr create --fill", "pass"),
            ("DONE", "DONE", "gh pr merge 12", "pass"),
            ("DONE", "BLOCKED", "gh pr merge 12", "deny"),
        ]
        for review, qa, command, expected in cases:
            with self.subTest(review=review, qa=qa, command=command):
                self.clear_verdicts()
                if review != "none":
                    self.verdict("review-pr", f"STATUS: {review}\n")
                if qa != "none":
                    self.verdict("qa", f"STATUS: {qa}\nVERDICT: PASS\n"
                                       "EVIDENCE: browser journey ok, "
                                       "screenshot evidence/qa/shot.png\n")
                    (self.ticket_home / "evidence" / "qa").mkdir(parents=True, exist_ok=True)
                    (self.ticket_home / "evidence" / "qa" / "shot.png").write_text("png")
                self.assertEqual(self.decision(command), expected)

    def test_no_ticket_passes(self):
        env = {**self.env, "BABYSIT_TICKET": ""}
        result = self.run_hook(env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "")

    def test_grok_camel_case_and_deny_contract(self):
        self.verdict("review-pr", "STATUS: BLOCKED\n")
        payload = {"hookEventName": "PreToolUse",
                   "toolInput": {"command": "git push", "workdir": str(self.repo)}}
        result = self.run_hook(payload=payload)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["decision"], "deny")

    def test_tool_workdir_is_used(self):
        result = self.run_hook(payload={"cwd": str(self.repo), "tool_input": {
            "cmd": "git push", "workdir": str(self.root / "does not exist")}})
        self.assertEqual(json.loads(result.stdout)["hookSpecificOutput"]["permissionDecision"], "deny")

    def test_shim_invokes_compiled_gate(self):
        # bin/hooks/pre-tool-gate is a compat shim → exec bbs hooks.
        self.verdict("review-pr", "STATUS: BLOCKED\n")
        result = subprocess.run(
            ["/bin/sh", str(ROOT / "bin/hooks/pre-tool-gate")],
            input=json.dumps({"tool_input": {"command": "git push", "workdir": str(self.repo)}}),
            text=True, capture_output=True, env=self.env, timeout=15,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["hookSpecificOutput"]["permissionDecision"], "deny")

    # ── session writer ───────────────────────────────────────────
    def test_session_payloads(self):
        for agent, prefix in (("claude", "cc"), ("codex", "cx"), ("grok", "grok"), ("omp", "omp")):
            with self.subTest(agent=agent):
                result = self.run_hook("SessionStart", payload={
                    "session_id": f"s-{agent}", "agent": agent, "cwd": str(self.repo)})
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue((self.state / "sessions" / f"{prefix}-s-{agent}.yaml").exists())

    def test_session_id_rejects_traversal(self):
        for sid in ("../outside", "a/b", ".."):
            self.run_hook("SessionStart", payload={"session_id": sid, "cwd": str(self.repo)})
        self.assertFalse((self.state / "outside.yaml").exists())
        sessions = self.state / "sessions"
        if sessions.exists():
            for f in sessions.iterdir():
                self.assertTrue(f.name.startswith(".session.") or "/" not in f.name)


if __name__ == "__main__":
    unittest.main()
