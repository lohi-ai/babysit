#!/usr/bin/env bash
# tests/test_bbs_env.sh — differential guard for the bbs-env Go port.
#
# `bbs env` replaced the bin/bbs-env bash script, and skills depend on its exact
# stdout/stderr/exit contract. Rather than assert hand-written goldens, every
# case runs the frozen pre-port bash (tests/fixtures/bbs-env.reference) and the
# Go binary side by side under an identical environment and diffs all three
# channels — so any drift from the original is a failure, not a judgement call.
#
# Both binaries are staged into a throwaway project root (bin/ + lib/ + config/)
# so the bash's PROJECT_ROOT and the Go's projectRoot() resolve to the same tree.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-env.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

# mk_root <dir> — a fake project root serving both implementations.
mk_root() {
  local r="$1"
  mkdir -p "$r/bin" "$r/lib"
  cp "$REPO/lib/load-env-file.sh" "$r/lib/load-env-file.sh"
  cp "$REFERENCE" "$r/bin/bbs-env-oracle"; chmod +x "$r/bin/bbs-env-oracle"
  cp "$BIN" "$r/bin/bbs"
  ln -sf bbs "$r/bin/bbs-env"   # multicall: basename bbs-env -> `env` subcommand
}

# diff_case <desc> <args...> — run both under CASE_ENV/CASE_CWD, diff all channels.
CASE_ENV=(); CASE_CWD=""; ROOT=""
diff_case() {
  local desc="$1"; shift
  local orc grc msg=""
  ( cd "$CASE_CWD" && env -i PATH="$PATH" ${CASE_ENV[@]+"${CASE_ENV[@]}"} \
      "$ROOT/bin/bbs-env-oracle" "$@" >"$T/o.out" 2>"$T/o.err" ); orc=$?
  ( cd "$CASE_CWD" && env -i PATH="$PATH" ${CASE_ENV[@]+"${CASE_ENV[@]}"} \
      "$ROOT/bin/bbs-env" "$@" >"$T/g.out" 2>"$T/g.err" ); grc=$?
  [ "$orc" = "$grc" ] || msg="exit bash=$orc go=$grc;"
  cmp -s "$T/o.out" "$T/g.out" || msg="$msg stdout[$(diff "$T/o.out" "$T/g.out" | tr '\n' '|')]"
  cmp -s "$T/o.err" "$T/g.err" || msg="$msg stderr[$(diff "$T/o.err" "$T/g.err" | tr '\n' '|')]"
  if [ -z "$msg" ]; then ok "$desc"; else fail "$desc" "$msg"; fi
}

# ── Plain root: no config/, mirroring the real repo ────────────────────
ROOT="$T/plain"; mk_root "$ROOT"; CASE_CWD="$ROOT"

echo "resolve:"
CASE_ENV=(FOO=bar);      diff_case "resolve-set" resolve FOO
CASE_ENV=();             diff_case "resolve-unset-exits-1" resolve FOO
CASE_ENV=(FOO=);         diff_case "resolve-empty-counts-unset" resolve FOO
CASE_ENV=(B=2);          diff_case "resolve-first-set-of-many" resolve A B
CASE_ENV=(FOO="a b c");  diff_case "resolve-value-with-spaces" resolve FOO
CASE_ENV=();             diff_case "resolve-no-args" resolve
CASE_ENV=(LOCAL_X=l STG_X=s X=b); diff_case "resolve-prefix-local-wins" resolve --prefix X
CASE_ENV=(STG_X=s X=b);  diff_case "resolve-prefix-stg-beats-base" resolve --prefix X
CASE_ENV=(X=b);          diff_case "resolve-prefix-falls-to-base" resolve --prefix X
CASE_ENV=();             diff_case "resolve-prefix-none-exits-1" resolve --prefix X
CASE_ENV=();             diff_case "resolve-prefix-without-varname" resolve --prefix
CASE_ENV=(LOCAL_Y=ly);   diff_case "resolve-prefix-second-varname" resolve --prefix X Y
CASE_ENV=(X= LOCAL_X=);  diff_case "resolve-prefix-all-empty-exits-1" resolve --prefix X

