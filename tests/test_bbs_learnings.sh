#!/usr/bin/env bash
# tests/test_bbs_learnings.sh — differential guard for the learnings Go ports.
#
# `bbs learnings-log` / `bbs learnings-search` replaced the two bash scripts,
# and skills depend on their exact stdout/stderr/exit contract. Every case
# runs the frozen pre-port bash (tests/fixtures/bbs-learnings-*.reference) and
# the Go binary side by side under an identical pinned environment and diffs
# all three channels — any drift from the originals is a failure, not a
# judgement call. For learnings-log, the written decisions.jsonl is diffed too
# (ts normalized — the only nondeterministic byte).
#
# Every case pins HOME + BABYSIT_STATE_DIR (or BABYSIT_ANALYTICS_DIR) to
# throwaway dirs so the suite can never read or write real ~/.babysit state,
# and search cases run inside purpose-built git repos so slug scoping is
# actually exercised, never vacuous.
#
# DIFF_STDERR=0 cases: the bash dies via `set -u` ($2 unbound) or a failed
# `>>` redirect, whose stderr names the script's own $0 path and line number.
# Exit code and stdout are the contract there; the $0-dependent spew is not
# (same call as the bbs-env port, commit 5f7e9df).

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
ORACLE_LOG="$REPO/tests/fixtures/bbs-learnings-log.reference"
ORACLE_SEARCH="$REPO/tests/fixtures/bbs-learnings-search.reference"
[ -f "$ORACLE_LOG" ] && [ -f "$ORACLE_SEARCH" ] || { echo "FAIL: missing oracles in tests/fixtures/" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }
ln -s bbs "$T/bbs-learnings-log"    # multicall: compat-symlink entry, like bin/
ln -s bbs "$T/bbs-learnings-search"

mkdir -p "$T/home"   # pinned $HOME — empty, so the fallback ladder never sees real state

