#!/usr/bin/env bash
# tests/test_bbs_update_check.sh — differential guard for the bbs-update-check Go port.
#
# `bbs update-check` replaced the bin/bbs-update-check bash script, and the
# session preamble depends on its exact stdout/stderr/exit contract. Rather than
# assert hand-written goldens, every case runs the frozen pre-port bash
# (tests/fixtures/bbs-update-check.reference) and the Go binary side by side
# under an identical environment, then diffs all three channels *plus* the
# resulting ~/.babysit state (the cache/snooze/marker files the script mutates).
# Any drift from the original is a failure, not a judgement call.
#
# SAFETY: BABYSIT_STATE_DIR and BABYSIT_DIR are pinned to throwaway temp dirs in
# EVERY case, and HOME on top of that. The script does `rm -f` on the cache,
# snooze and marker files, so an unpinned case would delete the developer's real
# ~/.babysit state.
#
# ─── Known divergence (deliberate, not parity) ───────────────────────────────
# The bash reads the "updates disabled" flag by exec'ing
#   "$BABYSIT_DIR/bin/bbs-config" get update_check      (reference:29)
# The Go port calls internal/config.Get("update_check") natively instead — the
# port's mandate is "reuse the internal packages, no shelling out where a lib
# exists". Both resolve the same file (BABYSIT_STATE_DIR/config.yaml, or
# ~/.babysit/config.yaml), so they agree wherever BABYSIT_DIR contains a real
# bin/bbs-config. They diverge only in a broken install where it does not: the
# bash then gets "" (|| true) and proceeds, while Go still reads the config.
# Every case below stages a bin/bbs-config into BABYSIT_DIR so the two agree;
# see cfg_* cases. This is the ONE accepted divergence in the port.
#
# The $0 bug (reference:15 derives BABYSIT_DIR from `dirname "$0"` with no
# readlink -f, unlike bin/bbs-env) is NOT a divergence — it is reproduced
# faithfully and pinned by the argv0_* cases.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-update-check.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }
command -v python3 >/dev/null 2>&1 || { echo "SKIP: python3 not installed (needed for the version server)" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m    %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; shift; while [ $# -gt 0 ]; do printf '        %s\n' "$1"; shift; done; }

T="$(mktemp -d)"
BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

# ─── Local VERSION server (curl supports file://, Go's http.Client does not,
#     so the oracle URL has to be real HTTP for both sides) ──────────────────
DOC="$T/doc"
mkdir -p "$DOC/same" "$DOC/newer" "$DOC/html" "$DOC/empty" "$DOC/junk"
printf '1.2.3\n'                      > "$DOC/same/VERSION"
printf '9.9.9\n'                      > "$DOC/newer/VERSION"
printf '<html>404 not found</html>\n' > "$DOC/html/VERSION"
printf ''                             > "$DOC/empty/VERSION"
printf 'not-a-version\n'              > "$DOC/junk/VERSION"

python3 -c '
import http.server, socketserver, sys, os
os.chdir(sys.argv[1])
h = http.server.SimpleHTTPRequestHandler
h.log_message = lambda *a, **k: None
s = socketserver.TCPServer(("127.0.0.1", 0), h)
print(s.server_address[1], flush=True)
s.serve_forever()
' "$DOC" > "$T/port" 2>/dev/null &
SRV_PID=$!
trap 'kill $SRV_PID 2>/dev/null; rm -rf "$T"' EXIT
for _ in $(seq 1 60); do [ -s "$T/port" ] && break; sleep 0.1; done
PORT="$(cat "$T/port" 2>/dev/null)"
[ -n "$PORT" ] || { echo "FAIL: version server did not start" >&2; exit 1; }
BASE="http://127.0.0.1:$PORT"
# A port nothing is listening on → connection refused → curl -f and Go both fail.
DEAD_PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"

# age_file <path> <seconds_ago> — set mtime, for the -mmin cache TTL cases.
age_file() { python3 -c 'import os,sys;t=float(sys.argv[2]);os.utime(sys.argv[1],(t,t))' "$1" "$(( $(date +%s) - $2 ))"; }

# snapshot <state_dir> — the mutated state, for side-effect parity.
snapshot() {
  local d="$1" f
  for f in last-update-check just-upgraded-from update-snoozed config.yaml; do
    if [ -f "$d/$f" ]; then printf '%s: %s\n' "$f" "$(cat "$d/$f" | tr '\n' '|')"
    else printf '%s: <absent>\n' "$f"; fi
  done
}

# ─── Case runner ─────────────────────────────────────────────────────────────
# c <desc> <setup-fn-body> <url-path> [args...]
#   setup body runs as _setup <root>, where <root>/dir is BABYSIT_DIR and
#   <root>/state is BABYSIT_STATE_DIR. Both sides get an identical fresh tree.
#   MODE=sub  → invoke the Go side as `bbs update-check` instead of via the
#               bbs-update-check compat-symlink argv[0].
MODE=symlink
c() {
  local desc="$1" setup="$2" urlpath="$3"; shift 3
  local A B ao an ac nc as ns url NOW
  A="$T/a.$$.$RANDOM"; B="$T/b.$$.$RANDOM"
  # One timestamp per case, shared by both sides: seeding the snooze file from
  # $(date +%s) inside _setup would give A and B different epochs whenever the
  # two runs straddle a second boundary, failing the state diff spuriously.
  NOW="$(date +%s)"
  eval "$setup"
  local r
  for r in "$A" "$B"; do
    mkdir -p "$r/dir/bin" "$r/state" "$r/home"
    # Stage a real bbs-config into BABYSIT_DIR so the bash's exec probe and the
    # Go's native config read agree (see the known-divergence note above).
    ln -sf "$REPO/bin/bbs-config" "$r/dir/bin/bbs-config" 2>/dev/null
    _setup "$r"
  done
  case "$urlpath" in
    DEAD) url="http://127.0.0.1:$DEAD_PORT/VERSION" ;;
    NONE) url="$BASE/nope/VERSION" ;;
    *)    url="$BASE/$urlpath/VERSION" ;;
  esac

  ao=$(env HOME="$A/home" BABYSIT_DIR="$A/dir" BABYSIT_STATE_DIR="$A/state" \
        BABYSIT_REMOTE_URL="$url" bash "$REFERENCE" "$@" 2>&1); ac=$?
  if [ "$MODE" = "sub" ]; then
    an=$(env HOME="$B/home" BABYSIT_DIR="$B/dir" BABYSIT_STATE_DIR="$B/state" \
          BABYSIT_REMOTE_URL="$url" "$BIN" update-check "$@" 2>&1); nc=$?
  else
    an=$(env HOME="$B/home" BABYSIT_DIR="$B/dir" BABYSIT_STATE_DIR="$B/state" \
          BABYSIT_REMOTE_URL="$url" \
          sh -c 'exec -a bbs-update-check "$0" "$@"' "$BIN" "$@" 2>&1); nc=$?
  fi
  as="$(snapshot "$A/state")"; ns="$(snapshot "$B/state")"

  if [ "$ao" = "$an" ] && [ "$ac" = "$nc" ] && [ "$as" = "$ns" ]; then
    ok "$desc (rc=$ac)"
  else
    fail "$desc" "old rc=$ac out=[$ao]" "new rc=$nc out=[$an]" \
         "old state: $(echo "$as" | tr '\n' ' ')" "new state: $(echo "$ns" | tr '\n' ' ')"
  fi
  rm -rf "$A" "$B"
}

