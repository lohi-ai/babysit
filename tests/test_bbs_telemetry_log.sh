#!/usr/bin/env bash
# tests/test_bbs_telemetry_log.sh — differential guard for the bbs-telemetry-log Go port.
#
# `bbs telemetry-log` replaced the bin/bbs-telemetry-log bash script, and the
# dashboard plus analytics-review consume its exact JSONL contract. Rather than
# assert hand-written goldens, every case runs the frozen pre-port bash
# (tests/fixtures/bbs-telemetry-log.reference) and the Go binary side by side
# under an identical environment, then diffs all four channels: stdout, stderr,
# exit code, and the resulting skill-usage.jsonl.
#
# Isolation: HOME, BABYSIT_STATE_DIR and BABYSIT_DIR are pinned into a
# throwaway tree for EVERY case — the real ~/.babysit is never read or written.
# Each implementation gets its own root so neither can observe the other's log.
#
# The `ts` field is wall-clock, so the two runs can straddle a second boundary.
# It is normalized — but each side is first asserted to match the oracle's
# shape independently, so a genuinely malformed timestamp still fails rather
# than being blanked away on both sides.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-telemetry-log.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

# Guard the guard: every fixture root carries this sentinel as its VERSION, so
# any row it produces is unmistakably ours. At the end we assert the real log
# contains none.
#
# Size or row-count would be the wrong invariant: babysit's own Stop hooks
# append to the real log while this suite runs, so those checks false-positive
# on unrelated concurrent writes.
SENTINEL_VERSION="0.0.0-test-bs5koscdg9"
REAL_LOG="$HOME/.babysit/analytics/skill-usage.jsonl"

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

TS_RE='^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$'

# mk_root <impl> — a private tree: bin/ entrypoint, VERSION, state dir, HOME.
# The bash gets the frozen oracle; the Go side gets a bbs-telemetry-log symlink
# to the multicall binary, exercising the same compat path we ship.
mk_root() {
  local impl="$1"
  local r="$T/$CASE/$impl"
  mkdir -p "$r/root/bin" "$r/state" "$r/home"
  printf '%s\n' "$SENTINEL_VERSION" > "$r/root/VERSION"
  if [ "$impl" = bash ]; then
    cp "$REFERENCE" "$r/root/bin/bbs-telemetry-log"
    chmod +x "$r/root/bin/bbs-telemetry-log"
  else
    cp "$BIN" "$r/root/bin/bbs"
    ln -s bbs "$r/root/bin/bbs-telemetry-log"
  fi
  echo "$r"
}

# normalize_pair <bash-jsonl> <go-jsonl> <window-start> <window-end>
#   -> sets NB / NG to the comparable contents, or returns 1 with TS_ERR set.
#
# Only timestamps that actually DIFFER between the two runs are placeheld, and
# only after each is proven well-formed AND inside the window the case ran in
# (ISO-8601 UTC sorts lexicographically, so string compare is exact). A
# timestamp that is wrong in the same way on both sides stays untouched and is
# diffed normally — parity is the contract, so identical is correct by
# definition.
#
# This is why the field is not simply blanked on both sides: that would mask a
# port whose clock, format, or field plumbing is broken. Rows whose ts is data
# derived rather than wall-clock (the .pending close-out shape reads ts out of
# the marker file) are deterministic, so they never differ and are compared
# verbatim — including the empty ts a garbage marker produces.
join_rows() { # $1 = array name -> echoes rows, empty for an empty array
  local -n _a="$1"
  [ "${#_a[@]}" -eq 0 ] && return 0
  printf '%s\n' "${_a[@]}"
}

ts_of() { sed -n 's/.*"ts":"\([^"]*\)".*/\1/p' <<<"$1"; }

