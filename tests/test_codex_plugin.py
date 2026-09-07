#!/usr/bin/env python3
"""Structural checks for the Codex plugin package."""

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
CANONICAL = ROOT / ".claude" / "skills"
ADAPTERS = ROOT / "skills"


def skill_names(root: Path) -> set[str]:
    return {
        path.parent.name
        for path in root.glob("*/SKILL.md")
        if not path.parent.name.startswith(".")
    }


def test_every_canonical_skill_has_a_codex_adapter() -> None:
    assert skill_names(ADAPTERS) == skill_names(CANONICAL)


def test_adapters_are_real_files_that_load_the_canonical_skill() -> None:
    for name in skill_names(ADAPTERS):
        adapter = ADAPTERS / name / "SKILL.md"
        assert not adapter.is_symlink(), f"Codex strips symlinked skill files: {name}"
        body = adapter.read_text()
        assert f"name: {name}\n" in body
        assert f"(../../.claude/skills/{name}/SKILL.md)" in body


def test_manifest_points_at_adapters_and_matches_release_version() -> None:
    manifest = json.loads((ROOT / ".codex-plugin" / "plugin.json").read_text())
    assert manifest["name"] == "bbs"
    assert manifest["skills"] == "./skills/"
    assert manifest["version"] == (ROOT / "VERSION").read_text().strip()