# Repos exercising slug scoping: origin remote → slug is the repo basename.
mkdir -p "$T/proj" && (cd "$T/proj" && git init -q && git remote add origin git@github.com:acme/proj-x.git)
mkdir -p "$T/projdot" && (cd "$T/projdot" && git init -q && git remote add origin https://example.com/acme/proj.x.git)
mkdir -p "$T/noremote" && (cd "$T/noremote" && git init -q)
mkdir -p "$T/norepo"

seed_store() { # seed_store <dir> — writes <dir>/analytics/decisions.jsonl
  mkdir -p "$1/analytics"
  cat > "$1/analytics/decisions.jsonl" <<'EOF'
{"v":1,"ts":"2026-01-01T00:00:00Z","skill":"qa","type":"taste","choice":"alpha choice","rationale":"first","ticket":"bs-1","workflow":"builder","state":"proj-x"}
{"v":1,"ts":"2026-01-02T00:00:00Z","skill":"implement","type":"mechanical","choice":"beta pick","rationale":"second","ticket":"bs-2","workflow":"builder","state":"otherproj"}
{"v":1,"ts":"2026-01-03T00:00:00Z","skill":"qa","type":"taste","choice":"gamma a+b literal","rationale":"third","ticket":"bs-3","workflow":"sweeper","state":"proj-x extra"}
{"v":1,"ts":"2026-01-04T00:00:00Z","skill":"plan-draft","type":"user_challenge","choice":"delta 42","rationale":"projax lookalike","ticket":"bs-4","workflow":"builder","state":""}
{"v":1,"ts":"2026-01-05T00:00:00Z","skill":"sweep","type":"taste","choice":"epsilon","rationale":"fifth assess","ticket":"bs-5","workflow":"maintainer","state":"proj-x"}
EOF
}

# ── search: diff stdout+stderr+exit, oracle vs Go, identical env ───────
CASE_ENV=(); CASE_CWD="$T/norepo"; CASE_HOME=""; DIFF_STDERR=1
diff_search() {
  local desc="$1"; shift
  local orc grc msg=""
  ( cd "$CASE_CWD" && env -i PATH="$PATH" HOME="${CASE_HOME:-$T/home}" ${CASE_ENV[@]+"${CASE_ENV[@]}"} \
      "$ORACLE_SEARCH" "$@" >"$T/o.out" 2>"$T/o.err" ); orc=$?
  ( cd "$CASE_CWD" && env -i PATH="$PATH" HOME="${CASE_HOME:-$T/home}" ${CASE_ENV[@]+"${CASE_ENV[@]}"} \
      "$T/bbs-learnings-search" "$@" >"$T/g.out" 2>"$T/g.err" ); grc=$?
  [ "$orc" = "$grc" ] || msg="exit bash=$orc go=$grc;"
  cmp -s "$T/o.out" "$T/g.out" || msg="$msg stdout[$(diff "$T/o.out" "$T/g.out" | tr '\n' '|')]"
  if [ "$DIFF_STDERR" = 1 ]; then
    cmp -s "$T/o.err" "$T/g.err" || msg="$msg stderr[$(diff "$T/o.err" "$T/g.err" | tr '\n' '|')]"
  fi
  if [ -z "$msg" ]; then ok "$desc"; else fail "$desc" "$msg"; fi
  DIFF_STDERR=1
}

STATE="$T/state"; seed_store "$STATE"
CASE_ENV=(BABYSIT_STATE_DIR="$STATE")

echo "learnings-search / filters:"
CASE_CWD="$T/proj";     diff_search "slug-scoped-default"                 # only proj-x rows, limit 10
CASE_CWD="$T/proj";     diff_search "cross-project-all-rows" --cross-project
CASE_CWD="$T/proj";     diff_search "both-filters-query-hits-slug-misses" beta   # beta row lacks proj-x → vanishes
CASE_CWD="$T/norepo";   diff_search "no-repo-no-slug-filter" beta
CASE_CWD="$T/noremote"; diff_search "repo-without-origin-no-slug-filter" beta
CASE_CWD="$T/proj";     diff_search "query-case-insensitive" ALPHA
CASE_CWD="$T/proj";     diff_search "last-positional-wins" beta alpha
CASE_CWD="$T/projdot";  diff_search "slug-dot-is-regex-wildcard" --limit 20      # proj.x also matches projax lookalike

echo "learnings-search / BRE regex queries:"
CASE_CWD="$T/norepo"
diff_search "regex-dot" "b.ta"
diff_search "regex-star" "gam*a"
diff_search "regex-bracket-class" "delta [0-9][0-9]"
diff_search "regex-posix-class" "delta [[:digit:]]*"
diff_search "regex-anchor-start" "^{\"v\":1"
diff_search "regex-anchor-end" "proj-x\"}$"
diff_search "bre-plus-is-literal" "a+b"
diff_search "bre-question-is-literal" "alpha?"
diff_search "bre-escaped-group" "\\(taste\\)"
diff_search "bre-interval" "s\\{2\\}"
diff_search "middle-caret-is-literal" "a^b"
diff_search "middle-dollar-is-literal" "a\$b"
diff_search "invalid-regex-unmatched-bracket" "["
diff_search "invalid-regex-trailing-backslash" "\\"
diff_search "bre-alternation" 'first\|second'
diff_search "bre-alternation-anchored-arm" 'zzz\|^{"v":1'
diff_search "bre-group-caret-anchor" '\(^{"v":1\)'
diff_search "bre-group-dollar-anchor" '\(proj-x"}$\)'
diff_search "bre-dollar-literal-before-alt" 'x"}$\|qqq'
diff_search "bre-escaped-plus-quantifier" 'as\+ess'
diff_search "bre-escaped-question-quantifier" 'firs\?t'
diff_search "bre-leading-plus-is-literal" '\+b'
diff_search "bre-word-class" '\wifth'
diff_search "bre-space-class" 'fifth\sassess'
diff_search "bre-word-boundary" '\bfirst\b'

echo "learnings-search / limit:"
CASE_CWD="$T/proj"
diff_search "limit-2" --limit 2
diff_search "limit-0" --limit 0
diff_search "limit-negative" --limit -1
diff_search "limit-from-start" --limit +2
diff_search "limit-leading-zeros" --limit 010
diff_search "limit-nonnumeric-tail-error" --limit abc
diff_search "limit-nonnumeric-empty-result-no-tail" --limit abc nosuchquery
diff_search "limit-overflow-alloc-error" --limit 9999999999999999999
diff_search "limit-overflow-from-start-silent" --limit +9999999999999999999
diff_search "limit-uint64-max-alloc-error" --limit 18446744073709551615
diff_search "limit-from-start-uint64-max-silent" --limit +18446744073709551615
diff_search "limit-past-uint64-illegal-offset" --limit 18446744073709551616
diff_search "limit-past-uint64-from-start-illegal-offset" --limit +99999999999999999999
diff_search "limit-past-uint64-negative-illegal-offset" --limit -99999999999999999999
DIFF_STDERR=0 diff_search "limit-missing-value-set-u-death" --limit

echo "learnings-search / flags + store edge cases:"
diff_search "unknown-flag" -x
diff_search "unknown-flag-equals-form" --limit=5
CASE_ENV=(BABYSIT_STATE_DIR="$T/nostate");        diff_search "missing-store-silent-exit-0" foo
EMPTY="$T/empty"; mkdir -p "$EMPTY/analytics"; : > "$EMPTY/analytics/decisions.jsonl"
CASE_ENV=(BABYSIT_STATE_DIR="$EMPTY");            diff_search "empty-store-silent-exit-0"
ADIR="$T/adir"; mkdir -p "$ADIR"; cp "$STATE/analytics/decisions.jsonl" "$ADIR/"
CASE_ENV=(BABYSIT_STATE_DIR="$T/nostate" BABYSIT_ANALYTICS_DIR="$ADIR"); diff_search "analytics-dir-override" alpha --cross-project
HOME2="$T/home2"; seed_store "$HOME2/.babysit"
CASE_ENV=(); CASE_HOME="$HOME2"; CASE_CWD="$T/norepo"
diff_search "home-dot-babysit-fallback" alpha --cross-project
CASE_HOME=""
UNREAD="$T/unread"; seed_store "$UNREAD"; chmod 000 "$UNREAD/analytics/decisions.jsonl"
CASE_ENV=(BABYSIT_STATE_DIR="$UNREAD"); diff_search "unreadable-store-cat-stderr-exit-0"
chmod 644 "$UNREAD/analytics/decisions.jsonl"
CASE_ENV=(BABYSIT_STATE_DIR="$STATE")

# ── log: diff stdout+stderr+exit AND the written store (ts normalized) ─
diff_log() {
  local desc="$1"; shift
  local so="$T/log-o"; local sg="$T/log-g"
  rm -rf "$so" "$sg"; mkdir -p "$so" "$sg"
  local orc grc msg=""
  ( cd "$T/norepo" && env -i PATH="$PATH" HOME="$T/home" BABYSIT_STATE_DIR="$so" \
      "$ORACLE_LOG" "$@" >"$T/o.out" 2>"$T/o.err" ); orc=$?
  ( cd "$T/norepo" && env -i PATH="$PATH" HOME="$T/home" BABYSIT_STATE_DIR="$sg" \
      "$T/bbs-learnings-log" "$@" >"$T/g.out" 2>"$T/g.err" ); grc=$?
  [ "$orc" = "$grc" ] || msg="exit bash=$orc go=$grc;"
  cmp -s "$T/o.out" "$T/g.out" || msg="$msg stdout[$(diff "$T/o.out" "$T/g.out" | tr '\n' '|')]"
  if [ "$DIFF_STDERR" = 1 ]; then
    cmp -s "$T/o.err" "$T/g.err" || msg="$msg stderr[$(diff "$T/o.err" "$T/g.err" | tr '\n' '|')]"
  fi
  norm() { sed 's/"ts":"[^"]*"/"ts":"TS"/' "$1" 2>/dev/null || true; }
  if ! diff <(norm "$so/analytics/decisions.jsonl") <(norm "$sg/analytics/decisions.jsonl") >"$T/f.diff" 2>&1; then
    msg="$msg file[$(tr '\n' '|' <"$T/f.diff")]"
  fi
  if [ -z "$msg" ]; then ok "$desc"; else fail "$desc" "$msg"; fi
  DIFF_STDERR=1
}

echo "learnings-log / usage errors:"
diff_log "no-subcommand"
diff_log "unknown-subcommand" frobnicate
diff_log "missing-required" decision --skill s
diff_log "empty-choice-counts-missing" decision --skill s --type taste --choice ""
diff_log "bad-type" decision --skill s --type bogus --choice c
DIFF_STDERR=0 diff_log "flag-missing-value-set-u-death" decision --type taste --choice c --skill

echo "learnings-log / append:"
diff_log "happy-path-all-fields" decision --skill qa --type taste --choice "pick A" \
  --rationale "why not" --ticket bs-9 --workflow builder --state '{"k":"v"}'
diff_log "sanitizer-strips-quotes-backslash-ctrl" decision \
  --skill "$(printf 's"k\\il\tl')" --type taste --choice "$(printf 'a\nb')"
diff_log "truncates-at-500-bytes" decision --skill s --type taste \
  --choice "$(python3 -c "print('x'*600)")"
diff_log "truncates-mid-rune-at-500-bytes" decision --skill s --type taste \
  --choice "$(python3 -c "print('x' + 'é'*300)")"
diff_log "unknown-flags-skipped" decision --skill s --type taste --choice c --bogus val
diff_log "dup-flag-last-wins" decision --skill s1 --skill s2 --type taste --choice c

# Unwritable analytics dir: mkdir + append both fail; logging still exits 0.
# (The bash's failed-redirect stderr names its own $0 path — not the contract.)
RO="$T/ro"; mkdir -p "$RO"; chmod 555 "$RO"
ro_case() {
  local orc grc
  ( env -i PATH="$PATH" HOME="$T/home" BABYSIT_STATE_DIR="$T/rostate" BABYSIT_ANALYTICS_DIR="$RO/nested" \
      "$ORACLE_LOG" decision --skill s --type taste --choice c >"$T/o.out" 2>/dev/null ); orc=$?
  ( env -i PATH="$PATH" HOME="$T/home" BABYSIT_STATE_DIR="$T/rostate" BABYSIT_ANALYTICS_DIR="$RO/nested" \
      "$T/bbs-learnings-log" decision --skill s --type taste --choice c >"$T/g.out" 2>/dev/null ); grc=$?
  if [ "$orc" = "$grc" ] && cmp -s "$T/o.out" "$T/g.out" && [ ! -e "$RO/nested" ]; then
    ok "unwritable-dir-still-exits-0"
  else
    fail "unwritable-dir-still-exits-0" "exit bash=$orc go=$grc"
  fi
}
ro_case
chmod 755 "$RO"

# ── `bbs <sub>` spelling matches the compat-symlink spelling ──────────
sub_vs_symlink() {
  ( cd "$T/proj" && env -i PATH="$PATH" HOME="$T/home" BABYSIT_STATE_DIR="$STATE" \
      "$BIN" learnings-search alpha >"$T/s.out" 2>"$T/s.err" ); local src=$?
  ( cd "$T/proj" && env -i PATH="$PATH" HOME="$T/home" BABYSIT_STATE_DIR="$STATE" \
      "$T/bbs-learnings-search" alpha >"$T/l.out" 2>"$T/l.err" ); local lrc=$?
  if [ "$src" = "$lrc" ] && cmp -s "$T/s.out" "$T/l.out" && cmp -s "$T/s.err" "$T/l.err"; then
    ok "bbs-subcommand-equals-symlink"
  else
    fail "bbs-subcommand-equals-symlink" "exit sub=$src link=$lrc"
  fi
}
sub_vs_symlink

# ── Summary ────────────────────────────────────────────────────────────
echo ""
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d/%d cases match the pre-port bash exactly\n' "$PASS" "$((PASS + FAIL))"
  exit 0
fi
printf '\033[0;31mFAIL\033[0m  %d/%d failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
exit 1
