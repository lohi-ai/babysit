#!/usr/bin/env bash
# tests/test_dashboard_v118_snapshot.sh — pins the dashboard's v1.18 identity
# surfacing. The dashboard must:
#   1. Emit `sessions.sessions[]` with {id, ticket, product, cwd, age_min}
#      from session yamls (not just {count, slugs}).
#   2. Emit `ticketDetail[id].repos` from manifest.yaml — one row per repo
#      with {name, branch, canonical, worktree, base, pushed}.
#
# Both surfaces are how a human auditor sees post-1.18 state without dropping
# into the shell; pre-1.19 the dashboard only knew session slugs and never
# read manifest.yaml at all.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DASH="$SCRIPT_DIR/bin/bbs-dashboard"
[ -x "$DASH" ] || { echo "FAIL: bbs-dashboard not executable" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "FAIL: jq required" >&2; exit 1; }
command -v node >/dev/null 2>&1 || { echo "FAIL: node required to eval data.js" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Extract data.js → JSON via node (data.js is `window.__BBS_DATA__ = {...};`)
read_snapshot_json() {
  local data_js="$1"
  node -e "global.window={}; eval(require('fs').readFileSync('$data_js','utf8')); process.stdout.write(JSON.stringify(window.__BBS_DATA__));"
}

# ── sessions-emits-structured-rows ─────────────────────────────────────
T="$(mktemp -d)"
(
  set -e
  STATE="$T/.babysit"
  mkdir -p "$STATE/sessions" "$STATE/projects/demo/tickets/bs-aaa"

  # Stub a project so `compose_snapshot` has something to walk.
  cat > "$STATE/projects/demo/tickets/bs-aaa/index.json" <<JSON
{"version":1,"ticket":"bs-aaa","status":"in_progress","phase":"plan-draft","pointers":{"branch":"feat/bs-aaa_demo","ticket_size":"M"},"updated_at":"2026-04-30T12:00:00Z","created_at":"2026-04-30T11:00:00Z"}
JSON

  # Live session yaml — written by the preamble session-writer hook shape.
  cat > "$STATE/sessions/sess-001.yaml" <<YAML
version: 1
session_id: sess-001
ticket: bs-aaa
product: demo-product
started_at: 2026-04-30T11:30:00Z
last_seen_at: 2026-05-01T13:00:00Z
pid: 99999
cwd: /Users/me/work/demo
YAML

  DIST="$T/dist"
  mkdir -p "$DIST"
  BABYSIT_STATE_DIR="$STATE" \
    BABYSIT_DASHBOARD_REPO="$T/repo" \
    bash -c "
      mkdir -p '$T/repo/web/dist'
      ln -sf '$DASH' '$T/repo/bin-bbs-dashboard'
      cp '$SCRIPT_DIR/VERSION' '$T/repo/VERSION'
    "

  # Run dashboard with custom STATE_DIR + DASHBOARD_REPO
  BABYSIT_STATE_DIR="$STATE" BABYSIT_DASHBOARD_REPO="$T/repo" \
    "$DASH" --no-open >/dev/null 2>&1 \
    || { echo "dashboard run failed"; exit 1; }

  DATA_JS="$T/repo/web/dist/data.js"
  [ -f "$DATA_JS" ] || { echo "data.js missing at $DATA_JS"; exit 1; }

  json="$(read_snapshot_json "$DATA_JS")"

  count="$(jq -r '.sessions.count' <<<"$json")"
  [ "$count" = "1" ] || { echo "sessions.count = '$count', want 1"; exit 1; }

  rows="$(jq -r '.sessions.sessions | length' <<<"$json")"
  [ "$rows" = "1" ] || { echo "sessions.sessions length = '$rows', want 1"; exit 1; }

  ticket="$(jq -r '.sessions.sessions[0].ticket' <<<"$json")"
  [ "$ticket" = "bs-aaa" ] || { echo "session ticket = '$ticket', want bs-aaa"; exit 1; }

  product="$(jq -r '.sessions.sessions[0].product' <<<"$json")"
  [ "$product" = "demo-product" ] || { echo "session product = '$product', want demo-product"; exit 1; }

  cwd="$(jq -r '.sessions.sessions[0].cwd' <<<"$json")"
  [ "$cwd" = "/Users/me/work/demo" ] || { echo "session cwd = '$cwd'"; exit 1; }

  # age_min must be a non-negative integer
  age="$(jq -r '.sessions.sessions[0].age_min' <<<"$json")"
  case "$age" in ''|*[!0-9]*) echo "session age_min = '$age', want integer"; exit 1 ;; esac
) && ok "sessions-emits-structured-rows" || fail "sessions-emits-structured-rows"
rm -rf "$T"

