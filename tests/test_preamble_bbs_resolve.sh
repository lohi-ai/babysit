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
#
# SHELL MATRIX: the preamble block runs under whatever shell drives the skill —
# zsh on a stock macOS box, not just bash. zsh differs on unquoted-$PATH
# splitting and command hashing, so every case runs the resolver under bash AND
# (when present) zsh, each in a clean `env -i` shell.
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

# Pull _bbs_resolve straight out of the preamble into a sourceable file, so the
# test tracks the file, not a copy. The def line through the first column-0 `}`
# is the whole function (inner `}` are indented). `local` runs only on call.
FN="$T/fn.sh"
awk '/^_bbs_resolve\(\) \{/,/^\}/' "$PREAMBLE" > "$FN"
grep -q '^_bbs_resolve() {' "$FN" || { r FAIL; echo "  could not extract _bbs_resolve from $PREAMBLE"; exit 1; }

# Shells to exercise: bash always, zsh when installed (the real macOS default).
SHELLS=(bash); command -v zsh >/dev/null 2>&1 && SHELLS+=(zsh)
echo "shell matrix: ${SHELLS[*]}"

# Run _bbs_resolve in a pristine shell: env -i wipes inherited state (incl. a
# stale command hash), PATH is set explicitly, HOME points at the case fixture.
# zsh gets -f (skip rc) for determinism. PATH always carries /usr/bin:/bin so
# the resolver's own mkdir/ln are found.
resolve() { # $1=shell $2=HOME $3=extra-path-dir("" for none) $4=sub
  local base="/usr/bin:/bin" p flags=""
  [ -n "$3" ] && p="$3:$base" || p="$base"
  [ "$1" = zsh ] && flags="-f"
  env -i HOME="$2" PATH="$p" FN="$FN" SUB="$4" "$1" $flags -c '. "$FN"; _bbs_resolve "$SUB"' 2>/dev/null
}

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

# Build a fixture HOME with dangling shim + dangling plugin copy for <sub>.
dangle_home() { # $1=dir $2=sub
  mkdir -p "$1/.claude/skills/babysit/bin"
  ln -s "$1/.claude/nonexistent-bbs" "$1/.claude/bbs-$2"           # dangling shim
  ln -s bbs "$1/.claude/skills/babysit/bin/bbs-$2"                 # dangling plugin copy (→ absent bbs)
}

for sh in "${SHELLS[@]}"; do
  # ── Case A: dangling symlinks + working `bbs` on PATH → the live bug.
  # bbs-config and bbs-env are symlinks on main today; prove both are fixed,
  # and prove the resolved token actually EXECUTES via argv[0] dispatch.
  for sub in config env; do
    H="$T/$sh-A-$sub"; PB="$T/$sh-pathA-$sub"; mkdir -p "$PB"
    dangle_home "$H" "$sub"; make_bbs "$PB/bbs"
    RES="$(resolve "$sh" "$H" "$PB" "bbs-$sub")"
    [ "$RES" != "bbs-$sub" ] && check "[$sh] A/$sub: resolves to a path, not bare name" runnable runnable \
                             || check "[$sh] A/$sub: resolves to a path, not bare name" runnable "$RES"
    OUT="$("$RES" run-me 2>&1)"
    check "[$sh] A/$sub: quoted \"\$RES\" executes via multicall" "RAN sub=$sub args=run-me" "$OUT"
  done

  # ── Case B: an installed shim (real executable) resolves to itself, unchanged.
  H="$T/$sh-B"; mkdir -p "$H/.claude"; make_bbs "$H/.claude/bbs-config"
  RES="$(resolve "$sh" "$H" "" bbs-config)"
  check "[$sh] B: installed shim resolves to itself, unchanged" "$H/.claude/bbs-config" "$RES"

  # ── Case C: `bbs` present but the subcommand is absent (fails --help probe).
  # Must be REFUSED — never silently accepted — falling through to the bare name.
  H="$T/$sh-C"; PB="$T/$sh-pathC"; mkdir -p "$PB"
  dangle_home "$H" ticket; make_bbs "$PB/bbs"          # serves config/env, NOT ticket
  RES="$(resolve "$sh" "$H" "$PB" bbs-ticket)"
  check "[$sh] C: probe-fail (ticket unported) refused → bare name" bbs-ticket "$RES"

  # ── Case D: no `bbs` anywhere → honest degraded state, the pre-fix behavior.
  H="$T/$sh-D"; dangle_home "$H" config
  RES="$(resolve "$sh" "$H" "" bbs-config)"
  check "[$sh] D: no bbs at all → bare name (unchanged degraded state)" bbs-config "$RES"
done

# ── Parse the whole preamble bash block under every shell (`bash -n`, `zsh -n`).
SNIP="$T/preamble-snippet.sh"
awk '/^## Preamble \(run first\)/{f=1} f&&/^```bash$/{c=1;next} f&&/^```$/{if(c)exit} c' "$PREAMBLE" > "$SNIP"
for sh in "${SHELLS[@]}"; do
  if [ -s "$SNIP" ] && "$sh" -n "$SNIP" 2>/dev/null; then
    check "[$sh] -n: preamble snippet parses" ok ok
  else
    check "[$sh] -n: preamble snippet parses" ok "parse-error-or-empty($(wc -l <"$SNIP") lines)"
  fi
done

echo
printf 'PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
