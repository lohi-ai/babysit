"""Exercise the shipped launch commands and native agent payloads, offline."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class Hooks(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="bbs hooks ")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.plugin = self.root / "plugin with spaces"
        shutil.copytree(ROOT / "bin/hooks", self.plugin / "bin/hooks")
        self.home = self.root / "home"
        self.home.mkdir()
        self.env = {
            "HOME": str(self.home), "PATH": os.environ["PATH"],
            "BABYSIT_HOME": str(self.root / "state"),
            "CLAUDE_PLUGIN_ROOT": str(self.plugin),
        }
        # Keep all ticket decisions isolated from the real repository/install.
        stub = self.plugin / "bin/bbs-ticket"
        stub.write_text('''#!/bin/bash
case "$1" in
  resolve) echo 'bs-test'; exit "${RESOLVE_EXIT:-0}" ;;
  verdict-status)
    if [ "$3" = qa ]; then echo "${QA:-DONE}"; else echo "${REVIEW:-DONE}"; fi ;;
  qa-evidence) echo "${EVIDENCE:-ok}" ;;
  *) exit 1 ;;
esac
''')
        stub.chmod(0o755)
        self.hooks = json.loads((ROOT / "hooks/hooks.json").read_text())["hooks"]

    def run_hook(self, event="PreToolUse", payload=None, env=None):
        command = self.hooks[event][0]["hooks"][0]["command"]
        return subprocess.run(
            ["/bin/sh", "-c", command], input=json.dumps(payload or {
                "tool_input": {"command": "git push origin HEAD"}, "cwd": str(self.root),
            }), text=True, capture_output=True, env=env or self.env, timeout=15,
        )

    def decision(self, **changes):
        result = self.run_hook(env={**self.env, **changes})
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)["hookSpecificOutput"]["permissionDecision"]

    def test_all_plugin_roots_and_nonexecutable_scripts(self):
        # Reproduces 127 from unquoted roots; bash also tolerates lost mode bits.
        legacy = subprocess.run(["/bin/sh", "-c", "${CLAUDE_PLUGIN_ROOT}/bin/hooks/pre-tool-gate"],
            input="{}", text=True, capture_output=True, env=self.env)
        self.assertEqual(legacy.returncode, 127)
        for script in (self.plugin / "bin/hooks").iterdir():
            script.chmod(0o644)
        for key in ("CLAUDE_PLUGIN_ROOT", "PLUGIN_ROOT", "CODEX_PLUGIN_ROOT", "GROK_PLUGIN_ROOT"):
            with self.subTest(root=key):
                env = {k: v for k, v in self.env.items() if k != "CLAUDE_PLUGIN_ROOT"}
                env[key] = str(self.plugin)
                result = self.run_hook(env=env)
                self.assertEqual((result.returncode, result.stdout), (0, ""))
                self.assertIn("pre-tool-gate: review-pr=DONE — ok to push", result.stderr)

    def test_no_root_is_actionable_not_127(self):
        env = {k: v for k, v in self.env.items() if k != "CLAUDE_PLUGIN_ROOT"}
        result = self.run_hook(env=env)
        self.assertNotEqual(result.returncode, 127)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("plugin root is missing", result.stderr)

    def test_release_decisions(self):
        self.assertEqual(self.decision(REVIEW="BLOCKED"), "deny")
        self.assertEqual(self.decision(REVIEW="none"), "ask")
        self.assertEqual(self.decision(REVIEW="unexpected"), "ask")
        self.assertEqual(self.decision(RESOLVE_EXIT="2"), "deny")
        for status in ({}, {"RESOLVE_EXIT": "1"}):
            result = self.run_hook(env={**self.env, **status})
            self.assertEqual((result.returncode, result.stdout), (0, ""))
        for evidence, expected in (("contradiction:rubric", "deny"), ("thin:e2e", "ask")):
            result = self.run_hook(payload={"tool_input": {"command": "gh pr create --fill"}},
                                   env={**self.env, "EVIDENCE": evidence})
            self.assertEqual(json.loads(result.stdout)["hookSpecificOutput"]["permissionDecision"], expected)

    def test_grok_camel_case_and_deny_contract(self):
        for status in ("BLOCKED", "none"):
            result = self.run_hook(payload={"hookEventName": "PreToolUse", "sessionId": "g1",
                "toolName": "Bash", "toolInput": {"command": "git push"}},
                env={**self.env, "REVIEW": status})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout)["decision"], "deny")

    def test_tool_workdir_is_used(self):
        result = self.run_hook(payload={"cwd": str(self.root), "tool_input": {
            "cmd": "git push", "workdir": str(self.root / "does not exist")}})
        self.assertEqual(json.loads(result.stdout)["hookSpecificOutput"]["permissionDecision"], "deny")

    def test_missing_jq_is_an_explicit_failure(self):
        result = subprocess.run(["/bin/bash", str(self.plugin / "bin/hooks/pre-tool-gate")],
            input='{"tool_input":{"command":"git push"}}', text=True, capture_output=True,
            env={**self.env, "PATH": str(self.root / "empty-path")})
        self.assertEqual(result.returncode, 2)
        self.assertIn("requires jq", result.stderr)

    def test_session_payloads(self):
        for agent, prefix in (("claude", "cc"), ("codex", "cx"), ("grok", "grok"), ("omp", "omp")):
            with self.subTest(agent=agent):
                payload = {"session_id": agent, "cwd": str(self.root)}
                env = {**self.env}
                if agent == "codex": env["CODEX_THREAD_ID"] = agent
                if agent == "omp": payload["agent"] = "omp"
                if agent == "grok":
                    payload = {"sessionId": agent, "hookEventName": "SessionStart", "cwd": str(self.root)}
                result = self.run_hook("SessionStart", payload, env)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue((self.root / f"state/sessions/{prefix}-{agent}.yaml").exists())
        result = self.run_hook("SessionStart", {"session_id": "../../outside", "cwd": str(self.root)})
        self.assertEqual(result.returncode, 0)
        self.assertFalse((self.root / "state/outside.yaml").exists())

    def test_single_gate_and_no_audit_hooks(self):
        self.assertEqual(set(self.hooks), {"PreToolUse", "PostToolUse", "SessionStart"})
        self.assertEqual(len(self.hooks["PreToolUse"][0]["hooks"]), 1)
        self.assertNotIn("if", self.hooks["PreToolUse"][0]["hooks"][0])


if __name__ == "__main__":
    unittest.main()
