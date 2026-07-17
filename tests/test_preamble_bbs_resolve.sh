#!/usr/bin/env bash
# tests/test_preamble_bbs_resolve.sh — the preamble's _bbs_resolve must fall
# back to the `bbs` multicall (as a runnable single token) when both compat
# symlinks dangle, and must stay byte-for-byte unchanged when a shim resolves.
#
# The bug: bin/bbs-<sub> is a symlink → the gitignored bin/bbs. On a checkout
# where nothing built the binary, both ~/.claude/bbs-<sub> and the plugin copy
# dangle, `[ -x ]` is false for each, and the old resolver echoed the bare name
# `bbs-<sub>` — an unrunnable command. Every skill that then runs "$BBS_*_BIN"
# broke with command-not-found. This is already live for bbs-config / bbs-env
# (both symlinks on main) and widens to every preamble bin as the ports land.
#
# Every call site consumes "$BBS_*_BIN" as a SINGLE QUOTED WORD, so the fix
# cannot hand back a two-word `bbs <sub>` string. It returns a bbs-<sub>-named
# symlink to the multicall binary; argv[0]-basename dispatch routes it to <sub>.
# The execute-it assertions are the real guard — a resolved-path check alone
# would pass for a bare name that cannot run, which is exactly the false-clean
# that shipped.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREAMBLE="$ROOT/.claude/skills/references/preamble.md"
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
PASS=0; FAIL=0
g() { printf '\033[0;32m%s\033[0m' "$1"; }; r() { printf '\033[0;31m%s\033[0m' "$1"; }
check() { # $1=name $2=want $3=got
  if [ "$2" = "$3" ]; then g ok; printf '    %s\n' "$1"; PASS=$((PASS+1))
  else r FAIL; printf '  %s\n      want=[%s] got=[%s]\n' "$1" "$2" "$3"; FAIL=$((FAIL+1)); fi
}

# Pull _bbs_resolve straight out of the preamble so the test tracks the file,
# not a copy. The def line through the first column-0 `}` is the whole function
# (inner `}` are indented). `local` runs only when the function is called.
eval "$(awk '/^_bbs_resolve\(\) \{/,/^\}/' "$PREAMBLE")"
[ "$(type -t _bbs_resolve)" = function ] || { r FAIL; echo "  could not extract _bbs_resolve from $PREAMBLE"; exit 1; }

# A stand-in multicall binary. Mirrors bin/bbs: argv[0]-basename dispatch, cobra
# subcommand routing, and — critically — an UNKNOWN subcommand exits 1 silently
# (root.go SilenceErrors), which is what makes the capability probe necessary.
# Serves config + env; refuses everything else (e.g. the not-yet-ported ticket).
make_bbs() { # $1 = dest path
  cat > "$1" <<'STUB'
#!/usr/bin/env bash
self=$(basename "$0")
if [[ $self == bbs-* ]]; then sub=${self#bbs-}; else sub=${1:-}; shift 2>/dev/null || true; fi
case "$sub" in
  config|env) [ "${1:-}" = "--help" ] && { echo "usage: bbs $sub"; exit 0; }
              echo "RAN sub=$sub args=$*"; exit 0 ;;
  *) exit 1 ;;
esac
STUB
  chmod +x "$1"
}

# Run _bbs_resolve under a scratch HOME/PATH so nothing on the real machine
# leaks in. Echoes the resolved value.
resolve() { ( export HOME="$1" PATH="$2"; _bbs_resolve "$3" ); }

# ── Case A: dangling compat symlinks + a working `bbs` on PATH → the live bug.
# bbs-config and bbs-env are symlinks on main today; prove both are fixed.
for sub in config env; do
  H="$T/A-$sub"; mkdir -p "$H/.claude/skills/babysit/bin" "$T/pathA"
  ln -s "$H/.claude/nonexistent-bbs"  "$H/.claude/bbs-$sub"                     # dangling shim
  ln -s bbs                           "$H/.claude/skills/babysit/bin/bbs-$sub"  # dangling repo copy (→ absent bbs)
  make_bbs "$T/pathA/bbs"
  RES="$(resolve "$H" "$T/pathA:/usr/bin:/bin" "bbs-$sub")"
  # Not the bare, unrunnable name.
  [ "$RES" != "bbs-$sub" ] && check "A/$sub: resolves to a path, not bare name" runnable runnable \
                           || check "A/$sub: resolves to a path, not bare name" runnable "$RES"
  # And it actually executes as a single quoted token → argv[0] dispatch to <sub>.
  OUT="$("$RES" run-me 2>&1)"
  check "A/$sub: quoted \"\$RES\" executes via multicall" "RAN sub=$sub args=run-me" "$OUT"
done

# ── Case B: an installed shim (real executable) resolves to itself, unchanged.
H="$T/B"; mkdir -p "$H/.claude"
make_bbs "$H/.claude/bbs-config"          # a real, executable shim
RES="$(resolve "$H" "/usr/bin:/bin" bbs-config)"
check "B: installed shim resolves to itself, unchanged" "$H/.claude/bbs-config" "$RES"

# ── Case C: `bbs` present but the subcommand is absent (fails --help probe).
# Must be REFUSED — never silently accepted — falling through to the bare name.
H="$T/C"; mkdir -p "$H/.claude/skills/babysit/bin" "$T/pathC"
ln -s "$H/.claude/nonexistent-bbs" "$H/.claude/bbs-ticket"
ln -s bbs "$H/.claude/skills/babysit/bin/bbs-ticket"
make_bbs "$T/pathC/bbs"                    # serves config/env, NOT ticket
RES="$(resolve "$H" "$T/pathC:/usr/bin:/bin" bbs-ticket)"
check "C: probe-fail (ticket unported) refused → bare name" bbs-ticket "$RES"

# ── Case D: no `bbs` anywhere → honest degraded state, the pre-fix behavior.
H="$T/D"; mkdir -p "$H/.claude/skills/babysit/bin"
ln -s "$H/.claude/nonexistent-bbs" "$H/.claude/bbs-config"
ln -s bbs "$H/.claude/skills/babysit/bin/bbs-config"
RES="$(resolve "$H" "/usr/bin:/bin" bbs-config)"
check "D: no bbs at all → bare name (unchanged degraded state)" bbs-config "$RES"

# ── bash -n the whole preamble bash block (the "run first" snippet).
SNIP="$T/preamble-snippet.sh"
awk '/^## Preamble \(run first\)/{f=1} f&&/^```bash$/{c=1;next} f&&/^```$/{if(c)exit} c' "$PREAMBLE" > "$SNIP"
if [ -s "$SNIP" ] && bash -n "$SNIP" 2>/dev/null; then
  check "bash -n: preamble snippet parses" ok ok
else
  check "bash -n: preamble snippet parses" ok "parse-error-or-empty($(wc -l <"$SNIP") lines)"
fi

echo
printf 'PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
