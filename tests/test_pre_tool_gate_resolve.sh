#!/usr/bin/env bash
# tests/test_pre_tool_gate_resolve.sh — the gate must fail CLOSED when bbs-ticket
# is unresolvable, and must stay unchanged when it resolves.
#
# Guards the bug that shipped on chore/bs-6sck3n02_go-ticket-identity-core: that
# branch turns bin/bbs-ticket into a symlink → the gitignored bin/bbs, so on a
# checkout where nothing built the binary `[ -x ]` is false, the gate emitted
# `defer`, and `git push` / `gh pr create` sailed through with the enforcement
# boundary silently off. The reason string was indistinguishable from the
# legitimate ad-hoc-shell defer, so nothing looked wrong.
#
# Both halves matter. The bug was invisible to every developer with an install
# (`command -v` found the global shim and rescued it), and only bit fresh clones
# and CI. A green run on the with-install half alone is exactly the false-clean
# that let this through the first time — so half 1 is the real guard, and half 2
# proves we didn't fix it by breaking everyone else.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$ROOT/bin/hooks/pre-tool-gate"
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
PASS=0; FAIL=0
g() { printf '\033[0;32m%s\033[0m' "$1"; }; r() { printf '\033[0;31m%s\033[0m' "$1"; }

hook() { # $1=command  → prints permissionDecision; env passed via caller
  echo "{\"tool_input\":{\"command\":\"$1\"}}" | bash "$GATE" 2>/dev/null
}
decision() { printf '%s' "$1" | sed -n 's/.*"permissionDecision":"\([a-z]*\)".*/\1/p'; }
reason()   { printf '%s' "$1" | sed -n 's/.*"permissionDecisionReason":"\([^"]*\)".*/\1/p'; }

check() { # $1=name $2=want $3=got
  if [ "$2" = "$3" ]; then g ok; printf '    %s\n' "$1"; PASS=$((PASS+1))
  else r FAIL; printf '  %s\n      want=%s got=%s\n' "$1" "$2" "$3"; FAIL=$((FAIL+1)); fi
}

# A repo whose bin/bbs-ticket is a dangling symlink → bin/bbs, reproducing
# bs-6sck3n02 without needing that branch checked out.
RIG="$T/rig"; mkdir -p "$RIG/bin/hooks"
cp "$GATE" "$RIG/bin/hooks/pre-tool-gate"
ln -sf bbs "$RIG/bin/bbs-ticket"   # dangles: bin/bbs not built
GATE="$RIG/bin/hooks/pre-tool-gate"

# ── Half 1: no install, no PATH shim → the dangling symlink is the only
# candidate. Must DENY, and the reason must be unmistakable.
EMPTY="$T/emptyhome"; mkdir -p "$EMPTY"
noinstall() { echo "{\"tool_input\":{\"command\":\"$1\"}}" | \
  env -i PATH=/usr/bin:/bin HOME="$EMPTY" bash "$GATE" 2>/dev/null; }

for c in "git push origin HEAD" "gh pr create --fill" "gh pr merge 12"; do
  out="$(noinstall "$c")"
  check "no-install: '$c' denies (gate offline)" deny "$(decision "$out")"
  case "$(reason "$out")" in
    *"GATE OFFLINE"*) check "no-install: '$c' reason is distinct" distinct distinct ;;
    *) check "no-install: '$c' reason is distinct" distinct "$(reason "$out")" ;;
  esac
done

# The whole defect was that a switched-off gate read like a normal defer. The
# deny reason must share no wording with the defer family.
out="$(noinstall "git push origin HEAD")"
case "$(reason "$out")" in
  *defer*|*"not found"*) check "deny reason never reads as a defer" clean "$(reason "$out")" ;;
  *) check "deny reason never reads as a defer" clean clean ;;
esac

# ── Half 2: with a working bbs-ticket on PATH → behavior unchanged from today.
# Stub it: `resolve` yields a ticket, verdicts are absent → today's answer is ask.
BIN="$T/bin"; mkdir -p "$BIN"
cat >"$BIN/bbs-ticket" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  resolve) echo "bs-test0001" ;;
  verdict-status) echo none ;;
  qa-evidence) echo none ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$BIN/bbs-ticket"
withinstall() { echo "{\"tool_input\":{\"command\":\"$1\"}}" | \
  env -i PATH="$BIN:/usr/bin:/bin" HOME="$EMPTY" bash "$GATE" 2>/dev/null; }

check "with-install: git push → ask (unchanged)"      ask "$(decision "$(withinstall "git push origin HEAD")")"
check "with-install: gh pr create → ask (unchanged)"  ask "$(decision "$(withinstall "gh pr create --fill")")"
check "with-install: non-hard-stage → defer"          defer "$(decision "$(withinstall "ls -la")")"

# ── The multicall fallback: bbs-ticket unresolvable, but `bbs ticket` serves it.
# Must resolve through the fallback and behave exactly like the real bin (ask),
# NOT fail closed — a dangling alias is not an offline gate if bbs can serve it.
MC="$T/mc"; mkdir -p "$MC"
cat >"$MC/bbs" <<'EOF'
#!/usr/bin/env bash
[ "$1" = ticket ] || exit 1          # only `ticket` exists (mimics root.go)
shift
case "$1" in
  --help) exit 0 ;;
  resolve) echo "bs-test0001" ;;
  verdict-status) echo none ;;
  qa-evidence) echo none ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$MC/bbs"
out="$(echo '{"tool_input":{"command":"git push origin HEAD"}}' | \
  env -i PATH="$MC:/usr/bin:/bin" HOME="$EMPTY" bash "$GATE" 2>/dev/null)"
check "fallback: dangling alias + capable bbs → ask via 'bbs ticket'" ask "$(decision "$out")"

# ── Capability probe: a bbs that does NOT serve `ticket` must NOT be accepted.
# `bbs ticket resolve` there exits 1 *silently* (root.go SilenceErrors), which is
# identical to a real bbs-ticket's "no ticket" exit 1 — trusting it would rebuild
# the original bug behind a fallback. Must still deny.
NC="$T/nc"; mkdir -p "$NC"
cat >"$NC/bbs" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  config) exit 0 ;;
  help) exit 0 ;;   # cobra exits 0 for unknown help topics — not a valid probe
  *) exit 1 ;;      # `ticket` absent: silent exit 1
esac
EOF
chmod +x "$NC/bbs"
out="$(echo '{"tool_input":{"command":"git push origin HEAD"}}' | \
  env -i PATH="$NC:/usr/bin:/bin" HOME="$EMPTY" bash "$GATE" 2>/dev/null)"
check "probe: bbs without 'ticket' is rejected, still denies" deny "$(decision "$out")"

echo
if [ "$FAIL" -eq 0 ]; then g PASS; printf '  %d/%d\n' "$PASS" "$((PASS+FAIL))"; exit 0
else r FAIL; printf '  %d/%d\n' "$PASS" "$((PASS+FAIL))"; exit 1; fi
