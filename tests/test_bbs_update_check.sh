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
# Two further divergences, both on corrupt/hostile input only, both argued in
# comments at internal/cmd/update_check.go (stripSpace, checkSnooze):
#   - invalid UTF-8 in VERSION/marker/remote: BSD tr under a UTF-8 locale exits
#     "Illegal byte sequence", which pipefail+set -e turn into a silent exit 1;
#     under LC_ALL=C it passes the bytes through. The port matches the C locale,
#     because an exit code that depends on the caller's $LANG is not a contract.
#   - a >19-digit snooze epoch: bash's arithmetic wraps (C overflow), the port
#     reports "not snoozed".
# Neither is exercised below: the oracle's own answer is locale- or UB-dependent,
# so there is nothing stable to diff against.
#
# The $0 bug (reference:15 derives BABYSIT_DIR from `dirname "$0"` with no
# readlink -f, unlike bin/bbs-env) is NOT a divergence — it is reproduced
# faithfully. The argv0_subcommand cases pin the invocation shape; the c0 cases
# pin the derivation itself, with BABYSIT_DIR UNSET — including the fact that a
# ~/.claude-style shim makes it resolve to the WRONG directory, and that a bare
# PATH invocation must still agree with a script's always-path-ful $0.
#
# Caveat, so the next reader does not trust these cases for more than they
# prove: on darwin os.Executable() does NOT resolve symlinks, so it and
# os.Args[0] derive the same directory for every shape an oracle run can
# produce — no case here can tell them apart. They diverge on linux, where
# /proc/self/exe resolves the shim and would "fix" the bug. The argument for
# os.Args[0] lives in babysitDir's comment; it is not pinned by a test.

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

# ─── Local VERSION server (most cases need real HTTP: status codes, redirects
#     and connection-refused have no file:// equivalent) ─────────────────────
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
class H(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *a, **k): pass
    def do_GET(self):
        # /redirect ⇒ 302 to a VALID VERSION, so following vs not following
        # give different answers (UPGRADE_AVAILABLE vs CHECK_FAILED).
        if self.path == "/redirect":
            self.send_response(302)
            self.send_header("Location", "/newer/VERSION")
            self.end_headers()
            return
        return super().do_GET()
s = socketserver.TCPServer(("127.0.0.1", 0), H)
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
#   LOOSE_STDERR=1 → compare stdout + exit code + state, but not stderr text.
#               Only for the set -e abort cases: there the bash's stderr is
#               whatever mkdir/rm/bash happened to emit, so the wording is not
#               contractual (the caller runs this with 2>/dev/null anyway).
MODE=symlink
LOOSE_STDERR=0
c() {
  local desc="$1" setup="$2" urlpath="$3"; shift 3
  local A B ao an ac nc as ns url NOW aerr nerr
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
    DEAD)     url="http://127.0.0.1:$DEAD_PORT/VERSION" ;;
    NONE)     url="$BASE/nope/VERSION" ;;
    FILE)     url="file://$DOC/newer/VERSION" ;;
    FILEMISS) url="file://$DOC/nope/VERSION" ;;
    REDIR)    url="$BASE/redirect" ;;  # 302 → /newer/VERSION (a valid version)
    *)        url="$BASE/$urlpath/VERSION" ;;
  esac

  ao=$(env HOME="$A/home" BABYSIT_DIR="$A/dir" BABYSIT_STATE_DIR="$A/state" \
        BABYSIT_REMOTE_URL="$url" bash "$REFERENCE" "$@" 2>"$T/aerr"); ac=$?
  if [ "$MODE" = "sub" ]; then
    an=$(env HOME="$B/home" BABYSIT_DIR="$B/dir" BABYSIT_STATE_DIR="$B/state" \
          BABYSIT_REMOTE_URL="$url" "$BIN" update-check "$@" 2>"$T/nerr"); nc=$?
  else
    an=$(env HOME="$B/home" BABYSIT_DIR="$B/dir" BABYSIT_STATE_DIR="$B/state" \
          BABYSIT_REMOTE_URL="$url" \
          sh -c 'exec -a bbs-update-check "$0" "$@"' "$BIN" "$@" 2>"$T/nerr"); nc=$?
  fi
  aerr="$(cat "$T/aerr")"; nerr="$(cat "$T/nerr")"
  as="$(snapshot "$A/state")"; ns="$(snapshot "$B/state")"
  [ "$LOOSE_STDERR" = "1" ] && { aerr=""; nerr=""; }

  if [ "$ao" = "$an" ] && [ "$ac" = "$nc" ] && [ "$as" = "$ns" ] && [ "$aerr" = "$nerr" ]; then
    ok "$desc (rc=$ac)"
  else
    fail "$desc" "old rc=$ac out=[$ao] err=[$aerr]" "new rc=$nc out=[$an] err=[$nerr]" \
         "old state: $(echo "$as" | tr '\n' ' ')" "new state: $(echo "$ns" | tr '\n' ' ')"
  fi
  chmod -R u+rwX "$A" "$B" 2>/dev/null
  rm -rf "$A" "$B"
}

