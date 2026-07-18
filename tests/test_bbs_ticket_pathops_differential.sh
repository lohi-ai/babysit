#!/usr/bin/env bash
# tests/test_bbs_ticket_pathops_differential.sh — the Go path-broker port
# (internal/cmd/ticket_path.go, ticket_handoff.go, ticket_findsimilar.go) must
# behave identically to the frozen bash oracle.
#
# Covers the file-only Slice D subcommands: path (--read/--write across kinds),
# list, reconcile, find-similar, add-handoff, latest-handoff, set-review,
# set-evidence, evidence-status, qa-evidence. All operate over the Layout C home
# with a fixed ticket, so the sequence is deterministic once timestamps and the
# per-impl project-home prefix are masked. Git-mutating base-ops + ensure have
# their own harness (test_bbs_ticket_baseops_differential.sh).
#
# Method: seed identical ticket state into each impl's project home, replay one
# command sequence against the Go binary and the frozen bash reference, and
# assert per-command stdout/stderr/exit + the resulting on-disk files match.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
GO_BIN="$SCRIPT_DIR/bin/bbs-ticket"
REF="$SCRIPT_DIR/tests/fixtures/bbs-ticket.reference"

command -v jq >/dev/null 2>&1 || { echo "SKIP: jq not installed"; exit 0; }
[ -x "$GO_BIN" ] || { echo "FAIL: $GO_BIN not built (go build -o bin/bbs ./cmd/bbs)"; exit 1; }
[ -f "$REF" ]    || { echo "FAIL: missing $REF"; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

export HOME="$ROOT/home"
export BABYSIT_HOME="$ROOT/home/.babysit"
export BABYSIT_TICKET="bs-path01"
export PATH="$SCRIPT_DIR/bin:$PATH"
export BBS_LIB="$SCRIPT_DIR/bin/lib"
# Pin legacy dates far in the future so no sunset/hardfail branch fires, and
# silence the telemetry-gated BBS_PATH_* stderr so the diff is on behavior only.
export BBS_LEGACY_SUNSET="2099-01-01"
export BBS_LEGACY_HARDFAIL="2099-06-01"
export BBS_PATH_TELEMETRY=0
unset BBS_TICKET AGENT_ROLE GT_ROLE 2>/dev/null || true
mkdir -p "$HOME"

mask() {
  # Collapse the JSON-parser error tail: Go's encoding/json and Python's json
  # word the same malformed-input rejection differently (both exit 2). Behavior,
  # not the parser's prose, is the contract.
  sed -E -e 's/"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z"/"TS"/g' \
         -e 's/(set-evidence: not valid JSON:).*/\1 <PARSER MSG>/' \
         -e "s#$ROOT/bash-home#IMPLHOME#g" \
         -e "s#$ROOT/go-home#IMPLHOME#g"
}

# seed <impl> — build an identical ticket tree in the impl's project home.
seed() {
  local home="$ROOT/$1-home" ph
  ph="$home/projects/slug"
  local t="$ph/tickets/bs-path01"
  mkdir -p "$t/handoffs" "$t/verdicts" "$t/reviews" "$t/evidence"
  # index.json with a branch pointer (find-similar reads slug from it).
  cat > "$t/index.json" <<'JSON'
{"id":"bs-path01","status":"in_progress","pointers":{"branch":"feat/bs-path01_add_user_export"}}
JSON
  printf 'Add a user data export button to the settings page.\n' > "$t/requirement.md"
  printf '# plan\n' > "$t/plan.md"
  # A second, closed ticket + an open one so find-similar/list/reconcile have
  # a population to rank and scan.
  local t2="$ph/tickets/bs-other2"
  mkdir -p "$t2"
  cat > "$t2/index.json" <<'JSON'
{"id":"bs-other2","status":"backlog","pointers":{"branch":"feat/bs-other2_user_export_csv"}}
JSON
  printf 'Export user list as CSV download.\n' > "$t2/requirement.md"
  local t3="$ph/tickets/bs-done3"
  mkdir -p "$t3"
  cat > "$t3/index.json" <<'JSON'
{"id":"bs-done3","status":"done","pointers":{"branch":"feat/bs-done3_user_export_pdf"}}
JSON
  printf 'Export user profile as PDF.\n' > "$t3/requirement.md"
}
seed bash
seed go

printf '#!/usr/bin/env bash\nexec "%s" "$@"\n' "$GO_BIN" > "$ROOT/go-cmd"
printf '#!/usr/bin/env bash\nexec bash "%s" "$@"\n' "$REF" > "$ROOT/bash-cmd"
chmod +x "$ROOT/go-cmd" "$ROOT/bash-cmd"

run_impl() {
  local impl="$1"; shift
  export BABYSIT_PROJECT_HOME="$ROOT/$impl-home/projects/slug"
  local out err rc
  out="$( "$ROOT/$impl-cmd" "$@" 2>"$ROOT/.err" )"; rc=$?
  err="$(cat "$ROOT/.err")"
  {
    printf '### %s\n' "$*"
    printf 'RC=%s\n' "$rc"
    printf 'OUT=%s\n' "$out"
    printf 'ERR=%s\n' "$err"
  } >> "$ROOT/$impl.steps"
}

# ── the shared command sequence ───────────────────────────────────────
SEQ=(
  # path — read/write across kinds
  "path home --read"
  "path index --read"
  "path requirement --read"
  "path plan --read"
  "path design --read"                       # missing → fallback/empty
  "path history --write"
  "path verdict --read --skill qa"
  "path review --read --skill review-pr"
  "path evidence --read --skill qa --name run1"
  "path handoff --read --latest"
  "path handoff --write --skill implement"   # rejected (use add-handoff)
  "path boguskind --read"                    # unknown kind
  "path requirement"                         # no mode
  # handoffs
  "add-handoff --skill plan-draft --status DONE"
  "add-handoff --skill implement --status DONE_WITH_CONCERNS"
  "latest-handoff"
  "path handoff --read --latest"
  # reviews + evidence
  "set-review --skill review-pr --body-file $ROOT/review.md"
  "set-evidence --skill qa --kind verification --json {\"result\":\"pass\"}"
  "set-evidence --skill qa --kind risk-gate --json {\"high_risk\":false}"
  "evidence-status --skill qa"
  "set-evidence --skill qa --kind verification --json {\"nope\":1}"   # missing field
  # find-similar
  "find-similar --from-input export user data"
  "find-similar --from-input totally unrelated quantum topic --min-score 0.6"
  "find-similar --from-input user export --limit 1"
  # list
  "list"
)

printf 'reviewed clean\n' > "$ROOT/review.md"

for cmd in "${SEQ[@]}"; do
  eval "set -- $cmd"
  run_impl bash "$@"
  run_impl go "$@"
done

# ── qa-evidence needs a verdict body on stdin; run it separately ──────
QA_BODY='STATUS: DONE
VERDICT: PASS
RUBRIC: correctness=A completeness=A freshness=A
EVIDENCE: ran agent-browser journey, captured screenshot evidence/qa/run.png
SUMMARY: all flows pass'
run_qa() {
  local impl="$1"
  export BABYSIT_PROJECT_HOME="$ROOT/$impl-home/projects/slug"
  local out err rc
  out="$(printf '%s' "$QA_BODY" | "$ROOT/$impl-cmd" qa-evidence 2>"$ROOT/.err")"; rc=$?
  err="$(cat "$ROOT/.err")"
  { printf '### qa-evidence (stdin)\nRC=%s\nOUT=%s\nERR=%s\n' "$rc" "$out" "$err"; } >> "$ROOT/$impl.steps"
}
run_qa bash
run_qa go

# ── assert per-step stdout/stderr/exit identical ─────────────────────
if diff -u <(mask < "$ROOT/bash.steps") <(mask < "$ROOT/go.steps") > "$ROOT/steps.diff"; then
  ok "per-command stdout/stderr/exit identical across all $(( ${#SEQ[@]} + 1 )) steps"
else
  fail "per-command output diverged" "$(head -60 "$ROOT/steps.diff")"
fi

# ── assert resulting on-disk files match (handoffs, reviews, evidence) ─
cmp_tree() {
  local rel="$1"
  local b="$ROOT/bash-home/projects/slug/tickets/bs-path01/$rel"
  local g="$ROOT/go-home/projects/slug/tickets/bs-path01/$rel"
  if [ -e "$b" ] && [ -e "$g" ]; then
    if diff -r <(cd "$b" 2>/dev/null && ls -1) <(cd "$g" 2>/dev/null && ls -1) >/dev/null 2>&1 \
       || diff -q "$b" "$g" >/dev/null 2>&1; then
      ok "$rel present in both"
    else
      fail "$rel diverged"
    fi
  else
    fail "$rel missing" "bash=$b go=$g"
  fi
}
# handoffs dir: same file names (seq-skill-status.md) + LATEST
if diff <(cd "$ROOT/bash-home/projects/slug/tickets/bs-path01/handoffs" && ls -1 | mask) \
        <(cd "$ROOT/go-home/projects/slug/tickets/bs-path01/handoffs"   && ls -1 | mask) >/dev/null; then
  ok "handoffs/ file set identical"
else
  fail "handoffs/ file set diverged"
fi

# index.json (reconcile/handoff history) semantically identical
BIDX="$ROOT/bash-home/projects/slug/tickets/bs-path01/index.json"
GIDX="$ROOT/go-home/projects/slug/tickets/bs-path01/index.json"
if diff -u <(jq -S . "$BIDX" | mask) <(jq -S . "$GIDX" | mask) > "$ROOT/idx.diff"; then
  ok "index.json semantically identical (jq -S)"
else
  fail "index.json diverged" "$(cat "$ROOT/idx.diff")"
fi

# history.jsonl semantically identical line-for-line
BH="$ROOT/bash-home/projects/slug/tickets/bs-path01/history.jsonl"
GH="$ROOT/go-home/projects/slug/tickets/bs-path01/history.jsonl"
if [ -f "$BH" ] && [ -f "$GH" ]; then
  norm() { while IFS= read -r ln; do printf '%s\n' "$ln" | jq -Sc . ; done < "$1" | mask; }
  if diff -u <(norm "$BH") <(norm "$GH") > "$ROOT/hist.diff"; then
    ok "history.jsonl semantically identical, line-for-line"
  else
    fail "history.jsonl diverged" "$(cat "$ROOT/hist.diff")"
  fi
else
  fail "history.jsonl missing" "bash=$BH go=$GH"
fi

echo
echo "  $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || { printf '  failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; }