echo "bbs-update-check parity (oracle: tests/fixtures/bbs-update-check.reference)"

# ─── Local VERSION handling ──────────────────────────────────────────────────
NOVER='_setup(){ :; }'
V123='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; }'
VPAD='_setup(){ printf "  1.2.3 \t\n\n" > "$1/dir/VERSION"; }'
VWS='_setup(){   printf "  \n\t \n" > "$1/dir/VERSION"; }'
VDIR='_setup(){  mkdir -p "$1/dir/VERSION"; }'
c "no VERSION file"                  "$NOVER" newer
c "VERSION whitespace-only"          "$VWS"   newer
c "VERSION is a directory"           "$VDIR"  newer
c "VERSION padded w/ spaces+newline" "$VPAD"  newer
c "up-to-date (remote == local)"     "$V123"  same
c "upgrade available"                "$V123"  newer

# ─── Remote failure modes ────────────────────────────────────────────────────
c "remote 404"               "$V123" NONE
c "remote connection refused" "$V123" DEAD
c "remote returns HTML"      "$V123" html
c "remote returns empty"     "$V123" empty
c "remote returns non-version" "$V123" junk

# ─── update_check config flag ────────────────────────────────────────────────
CFG_OFF='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "update_check: false\n" > "$1/state/config.yaml"; }'
CFG_ON='_setup(){  printf "1.2.3\n" > "$1/dir/VERSION"; printf "update_check: true\n"  > "$1/state/config.yaml"; }'
CFG_JUNK='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "update_check: maybe\n" > "$1/state/config.yaml"; }'
c "cfg_update_check=false disables" "$CFG_OFF"  newer
c "cfg_update_check=true allows"    "$CFG_ON"   newer
c "cfg_update_check=junk allows"    "$CFG_JUNK" newer