# ─── argv[0] runner (BABYSIT_DIR UNSET) ──────────────────────────────────────
# c0 <desc> <invoke-path-under-root> <version-dir-under-root|none> <urlpath>
# Every c() case pins BABYSIT_DIR, so the reference:15 fallback
# `$(cd "$(dirname "$0")/.." && pwd)` is never exercised there. These cases
# unset it and stage a realistic install instead — the script/binary lives at
# <root>/repo/bin/bbs-update-check, with a ~/.claude-style shim symlinked at
# <root>/claude/bbs-update-check. Because neither side resolves the symlink,
# invoking via the shim derives <root> rather than <root>/repo: the $0 bug.
# The port reproduces it (os.Args[0], not os.Executable), and these cases are
# what hold it in place.
c0() {
  local desc="$1" invoke="$2" vloc="$3" urlpath="$4"
  local A B ao an ac nc url aerr nerr as ns r
  A="$T/a0.$$.$RANDOM"; B="$T/b0.$$.$RANDOM"
  for r in "$A" "$B"; do
    mkdir -p "$r/repo/bin" "$r/claude" "$r/state" "$r/home"
    ln -sf "$REPO/bin/bbs-config" "$r/repo/bin/bbs-config" 2>/dev/null
    [ "$vloc" = "none" ] || printf '1.2.3\n' > "$r/$vloc/VERSION"
    ln -s "$r/repo/bin/bbs-update-check" "$r/claude/bbs-update-check"
  done
  cp "$REFERENCE" "$A/repo/bin/bbs-update-check"; chmod +x "$A/repo/bin/bbs-update-check"
  cp "$BIN"       "$B/repo/bin/bbs-update-check"; chmod +x "$B/repo/bin/bbs-update-check"
  case "$urlpath" in NONE) url="$BASE/nope/VERSION" ;; *) url="$BASE/$urlpath/VERSION" ;; esac

  # invoke=PATH ⇒ bare name found on PATH, run from <root>/work/sub. This is the
  # shape the session preamble actually uses, and the only one where argv[0] and
  # a script's $0 disagree (see babysitDir in internal/cmd/update_check.go).
  if [ "$invoke" = "PATH" ]; then
    mkdir -p "$A/work/sub" "$B/work/sub"
    ao=$(cd "$A/work/sub" && env -u BABYSIT_DIR HOME="$A/home" BABYSIT_STATE_DIR="$A/state" \
          PATH="$A/claude:$PATH" BABYSIT_REMOTE_URL="$url" bbs-update-check 2>"$T/aerr"); ac=$?
    an=$(cd "$B/work/sub" && env -u BABYSIT_DIR HOME="$B/home" BABYSIT_STATE_DIR="$B/state" \
          PATH="$B/claude:$PATH" BABYSIT_REMOTE_URL="$url" bbs-update-check 2>"$T/nerr"); nc=$?
  else
    ao=$(env -u BABYSIT_DIR HOME="$A/home" BABYSIT_STATE_DIR="$A/state" \
          BABYSIT_REMOTE_URL="$url" "$A/$invoke" 2>"$T/aerr"); ac=$?
    an=$(env -u BABYSIT_DIR HOME="$B/home" BABYSIT_STATE_DIR="$B/state" \
          BABYSIT_REMOTE_URL="$url" "$B/$invoke" 2>"$T/nerr"); nc=$?
  fi
  aerr="$(cat "$T/aerr")"; nerr="$(cat "$T/nerr")"
  as="$(snapshot "$A/state")"; ns="$(snapshot "$B/state")"

  if [ "$ao" = "$an" ] && [ "$ac" = "$nc" ] && [ "$as" = "$ns" ] && [ "$aerr" = "$nerr" ]; then
    ok "$desc (rc=$ac)"
  else
    fail "$desc" "old rc=$ac out=[$ao] err=[$aerr]" "new rc=$nc out=[$an] err=[$nerr]" \
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

