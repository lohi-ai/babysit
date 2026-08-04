#!/usr/bin/env bash
# Differential harness: the Go `bbs dashboard` vs the frozen bash oracle
# (tests/fixtures/bbs-dashboard.reference). Both snapshot an identical seeded
# STATE_DIR with --no-open; the emitted data.js is stripped back to JSON and
# compared semantically via `jq -S` (generated_at nulled). Reconcile is
# disabled on both sides so the diff is pure composition, no bbs-ticket dep.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ORACLE="$ROOT/tests/fixtures/bbs-dashboard.reference"

command -v jq >/dev/null 2>&1      || { echo "SKIP: jq not found"; exit 0; }
command -v python3 >/dev/null 2>&1 || { echo "SKIP: python3 not found"; exit 0; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Build the Go binary; expose as bbs-dashboard via argv0 symlink.
BIN="$WORK/bin"; mkdir -p "$BIN"
( cd "$ROOT" && go build -o "$BIN/bbs" ./cmd/bbs )
ln -sf bbs "$BIN/bbs-dashboard"
GO="$BIN/bbs-dashboard"

export BBS_DASHBOARD_NO_RECONCILE=1
export PATH="$BIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin"

# ── seed a rich state dir ────────────────────────────────────────
STATE="$WORK/state"
seed() {
  local t="$STATE/projects/demo/tickets/$1"; shift
  mkdir -p "$t/handoffs" "$t/verdicts" "$t/reviews"
  cat > "$t/index.json"
}
mkdir -p "$STATE/analytics" "$STATE/sessions"

seed bs-t001 <<'JSON'
{"id":"bs-t001","status":"in_progress","phase":"implement","parent":null,"created_at":"2026-04-20T00:00:00Z","updated_at":"2026-04-22T00:00:00Z","pointers":{"branch":"feat/one","ticket_size":"M"}}
JSON
T1="$STATE/projects/demo/tickets/bs-t001"
printf '# First ticket title\n\nBody.\n' > "$T1/requirement.md"
printf '## Plan\n- step\n' > "$T1/plan.md"
printf 'STATUS: DONE\n\nqa green\n' > "$T1/verdicts/qa.md"
printf 'STATUS: DONE_WITH_CONCERNS\nminor\n' > "$T1/verdicts/review-pr.md"
printf '# no status here\n' > "$T1/verdicts/notes.md"
printf 'handoff body one\n' > "$T1/handoffs/plan.md"
printf 'handoff body two\n' > "$T1/handoffs/impl.md"
printf 'review body\n' > "$T1/reviews/r1.md"
echo '{"version":1,"data":{"nested":true},"n":3}' > "$T1/checkpoint.json"
{ echo '{"ts":"2026-04-22T00:00:00Z","event":"a"}'
  echo '{"ts":"2026-04-22T01:00:00Z","event":"b"}'; } > "$T1/history.jsonl"
cat > "$T1/manifest.yaml" <<'YAML'
version: 1
ticket: bs-t001
repos:
  - name: fe
    branch: feat/one-fe
    canonical: /tmp/fe
    worktree: /tmp/wt/fe
    base: main
    pushed: true
  - name: be
    branch: feat/one-be
    canonical: /tmp/be
    worktree: /tmp/wt/be
    base: main
    pushed: false
YAML

seed bs-t002 <<'JSON'
{"id":"bs-t002","status":"planned","phase":"plan","parent":"bs-t001","created_at":"2026-04-23T00:00:00Z","updated_at":"2026-04-25T00:00:00Z","pointers":{"branch":"feat/two","ticket_size":"S"}}
JSON
T2="$STATE/projects/demo/tickets/bs-t002"
printf '# Second ticket\n' > "$T2/requirement.md"
echo '{"ts":"2026-04-24T00:00:00Z","event":"c"}' > "$T2/history.jsonl"

# analytics: top-level (state) + per-project — same file content, both consumed.
ANALYTICS='{"ts":"2026-04-20T09:00:00Z","skill":"plan-draft","duration_s":10,"outcome":"success"}
{"ts":"2026-04-20T10:00:00Z","skill":"implement","duration_s":30,"outcome":"success"}
{"ts":"2026-04-21T11:00:00Z","skill":"implement","duration_s":20,"outcome":"error"}
{"ts":"2026-04-21T12:00:00Z","skill":"qa","duration_s":5,"outcome":"success"}
{"ts":"2026-04-22T13:00:00Z","skill":"implement","duration_s":15,"outcome":"success"}'
printf '%s\n' "$ANALYTICS" > "$STATE/analytics/skill-usage.jsonl"
mkdir -p "$STATE/projects/demo/analytics"
printf '%s\n' "$ANALYTICS" > "$STATE/projects/demo/analytics/skill-usage.jsonl"

{ echo '{"ts":"2026-04-20T09:00:00Z","classification":"Mechanical","decision":"d1"}'
  echo '{"ts":"2026-04-21T09:00:00Z","classification":"Taste","decision":"d2"}'; } \
  > "$STATE/analytics/decisions.jsonl"
{ echo '{"ts":"2026-04-20T00:00:00Z","builder":"x","score":1}'; } \
  > "$STATE/builder-profile.jsonl"
printf 'line one\nline two\nline three\n' > "$STATE/journal.log"

cat > "$STATE/sessions/sess-01.yaml" <<'YAML'
version: 1
session_id: sess-01
ticket: bs-t001
product: demo-product
started_at: 2026-04-30T11:30:00Z
cwd: /Users/me/work/demo
YAML
# keep the session fresh (age < 120 min)
touch "$STATE/sessions/sess-01.yaml"

# ── run both impls ───────────────────────────────────────────────
run_impl() {
  local bin="$1" repo="$2"
  mkdir -p "$repo/web/dist"
  echo "9.9.9" > "$repo/VERSION"
  BABYSIT_STATE_DIR="$STATE" BABYSIT_DASHBOARD_REPO="$repo" \
    "$bin" --no-open >/dev/null 2>"$repo/err.txt"
  # strip the JS wrapper back to JSON, null the timestamp, canonicalize
  sed -e 's/^window\.__BBS_DATA__ = //' -e 's/;$//' "$repo/web/dist/data.js" \
    | jq -S 'del(.meta.generated_at)'
}

# The oracle is frozen at the 2026-07-18 port. Every field the schema gained
# afterwards (dashboard control plane, approvals, design/prototype pointers) is
# absent from it, so a raw diff reports each later feature as a regression and
# the suite fails permanently — which is exactly what happened.
#
# Project the candidate onto the key names the oracle uses *anywhere*, and the
# differential keeps the property it was built for — no field the oracle knows
# about may diverge, in value or in absence — while tolerating additive growth.
# Matching by name rather than by path is deliberate: a name the oracle never
# uses at any depth is new by definition, and a name it does use stays compared
# at every depth.
project_onto_oracle() { # stdin = candidate JSON; $1 = oracle JSON file
  jq -S --argjson keys "$(jq -c '[paths | .[-1] | select(type == "string")] | unique' "$1")" '
    walk(if type == "object"
         then with_entries(select(.key as $k | $keys | index($k)))
         else . end)'
}

# Names dropped by the projection — printed, never silent, so schema growth is
# visible in the run instead of being quietly excused.
added_since_oracle() { # $1 = oracle JSON file, $2 = candidate JSON file
  jq -r -n --slurpfile o "$1" --slurpfile c "$2" '
    ([$c[0] | paths | .[-1] | select(type == "string")] | unique)
    - ([$o[0] | paths | .[-1] | select(type == "string")] | unique)
    | join(", ")'
}

BASH_REPO="$WORK/repo-bash"
GO_REPO="$WORK/repo-go"
run_impl "bash $ORACLE" "$BASH_REPO" >/dev/null 2>&1 || true   # warm mkdir
BABYSIT_STATE_DIR="$STATE" BABYSIT_DASHBOARD_REPO="$BASH_REPO" \
  bash "$ORACLE" --no-open >/dev/null 2>"$BASH_REPO/err.txt"
sed -e 's/^window\.__BBS_DATA__ = //' -e 's/;$//' "$BASH_REPO/web/dist/data.js" \
  | jq -S 'del(.meta.generated_at)' > "$WORK/bash.json"
run_impl "$GO" "$GO_REPO" > "$WORK/go.raw.json"

NEW_KEYS="$(added_since_oracle "$WORK/bash.json" "$WORK/go.raw.json")"
[ -n "$NEW_KEYS" ] && echo "note added since the oracle was frozen (not compared): $NEW_KEYS"

BASH_JSON="$(cat "$WORK/bash.json")"
GO_JSON="$(project_onto_oracle "$WORK/bash.json" < "$WORK/go.raw.json")"

FAILS=0
if [ "$BASH_JSON" = "$GO_JSON" ]; then
  echo "ok   full snapshot semantically identical (jq -S)"
else
  echo "FAIL full snapshot differs:"
  diff <(printf '%s\n' "$BASH_JSON") <(printf '%s\n' "$GO_JSON") | head -60
  FAILS=$((FAILS + 1))
fi

# stderr parity (both should warn nothing fatal on a healthy fixture)
if diff -q "$BASH_REPO/err.txt" "$GO_REPO/err.txt" >/dev/null; then
  echo "ok   stderr identical"
else
  echo "note stderr differs (non-fatal):"
  diff "$BASH_REPO/err.txt" "$GO_REPO/err.txt" | head -10
fi

echo
if [ "$FAILS" -eq 0 ]; then echo "ALL PASS"; else echo "$FAILS FAILED"; exit 1; fi