# ─── just-upgraded marker (reference:70 prints AND falls through) ────────────
MARK='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "1.0.0\n" > "$1/state/just-upgraded-from"; }'
MARK_EMPTY='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; : > "$1/state/just-upgraded-from"; }'
MARK_WS='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf " \n\t\n" > "$1/state/just-upgraded-from"; }'
# marker + fresh cache ⇒ BOTH a JUST_UPGRADED and an UPGRADE_AVAILABLE line.
MARK_CACHE='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "1.0.0\n" > "$1/state/just-upgraded-from"; printf "UPGRADE_AVAILABLE 1.2.3 9.9.9\n" > "$1/state/last-update-check"; }'
c "marker prints JUST_UPGRADED + falls through" "$MARK"       same
c "marker empty ⇒ no line (set -e fall-through)" "$MARK_EMPTY" same
c "marker whitespace-only ⇒ no line"             "$MARK_WS"    same
c "marker + fresh cache ⇒ two lines"             "$MARK_CACHE" newer

# ─── Cache freshness / TTL ───────────────────────────────────────────────────
FRESH_UTD='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UP_TO_DATE 1.2.3\n" > "$1/state/last-update-check"; }'
STALE_UTD='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UP_TO_DATE 1.2.3\n" > "$1/state/last-update-check"; age_file "$1/state/last-update-check" 7200; }'
UTD_OTHER='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UP_TO_DATE 0.0.1\n" > "$1/state/last-update-check"; }'
FRESH_UA='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UPGRADE_AVAILABLE 1.2.3 9.9.9\n" > "$1/state/last-update-check"; }'
STALE_UA='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UPGRADE_AVAILABLE 1.2.3 9.9.9\n" > "$1/state/last-update-check"; age_file "$1/state/last-update-check" 90000; }'
UA_OTHER='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UPGRADE_AVAILABLE 0.0.1 9.9.9\n" > "$1/state/last-update-check"; }'
FRESH_CF='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "CHECK_FAILED 1.2.3\n" > "$1/state/last-update-check"; }'
GARBAGE='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "WAT 1.2.3\n" > "$1/state/last-update-check"; }'
CORRUPT='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UP_TO_DATE 1.2.3\nJUNK LINE\n" > "$1/state/last-update-check"; }'
EMPTY_CACHE='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; : > "$1/state/last-update-check"; }'
c "fresh UP_TO_DATE ⇒ silent, no fetch" "$FRESH_UTD"   newer
c "stale UP_TO_DATE ⇒ refetch"          "$STALE_UTD"   newer
c "UP_TO_DATE other version ⇒ refetch"  "$UTD_OTHER"   newer
c "fresh UPGRADE_AVAILABLE ⇒ cached line" "$FRESH_UA"  same
c "stale UPGRADE_AVAILABLE ⇒ refetch"   "$STALE_UA"    same
c "UPGRADE_AVAILABLE other old ⇒ refetch" "$UA_OTHER"  newer
c "fresh CHECK_FAILED ⇒ refetch"        "$FRESH_CF"    newer
c "garbage cache (ttl 0) ⇒ refetch"     "$GARBAGE"     newer
c "corrupt multiline cache"             "$CORRUPT"     newer
c "empty cache file"                    "$EMPTY_CACHE" newer

# -mmin rounds the age UP to the next full minute, so 60.5min is stale at ttl=60
# while a truncating implementation would call it fresh. This case is what pins
# ceil-vs-truncate; ±30s of drift keeps both sides on the same side of it.
MMIN_OVER='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UP_TO_DATE 1.2.3\n" > "$1/state/last-update-check"; age_file "$1/state/last-update-check" 3630; }'
MMIN_UNDER='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UP_TO_DATE 1.2.3\n" > "$1/state/last-update-check"; age_file "$1/state/last-update-check" 3570; }'
c "cache 60.5min old ⇒ stale (ceil)" "$MMIN_OVER"  newer
c "cache 59.5min old ⇒ fresh"        "$MMIN_UNDER" newer

