#!/usr/bin/env bash
# tests/test_bbs_analytics_cron.sh — differential guard for the bbs-analytics-cron Go port.
#
# `bbs analytics-cron` replaced bin/bbs-analytics-cron. The bin mutates real
# scheduler state (launchd on macOS, crontab on Linux) and shells out to the
# `claude` CLI, so a naive differential harness would install/remove real cron
# jobs and burn API calls on every run. Instead every external mutation is
# SANDBOXED behind stub binaries on a throwaway PATH, and HOME / BABYSIT_HOME
# are pinned into a scratch tree — the real launchd, crontab, and ~/.babysit are
# never read or written.
#
# For each case the frozen pre-port bash (tests/fixtures/bbs-analytics-cron.reference)
# and the Go binary run under an identical stubbed environment; we then diff all
# four channels: stdout, stderr, exit code, and the artifact each case produces
# (the generated plist bytes, the captured crontab content, the launchctl call
# log, or the appended skill-usage.jsonl row). The date stamp in review paths is
# normalized (a run can straddle a midnight boundary) after each side is checked
# to carry today's shape.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-analytics-cron.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

TODAY="$(date +%Y-%m-%d)"
DATE_RE='[0-9]{4}-[0-9]{2}-[0-9]{2}'

# ─── Stub factory ────────────────────────────────────────────────────────────
# Every stub lives in $STUBS (prepended to PATH) and records into files under
# the per-case sandbox so nothing escapes to the real system.
#
#   uname     → echoes $STUB_UNAME (default Darwin), letting one host exercise
#               both the launchd and the crontab branch.
#   launchctl → appends its argv to $LAUNCHLOG, exits $STUB_LAUNCHCTL_RC (0).
#   crontab   → `-l` cats $CRONFILE (rc 1 if absent, mirroring "no crontab yet");
#               `-` slurps stdin into $CRONFILE. The real user crontab is untouched.
#   claude    → writes a fixed report to stdout, exits $STUB_CLAUDE_RC. Absent
#               from PATH entirely when a case needs the not-found path.
make_stubs() {
  local dir="$1"
  mkdir -p "$dir"

  cat > "$dir/uname" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "${STUB_UNAME:-Darwin}"
STUB

  cat > "$dir/launchctl" <<'STUB'
#!/usr/bin/env bash
printf 'launchctl %s\n' "$*" >> "$LAUNCHLOG"
exit "${STUB_LAUNCHCTL_RC:-0}"
STUB

  # `-` writes atomically (temp + mv) so a concurrent `crontab -l | … | crontab -`
  # pipeline still reads the pre-replacement content — matching the real
  # crontab, which snapshots before it replaces. A naive `cat > $CRONFILE` would
  # truncate the file the pipeline's own `-l` is still reading.
  cat > "$dir/crontab" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  -l) [ -f "$CRONFILE" ] && cat "$CRONFILE" || exit 1 ;;
  -)  tmp="$CRONFILE.$$.tmp"; cat > "$tmp"; mv "$tmp" "$CRONFILE" ;;
esac
STUB

  cat > "$dir/claude" <<'STUB'