normalize_pair() {
  local bf="$1" gf="$2" start="$3" end="$4"
  local -a BL=() GL=()
  [ -f "$bf" ] && IFS=$'\n' read -r -d '' -a BL < "$bf"
  [ -f "$gf" ] && IFS=$'\n' read -r -d '' -a GL < "$gf"

  # Row-count mismatch is itself a failure; hand both back and let diff report.
  if [ "${#BL[@]}" -ne "${#GL[@]}" ]; then
    NB="$(join_rows BL)"; NG="$(join_rows GL)"
    return 0
  fi

  local i tb tg
  for ((i = 0; i < ${#BL[@]}; i++)); do
    tb="$(ts_of "${BL[$i]}")"
    tg="$(ts_of "${GL[$i]}")"
    [ "$tb" = "$tg" ] && continue

    [[ "$tb" =~ $TS_RE ]] || { TS_ERR="bash ts malformed: '$tb'"; return 1; }
    [[ "$tg" =~ $TS_RE ]] || { TS_ERR="go ts malformed: '$tg'"; return 1; }
    if [[ "$tb" < "$start" || "$tb" > "$end" ]]; then
      TS_ERR="bash ts $tb outside run window [$start,$end]"; return 1
    fi
    if [[ "$tg" < "$start" || "$tg" > "$end" ]]; then
      TS_ERR="go ts $tg outside run window [$start,$end]"; return 1
    fi
    BL[$i]="${BL[$i]/\"ts\":\"$tb\"/\"ts\":\"<TS>\"}"
    GL[$i]="${GL[$i]/\"ts\":\"$tg\"/\"ts\":\"<TS>\"}"
  done
  NB="$(join_rows BL)"; NG="$(join_rows GL)"
  return 0
}
export LC_ALL=C # make the lexicographic ts window compare locale-independent

now_ts() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# diff_case <name> — run both impls under identical env and compare channels.
# Callers set CASE, and may define prep_root() to seed per-impl state and
# with_env() to add env vars. Args after the name go to the binary.
diff_case() {
  local name="$1"; shift
  local rb rg
  rb="$(mk_root bash)"; rg="$(mk_root go)"

  if declare -F prep_root >/dev/null; then prep_root "$rb"; prep_root "$rg"; fi

  local win_start win_end
  win_start="$(now_ts)"

  local impl r out err code
  for impl in bash go; do
    if [ "$impl" = bash ]; then r="$rb"; else r="$rg"; fi
    out="$T/$CASE/$impl.out"; err="$T/$CASE/$impl.err"
    (
      # RUN_SUBDIR is relative to the per-impl root, so a case can run each
      # impl inside its own private git repo ("$r/repo") with one hook.
      cd "$r/${RUN_SUBDIR:-.}" || exit 99
      export HOME="$r/home" BABYSIT_STATE_DIR="$r/state"
      # BABYSIT_DIR pinned by default so a case's state is contained. Cases that
      # exercise the argv0-derivation branch of ResolveDirs set NO_BABYSIT_DIR=1
      # instead — that branch is what runs in a real install, so leaving it
      # permanently short-circuited by the env override would leave the code
      # path behind BUG 1 untested. HOME/BABYSIT_STATE_DIR stay pinned either
      # way, so nothing escapes the temp tree.
      [ -n "${NO_BABYSIT_DIR:-}" ] || export BABYSIT_DIR="$r/root"
      if declare -F with_env >/dev/null; then with_env; fi
      "$r/${RUN_BIN:-root/bin/bbs-telemetry-log}" "$@"
    ) >"$out" 2>"$err"
    code=$?
    echo "$code" > "$T/$CASE/$impl.code"
  done
  win_end="$(now_ts)"

  # exit code
  local cb cg
  cb="$(cat "$T/$CASE/bash.code")"; cg="$(cat "$T/$CASE/go.code")"
  [ "$cb" = "$cg" ] || { fail "$name" "exit: bash=$cb go=$cg"; return; }

  # stdout (both are empty in every real path; diffed rather than assumed)
  if ! diff -q "$T/$CASE/bash.out" "$T/$CASE/go.out" >/dev/null; then
    fail "$name" "stdout differs: $(head -c 120 "$T/$CASE/bash.out") vs $(head -c 120 "$T/$CASE/go.out")"; return
  fi

  # stderr — compared as empty/non-empty only. The bash emits line-numbered
  # interpreter diagnostics ("…: line 43: $2: unbound variable") that a
  # compiled binary cannot reproduce byte-for-byte; documented divergence.
  local sb=0 sg=0
  [ -s "$T/$CASE/bash.err" ] && sb=1
  [ -s "$T/$CASE/go.err" ] && sg=1
  [ "$sb" = "$sg" ] || { fail "$name" "stderr non-empty: bash=$sb go=$sg (bash: $(head -c 100 "$T/$CASE/bash.err"))"; return; }

  # the JSONL side effect
  normalize_pair "$rb/state/analytics/skill-usage.jsonl" \
                 "$rg/state/analytics/skill-usage.jsonl" "$win_start" "$win_end" \
    || { fail "$name" "$TS_ERR"; return; }
  if [ "$NB" != "$NG" ]; then
    fail "$name" "jsonl differs:
        bash: $NB
        go:   $NG"; return
  fi

  ok "$name"
}

# reset_hooks — clear per-case customization.
reset_hooks() {
  unset -f prep_root with_env 2>/dev/null
  unset RUN_SUBDIR NO_BABYSIT_DIR RUN_BIN 2>/dev/null
}

echo "bbs-telemetry-log — differential vs frozen bash oracle"

# ── Core event shape ─────────────────────────────────────────
CASE=basic; reset_hooks
diff_case "basic skill_run" --skill qa --duration 142 --outcome success

CASE=allflags; reset_hooks
diff_case "every flag set" --skill qa --duration 12 --outcome failure \
  --used-browse true --session-id s-1 --error-class timeout \
  --error-message "boom happened" --failed-step step-3 \
  --event-type custom_event --invoker mayor

# used_browse is a strict equality test against "true", not a truthiness test:
# every other value collapses to false. Without this case, a port that emitted
# the flag's raw value would still pass the --used-browse true case above.
CASE=browse_junk; reset_hooks
diff_case "used-browse non-true -> false" --skill qa --used-browse yes

CASE=noargs; reset_hooks
diff_case "no arguments"

CASE=unknown; reset_hooks
diff_case "unknown args silently skipped" --skill qa --bogus x positional

# ── Invoker precedence ───────────────────────────────────────
CASE=inv_default; reset_hooks
diff_case "invoker defaults to developer" --skill qa

# Both vars are set here on purpose: with only AGENT_ROLE set, either ordering
# of the chain yields the same answer, so the case would assert nothing.
CASE=inv_agentrole; reset_hooks
with_env() { export AGENT_ROLE=general GT_ROLE=scanner; }
diff_case "AGENT_ROLE wins over GT_ROLE" --skill qa

CASE=inv_gtrole; reset_hooks
with_env() { export GT_ROLE=scanner INVOKER_ENV=ie; }
diff_case "GT_ROLE used when AGENT_ROLE unset" --skill qa

CASE=inv_chain; reset_hooks
with_env() { export BABYSIT_INVOKER=bi INVOKER_ENV=ie; }
diff_case "INVOKER_ENV precedes BABYSIT_INVOKER" --skill qa

CASE=inv_flag; reset_hooks
with_env() { export AGENT_ROLE=general; }
diff_case "--invoker flag beats env" --skill qa --invoker explicit

# ── BUG 3: duration validation ───────────────────────────────
CASE=dur_ok; reset_hooks
diff_case "duration in range" --skill qa --duration 86400

CASE=dur_over; reset_hooks
diff_case "duration over 86400 -> null" --skill qa --duration 86401

CASE=dur_neg; reset_hooks
diff_case "negative duration -> null" --skill qa --duration -5

CASE=dur_nan; reset_hooks
diff_case "non-numeric duration -> null" --skill qa --duration abc

CASE=dur_empty; reset_hooks
diff_case "empty duration -> null" --skill qa --duration ""

CASE=dur_huge; reset_hooks
# BUG 3: too big for the shell's integer type, so `[ -gt ]` errors and the
# raw value survives into duration_s. Replicated, not fixed.
diff_case "BUG3 oversized duration lands raw" --skill qa --duration 99999999999999999999

CASE=dur_zeros; reset_hooks
# Leading zeros pass through, emitting invalid JSON. Bash behavior.
diff_case "leading-zero duration passes through" --skill qa --duration 007

# ── BUG 2: missing flag value ────────────────────────────────
CASE=noval_skill; reset_hooks
diff_case "BUG2 --skill with no value exits 1" --skill

CASE=noval_dur; reset_hooks
diff_case "BUG2 --duration with no value exits 1" --skill qa --duration

CASE=steal; reset_hooks
diff_case "flag steals next token verbatim" --skill --duration --outcome ok

# ── json_safe ────────────────────────────────────────────────
CASE=js_quote; reset_hooks
diff_case "quotes and backslashes deleted" --skill 'a"b\c' --outcome 'x"y'

CASE=js_ctrl; reset_hooks
diff_case "control chars deleted" --skill "$(printf 'a\tb\x01c')"

CASE=js_long; reset_hooks
diff_case "truncated at 200 bytes" --skill "$(printf 'x%.0s' {1..300})"

CASE=js_utf8; reset_hooks
# 200-BYTE truncation can split a multi-byte rune mid-sequence.
diff_case "utf8 truncation splits runes" --skill "$(printf 'é%.0s' {1..150})"

# ── BUG 1 + tier ─────────────────────────────────────────────
CASE=tier_off_gated; reset_hooks
# BUG 1: telemetry:off is IGNORED because $BABYSIT_DIR/bin/bbs-config does not
# exist here, so the bash's shell-out fails and the tier collapses to local.
# The event is written anyway. This is the production path.
prep_root() { printf 'telemetry: off\n' > "$1/state/config.yaml"; }
diff_case "BUG1 telemetry:off ignored without bbs-config" --skill qa

CASE=tier_off_real; reset_hooks
# With a resolvable bbs-config, `off` does suppress the write.
prep_root() {
  printf 'telemetry: off\n' > "$1/state/config.yaml"
  cp "$BIN" "$1/root/bin/bbs"; ln -sf bbs "$1/root/bin/bbs-config"
}
diff_case "telemetry:off honored with bbs-config present" --skill qa

CASE=tier_local_real; reset_hooks
prep_root() {
  printf 'telemetry: local\n' > "$1/state/config.yaml"
  cp "$BIN" "$1/root/bin/bbs"; ln -sf bbs "$1/root/bin/bbs-config"
}
diff_case "telemetry:local writes" --skill qa

CASE=tier_junk; reset_hooks
prep_root() {
  printf 'telemetry: banana\n' > "$1/state/config.yaml"
  cp "$BIN" "$1/root/bin/bbs"; ln -sf bbs "$1/root/bin/bbs-config"
}
diff_case "unknown tier falls back to local" --skill qa

CASE=tier_broken_link; reset_hooks
# A broken bbs-config symlink fails to exec, so the tier collapses to local.
prep_root() {
  printf 'telemetry: off\n' > "$1/state/config.yaml"
  ln -sf does-not-exist "$1/root/bin/bbs-config"
}
diff_case "broken bbs-config symlink -> local" --skill qa

# ── BABYSIT_DIR derived from $0 (no env override) ────────────
# Every other case pins BABYSIT_DIR, which short-circuits ResolveDirs before it
# ever looks at argv0. That branch is the one a real install runs, and it is
# what makes BUG 1 bite: the shipped entrypoint is a symlink at
# ~/.claude/bbs-telemetry-log pointing into the repo, and the bash takes
# dirname "$0" WITHOUT resolving it — so BABYSIT_DIR lands next to the symlink
# ($HOME), not in the repo. VERSION and bin/bbs-config are then both missing,
# which is why production rows read "babysit_version":"unknown" and why
# `telemetry: off` has never taken effect there.
#
# The link deliberately lives in a different directory from its target: with a
# same-directory link (the mk_root layout) resolving the symlink is a no-op, so
# the case could not tell a faithful port from one that calls EvalSymlinks.
CASE=dir_from_argv0; reset_hooks
NO_BABYSIT_DIR=1
RUN_BIN=link/bbs-telemetry-log
prep_root() {
  mkdir -p "$1/link"
  ln -sf "$1/root/bin/bbs-telemetry-log" "$1/link/bbs-telemetry-log"
}
diff_case "BABYSIT_DIR from \$0, symlink NOT resolved" --skill qa

# ── VERSION ──────────────────────────────────────────────────
CASE=ver_missing; reset_hooks
prep_root() { rm -f "$1/root/VERSION"; }
diff_case "missing VERSION -> unknown" --skill qa

CASE=ver_empty; reset_hooks
# Readable-but-empty yields "", not "unknown" — pipefail only fires on cat.
prep_root() { : > "$1/root/VERSION"; }
diff_case "empty VERSION -> empty string" --skill qa

CASE=ver_ws; reset_hooks
prep_root() { printf '  1.2.3 \n\n' > "$1/root/VERSION"; }
diff_case "VERSION whitespace stripped" --skill qa

# ── sessions ─────────────────────────────────────────────────
CASE=sess_none; reset_hooks
diff_case "no sessions dir -> 1" --skill qa

CASE=sess_three; reset_hooks
prep_root() { mkdir -p "$1/state/sessions"; touch "$1/state/sessions/a" "$1/state/sessions/b" "$1/state/sessions/c"; }
diff_case "three fresh sessions counted" --skill qa

CASE=sess_stale; reset_hooks
# Well clear of the 120-minute boundary, whose minute-rounding differs by find impl.
prep_root() {
  mkdir -p "$1/state/sessions"
  touch "$1/state/sessions/fresh"
  touch -t "$(date -u -v-1d '+%Y%m%d%H%M' 2>/dev/null || date -u -d '1 day ago' '+%Y%m%d%H%M')" "$1/state/sessions/stale"
}
diff_case "stale sessions excluded" --skill qa

CASE=sess_empty_dir; reset_hooks
prep_root() { mkdir -p "$1/state/sessions"; }
diff_case "empty sessions dir -> 1" --skill qa

CASE=sess_nested; reset_hooks
prep_root() { mkdir -p "$1/state/sessions/sub"; touch "$1/state/sessions/sub/a" "$1/state/sessions/top"; }
diff_case "sessions counted recursively" --skill qa

CASE=sess_symlink; reset_hooks
# `find -type f` tests the link itself, not its target, so a symlink to a fresh
# regular file is not counted: the expected count here is 1 (real), not 2. The
# dangling link also proves a broken symlink is skipped rather than erroring.
prep_root() {
  mkdir -p "$1/state/sessions"
  touch "$1/state/sessions/real"
  ln -sf real "$1/state/sessions/link"
  ln -sf nowhere "$1/state/sessions/dangling"
}
diff_case "session symlinks not counted" --skill qa

# ── git: repo slug + branch ──────────────────────────────────
# Each case builds a real repo and runs both impls from inside it.
mk_repo() { # $1 = dir, $2 = origin url ("" for none)
  mkdir -p "$1"; git -C "$1" init -q -b main
  git -C "$1" config user.email t@t.t; git -C "$1" config user.name t
  git -C "$1" commit -q --allow-empty -m one
  [ -n "$2" ] && git -C "$1" remote add origin "$2"
  return 0
}

# The git cases run through the same diff_case as everything else (via
# RUN_SUBDIR=repo). They used to have a private copy of the runner that
# compared only the exit code and the JSONL — so a stray diagnostic on the git
# path, which is exactly where one would appear, went unasserted.

CASE=git_ssh; reset_hooks
prep_root() { mk_repo "$1/repo" "git@github.com:foo/bar.git"; }
RUN_SUBDIR=repo; diff_case "git ssh remote -> foo-bar" --skill qa

CASE=git_https; reset_hooks
prep_root() { mk_repo "$1/repo" "https://github.com/foo/bar.git"; }
RUN_SUBDIR=repo; diff_case "git https remote -> foo-bar" --skill qa

CASE=git_nodotgit; reset_hooks
prep_root() { mk_repo "$1/repo" "https://github.com/foo/bar"; }
RUN_SUBDIR=repo; diff_case "https remote without .git" --skill qa

CASE=git_multiseg; reset_hooks
prep_root() { mk_repo "$1/repo" "git@github.com:a/b/c.git"; }
RUN_SUBDIR=repo; diff_case "multi-segment remote -> b-c" --skill qa

CASE=git_localpath; reset_hooks
prep_root() { mk_repo "$1/repo" "/local/path/repo"; }
RUN_SUBDIR=repo; diff_case "bare path remote -> path-repo" --skill qa

CASE=git_noorigin; reset_hooks
prep_root() { mk_repo "$1/repo" ""; }
RUN_SUBDIR=repo; diff_case "repo with no origin remote" --skill qa

CASE=git_detached; reset_hooks
prep_root() {
  mk_repo "$1/repo" "git@github.com:foo/bar.git"
  git -C "$1/repo" checkout -q --detach HEAD
}
RUN_SUBDIR=repo; diff_case "detached HEAD -> literal HEAD" --skill qa

CASE=git_nopath; reset_hooks
# git uninstalled: PATH keeps the tools the bash needs but drops git, so both
# impls take their `command -v git` / exec.LookPath miss.
#
# A literally empty PATH is NOT differentially testable and is deliberately not
# asserted: the oracle is `#!/usr/bin/env bash`, so with PATH= it dies at exit
# 127 ("env: bash: No such file or directory") before running a single line,
# and even given an interpreter it would lose date/uname/cat too. A compiled
# binary needs none of them. That gap is inherent to shell->binary porting and
# reflects the port being strictly more robust, not a parity break.
prep_root() {
  mk_repo "$1/repo" "git@github.com:foo/bar.git"
  mkdir -p "$1/nogit"
  local tool src
  # bash included: the oracle's `#!/usr/bin/env bash` resolves through PATH.
  for tool in bash date uname cat tr basename find wc sed grep awk head rm mkdir; do
    src="$(command -v "$tool")" && ln -sf "$src" "$1/nogit/$tool"
  done
  return 0
}
with_env() { export PATH="$r/nogit"; }
RUN_SUBDIR=repo; diff_case "git absent from PATH" --skill qa
unset -f with_env

CASE=git_outside; reset_hooks
# Not a repo at all: both git calls fail, slug and branch stay empty.
prep_root() { mkdir -p "$1/repo"; }
RUN_SUBDIR=repo; diff_case "outside any repo" --skill qa

# ── stale .pending markers ───────────────────────────────────
CASE=pend_stale; reset_hooks
prep_root() {
  mkdir -p "$1/state/analytics"
  printf '{"skill":"plan-draft","ts":"2026-01-01T00:00:00Z","session_id":"old-1","babysit_version":"1.0.0"}' \
    > "$1/state/analytics/.pending-old-1"
}
diff_case "stale pending marker finalized" --skill qa --session-id mine

CASE=pend_own; reset_hooks
# Our own marker is skipped by the loop, then removed at the end.
prep_root() {
  mkdir -p "$1/state/analytics"
  printf '{"skill":"qa","ts":"2026-01-01T00:00:00Z","session_id":"mine","babysit_version":"1.0.0"}' \
    > "$1/state/analytics/.pending-mine"
}
diff_case "own pending marker not finalized" --skill qa --session-id mine

CASE=pend_empty; reset_hooks
prep_root() { mkdir -p "$1/state/analytics"; : > "$1/state/analytics/.pending-empty-1"; }
diff_case "empty pending marker dropped silently" --skill qa --session-id mine

CASE=pend_multi; reset_hooks
prep_root() {
  mkdir -p "$1/state/analytics"
  printf '{"skill":"a","ts":"2026-01-01T00:00:00Z","session_id":"s1","babysit_version":"1.0.0"}' > "$1/state/analytics/.pending-s1"
  printf '{"skill":"b","ts":"2026-01-02T00:00:00Z","session_id":"s2","babysit_version":"2.0.0"}' > "$1/state/analytics/.pending-s2"
}
diff_case "multiple stale markers finalized" --skill qa --session-id mine

CASE=pend_symlink; reset_hooks
# `[ -f "$PFILE" ]` FOLLOWS symlinks (unlike the `find -type f` used for the
# session count), so a marker that is a symlink to a regular file IS finalized.
# The dangling link fails the same test and is skipped.
prep_root() {
  mkdir -p "$1/state/analytics"
  printf '{"skill":"c","ts":"2026-01-03T00:00:00Z","session_id":"s3","babysit_version":"3.0.0"}' \
    > "$1/state/analytics/target"
  ln -sf target "$1/state/analytics/.pending-s3"
  ln -sf nowhere "$1/state/analytics/.pending-dangling"
}
diff_case "symlinked pending marker finalized" --skill qa --session-id mine

CASE=pend_garbage; reset_hooks
prep_root() { mkdir -p "$1/state/analytics"; printf 'not json at all' > "$1/state/analytics/.pending-g1"; }
diff_case "garbage pending marker -> empty fields" --skill qa --session-id mine

CASE=pend_off; reset_hooks
# tier=off removes our own marker but finalizes nothing.
prep_root() {
  printf 'telemetry: off\n' > "$1/state/config.yaml"
  cp "$BIN" "$1/root/bin/bbs"; ln -sf bbs "$1/root/bin/bbs-config"
  mkdir -p "$1/state/analytics"
  printf '{"skill":"a","ts":"2026-01-01T00:00:00Z","session_id":"s1","babysit_version":"1.0.0"}' > "$1/state/analytics/.pending-s1"
  : > "$1/state/analytics/.pending-mine"
}
diff_case "tier off: own marker removed, others kept" --skill qa --session-id mine

# marker-removal side effects are not visible in the JSONL, so assert the
# filesystem directly for the tier-off case.
CASE=pend_off_fs; reset_hooks
for impl in bash go; do
  r="$(mk_root "$impl")"
  printf 'telemetry: off\n' > "$r/state/config.yaml"
  cp "$BIN" "$r/root/bin/bbs"; ln -sf bbs "$r/root/bin/bbs-config"
  mkdir -p "$r/state/analytics"
  printf '{"skill":"a"}' > "$r/state/analytics/.pending-s1"
  : > "$r/state/analytics/.pending-mine"
  ( cd "$r" && HOME="$r/home" BABYSIT_STATE_DIR="$r/state" BABYSIT_DIR="$r/root" \
    "$r/root/bin/bbs-telemetry-log" --skill qa --session-id mine ) >/dev/null 2>&1
  ls "$r/state/analytics" | sort > "$T/$CASE/$impl.ls"
done
if diff -q "$T/$CASE/bash.ls" "$T/$CASE/go.ls" >/dev/null; then
  ok "tier off: pending dir state matches"
else
  fail "tier off: pending dir state matches" "$(diff "$T/$CASE/bash.ls" "$T/$CASE/go.ls" | tr '\n' ' ')"
fi

# ── HOME: the line-22 abort ──────────────────────────────────
# Every case above pins HOME, so none of them reach `STATE_DIR=
# "${BABYSIT_STATE_DIR:-$HOME/.babysit}"` with HOME absent. Unsetting it here is
# still leak-safe precisely because it is unset: nothing expands to the real
# home, and both impls aim at the read-only "/.babysit". with_env runs after
# diff_case's pinning exports, so it can take them back off.

CASE=home_unset; reset_hooks
# `set -u` + `:-` deref => exit 1 before a single flag is parsed.
with_env() { unset HOME BABYSIT_STATE_DIR; }
diff_case "unset HOME aborts before flag parsing" --skill qa

# No case pins line 22 *ahead of* BUG 2: mutation-testing showed the ordering is
# unobservable — both abort with exit 1, empty stdout, non-empty stderr and no
# row, so a case asserting it can never go RED. ResolveDirs still runs first in
# the port, to mirror the oracle's shape, but nothing here depends on that.

CASE=state_empty_home_unset; reset_hooks
# `:-` (not `-`) fires on set-but-empty too, so this still derefs HOME.
with_env() { unset HOME; export BABYSIT_STATE_DIR=""; }
diff_case "empty BABYSIT_STATE_DIR still derefs HOME" --skill qa

CASE=home_empty; reset_hooks
# HOME="" is set, so `set -u` stays quiet: STATE_DIR becomes the absolute
# "/.babysit" and only the mkdir fails. Exit stays 0. This is what pins the
# concatenation in ResolveDirs — filepath.Join would clean "" + "/.babysit"
# into a relative ".babysit" and write the log under $PWD.
with_env() { export HOME=""; unset BABYSIT_STATE_DIR; }
diff_case "empty HOME -> /.babysit, no abort" --skill qa

CASE=mkdir_blocked; reset_hooks
# `mkdir -p "$ANALYTICS_DIR"` carries no `|| true`, so it is the one write in
# the script whose failure reaches stderr. Exit is still 0 (-e is off).
prep_root() { : > "$1/state/analytics"; }
diff_case "blocked analytics mkdir warns, exit 0" --skill qa

CASE=config_no_owner_x; reset_hooks
# 0645: an exec bit is set (other), but not one that applies to the owner
# running the test, so the bash's shell-out fails with EACCES and the tier
# collapses to local — the event is written despite `telemetry: off`. Pins
# isExecutable to Access(2) rather than a Perm()&0o111 bit test.
prep_root() {
  printf 'telemetry: off\n' > "$1/state/config.yaml"
  printf '#!/bin/sh\necho off\n' > "$1/root/bin/bbs-config"
  chmod 0645 "$1/root/bin/bbs-config"
}
diff_case "bbs-config without owner-x -> local" --skill qa

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32m%d passed\033[0m\n' "$PASS"
else
  printf '\033[0;31m%d failed\033[0m, %d passed\n' "$FAIL" "$PASS"
  printf '  - %s\n' "${FAIL_NAMES[@]}"
fi

# Guard the guard: no fixture row may ever reach the developer's real log.
if [ -e "$REAL_LOG" ] && grep -qF "$SENTINEL_VERSION" "$REAL_LOG"; then
  echo "FAIL: fixture rows leaked into the real analytics log ($REAL_LOG)" >&2
  exit 1
fi

[ "$FAIL" -eq 0 ]