echo "is-set:"
CASE_ENV=(FOO=bar);      diff_case "is-set-yes" is-set FOO
CASE_ENV=();             diff_case "is-set-no" is-set FOO
CASE_ENV=(FOO=);         diff_case "is-set-empty-is-no" is-set FOO
CASE_ENV=();             diff_case "is-set-no-args" is-set
CASE_ENV=(LOCAL_X=l);    diff_case "is-set-prefix-yes" is-set --prefix X
CASE_ENV=();             diff_case "is-set-prefix-no" is-set --prefix X
# Deliberate divergence — the one case not diffed against the oracle.
# `is-set --prefix` with no varname clears the bash's arg guard and then reads
# an unset "$1" under `set -u`, so the shell spews three
# "line 157: $1: unbound variable" diagnostics before answering. That noise is a
# bug in the bash, not a contract: the port reproduces the observable behavior
# (stdout "no", exit 0) and stays quiet on stderr.
CASE_ENV=()
(
  cd "$CASE_CWD" || exit 1
  out="$(env -i PATH="$PATH" "$ROOT/bin/bbs-env" is-set --prefix 2>"$T/g.err")"; rc=$?
  [ "$rc" = 0 ]           || { echo "exit=$rc want 0"; exit 1; }
  [ "$out" = "no" ]       || { echo "stdout=[$out] want [no]"; exit 1; }
  [ ! -s "$T/g.err" ]     || { echo "stderr not empty: $(cat "$T/g.err")"; exit 1; }
  # The oracle agrees on the contract, and differs only by the set -u noise.
  bout="$(env -i PATH="$PATH" "$ROOT/bin/bbs-env-oracle" is-set --prefix 2>/dev/null)"; brc=$?
  [ "$brc" = 0 ] && [ "$bout" = "no" ] || { echo "oracle contract drifted: rc=$brc out=[$bout]"; exit 1; }
) && ok "is-set-prefix-without-varname (contract only; bash leaks set -u noise)" \
  || fail "is-set-prefix-without-varname"

echo "list-prefix:"
CASE_ENV=(LOCAL_C=3 LOCAL_A=1 LOCAL_B=2 OTHER=x); diff_case "list-prefix-sorted" list-prefix LOCAL_
CASE_ENV=(OTHER=x);      diff_case "list-prefix-no-match-empty" list-prefix NOPE_
CASE_ENV=();             diff_case "list-prefix-no-args" list-prefix
CASE_ENV=(STG_A=1 STG_B=2 STG_LONG_NAME=3); diff_case "list-prefix-stg" list-prefix STG_
# These names are chosen so byte order and locale collation disagree ('_' 0x5F
# sorts after 'B' 0x42, but UTF-8 collation is punctuation-insensitive).
COLLIDE=(LOCAL_A_B=1 LOCAL_AB=2 LOCAL_a=3 LOCAL_B=4)
CASE_ENV=(LC_ALL=C LANG=C "${COLLIDE[@]}"); diff_case "list-prefix-byte-order-under-C-locale" list-prefix LOCAL_

# Deliberate divergence #2 — the port's ordering is locale-independent.
# The bash pipes through `sort`, which collates per the caller's locale, so its
# order changes with LANG (and with the platform's sort). The port always emits
# byte order = `sort` under LC_ALL=C. Nothing consumes this ordering
# programmatically, and libc collation isn't reachable natively, so determinism
# wins. Pin it: the Go output must be identical under C and under en_US.UTF-8.
(
  cd "$CASE_CWD" || exit 1
  c=$(env -i PATH="$PATH" LC_ALL=C LANG=C "${COLLIDE[@]}" "$ROOT/bin/bbs-env" list-prefix LOCAL_)
  u=$(env -i PATH="$PATH" LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8 "${COLLIDE[@]}" "$ROOT/bin/bbs-env" list-prefix LOCAL_)
  [ "$c" = "$u" ] || { echo "port drifted with locale:"; echo "C=[$c]"; echo "UTF-8=[$u]"; exit 1; }
  [ "$c" = "$(printf 'LOCAL_AB=2\nLOCAL_A_B=1\nLOCAL_B=4\nLOCAL_a=3')" ] || { echo "not byte order: [$c]"; exit 1; }
) && ok "list-prefix-locale-independent (byte order; bash follows LANG)" \
  || fail "list-prefix-locale-independent"

echo "prompt:"
CASE_ENV=(A=1);          diff_case "prompt-reports-missing" prompt A B C
CASE_ENV=(A=1 B=2);      diff_case "prompt-all-set-silent" prompt A B
CASE_ENV=();             diff_case "prompt-no-args-silent" prompt
CASE_ENV=(A=);           diff_case "prompt-empty-is-missing" prompt A

echo "help / errors:"
CASE_ENV=();             diff_case "help" help
CASE_ENV=();             diff_case "help-long-flag" --help
CASE_ENV=();             diff_case "help-short-flag" -h
CASE_ENV=();             diff_case "no-subcommand-usage-exit-1"
CASE_ENV=();             diff_case "unknown-subcommand" bogus
CASE_ENV=();             diff_case "env-file-flag-missing-value" --env-file
CASE_ENV=();             diff_case "app-flag-missing-value" --app

# ── .env file loading ──────────────────────────────────────────────────
echo "--env-file auto-load:"
cat > "$T/basic.env" <<'EOF'
# a comment
DBQ=fromfile

  # indented comment
QUOTED="quoted value"
SQUOTED='single value'
INLINE=val # trailing comment
PLACEHOLDER=${SOMETHING}
NOT_A_VAR line without equals
9BAD=starts-with-digit
EOF
printf 'CRLF=win\r\nAFTER=next\r\n' > "$T/crlf.env"
# Bug-for-bug: on a CRLF line the trailing \r defeats the quote-strip regex
# (it runs before the \r is trimmed), so the quotes survive. The LF twin below
# pins the contrast — the port must not "fix" this.
printf 'QCRLF="v"\r\n' > "$T/crlfq.env"
printf 'QLF="v"\n'      > "$T/lfq.env"

