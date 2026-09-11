#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SKILL="$ROOT/.claude/skills/autopilot/SKILL.md"
README="$ROOT/README.md"

require() {
  local needle=$1 file=$2
  grep -Fq -- "$needle" "$file" || {
    echo "missing planner contract '$needle' in $file" >&2
    exit 1
  }
}

require '--planner <model>' "$SKILL"
require '--planner-effort <effort>' "$SKILL"
require 'planner_model' "$SKILL"
require 'planner_effort' "$SKILL"
require '| Codex | `gpt-5.6-terra`, `high` | `gpt-5.6-sol`, `high` | `gpt-6-astra`, `high` |' "$SKILL"
require '| Claude Code | `opus`, `high` | `opus`, `high` | advertised Fable 5.1 model/profile, `high` |' "$SKILL"
require '| OMP | `default` profile, `high` | `slow` profile, `high` | `slow` profile, `high` |' "$SKILL"
require 'OMP launch rule' "$SKILL"
require 'omp --model @default|@slow --thinking <effort>' "$SKILL"
require 'never invoke `/autopilot` in the child' "$SKILL"
require 'Add an isolated `--session-dir` and a reasonable `--max-time`' "$SKILL"
require 'terminate only its verified PID' "$SKILL"
require '`--slow <model>` configures the role; it does not activate it' "$SKILL"
require 'honor an explicit value, report `BLOCKED`' "$SKILL"
require 'bbs ticket ensure --no-branch' "$SKILL"
require 'BABYSIT_TICKET="$TICKET"' "$SKILL"
require '--planner <model>' "$README"

echo "autopilot planner routing contract: ok"
