#!/usr/bin/env bash
# tests/test_bbs_ticket_manifest_differential.sh — the Go manifest.yaml port must
# behave identically to the frozen bash oracle.
#
# Covers the slice ported in internal/cmd/ticket_manifest.go +
# internal/ticket/manifest.go: init (index.json seed + manifest.yaml seed),
# get-manifest, set-branch. `ensure` and the base-ops stay bash-delegated and are
# out of scope here.
#
# Method: run each command against the Go binary (bin/bbs-ticket) and the frozen
# bash reference (tests/fixtures/bbs-ticket.reference), each inside its own clone
# of one source repo (so bbs-slug derives an identical SLUG/BRANCH) and pinned to
# its own BABYSIT_PROJECT_HOME. Assert per-command stdout/stderr/exit match, then
# assert the resulting index.json (jq -S), manifest.yaml (timestamps masked), and
# history.jsonl (jq -Sc) are equivalent.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
GO_BIN="$SCRIPT_DIR/bin/bbs-ticket"
REF="$SCRIPT_DIR/tests/fixtures/bbs-ticket.reference"

command -v jq >/dev/null 2>&1 || { echo "SKIP: jq not installed"; exit 0; }
[ -x "$GO_BIN" ] || { echo "FAIL: $GO_BIN not built (go build -o bin/bbs ./cmd/bbs)"; exit 1; }
[ -f "$REF" ]    || { echo "FAIL: missing $REF"; exit 1; }

# The oracle is frozen at the 2026-07-18 port, so index.json fields the schema
# gained afterwards (ticket control state, approvals, design/prototype pointers)
# are absent from it and a raw diff reports every later feature as a regression.
# Project the candidate onto the key names the oracle uses anywhere: no field
# the oracle knows about may diverge — in value or in absence — while additive
# growth is tolerated and named rather than silently excused.
project_onto_oracle() { # stdin = candidate JSON; $1 = oracle JSON file
  jq -S --argjson keys "$(jq -c '[paths | .[-1] | select(type == "string")] | unique' "$1")" '
    walk(if type == "object"
         then with_entries(select(.key as $k | $keys | index($k)))
         else . end)'
}

added_since_oracle() { # $1 = oracle JSON file, $2 = candidate JSON file
  jq -r -n --slurpfile o "$1" --slurpfile c "$2" '
    ([$c[0] | paths | .[-1] | select(type == "string")] | unique)
    - ([$o[0] | paths | .[-1] | select(type == "string")] | unique)
    | join(", ")'
}

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

export HOME="$ROOT/home"
export BABYSIT_HOME="$ROOT/home/.babysit"
export PATH="$SCRIPT_DIR/bin:$PATH"
export BBS_LIB="$SCRIPT_DIR/bin/lib"
unset BBS_TICKET BABYSIT_TICKET AGENT_ROLE GT_ROLE 2>/dev/null || true
mkdir -p "$HOME"

# Mask timestamps (JSON "…Z" and YAML created_at/updated_at lines) and the
# per-impl project-home/repo prefixes (bash vs go is an expected path difference).
mask() {
  sed -E -e 's/"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z"/"TS"/g' \
         -e "s/^(created_at|updated_at): .*/\\1: TS/" \
         -e "s#$ROOT/bash-home#IMPLHOME#g" \
         -e "s#$ROOT/go-home#IMPLHOME#g" \
         -e "s#$ROOT/bash-repo#IMPLREPO#g" \
         -e "s#$ROOT/go-repo#IMPLREPO#g"
}

# One source repo cloned per impl → identical SLUG (remote basename) + BRANCH.
GITSRC="$ROOT/gitsrc"
git init -q "$GITSRC"
( cd "$GITSRC"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git branch -M main
  git checkout -q -b feat/bs-mfst1_demo )
git clone -q "$GITSRC" "$ROOT/bash-repo" 2>/dev/null
git clone -q "$GITSRC" "$ROOT/go-repo" 2>/dev/null
# Clones land on the default branch (main); put both on the ticket branch.
( cd "$ROOT/bash-repo" && git checkout -q feat/bs-mfst1_demo )
( cd "$ROOT/go-repo"   && git checkout -q feat/bs-mfst1_demo )

printf '#!/usr/bin/env bash\nexec "%s" "$@"\n' "$GO_BIN" > "$ROOT/go-cmd"
printf '#!/usr/bin/env bash\nexec bash "%s" "$@"\n' "$REF" > "$ROOT/bash-cmd"
chmod +x "$ROOT/go-cmd" "$ROOT/bash-cmd"

# run_step <impl> <ticket> <args…> — run one command in the impl's repo clone,
# pinned to the impl's project home, logging out/err/rc to $impl.steps.
run_step() {
  local impl="$1" ticket="$2"; shift 2
  local out err rc
  out="$( cd "$ROOT/$impl-repo" && \
    env BABYSIT_TICKET="$ticket" \
        BABYSIT_PROJECT_HOME="$ROOT/$impl-home/projects/gitsrc" \
        "$ROOT/$impl-cmd" "$@" 2>"$ROOT/.err" )"; rc=$?
  err="$(cat "$ROOT/.err")"
  {
    printf '### [%s] %s\n' "$ticket" "$*"
    printf 'RC=%s\n' "$rc"
    printf 'OUT=%s\n' "$out"
    printf 'ERR=%s\n' "$err"
  } >> "$ROOT/$impl.steps"
}

