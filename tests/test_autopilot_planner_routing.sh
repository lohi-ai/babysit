#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SKILL="$ROOT/.claude/skills/autopilot/SKILL.md"
README="$ROOT/README.md"
REF="$ROOT/.claude/skills/references/model-routing.md"

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
require '../references/model-routing.md' "$SKILL"
require '| Codex | #3 `gpt-5.6-terra`, `high` | #2 `gpt-5.6-sol`, `high` | #2 `gpt-5.6-sol`, `high` |' "$REF"
require '| Claude Code | #2 `opus`, `high` | #2 `opus`, `high` | #2 `opus`, `high` |' "$REF"
require '| OMP | #3 `@smol`, `high` | #2 `@default`, `high` | #1 `@slow`, `high` |' "$REF"
require '| Grok | `grok-4.6` | `grok-4.6` | `grok-4.6` |' "$REF"
require 'grok models' "$REF"
require '## Escalating to the top rung' "$REF"
require 'escalation, never a tier default' "$REF"
require 'came back short' "$REF"
require '| `simple` | an obvious local docs/config edit' "$REF"
require '| `critical` / `hard` | security, auth, money' "$REF"
require 'gpt-6-astra' "$REF"
require 'gpt-5.6-sol' "$REF"
require 'gpt-5.6-terra' "$REF"
require 'Fable 5.1 (`fable`)' "$REF"
require 'Sonnet 5 (`sonnet`)' "$REF"
require '`@slow`' "$REF"
require '`@default`' "$REF"
require '`@smol`' "$REF"
require 'omp config get modelRoles' "$REF"
require '10.00 | 50.00 |' "$REF"
require '2.00 | 12.00 |' "$REF"
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