CASE_ENV=();             diff_case "env-file-loads-value" --env-file "$T/basic.env" resolve DBQ
CASE_ENV=(DBQ=shell);    diff_case "env-file-shell-env-wins" --env-file "$T/basic.env" resolve DBQ
CASE_ENV=();             diff_case "env-file-strips-double-quotes" --env-file "$T/basic.env" resolve QUOTED
CASE_ENV=();             diff_case "env-file-strips-single-quotes" --env-file "$T/basic.env" resolve SQUOTED
CASE_ENV=();             diff_case "env-file-trims-inline-comment" --env-file "$T/basic.env" resolve INLINE
CASE_ENV=();             diff_case "env-file-skips-placeholder" --env-file "$T/basic.env" resolve PLACEHOLDER
CASE_ENV=();             diff_case "env-file-skips-bad-key" --env-file "$T/basic.env" resolve 9BAD
CASE_ENV=();             diff_case "env-file-list-prefix-sees-loaded" --env-file "$T/basic.env" list-prefix DBQ
CASE_ENV=();             diff_case "env-file-crlf-stripped" --env-file "$T/crlf.env" resolve CRLF
CASE_ENV=();             diff_case "env-file-crlf-quoted-keeps-quotes" --env-file "$T/crlfq.env" resolve QCRLF
CASE_ENV=();             diff_case "env-file-lf-quoted-strips-quotes" --env-file "$T/lfq.env" resolve QLF
CASE_ENV=();             diff_case "env-file-missing-file-is-noop" --env-file "$T/nope.env" resolve DBQ
CASE_ENV=();             diff_case "env-file-prompt-sees-loaded" --env-file "$T/basic.env" prompt DBQ MISSING_ONE
# An explicitly empty flag value reads as unset — the bash guards with [ -n … ].
CASE_ENV=(FOO=x);        diff_case "empty-env-file-value-falls-through" --env-file "" resolve FOO
CASE_ENV=(FOO=x);        diff_case "empty-app-value-falls-through" --app "" resolve FOO

# ── config/<app>/.env.base detection ───────────────────────────────────
echo "app detection:"
ROOT="$T/proj"; mk_root "$ROOT"
mkdir -p "$ROOT/config/my-app" "$ROOT/config/other"
printf 'APPVAR=from-my-app\nSHARED=my-app\n' > "$ROOT/config/my-app/.env.base"
printf 'OTHERVAR=from-other\nSHARED=other\n'  > "$ROOT/config/other/.env.base"
CASE_CWD="$ROOT"

CASE_ENV=(BABYSIT_APP=my-app); diff_case "app-from-BABYSIT_APP" resolve APPVAR
CASE_ENV=();                   diff_case "app-flag" --app my-app resolve APPVAR
CASE_ENV=(BABYSIT_APP=other);  diff_case "app-flag-beats-BABYSIT_APP" --app my-app resolve APPVAR
CASE_ENV=(BABYSIT_APP=my-app); diff_case "app-scoped-file-only" resolve OTHERVAR
CASE_ENV=();                   diff_case "no-app-loads-every-config" resolve OTHERVAR
CASE_ENV=();                   diff_case "no-app-loads-all-shared-first-wins" resolve SHARED
CASE_ENV=(BABYSIT_APP=my-app); diff_case "env-file-beats-app" --env-file "$T/basic.env" resolve APPVAR
CASE_ENV=(BABYSIT_APP=nosuch); diff_case "unknown-app-loads-nothing" resolve APPVAR
CASE_ENV=(BABYSIT_APP=my-app APPVAR=shell); diff_case "app-file-shell-env-wins" resolve APPVAR

# CWD detection: a `my_app` path component maps to the `my-app` config dir.
mkdir -p "$ROOT/work/my_app/nested" "$ROOT/work/my-app" "$ROOT/work/unrelated"
CASE_CWD="$ROOT/work/my_app"; CASE_ENV=();          diff_case "cwd-detects-rig-underscore-spelling" resolve APPVAR
CASE_CWD="$ROOT/work/my_app/nested"; CASE_ENV=();   diff_case "cwd-detects-nested-component" resolve APPVAR
CASE_CWD="$ROOT/work/my-app"; CASE_ENV=();          diff_case "cwd-detects-app-name" resolve APPVAR
CASE_CWD="$ROOT/work/unrelated"; CASE_ENV=();       diff_case "cwd-no-match-loads-all" resolve OTHERVAR

# ── Summary ────────────────────────────────────────────────────────────
echo ""
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d/%d cases match bin/bbs-env exactly\n' "$PASS" "$((PASS + FAIL))"
  exit 0
fi
printf '\033[0;31mFAIL\033[0m  %d/%d failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
exit 1