# both <ticket> <args…> — run the same step against both impls.
both() { run_step bash "$@"; run_step go "$@"; }

# ── the shared command sequence ───────────────────────────────────────
both bs-mfst1 init                                             # standalone fresh init
both bs-mfst1 get-manifest                                     # emit JSON
both bs-mfst1 get-manifest bs-mfst1                            # explicit-ticket arg
# The seeded repo name is the derived slug (mktemp-based); read it back so the
# set-branch success path targets a real entry rather than a guessed name.
REPO="$( cd "$ROOT/go-repo" && env BABYSIT_TICKET=bs-mfst1 \
  BABYSIT_PROJECT_HOME="$ROOT/go-home/projects/gitsrc" \
  "$ROOT/go-cmd" get-manifest | jq -r '.repos[0].name' )"
both bs-mfst1 set-branch bs-mfst1 "$REPO" feat/bs-mfst1_renamed # update the repo branch (success)
both bs-mfst1 get-manifest                                      # branch now reflects the rename
both bs-mfst1 set-branch bs-mfst1 no-such-repo feat/x           # unknown repo → exit 2
both bs-mfst1 init                                             # non-fresh: no 2nd history row, manifest untouched
both bs-mfst2 init --parent bs-mfst1 --origin-type child --seed s1 \
     --plan plan.md --design-doc design.md --position 2 --repo custom-repo --worktree wt
both bs-mfst2 get-manifest                                     # sub-ticket manifest reflects --repo/--worktree
both bs-nope  get-manifest                                     # missing manifest → exit 1
both bs-nope2 set-branch bs-nope2 gitsrc feat/y                # missing manifest → exit 1

# ── assert per-step stdout/stderr/exit identical ─────────────────────
if diff -u <(mask < "$ROOT/bash.steps") <(mask < "$ROOT/go.steps") > "$ROOT/steps.diff"; then
  ok "per-command stdout/stderr/exit identical across all steps"
else
  fail "per-command output diverged" "$(head -60 "$ROOT/steps.diff")"
fi

# ── assert resulting artifacts equivalent, per ticket ────────────────
cmp_artifacts() {
  local ticket="$1"
  local bdir="$ROOT/bash-home/projects/gitsrc/tickets/$ticket"
  local gdir="$ROOT/go-home/projects/gitsrc/tickets/$ticket"

  if [ -f "$bdir/index.json" ] && [ -f "$gdir/index.json" ]; then
    local new_keys; new_keys="$(added_since_oracle "$bdir/index.json" "$gdir/index.json")"
    [ -n "$new_keys" ] && printf '  note  %s\n' "$ticket index.json: added since the oracle was frozen (not compared): $new_keys"
    if diff -u <(jq -S . "$bdir/index.json" | mask) \
               <(project_onto_oracle "$bdir/index.json" < "$gdir/index.json" | mask) \
               > "$ROOT/idx.$ticket.diff"; then
      ok "$ticket index.json semantically identical (jq -S)"
    else
      fail "$ticket index.json diverged" "$(cat "$ROOT/idx.$ticket.diff")"
    fi
  else
    fail "$ticket index.json missing" "bash=$bdir go=$gdir"
  fi

  if [ -f "$bdir/manifest.yaml" ] && [ -f "$gdir/manifest.yaml" ]; then
    if diff -u <(mask < "$bdir/manifest.yaml") <(mask < "$gdir/manifest.yaml") > "$ROOT/mf.$ticket.diff"; then
      ok "$ticket manifest.yaml byte-identical (timestamps masked)"
    else
      fail "$ticket manifest.yaml diverged" "$(cat "$ROOT/mf.$ticket.diff")"
    fi
  else
    fail "$ticket manifest.yaml missing" "bash=$bdir go=$gdir"
  fi

  if [ -f "$bdir/history.jsonl" ] && [ -f "$gdir/history.jsonl" ]; then
    norm() { while IFS= read -r ln; do printf '%s\n' "$ln" | jq -Sc . ; done < "$1" | mask; }
    if diff -u <(norm "$bdir/history.jsonl") <(norm "$gdir/history.jsonl") > "$ROOT/hist.$ticket.diff"; then
      ok "$ticket history.jsonl semantically identical, line-for-line (jq -Sc)"
    else
      fail "$ticket history.jsonl diverged" "$(cat "$ROOT/hist.$ticket.diff")"
    fi
  else
    fail "$ticket history.jsonl missing" "bash=$bdir go=$gdir"
  fi
}
cmp_artifacts bs-mfst1
cmp_artifacts bs-mfst2

echo
echo "  $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || { printf '  failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; }
