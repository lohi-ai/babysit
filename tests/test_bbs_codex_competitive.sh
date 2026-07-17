#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# bin/bbs-codex-competitive is a symlink to the gitignored bin/bbs, so a clean
# checkout has nothing to exec until the binary is built.
(cd "$ROOT" && go build -o bin/bbs ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

mkdir -p "$TMP/home"
mkdir -p "$TMP/.claude/skills/example"

cat > "$TMP/CLAUDE.md" <<'EOF'
# CLAUDE.md

Babysit is a Claude Code skill pack.
Read .claude/skills/references/preamble.md and use $HOME/.claude/bbs-ticket.
EOF

cat > "$TMP/.claude/skills/example/SKILL.md" <<'EOF'
---
name: example
description: Use this for Claude Code examples.
---

See CLAUDE.md and .claude/skills/example/SKILL.md.
Run "$HOME/.claude/bbs-ticket" when needed.
EOF

env -u CODEX_HOME HOME="$TMP/home" "$ROOT/bin/bbs-codex-competitive" --root "$TMP"
env -u CODEX_HOME HOME="$TMP/home" "$ROOT/bin/bbs-codex-competitive" --root "$TMP" --check

test -L "$TMP/AGENTS.md"
test "$(readlink "$TMP/AGENTS.md")" = "CLAUDE.md"
test -L "$TMP/.agents/skills"
test "$(readlink "$TMP/.agents/skills")" = "../.claude/skills"

test -f "$TMP/.agents/skills/example/SKILL.md"
grep -q 'Use this for Claude Code examples' "$TMP/.agents/skills/example/SKILL.md"
test -L "$TMP/home/.codex/skills/bbs:example"
test "$(readlink "$TMP/home/.codex/skills/bbs:example")" = "$TMP/.claude/skills/example"
test -L "$TMP/home/.Codex/skills/bbs:example"
test "$(readlink "$TMP/home/.Codex/skills/bbs:example")" = "$TMP/.claude/skills/example"

rm "$TMP/AGENTS.md"
echo 'local edit' > "$TMP/AGENTS.md"
if env -u CODEX_HOME HOME="$TMP/home" "$ROOT/bin/bbs-codex-competitive" --root "$TMP" --check >/tmp/bbs-codex-check.out 2>/tmp/bbs-codex-check.err; then
  echo "expected --check to fail after replacing symlink" >&2
  exit 1
fi
grep -q 'Codex symlinks are stale' /tmp/bbs-codex-check.err

echo "bbs-codex-competitive ok"
