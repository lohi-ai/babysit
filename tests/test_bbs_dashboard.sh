#!/usr/bin/env bash
# tests/test_bbs_dashboard.sh -- coverage for bin/bbs-dashboard snapshot writer.
#
# Codepaths covered (per plan acceptance criteria #5 + Test plan section):
#   --help exits 0
#   --no-open happy path (fixture)
#   --slug <unknown> exits 1 (deprecated flag still works)
#   idempotent re-run produces byte-identical output
#   missing web/dist (default invocation, no --no-open)
#   corrupt index.json (skip+warn, exit 0 with partial output)
#   empty timeline.jsonl
#   no analytics file
#   no active sessions
#   titles containing </script> and U+2028 (escaping)
#   cross-project: both projects appear under .projects
#   path-traversal rejection: bad slug skipped, per-line stderr
#   empty projects dir: empty snapshot + stderr message
#   stale banner: v1 data.js -> loadSnapshot marks _stale=true
#   truncation: >2000 decisions -> meta.truncations records exact counts

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_DASHBOARD="$SCRIPT_DIR/bin/bbs-dashboard"
[ -x "$BBS_DASHBOARD" ] || { echo "FAIL: $BBS_DASHBOARD not executable" >&2; exit 1; }

PASS=0
FAIL=0
FAIL_NAMES=()

ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }
case_header() { printf '\n\033[1m%s\033[0m\n' "$1"; }

mk_state() {
  local t="$1"
  mkdir -p "$t/state/projects/sample/tickets/bs-tt0001/handoffs"
  mkdir -p "$t/state/projects/sample/tickets/bs-tt0001/verdicts"
  mkdir -p "$t/state/analytics"
  mkdir -p "$t/state/sessions"
  cat > "$t/state/projects/sample/tickets/bs-tt0001/index.json" <<'EOF'
{"id":"bs-tt0001","status":"in_progress","phase":"implement","parent":null,"created_at":"2026-04-20T00:00:00Z","updated_at":"2026-04-22T00:00:00Z","pointers":{"branch":"feat/x","ticket_size":"M"}}
EOF
  echo "# Sample title" > "$t/state/projects/sample/tickets/bs-tt0001/requirement.md"
  printf 'STATUS: DONE\n\nAll checks green.\n' \
    > "$t/state/projects/sample/tickets/bs-tt0001/verdicts/qa.md"
  echo '{"ts":"2026-04-22T00:00:00Z","ticket":"bs-tt0001","event":"x"}' \
    > "$t/state/projects/sample/tickets/bs-tt0001/history.jsonl"
  echo '{"ts":"2026-04-21T00:00:00Z","skill":"plan-draft","event":"end","duration_s":10,"outcome":"success"}' \
    > "$t/state/analytics/skill-usage.jsonl"
  : > "$t/state/sessions/active-1"
}

mk_repo_root() {
  local t="$1"
  # Provide a minimal "repo root" that bbs-dashboard can use for VERSION + WEB_DIR.
  mkdir -p "$t/repo/web/dist"
  echo "9.9.9" > "$t/repo/VERSION"
}

# ---- --help ------------------------------------------------------
case_header "--help"
out=$("$BBS_DASHBOARD" --help 2>&1) && rc=0 || rc=$?
if [ "$rc" = "0" ] && printf '%s' "$out" | grep -q "Usage:"; then
  ok "--help exits 0 with Usage"
else
  fail "--help exits 0 with Usage" "rc=$rc out=$out"
fi

# Verify --help mentions BBS_DASHBOARD_MAX_BYTES and deprecation note
if printf '%s' "$out" | grep -q "BBS_DASHBOARD_MAX_BYTES"; then
  ok "--help mentions BBS_DASHBOARD_MAX_BYTES"
else
  fail "--help mentions BBS_DASHBOARD_MAX_BYTES" "out=$out"
fi

if printf '%s' "$out" | grep -q "DEPRECATED"; then
  ok "--help mentions --slug deprecation"
else
  fail "--help mentions --slug deprecation" "out=$out"
fi

