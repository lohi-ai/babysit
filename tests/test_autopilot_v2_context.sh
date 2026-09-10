#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BBS="$ROOT/bin/bbs"
command -v jq >/dev/null 2>&1 || { echo "SKIP: jq not installed"; exit 0; }
[ -x "$BBS" ] || { echo "FAIL: build $BBS from this checkout first" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export HOME="$TMP/home"
export BABYSIT_STATE_DIR="$HOME/.babysit"
export BBS_TICKET=tkt
export BBS_BASE_BRANCH=main
export BABYSIT_SKIP_COMMENT=1
mkdir -p "$HOME" "$TMP/repo" "$HOME/.babysit/projects/repo/tickets/tkt"
git init -q -b main "$TMP/repo"
git -C "$TMP/repo" -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
cd "$TMP/repo"

TICKET_HOME="$HOME/.babysit/projects/repo/tickets/tkt"
printf '%s\n' '{"id":"tkt","origin":{"type":"standalone"},"control":null}' > "$TICKET_HOME/index.json"
printf '%s\n' '{"schema_version":2,"run_id":"run-test","revision":1,"ticket":"tkt","workflow":"builder","branch":"main"}' > "$TICKET_HOME/checkpoint.json"
printf '%s\n' 'must preserve this requirement' > "$TICKET_HOME/requirement.md"
printf '%s\n' 'implement the plan' > "$TICKET_HOME/plan.md"

"$BBS" autopilot context --json --ticket tkt > "$TMP/full.json"
jq -e '.ok == true and .data.kind == "full" and (.data.cursor | startswith("v2.")) and (.data.projection.artifacts[] | select(.role == "requirement") | .excerpt | contains("must preserve"))' "$TMP/full.json" >/dev/null
[ "$(wc -c < "$TMP/full.json")" -lt 12288 ]

CURSOR="$(jq -r '.data.cursor' "$TMP/full.json")"
"$BBS" autopilot context --json --ticket tkt --since "$CURSOR" > "$TMP/delta.json"
jq -e '.ok == true and .data.kind == "delta" and (.data.changes | length == 0)' "$TMP/delta.json" >/dev/null
[ "$(wc -c < "$TMP/delta.json")" -lt 512 ]

printf '%s\n' 'changed plan' > "$TICKET_HOME/plan.md"
"$BBS" autopilot context --json --ticket tkt --since "$CURSOR" | jq -e '.data.kind == "delta" and .data.changes.snapshot and .data.changes.artifacts' >/dev/null
"$BBS" autopilot context --json --ticket tkt --since invalid | jq -e '.data.kind == "full" and .data.reset_reason == "unknown_cursor"' >/dev/null
"$BBS" autopilot recover --json --ticket tkt | jq -e '.ok == true and .data.action == "continue"' >/dev/null

"$BBS" skill enter --name implement --json > "$TMP/enter.json"
INVOCATION="$(jq -r '.data.invocation_id' "$TMP/enter.json")"
"$BBS" skill exit --invocation "$INVOCATION" --outcome success > "$TMP/exit.json"
jq -e '.data.provider_usage.available == false and .data.provider_usage.input_tokens == null' "$TMP/exit.json" >/dev/null
[ "$(wc -l < "$HOME/.babysit/analytics/skill-usage.jsonl")" -eq 2 ]

echo "PASS: context full/delta/recovery and skill runtime contracts"
