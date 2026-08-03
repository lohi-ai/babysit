#!/usr/bin/env bash
# tests/test_bbs_ticket_state_differential.sh — the Go index.json state-accessor
# port must behave identically to the frozen bash oracle.
#
# Covers the slice ported in internal/cmd/ticket_state.go + internal/ticket/
# index.go: env, get, set-status, set-phase, set-parent, add-child,
# add-relation, set-sibling, add-label, set-pointer, get-pointer, ensure-size,
# append-history. Manifest.yaml ops have their own differential harness
# (test_bbs_ticket_manifest_differential.sh); base-ops stay bash-delegated. Both
# are out of scope here.
#
# Method: replay one identical command sequence against the Go binary
# (bin/bbs-ticket) and the frozen bash reference (tests/fixtures/
# bbs-ticket.reference), each pinned to its own BABYSIT_PROJECT_HOME, under an
# identical sandbox. Assert per-command stdout/stderr/exit match, then assert
# the resulting index.json + history.jsonl are JSON-equivalent (jq -S, with
# timestamps masked).

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
GO_BIN="$SCRIPT_DIR/bin/bbs-ticket"
REF="$SCRIPT_DIR/tests/fixtures/bbs-ticket.reference"

command -v jq >/dev/null 2>&1 || { echo "SKIP: jq not installed"; exit 0; }
[ -x "$GO_BIN" ] || { echo "FAIL: $GO_BIN not built (go build -o bin/bbs ./cmd/bbs)"; exit 1; }
[ -f "$REF" ]    || { echo "FAIL: missing $REF"; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

# Sandbox: pinned HOME + PATH so both impls resolve the same bbs-slug and never
# touch the real ~/.babysit. bbs-slug (Go) sits in bin/ next to bbs; put bin/ on
# PATH so the bash reference's `command -v bbs-slug` finds it too.
export HOME="$ROOT/home"
export BABYSIT_HOME="$ROOT/home/.babysit"
export BABYSIT_TICKET="bs-state01"
export PATH="$SCRIPT_DIR/bin:$PATH"
export BBS_LIB="$SCRIPT_DIR/bin/lib"
unset BBS_TICKET AGENT_ROLE GT_ROLE 2>/dev/null || true
mkdir -p "$HOME"

# Mask timestamps and the per-impl project-home prefix (bash-home vs go-home is
# an expected path difference, not a behavioral one).
mask() {
  sed -E -e 's/"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z"/"TS"/g' \
         -e "s#$ROOT/bash-home#IMPLHOME#g" \
         -e "s#$ROOT/go-home#IMPLHOME#g"
}

# run_impl <impl> <args...> — run one impl in its own project home, appending
# stdout/stderr/exit to per-impl step logs.
run_impl() {
  local impl="$1"; shift
  local home="$ROOT/$impl-home"
  export BABYSIT_PROJECT_HOME="$home/projects/slug"
  local out err rc
  out="$( "$ROOT/$impl-cmd" "$@" 2>"$ROOT/.err" )"; rc=$?
  err="$(cat "$ROOT/.err")"
  {
    printf '### %s\n' "$*"
    printf 'RC=%s\n' "$rc"
    printf 'OUT=%s\n' "$out"
    printf 'ERR=%s\n' "$err"
  } >> "$ROOT/$impl.steps"
}

# Wrapper scripts so run_impl can invoke either impl uniformly.
printf '#!/usr/bin/env bash\nexec "%s" "$@"\n' "$GO_BIN" > "$ROOT/go-cmd"
printf '#!/usr/bin/env bash\nexec bash "%s" "$@"\n' "$REF" > "$ROOT/bash-cmd"
chmod +x "$ROOT/go-cmd" "$ROOT/bash-cmd"

# ── the shared command sequence ───────────────────────────────────────
SEQ=(
  "get status"                       # missing-file quirk (double newline)
  "set-status in_progress"           # fresh: ensure_defaults + status + history
  "set-phase implement"
  "set-parent bs-parent1"
  "add-child bs-child1"
  "add-child bs-child1"              # dedup — no second entry
  "add-relation blocks bs-blk1"
  "add-relation blocked_by bs-blk2"
  "add-relation related bs-rel1"
  "add-relation duplicate_of bs-dup1"
  "set-sibling --role fe --repo org/fe --ticket bs-fe1"
  "set-sibling --role fe --repo org/fe --ticket bs-fe1"  # dedup by equality
  "set-sibling --role be --repo org/be --ticket bs-be1"
  "add-label urgent"
  "add-label urgent"                 # dedup
  "set-pointer pr https://ex/pr/9"
  "set-pointer ticket_size M"
  "get status"
  "get relations"
  "get siblings"
  "get pointers"
  "get-pointer pr"
  "get-pointer nonexistent"
  "get origin.parent"
  "append-history --event note --actor me --extra-json {\"k\":\"v\"}"
  "append-history --event badextra --extra-json not-json"
  "ensure-size"                      # ticket_size already M → echoes M, no git
)

for cmd in "${SEQ[@]}"; do
  eval "set -- $cmd"
  run_impl bash "$@"
  run_impl go "$@"
done

# ── error paths (each its own step) ──────────────────────────────────
ERRSEQ=(
  "set-status bogus"
  "get"
  "add-relation weird target"
  "set-sibling --role fe --repo org/fe"
  "get-pointer"
  "set-pointer"
  "set-phase"
  "set-parent"
  "add-child"
  "add-label"
  "append-history"
)
for cmd in "${ERRSEQ[@]}"; do
  eval "set -- $cmd"
  run_impl bash "$@"
  run_impl go "$@"
done

# ── assert per-step stdout/stderr/exit identical ─────────────────────
if diff -u <(mask < "$ROOT/bash.steps") <(mask < "$ROOT/go.steps") > "$ROOT/steps.diff"; then
  ok "per-command stdout/stderr/exit identical across all $(( ${#SEQ[@]} + ${#ERRSEQ[@]} )) steps"
else
  fail "per-command output diverged" "$(head -40 "$ROOT/steps.diff")"
fi

# ── env: deliberately no longer byte-identical ───────────────────────
# `env` absorbed the retired top-level `slug` command's derivation half, so it
# now emits SLUG/BRANCH/BABYSIT_PROJECT_HOME ahead of the three keys the bash
# printed. The oracle is frozen and can never grow them, so this is asserted as
# a superset instead of a diff: every bash line still present, same values.
BASHENV="$( BABYSIT_PROJECT_HOME="$ROOT/bash-home/projects/slug" "$ROOT/bash-cmd" env \
            | sed "s|$ROOT/bash-home|X|" )"
GOENV="$( BABYSIT_PROJECT_HOME="$ROOT/go-home/projects/slug" "$ROOT/go-cmd" env \
          | sed "s|$ROOT/go-home|X|" )"
missing=""
while IFS= read -r ln; do
  [ -n "$ln" ] || continue
  case "$GOENV" in *"$ln"*) ;; *) missing="$missing $ln" ;; esac