# ---- --no-open happy path (v2 schema) ---------------------------
case_header "--no-open happy path"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out1.txt" 2> "$T/err1.txt"; then
  if [ -f "$T/repo/web/dist/data.js" ] \
     && grep -q '^window\.__BBS_DATA__ = ' "$T/repo/web/dist/data.js"; then
    ok "data.js written with window.__BBS_DATA__ assignment"
  else
    fail "data.js shape wrong" "$(cat "$T/err1.txt")"
  fi
  # v2: must have schema_version:2 and projects key
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(d.meta.schema_version!==2){process.exit(1);}" 2>/dev/null; then
    ok "data.js has schema_version:2"
  else
    fail "data.js has schema_version:2"
  fi
  # v2: projects key present with 'sample' slug
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(!d.projects||!d.projects.sample){process.exit(1);}" 2>/dev/null; then
    ok "data.js has projects.sample"
  else
    fail "data.js has projects.sample"
  fi
  # v2: decisions, skillEvents, journalTail, sessions keys present
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(!Array.isArray(d.decisions)||!Array.isArray(d.skillEvents)||!Array.isArray(d.journalTail)||typeof d.sessions!=='object'){process.exit(1);}" 2>/dev/null; then
    ok "data.js has all v2 top-level keys"
  else
    fail "data.js has all v2 top-level keys"
  fi
  # verdict_statuses: categorical per-skill status parsed from verdicts/*.md
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__.projects.sample.ticketDetail['bs-tt0001']; \
    if(!d||!d.verdict_statuses||d.verdict_statuses.qa!=='DONE'){process.exit(1);}" 2>/dev/null; then
    ok "ticketDetail has verdict_statuses.qa=DONE"
  else
    fail "ticketDetail has verdict_statuses.qa=DONE"
  fi
  # stdout message check
  if grep -q "bbs-dashboard: wrote.*data.js" "$T/out1.txt"; then
    ok "stdout message: wrote <path>"
  else
    fail "stdout message: wrote <path>" "out=$(cat "$T/out1.txt")"
  fi
else
  fail "happy-path snapshot exits 0" "$(cat "$T/err1.txt")"
fi

# ---- --slug <unknown> (deprecated flag still errors correctly) --
case_header "--slug <unknown>"
T=$(mktemp -d); mk_repo_root "$T"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open --slug nonexistent > "$T/out.txt" 2> "$T/err.txt"; then
  fail "unknown slug should exit non-zero"
else
  rc=$?
  if [ "$rc" = "1" ] && grep -q "no babysit state" "$T/err.txt"; then
    ok "unknown slug exits 1 with clear message"
  else
    fail "unknown slug exits 1 with clear message" "rc=$rc err=$(cat "$T/err.txt")"
  fi
fi

# ---- idempotent re-run ------------------------------------------
case_header "idempotent re-run"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
# Strip generated_at (v2) to allow timestamp to differ between runs
BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
  "$BBS_DASHBOARD" --no-open > /dev/null 2>&1
A=$(sed 's/"generated_at":"[^"]*"/"generated_at":""/' "$T/repo/web/dist/data.js")
sleep 1
BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
  "$BBS_DASHBOARD" --no-open > /dev/null 2>&1
B=$(sed 's/"generated_at":"[^"]*"/"generated_at":""/' "$T/repo/web/dist/data.js")
if [ "$A" = "$B" ]; then
  ok "re-run is byte-identical (modulo generated_at)"
else
  fail "re-run is byte-identical (modulo generated_at)"
fi

# ---- missing web/dist (default invocation) ----------------------
case_header "missing web/dist"
T=$(mktemp -d); mk_state "$T"
mkdir -p "$T/repo"; echo 9.9.9 > "$T/repo/VERSION"
# Default invocation: --no-open omitted; should snapshot then fail at open
# stage with exit 1 because dist/index.html is missing.
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" > "$T/out.txt" 2> "$T/err.txt"; then
  fail "missing dist should exit 1"
else
  rc=$?
  if [ "$rc" = "1" ] && grep -q "web/dist/ missing; run: bbs-dashboard build" "$T/err.txt"; then
    ok "missing dist exits 1 with exact hint"
  else
    fail "missing dist exits 1 with exact hint" "rc=$rc err=$(cat "$T/err.txt")"
  fi
fi

# ---- corrupt index.json (skip+warn) -----------------------------
case_header "corrupt index.json"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
mkdir -p "$T/state/projects/sample/tickets/bs-bad000"
echo "not json" > "$T/state/projects/sample/tickets/bs-bad000/index.json"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  if grep -q "skipping bs-bad000 -- corrupt index" "$T/err.txt" \
     && grep -q '"id":"bs-tt0001"' "$T/repo/web/dist/data.js"; then
    ok "corrupt ticket skipped, others snapshotted, exit 0"
  else
    fail "corrupt ticket skip+warn" "err=$(cat "$T/err.txt")"
  fi
else
  fail "corrupt ticket skip+warn exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- empty timeline.jsonl ---------------------------------------