#!/usr/bin/env bash
printf 'analytics report body\n'
exit "${STUB_CLAUDE_RC:-0}"
STUB

  chmod +x "$dir"/*
}

# mk_env <impl> — a private sandbox: HOME, BABYSIT_HOME, stub dir, entrypoint.
# The bash gets the frozen oracle as a real file; the Go side gets a
# bbs-analytics-cron symlink to the multicall — the same compat path we ship,
# and the shape that makes SELF/argv[0] dispatch match.
mk_env() {
  local impl="$1"
  local r="$T/$CASE/$impl"
  mkdir -p "$r/home" "$r/bhome" "$r/bin" "$r/stubs"
  make_stubs "$r/stubs"
  if [ "$impl" = bash ]; then
    cp "$REFERENCE" "$r/bin/bbs-analytics-cron"
    chmod +x "$r/bin/bbs-analytics-cron"
  else
    cp "$BIN" "$r/bin/bbs"
    ln -s bbs "$r/bin/bbs-analytics-cron"
  fi
  printf '%s' "$r"
}

# run_impl <root> [args...] — invoke the entrypoint with the sandbox env and
# capture stdout/stderr/exit into files under <root>.
run_impl() {
  local r="$1"; shift
  HOME="$r/home" \
  BABYSIT_HOME="$r/bhome" \
  CLAUDE_BIN="${CASE_CLAUDE_BIN:-claude}" \
  PATH="$r/stubs:$PATH" \
  LAUNCHLOG="$r/launch.log" \
  CRONFILE="$r/crontab.txt" \
  STUB_UNAME="${CASE_UNAME:-Darwin}" \
  STUB_CLAUDE_RC="${CASE_CLAUDE_RC:-0}" \
    "$r/bin/bbs-analytics-cron" "$@" > "$r/out" 2> "$r/err"
  echo "$?" > "$r/rc"
}

# norm <root> <file> — strip the per-case sandbox root (bash and go run under
# different dirs) and the date stamp (a run can straddle midnight), so only
# structural differences remain.
norm() { sed -E -e "s#$1#ROOT#g" -e "s/$DATE_RE/DATE/g" "$2" 2>/dev/null; }

# diff_channels <name> <bash-root> <go-root> — stdout, stderr, exit.
diff_channels() {
  local name="$1" b="$2" g="$3" why=""
  [ "$(cat "$b/rc")" = "$(cat "$g/rc")" ] || why="exit ${name}: bash=$(cat "$b/rc") go=$(cat "$g/rc")"
  [ -z "$why" ] && ! diff <(norm "$b" "$b/out") <(norm "$g" "$g/out") >/dev/null && why="stdout differs"
  [ -z "$why" ] && ! diff <(norm "$b" "$b/err") <(norm "$g" "$g/err") >/dev/null && why="stderr differs"
  echo "$why"
}

# diff_file <bash-file> <go-file> — a produced artifact, date-normalized. Both
# absent is a match (e.g. crontab never written).
diff_file() {
  local bf="$1" gf="$2"
  local ba="absent" ga="absent"
  [ -f "$bf" ] && ba="$(norm "$bf")"
  [ -f "$gf" ] && ga="$(norm "$gf")"
  [ "$ba" = "$ga" ]
}

# ─── Case 1: --dry-run (claude present) ──────────────────────────────────────
CASE=dry_run
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR" --dry-run
run_impl "$GR" --dry-run
why="$(diff_channels dry_run "$BR" "$GR")"
[ -z "$why" ] && grep -q "would run:" "$GR/out" || why="${why:-go missing 'would run:' line}"
[ -z "$why" ] && ok "--dry-run stdout/exit match + reviews dir seeded" || fail "--dry-run" "$why"

# ─── Case 2: --dry-run with claude ABSENT (claude check precedes dry-run) ─────
CASE=dry_run_no_claude
BR="$(mk_env bash)"; GR="$(mk_env go)"
CASE_CLAUDE_BIN="definitely-not-a-real-binary-xyz" run_impl "$BR" --dry-run
CASE_CLAUDE_BIN="definitely-not-a-real-binary-xyz" run_impl "$GR" --dry-run
why="$(diff_channels dry_run_no_claude "$BR" "$GR")"
[ -z "$why" ] && [ "$(cat "$GR/rc")" = 1 ] && grep -q "claude CLI not found" "$GR/err" || why="${why:-expected exit 1 + not-found on stderr}"
[ -z "$why" ] && ok "--dry-run without claude → exit 1, not-found (order preserved)" || fail "--dry-run no-claude" "$why"

# ─── Case 3: --install on Darwin (plist + launchctl, SELF parity) ────────────
CASE=install_darwin
BR="$(mk_env bash)"; GR="$(mk_env go)"
CASE_UNAME=Darwin run_impl "$BR" --install
CASE_UNAME=Darwin run_impl "$GR" --install
why="$(diff_channels install_darwin "$BR" "$GR")"
BPLIST="$BR/home/Library/LaunchAgents/dev.babysit.analytics.plist"
GPLIST="$GR/home/Library/LaunchAgents/dev.babysit.analytics.plist"
# The plist embeds SELF and BABYSIT_HOME, which are per-root absolute paths;
# strip both roots to their sandbox-relative tails before comparing.
plist_norm() { sed -E -e "s#$1#ROOT#g" "$2"; }
[ -z "$why" ] && ! diff <(plist_norm "$BR" "$BPLIST") <(plist_norm "$GR" "$GPLIST") >/dev/null && why="plist content differs"
[ -z "$why" ] && ! diff <(plist_norm "$BR" "$BR/launch.log") <(plist_norm "$GR" "$GR/launch.log") >/dev/null && why="launchctl calls differ"
[ -z "$why" ] && grep -q "installed launchd agent" "$GR/out" || why="${why:-go missing install confirmation}"
[ -z "$why" ] && ok "--install Darwin: plist bytes + launchctl load + stdout match" || fail "--install darwin" "$why"

# ─── Case 4: --install on Linux (crontab line) ───────────────────────────────
CASE=install_linux
BR="$(mk_env bash)"; GR="$(mk_env go)"
CASE_UNAME=Linux run_impl "$BR" --install
CASE_UNAME=Linux run_impl "$GR" --install
why="$(diff_channels install_linux "$BR" "$GR")"
cron_norm() { sed -E "s#$1#ROOT#g" "$2" 2>/dev/null; }
[ -z "$why" ] && ! diff <(cron_norm "$BR" "$BR/crontab.txt") <(cron_norm "$GR" "$GR/crontab.txt") >/dev/null && why="crontab content differs"
[ -z "$why" ] && grep -q "installed cron entry" "$GR/out" || why="${why:-go missing cron confirmation}"
[ -z "$why" ] && ok "--install Linux: crontab line + stdout match" || fail "--install linux" "$why"

# ─── Case 5: --install Linux when entry already present (idempotent) ──────────
CASE=install_linux_dup
BR="$(mk_env bash)"; GR="$(mk_env go)"
# Pre-seed each crontab with a line mentioning that root's own SELF path.
printf '0 9 * * 1 %s\n' "$BR/bin/bbs-analytics-cron" > "$BR/crontab.txt"
printf '0 9 * * 1 %s\n' "$GR/bin/bbs-analytics-cron" > "$GR/crontab.txt"
CASE_UNAME=Linux run_impl "$BR" --install
CASE_UNAME=Linux run_impl "$GR" --install
why="$(diff_channels install_linux_dup "$BR" "$GR")"
[ -z "$why" ] && grep -q "already present" "$GR/out" || why="${why:-go missing 'already present'}"
[ -z "$why" ] && ok "--install Linux dup → 'already present', crontab untouched" || fail "--install linux dup" "$why"

# ─── Case 6: --uninstall Darwin (launchctl unload + rm plist) ────────────────
CASE=uninstall_darwin
BR="$(mk_env bash)"; GR="$(mk_env go)"
mkdir -p "$BR/home/Library/LaunchAgents" "$GR/home/Library/LaunchAgents"
touch "$BR/home/Library/LaunchAgents/dev.babysit.analytics.plist"
touch "$GR/home/Library/LaunchAgents/dev.babysit.analytics.plist"
CASE_UNAME=Darwin run_impl "$BR" --uninstall
CASE_UNAME=Darwin run_impl "$GR" --uninstall
why="$(diff_channels uninstall_darwin "$BR" "$GR")"
[ -z "$why" ] && [ -e "$GR/home/Library/LaunchAgents/dev.babysit.analytics.plist" ] && why="go left plist behind"
[ -z "$why" ] && ! diff <(sed -E "s#$BR#ROOT#g" "$BR/launch.log") <(sed -E "s#$GR#ROOT#g" "$GR/launch.log") >/dev/null && why="launchctl calls differ"
[ -z "$why" ] && grep -q "removed launchd agent" "$GR/out" || why="${why:-go missing removal confirmation}"
[ -z "$why" ] && ok "--uninstall Darwin: unload + plist removed + stdout match" || fail "--uninstall darwin" "$why"

# ─── Case 7: --uninstall Linux (crontab filtered) ────────────────────────────
CASE=uninstall_linux
BR="$(mk_env bash)"; GR="$(mk_env go)"
printf 'MAILTO=me\n0 9 * * 1 %s\n0 0 * * * other-job\n' "$BR/bin/bbs-analytics-cron" > "$BR/crontab.txt"
printf 'MAILTO=me\n0 9 * * 1 %s\n0 0 * * * other-job\n' "$GR/bin/bbs-analytics-cron" > "$GR/crontab.txt"
CASE_UNAME=Linux run_impl "$BR" --uninstall
CASE_UNAME=Linux run_impl "$GR" --uninstall
why="$(diff_channels uninstall_linux "$BR" "$GR")"
cron_norm() { sed -E "s#$1#ROOT#g" "$2" 2>/dev/null; }
[ -z "$why" ] && ! diff <(cron_norm "$BR" "$BR/crontab.txt") <(cron_norm "$GR" "$GR/crontab.txt") >/dev/null && why="filtered crontab differs"
[ -z "$why" ] && grep -q "other-job" "$GR/crontab.txt" || why="${why:-go dropped the unrelated cron line}"
[ -z "$why" ] && ok "--uninstall Linux: only our line filtered, others kept" || fail "--uninstall linux" "$why"

# ─── Case 8: default run, claude succeeds (review file + jsonl row) ───────────
CASE=run_success
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR"
run_impl "$GR"
why="$(diff_channels run_success "$BR" "$GR")"
BLOG="$BR/bhome/analytics/skill-usage.jsonl"
GLOG="$GR/bhome/analytics/skill-usage.jsonl"
# jsonl row: normalize ts (wall-clock) and the per-root report path.
row_norm() { sed -E -e 's/"ts":"[^"]*"/"ts":"TS"/' -e "s#$1#ROOT#g" -e "s/$DATE_RE/DATE/g" "$2" 2>/dev/null; }
[ -z "$why" ] && ! diff <(row_norm "$BR" "$BLOG") <(row_norm "$GR" "$GLOG") >/dev/null && why="jsonl row differs"
[ -z "$why" ] && grep -q '"outcome":"success"' "$GLOG" || why="${why:-go jsonl missing success outcome}"
[ -z "$why" ] && [ -f "$GR/bhome/analytics/reviews/$TODAY.md" ] || why="${why:-go review file missing}"
[ -z "$why" ] && [ ! -f "$GR/bhome/analytics/reviews/$TODAY.md.err" ] || why="${why:-go left .err scratch behind}"
[ -z "$why" ] && ok "default run success: review file + jsonl row + stdout match" || fail "run success" "$why"

# ─── Case 9: default run, claude fails (outcome=error, exit 1) ───────────────
CASE=run_error
BR="$(mk_env bash)"; GR="$(mk_env go)"
CASE_CLAUDE_RC=3 run_impl "$BR"
CASE_CLAUDE_RC=3 run_impl "$GR"
why="$(diff_channels run_error "$BR" "$GR")"
GLOG="$GR/bhome/analytics/skill-usage.jsonl"
[ -z "$why" ] && [ "$(cat "$GR/rc")" = 1 ] || why="${why:-expected exit 1 on claude failure}"
[ -z "$why" ] && grep -q '"outcome":"error"' "$GLOG" || why="${why:-go jsonl missing error outcome}"
[ -z "$why" ] && grep -q "analytics review (error)" "$GR/out" || why="${why:-go stdout missing error result}"
[ -z "$why" ] && ok "default run error: outcome=error, exit 1, stdout match" || fail "run error" "$why"

# ─── Summary ─────────────────────────────────────────────────────────────────
echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d/%d cases match bin/bbs-analytics-cron exactly (scheduler fully sandboxed)\n' "$PASS" "$((PASS + FAIL))"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m  %d/%d cases failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