# ─── Snooze ──────────────────────────────────────────────────────────────────
# Fresh UPGRADE_AVAILABLE cache + a snooze file: exercises check_snooze on the
# cache path (reference:98). now-60 keeps every "active" snooze far from expiry.
sn() { printf '%s\n' "$1" ; }
SN_BASE='printf "1.2.3\n" > "$1/dir/VERSION"; printf "UPGRADE_AVAILABLE 1.2.3 9.9.9\n" > "$1/state/last-update-check";'
c "snooze L1 active ⇒ silent"      "_setup(){ $SN_BASE printf \"9.9.9 1 \$(( \$NOW - 60 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze L1 expired ⇒ prints"     "_setup(){ $SN_BASE printf \"9.9.9 1 \$(( \$NOW - 90000 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze L2 active ⇒ silent"      "_setup(){ $SN_BASE printf \"9.9.9 2 \$(( \$NOW - 90000 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze L2 expired ⇒ prints"     "_setup(){ $SN_BASE printf \"9.9.9 2 \$(( \$NOW - 180000 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze L3 (7d) active ⇒ silent" "_setup(){ $SN_BASE printf \"9.9.9 3 \$(( \$NOW - 180000 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze L9 (7d) active ⇒ silent" "_setup(){ $SN_BASE printf \"9.9.9 9 \$(( \$NOW - 180000 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze L3 expired ⇒ prints"     "_setup(){ $SN_BASE printf \"9.9.9 3 \$(( \$NOW - 700000 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze version mismatch ⇒ prints" "_setup(){ $SN_BASE printf \"1.1.1 1 \$(( \$NOW - 60 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze non-numeric level ⇒ prints" "_setup(){ $SN_BASE printf \"9.9.9 x \$(( \$NOW - 60 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze non-numeric epoch ⇒ prints" "_setup(){ $SN_BASE printf \"9.9.9 1 abc\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze missing 3rd field ⇒ prints" "_setup(){ $SN_BASE printf \"9.9.9 1\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze empty file ⇒ prints"        "_setup(){ $SN_BASE : > \"\$1/state/update-snoozed\"; }" same
c "snooze level 007 ⇒ 7d bucket"      "_setup(){ $SN_BASE printf \"9.9.9 007 \$(( \$NOW - 180000 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze extra fields ignored"       "_setup(){ $SN_BASE printf \"9.9.9 1 \$(( \$NOW - 60 )) extra junk\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze leading whitespace"         "_setup(){ $SN_BASE printf \"   9.9.9   1   \$(( \$NOW - 60 ))\n\" > \"\$1/state/update-snoozed\"; }" same
c "snooze 2nd line ignored"           "_setup(){ $SN_BASE printf \"9.9.9 1 \$(( \$NOW - 60 ))\nGARBAGE\n\" > \"\$1/state/update-snoozed\"; }" same
# CRLF: awk sees a trailing \r on $3 ⇒ non-numeric ⇒ NOT snoozed. A
# strings.Fields-based split would strip the \r and wrongly snooze.
c "snooze CRLF ⇒ prints (\\r is non-numeric)" "_setup(){ $SN_BASE printf \"9.9.9 1 \$(( \$NOW - 60 ))\r\n\" > \"\$1/state/update-snoozed\"; }" same
# Snooze on the slow path (reference:129), no cache file at all.
c "snooze active on slow path ⇒ silent" "_setup(){ printf \"1.2.3\n\" > \"\$1/dir/VERSION\"; printf \"9.9.9 1 \$(( \$NOW - 60 ))\n\" > \"\$1/state/update-snoozed\"; }" newer

# ─── --force ─────────────────────────────────────────────────────────────────
c "--force busts fresh cache"   "$FRESH_UTD" newer --force
c "--force busts snooze"        "_setup(){ $SN_BASE printf \"9.9.9 1 \$(( \$NOW - 60 ))\n\" > \"\$1/state/update-snoozed\"; }" same --force
c "non-force arg ignored"       "$FRESH_UTD" newer frobnicate
c "--force only honored as \$1" "$FRESH_UTD" newer foo --force
c "--help is not special"       "$FRESH_UTD" newer --help
c "-h is not special"           "$FRESH_UTD" newer -h
c "unknown flag ignored"        "$FRESH_UTD" newer --nope

# ─── Invocation shape: `bbs update-check` subcommand == compat symlink ───────
MODE=sub
c "argv0_subcommand up-to-date"      "$V123"      same
c "argv0_subcommand upgrade"         "$V123"      newer
c "argv0_subcommand --force"         "$FRESH_UTD" newer --force
MODE=symlink

# ─── Summary ─────────────────────────────────────────────────────────────────
echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPARITY: %d ok, 0 diff\033[0m\n' "$PASS"
  exit 0
fi
printf '\033[0;31mPARITY: %d ok, %d diff\033[0m\n' "$PASS" "$FAIL"
printf '  failed: %s\n' "${FAIL_NAMES[@]}"
exit 1