case_header "empty timeline.jsonl"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
: > "$T/state/projects/sample/tickets/bs-tt0001/history.jsonl"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  # v2: timeline is nested under projects.sample.timeline
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(!d.projects.sample||d.projects.sample.timeline.length!==0){process.exit(1);}" 2>/dev/null; then
    ok "empty timeline produces empty array under projects.sample.timeline"
  else
    fail "empty timeline produces empty array under projects.sample.timeline"
  fi
else
  fail "empty timeline run exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- no analytics file ------------------------------------------
case_header "no analytics file"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
rm -f "$T/state/analytics/skill-usage.jsonl"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  # v2: skillEvents at top-level should be empty; per-project analytics.rows also empty
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(d.skillEvents.length!==0){process.exit(1);}" 2>/dev/null; then
    ok "missing analytics produces empty skillEvents"
  else
    fail "missing analytics produces empty skillEvents"
  fi
else
  fail "missing analytics run exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- no active sessions -----------------------------------------
case_header "no active sessions"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
rm -rf "$T/state/sessions"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  # v2: sessions is {count:0, slugs:[]}
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(d.sessions.count!==0){process.exit(1);}" 2>/dev/null; then
    ok "missing sessions dir produces count:0"
  else
    fail "missing sessions dir produces count:0"
  fi
else
  fail "no-sessions run exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- titles with </script> and U+2028 (security) ----------------
case_header "title with </script> + U+2028 escaping"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
# Inject a malicious title via requirement.md heading. Embed U+2028 directly.
printf '# Evil </script><script>alert(1)</script> and a U+2028\xe2\x80\xa8 here\n' \
  > "$T/state/projects/sample/tickets/bs-tt0001/requirement.md"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  # The literal sequence "</script>" must NOT appear in data.js (escaped to <\/script>).
  if grep -q '</script>' "$T/repo/web/dist/data.js"; then
    fail "</script> not escaped in data.js -- script-injection vector open"
  else
    ok "</script> escaped to <\\/script>"
  fi
  # Raw U+2028 (UTF-8 e2 80 a8) must NOT appear in data.js -- escaped to  .
  if LC_ALL=C grep -q $'\xe2\x80\xa8' "$T/repo/web/dist/data.js"; then
    fail "U+2028 not escaped in data.js"
  else
    ok "U+2028 escaped (no raw byte sequence in output)"
  fi
else
  fail "escaping run exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- cross-project: both slugs present in .projects -------------
case_header "cross-project: two projects"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
# Add a second project
mkdir -p "$T/state/projects/sample-two/tickets/bs-zz0001"
cat > "$T/state/projects/sample-two/tickets/bs-zz0001/index.json" <<'EOF'
{"id":"bs-zz0001","status":"planned","phase":"plan","parent":null,"created_at":"2026-04-23T00:00:00Z","updated_at":"2026-04-25T00:00:00Z","pointers":{"branch":"feat/bs-zz0001","ticket_size":"S"}}
EOF
echo "# Second project ticket" > "$T/state/projects/sample-two/tickets/bs-zz0001/requirement.md"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; var keys=Object.keys(d.projects).sort(); \
    if(keys.indexOf('sample')===-1||keys.indexOf('sample-two')===-1){process.exit(1);}" 2>/dev/null; then
    ok "projects contains both 'sample' and 'sample-two'"
  else
    fail "projects contains both slugs" "err=$(cat "$T/err.txt")"
  fi
else
  fail "cross-project run exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- path-traversal rejection -----------------------------------
case_header "path-traversal rejection"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
# Create a project directory with a bad name (contains @, which is not in [a-zA-Z0-9_.-])
# Note: mkdir needs quoting; the slug name is "bad@slug"
mkdir -p "$T/state/projects/bad@slug/tickets/bs-x0001"
cat > "$T/state/projects/bad@slug/tickets/bs-x0001/index.json" <<'EOF'
{"id":"bs-x0001","status":"planned","phase":"plan","parent":null,"created_at":"2026-04-23T00:00:00Z","updated_at":"2026-04-25T00:00:00Z","pointers":{"branch":"feat/bs-x0001","ticket_size":"S"}}
EOF
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  # The bad slug must appear in stderr as a skip message
  if grep -q "skipping 'bad@slug' (rejected by path-traversal guard)" "$T/err.txt"; then
    ok "path-traversal slug skipped with stderr message"
  else
    fail "path-traversal slug skipped with stderr message" "err=$(cat "$T/err.txt")"
  fi
  # The bad slug must NOT appear in the projects object
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(d.projects['bad@slug']){process.exit(1);}" 2>/dev/null; then
    ok "bad slug not in projects output"
  else
    fail "bad slug not in projects output"
  fi
else
  fail "path-traversal guard run exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- empty projects dir -----------------------------------------
