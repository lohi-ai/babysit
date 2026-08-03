#!/usr/bin/env bash
# Differential harness: the Go `bbs design` vs the frozen bash oracle
# (tests/fixtures/bbs-design.reference). Both run under an identical sandbox
# (pinned PATH, a stub bbs-ticket, the real read-only data dir). JSON output is
# compared *semantically* (canonicalized through `jq -S`), because consumers
# parse the JSON — jq's exact byte serialization is not part of the contract.
# Scalar `--field` leaves are byte-compared; JSONL/components are compared as
# sorted sets (find order ≠ Go walk order).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ORACLE="$ROOT/tests/fixtures/bbs-design.reference"
DATA="$ROOT/.claude/skills/design-ui/data"

command -v jq  >/dev/null 2>&1 || { echo "SKIP: jq not found";  exit 0; }
command -v awk >/dev/null 2>&1 || { echo "SKIP: awk not found"; exit 0; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Build the Go binary once; expose it as bbs-design via an argv0 symlink.
BIN="$WORK/bin"
mkdir -p "$BIN"
( cd "$ROOT" && go build -o "$BIN/bbs" ./cmd/bbs )
ln -sf bbs "$BIN/bbs-design"
GO="$BIN/bbs-design"

# The real ticket command on both sides. It used to be a stub that echoed
# $OVERRIDE_DESIGN, but the Go `design` no longer spawns a `bbs-ticket` off PATH
# — it reaches the ticket subcommand through its own binary — so a PATH stub can
# only intercept the bash oracle. Real ticket state under an isolated
# BABYSIT_HOME is what both impls now read, which is also the truer test.
ln -sf bbs "$BIN/bbs-ticket"

# Pinned PATH: toolchain + our bindir, nothing from ~/.claude.
export PATH="$BIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin"
export BABYSIT_HOME="$WORK/home"

FAILS=0
pass() { printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1"; FAILS=$((FAILS + 1)); }

# ── object comparison: canonicalize both stdouts through `jq -S`
cmp_object() {
  local name="$1"; shift
  ( cd "$WORK" && bash "$ORACLE" "$@" ) >"$WORK/b.out" 2>"$WORK/b.err" && local bc=0 || local bc=$?
  ( cd "$WORK" && "$GO" "$@" )         >"$WORK/g.out" 2>"$WORK/g.err" && local gc=0 || local gc=$?
  if [ "$bc" != "$gc" ]; then fail "$name (exit $bc≠$gc)"; return; fi
  local bj gj
  bj="$(jq -S . <"$WORK/b.out" 2>/dev/null)" || { fail "$name (bash not JSON)"; return; }
  gj="$(jq -S . <"$WORK/g.out" 2>/dev/null)" || { fail "$name (go not JSON)"; return; }
  if [ "$bj" = "$gj" ]; then pass "$name"; else
    fail "$name"; diff <(printf '%s' "$bj") <(printf '%s' "$gj") | head -20
  fi
}

# ── JSONL / sorted-set comparison (each line canonicalized, then sorted)
canon_set() { while IFS= read -r l; do [ -n "$l" ] && printf '%s\n' "$l" | jq -Sc .; done | LC_ALL=C sort; }
cmp_set() {
  local name="$1"; shift
  ( cd "$WORK" && bash "$ORACLE" "$@" ) >"$WORK/b.out" 2>/dev/null || true
  ( cd "$WORK" && "$GO" "$@" )          >"$WORK/g.out" 2>/dev/null || true
  local bs gs
  bs="$(canon_set <"$WORK/b.out")"; gs="$(canon_set <"$WORK/g.out")"
  if [ "$bs" = "$gs" ]; then pass "$name"; else
    fail "$name"; diff <(printf '%s' "$bs") <(printf '%s' "$gs") | head -20
  fi
}

# ── byte comparison of stdout + exit code (scalars, errors)
cmp_bytes() {
  local name="$1"; shift
  ( cd "$WORK" && bash "$ORACLE" "$@" ) >"$WORK/b.out" 2>"$WORK/b.err" && local bc=0 || local bc=$?
  ( cd "$WORK" && "$GO" "$@" )          >"$WORK/g.out" 2>"$WORK/g.err" && local gc=0 || local gc=$?
  if [ "$bc" = "$gc" ] && diff -q "$WORK/b.out" "$WORK/g.out" >/dev/null; then pass "$name"
  else
    fail "$name (exit $bc/$gc)"; diff "$WORK/b.out" "$WORK/g.out" | head -20
  fi
}

# ── stderr + exit comparison (die paths)
cmp_err() {
  local name="$1"; shift
  ( cd "$WORK" && bash "$ORACLE" "$@" ) >/dev/null 2>"$WORK/b.err" && local bc=0 || local bc=$?
  ( cd "$WORK" && "$GO" "$@" )          >/dev/null 2>"$WORK/g.err" && local gc=0 || local gc=$?
  if [ "$bc" = "$gc" ] && diff -q "$WORK/b.err" "$WORK/g.err" >/dev/null; then pass "$name"
  else
    fail "$name (exit $bc/$gc)"; diff "$WORK/b.err" "$WORK/g.err" | head -20
  fi
}

# ── DESIGN.md fixtures ───────────────────────────────────────────
cat > "$WORK/DESIGN.md" <<'YAML'
---
schema: design-tokens/v1
project: "Acme Console"
product_type: SaaS (General)
tokens:
  radius: 8
  colors:
    primary: "#2563EB"
    accent: "#EA580C"
  scale: [12, 14, 16]
  font: { family: "Inter", url: "https://fonts.example/inter" }
  dark_mode: false
---

Prose after frontmatter is ignored.
YAML

cat > "$WORK/override.md" <<'YAML'
---
tokens:
  colors:
    primary: "#111827"
  radius: 12
---
YAML

# ── tokens: master only (no ticket → no override) ───────────────
unset BABYSIT_TICKET
cmp_object "tokens full (master only)"      tokens --design "$WORK/DESIGN.md"
cmp_bytes  "tokens --field colors.primary"  tokens --design "$WORK/DESIGN.md" --field tokens.colors.primary
cmp_bytes  "tokens --field radius (int)"     tokens --design "$WORK/DESIGN.md" --field tokens.radius
cmp_bytes  "tokens --field project (string)" tokens --design "$WORK/DESIGN.md" --field project
cmp_bytes  "tokens --field dark_mode (bool)" tokens --design "$WORK/DESIGN.md" --field tokens.dark_mode
cmp_bytes  "tokens --field missing (null)"   tokens --design "$WORK/DESIGN.md" --field tokens.nope

# ── tokens: merged (ticket override wins per leaf) ──────────────
export BABYSIT_TICKET=bs-design01
"$BIN/bbs" ticket set-pointer design "$WORK/override.md" >/dev/null
cmp_object "tokens merged (override wins)"  tokens --design "$WORK/DESIGN.md"
cmp_bytes  "tokens merged --field primary"  tokens --design "$WORK/DESIGN.md" --field tokens.colors.primary
unset BABYSIT_TICKET

# ── suggest ──────────────────────────────────────────────────────
cmp_object "suggest SaaS (General)"    suggest --data "$DATA" --product "SaaS (General)"
cmp_object "suggest case-insensitive"  suggest --data "$DATA" --product "saas (general)"
cmp_bytes  "suggest not-found"         suggest --data "$DATA" --product "Nonexistent Widget"

# ── ux-check ─────────────────────────────────────────────────────
cmp_set "ux-check Navigation"  ux-check --data "$DATA" --category Navigation
cmp_set "ux-check nav lower"    ux-check --data "$DATA" --category navigation

# ── components ───────────────────────────────────────────────────
mkdir -p "$WORK/proj/components/nested"
: > "$WORK/proj/components/Button.tsx"
: > "$WORK/proj/components/Card.jsx"
: > "$WORK/proj/components/helper.ts"        # lowercase util → skipped
: > "$WORK/proj/components/nested/Modal.vue"
: > "$WORK/proj/components/readme.md"         # wrong ext → skipped
cmp_set "components autodetect" components --root "$WORK/proj/components"
cmp_set "components --ext tsx"  components --root "$WORK/proj/components" --ext .tsx

# ── error / dispatch paths ───────────────────────────────────────
cmp_err "no args"            # (die: usage)
cmp_err "unknown subcommand" bogus
cmp_err "suggest no product" suggest --data "$DATA"
cmp_err "ux-check no cat"    ux-check --data "$DATA"

# ── help: structure, not bytes (BSD/GNU sed diverge) ─────────────
if "$GO" --help | grep -q tokens && "$GO" --help | grep -q suggest \
   && "$GO" --help | grep -q components && "$GO" --help | grep -q ux-check; then
  pass "help lists all subcommands"
else
  fail "help missing a subcommand"
fi

echo
if [ "$FAILS" -eq 0 ]; then echo "ALL PASS"; else echo "$FAILS FAILED"; exit 1; fi