# ── repos-from-manifest-yaml ───────────────────────────────────────────
T="$(mktemp -d)"
(
  set -e
  STATE="$T/.babysit"
  TDIR="$STATE/projects/demo/tickets/bs-bbb"
  mkdir -p "$TDIR" "$STATE/sessions"

  cat > "$TDIR/index.json" <<JSON
{"version":1,"ticket":"bs-bbb","status":"in_progress","phase":"implement","pointers":{"branch":"feat/bs-bbb_demo","ticket_size":"M"},"updated_at":"2026-04-30T12:00:00Z","created_at":"2026-04-30T11:00:00Z"}
JSON

  # Two-repo manifest (product-mode shape).
  cat > "$TDIR/manifest.yaml" <<YAML
version: 1
ticket: bs-bbb
title: demo cross-repo ticket
created_at: 2026-04-30T11:00:00Z
updated_at: 2026-04-30T12:00:00Z
repos:
  - name: fe
    branch: feat/bs-bbb_demo-fe
    canonical: /tmp/fe
    worktree: /tmp/wt/bs-bbb/fe
    base: main
    pushed: true
  - name: be
    branch: feat/bs-bbb_demo-be
    canonical: /tmp/be
    worktree: /tmp/wt/bs-bbb/be
    base: main
    pushed: false
YAML

  mkdir -p "$T/repo/web/dist"
  cp "$SCRIPT_DIR/VERSION" "$T/repo/VERSION"

  BABYSIT_STATE_DIR="$STATE" BABYSIT_DASHBOARD_REPO="$T/repo" \
    "$DASH" --no-open >/dev/null 2>&1 \
    || { echo "dashboard run failed"; exit 1; }

  DATA_JS="$T/repo/web/dist/data.js"
  json="$(read_snapshot_json "$DATA_JS")"

  repos_len="$(jq -r '.projects.demo.ticketDetail."bs-bbb".repos | length' <<<"$json")"
  [ "$repos_len" = "2" ] || { echo "repos length = '$repos_len', want 2"; exit 1; }

  fe_branch="$(jq -r '.projects.demo.ticketDetail."bs-bbb".repos[0].branch' <<<"$json")"
  [ "$fe_branch" = "feat/bs-bbb_demo-fe" ] || { echo "fe branch = '$fe_branch'"; exit 1; }

  fe_pushed="$(jq -r '.projects.demo.ticketDetail."bs-bbb".repos[0].pushed' <<<"$json")"
  [ "$fe_pushed" = "true" ] || { echo "fe pushed = '$fe_pushed', want true"; exit 1; }

  be_pushed="$(jq -r '.projects.demo.ticketDetail."bs-bbb".repos[1].pushed' <<<"$json")"
  [ "$be_pushed" = "false" ] || { echo "be pushed = '$be_pushed', want false"; exit 1; }

  be_worktree="$(jq -r '.projects.demo.ticketDetail."bs-bbb".repos[1].worktree' <<<"$json")"
  [ "$be_worktree" = "/tmp/wt/bs-bbb/be" ] || { echo "be worktree = '$be_worktree'"; exit 1; }
) && ok "repos-from-manifest-yaml" || fail "repos-from-manifest-yaml"
rm -rf "$T"

# ── repos-empty-when-no-manifest-yaml ──────────────────────────────────
T="$(mktemp -d)"
(
  set -e
  STATE="$T/.babysit"
  TDIR="$STATE/projects/demo/tickets/bs-ccc"
  mkdir -p "$TDIR" "$STATE/sessions"

  cat > "$TDIR/index.json" <<JSON
{"version":1,"ticket":"bs-ccc","status":"triage","pointers":{"branch":"feat/bs-ccc_x"},"updated_at":"2026-04-30T12:00:00Z","created_at":"2026-04-30T11:00:00Z"}
JSON
  # No manifest.yaml.

  mkdir -p "$T/repo/web/dist"
  cp "$SCRIPT_DIR/VERSION" "$T/repo/VERSION"

  BABYSIT_STATE_DIR="$STATE" BABYSIT_DASHBOARD_REPO="$T/repo" \
    "$DASH" --no-open >/dev/null 2>&1 \
    || { echo "dashboard run failed"; exit 1; }

  json="$(read_snapshot_json "$T/repo/web/dist/data.js")"
  repos="$(jq -r '.projects.demo.ticketDetail."bs-ccc".repos' <<<"$json")"
  [ "$repos" = "[]" ] || { echo "repos for ticket without manifest.yaml = '$repos', want []"; exit 1; }
) && ok "repos-empty-when-no-manifest-yaml" || fail "repos-empty-when-no-manifest-yaml"
rm -rf "$T"

# ── stale-session-excluded-from-rows ───────────────────────────────────
# Sessions older than 120 minutes must not appear in `sessions.sessions[]`
# (same TTL as the existing slug list).
T="$(mktemp -d)"
(
  set -e
  STATE="$T/.babysit"
  mkdir -p "$STATE/sessions" "$STATE/projects/demo/tickets/bs-aaa"

  cat > "$STATE/projects/demo/tickets/bs-aaa/index.json" <<JSON
{"version":1,"ticket":"bs-aaa","status":"in_progress","pointers":{},"updated_at":"2026-04-30T12:00:00Z","created_at":"2026-04-30T11:00:00Z"}
JSON

  for sid in fresh stale; do
    cat > "$STATE/sessions/$sid.yaml" <<YAML
version: 1
session_id: $sid
ticket: bs-aaa
product: demo
started_at: 2026-04-30T11:30:00Z
last_seen_at: 2026-05-01T13:00:00Z
pid: 12345
cwd: /tmp/$sid
YAML
  done

  # Backdate stale yaml by 200 minutes.
  touch -t "$(date -u -v-200M +%Y%m%d%H%M.%S 2>/dev/null \
               || date -u -d '200 min ago' +%Y%m%d%H%M.%S 2>/dev/null)" \
        "$STATE/sessions/stale.yaml"

  mkdir -p "$T/repo/web/dist"
  cp "$SCRIPT_DIR/VERSION" "$T/repo/VERSION"

  BABYSIT_STATE_DIR="$STATE" BABYSIT_DASHBOARD_REPO="$T/repo" \
    "$DASH" --no-open >/dev/null 2>&1 \
    || { echo "dashboard run failed"; exit 1; }

  json="$(read_snapshot_json "$T/repo/web/dist/data.js")"
  ids="$(jq -r '.sessions.sessions[].id' <<<"$json" | sort | tr '\n' ' ')"
  [ "$ids" = "fresh " ] || { echo "session ids = '$ids', want 'fresh '"; exit 1; }
) && ok "stale-session-excluded-from-rows" || fail "stale-session-excluded-from-rows"
rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