case_header "empty projects dir"
T=$(mktemp -d); mk_repo_root "$T"
mkdir -p "$T/state/projects"
mkdir -p "$T/state/analytics"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  # Should exit 0 with empty projects and stderr message
  if grep -q "no projects found; run autopilot first" "$T/err.txt"; then
    ok "empty projects dir prints stderr message"
  else
    fail "empty projects dir prints stderr message" "err=$(cat "$T/err.txt")"
  fi
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    if(Object.keys(d.projects).length!==0){process.exit(1);}" 2>/dev/null; then
    ok "empty projects dir produces empty projects object"
  else
    fail "empty projects dir produces empty projects object"
  fi
else
  fail "empty projects dir exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- stale banner: v1 data.js -> _stale=true --------------------
case_header "stale banner (v1 snapshot)"
T=$(mktemp -d); mk_repo_root "$T"
# Write a v1-shaped data.js manually
cat > "$T/repo/web/dist/data.js" <<'EOF'
window.__BBS_DATA__ = {"meta":{"snapshot_at":"2026-04-20T00:00:00Z","slug":"old-project","babysit_version":"1.0.0","active_pair":null},"tickets":[],"ticketDetail":{},"timeline":[],"analytics":{"rows":[],"per_skill":[],"per_day":[],"outcome":[]},"sessions":[]};
EOF
# Use node to simulate loadSnapshot behavior (inline the TS logic)
NODE_RESULT=$(node -e "
  global.window={};
  $(cat "$T/repo/web/dist/data.js")
  var s = window.__BBS_DATA__;
  var version = s.meta && s.meta.schema_version ? s.meta.schema_version : 1;
  var stale = version < 2;
  console.log(JSON.stringify({stale: stale, version: version}));
" 2>/dev/null)
if echo "$NODE_RESULT" | grep -q '"stale":true'; then
  ok "v1 snapshot detected as stale (_stale=true)"
else
  fail "v1 snapshot detected as stale" "node result: $NODE_RESULT"
fi
# Also verify that a v2 snapshot is NOT stale
T2=$(mktemp -d); mk_state "$T2"; mk_repo_root "$T2"
BABYSIT_STATE_DIR="$T2/state" BABYSIT_DASHBOARD_REPO="$T2/repo" \
  "$BBS_DASHBOARD" --no-open > /dev/null 2>&1
NODE_RESULT2=$(node -e "
  global.window={};
  $(cat "$T2/repo/web/dist/data.js")
  var s = window.__BBS_DATA__;
  var version = s.meta && s.meta.schema_version ? s.meta.schema_version : 1;
  var stale = version < 2;
  console.log(JSON.stringify({stale: stale, version: version}));
" 2>/dev/null)
if echo "$NODE_RESULT2" | grep -q '"stale":false'; then
  ok "v2 snapshot is not stale"
else
  fail "v2 snapshot is not stale" "node result: $NODE_RESULT2"
fi

# ---- truncation banner: >2000 decisions --------------------------
case_header "truncation: >2000 decisions"
T=$(mktemp -d); mk_state "$T"; mk_repo_root "$T"
# Generate 2005 fake decision rows
python3 -c "
import json
for i in range(2005):
    row = {'ts': f'2026-04-{(i%28)+1:02d}T{(i%24):02d}:00:00Z', 'skill': 'plan-draft', 'phase': 'plan', 'classification': 'Mechanical', 'principle': 'auto', 'decision': f'decision {i}'}
    print(json.dumps(row))
" > "$T/state/analytics/decisions.jsonl"
if BABYSIT_STATE_DIR="$T/state" BABYSIT_DASHBOARD_REPO="$T/repo" \
   "$BBS_DASHBOARD" --no-open > "$T/out.txt" 2> "$T/err.txt"; then
  if node -e "global.window={}; $(cat "$T/repo/web/dist/data.js"); \
    var d=window.__BBS_DATA__; \
    var trunc=d.meta.truncations.find(function(t){return t.kind==='decisions';}); \
    if(!trunc||trunc.kept!==2000||trunc.total!==2005){process.exit(1);}
    if(d.decisions.length!==2000){process.exit(1);}" 2>/dev/null; then
    ok "truncation: decisions capped at 2000, meta.truncations records total=2005"
  else
    fail "truncation: decisions capped at 2000, meta.truncations records total=2005" \
         "err=$(cat "$T/err.txt")"
  fi
else
  fail "truncation run exits 0" "rc=$? err=$(cat "$T/err.txt")"
fi

# ---- summary -----------------------------------------------------
printf '\n\033[1mResult:\033[0m %d ok, %d fail\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
exit 0
