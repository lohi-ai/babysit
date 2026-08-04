#!/usr/bin/env bash
# Differential harness: the Go `bbs autopilot` vs the frozen bash oracle
# (tests/fixtures/bbs-autopilot.reference). Both run against the same seeded git
# repos with identical HOME + BABYSIT_PROJECT_HOME isolation and the same Go
# sibling bins (bbs-slug/bbs-ticket/bbs-config) on PATH, so any divergence is
# pure autopilot format. Timestamps are masked before comparison.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ORACLE="$ROOT/tests/fixtures/bbs-autopilot.reference"
[ -f "$ORACLE" ] || { echo "SKIP: oracle fixture missing"; exit 0; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Build bbs and expose it under every argv0 the oracle + the port need.
BIN="$WORK/bin"; mkdir -p "$BIN"
( cd "$ROOT" && go build -o "$BIN/bbs" ./cmd/bbs ) || { echo "FAIL: go build"; exit 1; }
for n in bbs-autopilot bbs-slug bbs-ticket bbs-config; do ln -sf bbs "$BIN/$n"; done
export PATH="$BIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin"
GO="$BIN/bbs-autopilot"

PASS=0; FAIL=0
mask() { sed -E 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g'; }

# Subcommands the Go impl grew *on purpose* after the port was frozen. The
# oracle can never list them, so the usage line would pin the Go side to bash
# forever; normalising just these names keeps the rest of the string honest —
# a reworded or reordered usage line still fails. Every entry needs a reason.
#   bs-85spcpj3: `git-flow` prints the profile-derived policy (BBS_MODE, …).
mask_usage() { sed -E 's/\|git-flow\|/|/'; }
cmp_case() { # name  expected(masked)  actual(masked)  ec_e  ec_a
  if [ "$2" = "$3" ] && [ "$4" = "$5" ]; then
    echo "ok   $1"; PASS=$((PASS+1))
  else
    echo "FAIL $1 (exit $4 vs $5)"; diff <(printf '%s\n' "$2") <(printf '%s\n' "$3") | head -25
    FAIL=$((FAIL+1))
  fi
}

# run <impl> <projecthome> <args...> → captures stdout(masked), sets EC
run() {
  local impl="$1" ph="$2"; shift 2
  local out ec
  out="$(BABYSIT_PROJECT_HOME="$ph" BABYSIT_SKIP_COMMENT=1 $impl "$@" 2>/dev/null)"; ec=$?
  printf '%s' "$out" | mask
  return $ec
}

# ── repo with no ticket branch, no origin ────────────────────────────
REPO="$WORK/repo"; export HOME="$WORK/home"; mkdir -p "$HOME"
git init -q "$REPO"; ( cd "$REPO"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init )
cd "$REPO"

for sub in "base-branch" "probe" "explain" "explain --details"; do
  # shellcheck disable=SC2086
  e="$(run "bash $ORACLE" "$WORK/ph-b" $sub)"; ec_e=$?
  # shellcheck disable=SC2086
  g="$(run "$GO" "$WORK/ph-g" $sub)"; ec_g=$?
  cmp_case "no-ticket: $sub" "$e" "$g" "$ec_e" "$ec_g"
done

# lint-workflow on a real workflow file
WF="$ROOT/.claude/skills/autopilot/workflows/builder.md"
if [ -f "$WF" ]; then
  e="$(BABYSIT_PROJECT_HOME="$WORK/ph-b" bash "$ORACLE" lint-workflow "$WF" 2>&1)"; ec_e=$?
  g="$(BABYSIT_PROJECT_HOME="$WORK/ph-g" "$GO" lint-workflow "$WF" 2>&1)"; ec_g=$?
  cmp_case "lint-workflow builder.md" "$e" "$g" "$ec_e" "$ec_g"
fi

# ── checkpoint / read / refresh / recover / timeline (masked) ─────────
cp_args=(checkpoint --ticket bs-x1 --workflow builder --step implement --status in_progress --note "a note")
BABYSIT_PROJECT_HOME="$WORK/cp-b" bash "$ORACLE" "${cp_args[@]}" >/dev/null 2>&1
BABYSIT_PROJECT_HOME="$WORK/cp-g" "$GO"        "${cp_args[@]}" >/dev/null 2>&1

e="$(mask < "$WORK/cp-b/tickets/bs-x1/checkpoint.json")"
g="$(mask < "$WORK/cp-g/tickets/bs-x1/checkpoint.json")"
cmp_case "checkpoint.json content" "$e" "$g" 0 0

e="$(mask < "$WORK/cp-b/tickets/bs-x1/history.jsonl")"
g="$(mask < "$WORK/cp-g/tickets/bs-x1/history.jsonl")"
cmp_case "history.jsonl content" "$e" "$g" 0 0

e="$(mask < "$WORK/cp-b/timeline.jsonl")"
g="$(mask < "$WORK/cp-g/timeline.jsonl")"
cmp_case "timeline.jsonl content" "$e" "$g" 0 0

# read
e="$(BABYSIT_PROJECT_HOME="$WORK/cp-b" bash "$ORACLE" read bs-x1 2>/dev/null | mask)"; ec_e=$?
g="$(BABYSIT_PROJECT_HOME="$WORK/cp-g" "$GO"        read bs-x1 2>/dev/null | mask)"; ec_g=$?
cmp_case "read bs-x1" "$e" "$g" "$ec_e" "$ec_g"

# refresh, then compare (preserves step/status/iteration, restamps ts+head_sha)
BABYSIT_PROJECT_HOME="$WORK/cp-b" bash "$ORACLE" checkpoint --refresh --ticket bs-x1 >/dev/null 2>&1
BABYSIT_PROJECT_HOME="$WORK/cp-g" "$GO"        checkpoint --refresh --ticket bs-x1 >/dev/null 2>&1
e="$(mask < "$WORK/cp-b/tickets/bs-x1/checkpoint.json")"
g="$(mask < "$WORK/cp-g/tickets/bs-x1/checkpoint.json")"
cmp_case "checkpoint.json after --refresh" "$e" "$g" 0 0

# recover (embeds the checkpoint; mask ts)
e="$(BABYSIT_PROJECT_HOME="$WORK/cp-b" bash "$ORACLE" recover 2>/dev/null | mask)"
g="$(BABYSIT_PROJECT_HOME="$WORK/cp-g" "$GO"        recover 2>/dev/null | mask)"
cmp_case "recover (no branch ticket)" "$e" "$g" 0 0

# ── error paths: exit codes + stderr ─────────────────────────────────
for args in "checkpoint --ticket t" "checkpoint --ticket t --workflow w --step s --status bogus" "read" "clear" "bogus-sub" ""; do
  # shellcheck disable=SC2086
  e="$(BABYSIT_PROJECT_HOME="$WORK/ph-b" bash "$ORACLE" $args 2>&1)"; ec_e=$?
  # shellcheck disable=SC2086
  g="$(BABYSIT_PROJECT_HOME="$WORK/ph-g" "$GO"        $args 2>&1 | mask_usage)"; ec_g=$?   # pipefail (set -o above) → the Go exit code survives the filter
  cmp_case "err: '${args:-<none>}'" "$e" "$g" "$ec_e" "$ec_g"
done

# ── git-flow base_branch resolution ──────────────────────────────────
mkdir -p "$REPO/.babysit"; printf 'base_branch: develop\n' > "$REPO/.babysit/git-flow.yaml"
e="$(BABYSIT_PROJECT_HOME="$WORK/ph-b" bash "$ORACLE" base-branch 2>/dev/null)"; ec_e=$?
g="$(BABYSIT_PROJECT_HOME="$WORK/ph-g" "$GO"        base-branch 2>/dev/null)"; ec_g=$?
cmp_case "base-branch (git-flow develop)" "$e" "$g" "$ec_e" "$ec_g"
rm -f "$REPO/.babysit/git-flow.yaml"

echo
if [ "$FAIL" -eq 0 ]; then echo "ALL PASS ($PASS)"; else echo "$FAIL FAILED / $((PASS+FAIL))"; exit 1; fi