done <<< "$BASHENV"
case "$GOENV" in SLUG=*) ;; *) missing="$missing SLUG=<absent>" ;; esac
if [ -z "$missing" ]; then
  ok "env is a superset of the bash keys (SLUG/BRANCH/PROJECT_HOME added)"
else
  fail "env diverged beyond the added keys" "missing:$missing"
fi

# ── assert resulting index.json JSON-equivalent ──────────────────────
BIDX="$ROOT/bash-home/projects/slug/tickets/bs-state01/index.json"
GIDX="$ROOT/go-home/projects/slug/tickets/bs-state01/index.json"
# Fields added to the schema *after* the port completed have no bash counterpart
# — the oracle is frozen, so it can never grow one. Drop them from both sides:
# this harness answers "does Go match bash on ported behavior", and post-port
# surface has its own tests (control → test_bbs_ticket_control.sh).
POST_PORT='del(.control)'
if [ -f "$BIDX" ] && [ -f "$GIDX" ]; then
  if diff -u <(jq -S "$POST_PORT" "$BIDX" | mask) <(jq -S "$POST_PORT" "$GIDX" | mask) > "$ROOT/idx.diff"; then
    ok "index.json semantically identical (jq -S)"
  else
    fail "index.json diverged" "$(cat "$ROOT/idx.diff")"
  fi
else
  fail "index.json missing" "bash=$BIDX go=$GIDX"
fi

# ── assert history.jsonl JSON-equivalent line-for-line ───────────────
BH="$ROOT/bash-home/projects/slug/tickets/bs-state01/history.jsonl"
GH="$ROOT/go-home/projects/slug/tickets/bs-state01/history.jsonl"
if [ -f "$BH" ] && [ -f "$GH" ]; then
  # Normalize each line through jq -S so key order / whitespace don't matter.
  norm() { while IFS= read -r ln; do printf '%s\n' "$ln" | jq -Sc . ; done < "$1" | mask; }
  if diff -u <(norm "$BH") <(norm "$GH") > "$ROOT/hist.diff"; then
    ok "history.jsonl semantically identical, line-for-line (jq -Sc)"
  else
    fail "history.jsonl diverged" "$(cat "$ROOT/hist.diff")"
  fi
else
  fail "history.jsonl missing" "bash=$BH go=$GH"
fi

# ── ensure-size estimate path (needs a git repo with a diff) ─────────
GITSCN() {
  local impl="$1" repo="$2"
  export BABYSIT_PROJECT_HOME="$ROOT/$impl-home/projects/slug"
  ( cd "$repo" && "$ROOT/$impl-cmd" ensure-size 2>"$ROOT/.err.$impl.size" )
}
# Build one repo, clone it per impl so both see an identical 1-file/≤20-loc diff.
GITSRC="$ROOT/gitsrc"
git init -q "$GITSRC"
( cd "$GITSRC"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git branch -M main
  printf 'a\nb\nc\n' > small.txt
  git add small.txt
  git -c user.email=t@t -c user.name=t commit -q -m "add small" )
git clone -q "$GITSRC" "$ROOT/bash-repo"
git clone -q "$GITSRC" "$ROOT/go-repo"
# Fresh ticket id so ticket_size is unset → forces the estimate branch.
export BABYSIT_TICKET="bs-sizecalc"
BS_SIZE="$(GITSCN bash "$ROOT/bash-repo")"; BS_SERR="$(cat "$ROOT/.err.bash.size")"
GO_SIZE="$(GITSCN go   "$ROOT/go-repo")";   GO_SERR="$(cat "$ROOT/.err.go.size")"
if [ "$BS_SIZE" = "$GO_SIZE" ] && [ "$BS_SERR" = "$GO_SERR" ]; then
  ok "ensure-size estimate identical (size=$GO_SIZE; stderr matched)"
else
  fail "ensure-size estimate diverged" "bash=[$BS_SIZE|$BS_SERR] go=[$GO_SIZE|$GO_SERR]"
fi

echo
echo "  $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || { printf '  failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; }