# ─── file:// and redirects ───────────────────────────────────────────────────
# curl handles file:// natively; the port registers an http.NewFileTransport to
# match. Without it these would be CHECK_FAILED on the Go side only.
c "remote file:// URL"          "$V123" FILE
c "remote file:// missing"      "$V123" FILEMISS
# curl has no -L, so a 302 yields an empty body and rc 0 ⇒ CHECK_FAILED. The
# port's CheckRedirect returns ErrUseLastResponse to match; a default
# http.Client would follow to a valid 9.9.9 and print UPGRADE_AVAILABLE.
c "remote 302 not followed"     "$V123" REDIR

# ─── set -e aborts on unwritable state (stdout+rc only; see LOOSE_STDERR) ────
# reference:128 writes the cache BEFORE echoing the line, so a failed write
# means rc=1 with NO stdout — the port must propagate the error, not swallow it.
if [ "$(id -u)" != 0 ]; then
  LOOSE_STDERR=1
  RO_STATE='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; chmod 500 "$1/state"; }'
  RO_PARENT='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; rmdir "$1/state"; chmod 500 "$1"; }'
  RO_FORCE='_setup(){ printf "1.2.3\n" > "$1/dir/VERSION"; printf "UP_TO_DATE 1.2.3\n" > "$1/state/last-update-check"; chmod 500 "$1/state"; }'
  c "state dir read-only ⇒ cache write fails" "$RO_STATE"  newer
  c "state dir absent + parent read-only"     "$RO_PARENT" newer
  c "--force rm -f fails on read-only state"  "$RO_FORCE"  newer --force
  LOOSE_STDERR=0
else
  echo "  (skipped 3 read-only cases: running as root)"
fi

# ─── argv[0] → BABYSIT_DIR derivation (the $0 bug) ───────────────────────────
c0 "argv0 direct invoke derives <root>/repo"     "repo/bin/bbs-update-check" repo newer
# The bug: the shim's dirname is <root>/claude, so ".." lands on <root> and the
# real VERSION at <root>/repo/VERSION is never seen ⇒ both sides exit silently.
c0 "argv0 shim: \$0 bug misses the real VERSION" "claude/bbs-update-check"   repo newer
# Same shim, decoy VERSION at the (wrong) dir both sides derive: both find it,
# proving they agree on <root> rather than merely both failing.
c0 "argv0 shim: both derive the same wrong dir"  "claude/bbs-update-check"   .    newer
c0 "argv0 direct, no VERSION anywhere"           "repo/bin/bbs-update-check" none newer
# Bare PATH invoke: a script's $0 carries the PATH hit, a binary's argv[0] does
# not. Without the LookPath step in babysitDir, Go derives the CALLER'S cwd's
# parent (<root>/work) while bash derives <root> — so the VERSION at <root> is
# found by bash and missed by Go, silently disabling the check.
c0 "argv0 bare on PATH == script \$0"           "PATH"                      .    newer

# ─── Summary ─────────────────────────────────────────────────────────────────
echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPARITY: %d ok, 0 diff\033[0m\n' "$PASS"
  exit 0
fi
printf '\033[0;31mPARITY: %d ok, %d diff\033[0m\n' "$PASS" "$FAIL"
printf '  failed: %s\n' "${FAIL_NAMES[@]}"
exit 1
