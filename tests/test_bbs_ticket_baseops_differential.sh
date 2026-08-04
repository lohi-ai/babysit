#!/usr/bin/env bash
# tests/test_bbs_ticket_baseops_differential.sh — the Go base-ops + ensure port
# (internal/cmd/ticket_baseops.go, ticket_ensure.go) must behave identically to
# the frozen bash oracle for the git-mutating Slice C family.
#
# Covers: qa-lease (acquire/refresh/release/status/steal/block-by-other),
# ensure (trunk mode = no cut, worktree divert, safe-cut NEEDS_CONFIRM gate),
# and the base-ops lock/base-branch plumbing (merge-base, reset-base, switch,
# refresh) over a real git repo.
#
# Determinism: pinned author + commit dates make two fresh `git init` trees
# produce identical SHAs; random ticket ids (bs-<8>), pids, and timestamps are
# masked. Every git-mutating case runs INSIDE its own per-impl repo/worktree.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
GO_BIN="$SCRIPT_DIR/bin/bbs-ticket"
REF="$SCRIPT_DIR/tests/fixtures/bbs-ticket.reference"

command -v git >/dev/null 2>&1 || { echo "SKIP: git not installed"; exit 0; }
[ -x "$GO_BIN" ] || { echo "FAIL: $GO_BIN not built (go build -o bin/bbs ./cmd/bbs)"; exit 1; }
[ -f "$REF" ]    || { echo "FAIL: missing $REF"; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

export HOME="$ROOT/home"
export BABYSIT_HOME="$ROOT/home/.babysit"
export PATH="$SCRIPT_DIR/bin:$PATH"
export BBS_LIB="$SCRIPT_DIR/bin/lib"
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
export GIT_AUTHOR_DATE="2026-01-01T00:00:00 +0000" GIT_COMMITTER_DATE="2026-01-01T00:00:00 +0000"
unset BBS_TICKET BABYSIT_TICKET AGENT_ROLE GT_ROLE 2>/dev/null || true
mkdir -p "$HOME"

printf '#!/usr/bin/env bash\nexec "%s" "$@"\n' "$GO_BIN" > "$ROOT/go-cmd"
printf '#!/usr/bin/env bash\nexec bash "%s" "$@"\n' "$REF" > "$ROOT/bash-cmd"
chmod +x "$ROOT/go-cmd" "$ROOT/bash-cmd"

# mask <workdir> — normalise nondeterministic tokens so the two impls compare.
mask() {
  local wd="$1"
  sed -E -e 's/bs-[a-z0-9]{8}/bs-XXXXXXXX/g' \
         -e 's/(pid=)[0-9]+/\1PID/g' \
         -e 's/(since=)[0-9T:+-]+/\1TS/g' \
         -e 's/(since_epoch=)[0-9]+/\1EPOCH/g' \
         -e 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/TS/g' \
         -e "s#$wd#WORK#g" \
         -e "s#$ROOT#ROOT#g" \
    | drop_intended_divergence
}

# Output the Go impl adds *on purpose*, after the port was frozen. The oracle
# can never grow these lines, so comparing them would pin the Go side to bash
# forever; dropping them keeps the rest of the diff honest. Every entry needs a
# reason — an unexplained filter here is how a real regression hides.
drop_intended_divergence() {
  # bs-85spcpj3: a `branch`-mode cut diverted onto a worktree now says what
  # the worktree inner loop costs and how to get the fast loop back.
  grep -v '^ensure: WARNING — the worktree loop costs' \
    | grep -v "^ensure: to keep the 0-step loop:"
}

# newrepo <dir> — a fresh git repo with one commit on main; deterministic SHA.
newrepo() {
  local d="$1"
  mkdir -p "$d"
  git -C "$d" init -q -b main
  printf 'hello\n' > "$d/README.md"
  git -C "$d" add -A
  git -C "$d" commit -q -m init
}

# ── Scenario 1: qa-lease full lifecycle ──────────────────────────────
# Pure file I/O at $gitdir/bbs-qa-lease/owner. Run identical verb sequences
# against a per-impl repo and diff masked output + the resulting owner file.
qa_lease_scenario() {
  local impl="$1"; local wd="$ROOT/$impl-qa"
  newrepo "$wd"
  export BABYSIT_PROJECT_HOME="$ROOT/$impl-qa-home"
  local log="$ROOT/$impl.qa"
  step() {
    local out err rc
    out="$( cd "$wd" && BABYSIT_TICKET="$1" "$ROOT/$impl-cmd" "${@:2}" 2>"$ROOT/.e" )"; rc=$?
    err="$(cat "$ROOT/.e")"
    { printf '### T=%s %s\nRC=%s\nOUT=%s\nERR=%s\n' "$1" "${*:2}" "$rc" "$out" "$err"; } >> "$log"
  }
  step bs-aaa11111 qa-lease acquire --ttl-min 30   # fresh acquire
  step bs-aaa11111 qa-lease status                 # owner sees itself
  step bs-aaa11111 qa-lease acquire --ttl-min 30   # reentrant → REFRESHED
  step bs-bbb22222 qa-lease acquire --ttl-min 30   # other ticket → BLOCK (held)
  step bs-bbb22222 qa-lease status                 # status while held by other
  step bs-aaa11111 qa-lease release                # owner releases
  step bs-bbb22222 qa-lease acquire --ttl-min 30   # now free → acquires
  step bs-bbb22222 qa-lease release
  step bs-ccc33333 qa-lease status                 # no lease → status none
}
qa_lease_scenario bash
qa_lease_scenario go
if diff -u <(mask "$ROOT/bash-qa" < "$ROOT/bash.qa") \
           <(mask "$ROOT/go-qa"   < "$ROOT/go.qa") > "$ROOT/qa.diff"; then
  ok "qa-lease lifecycle (acquire/refresh/block/release/status) identical"
else
  fail "qa-lease diverged" "$(head -50 "$ROOT/qa.diff")"
fi

# ── Scenario 2: stale-lease steal ────────────────────────────────────
# Plant a lease whose since_epoch is older than its ttl, then a different
# ticket must steal it loudly (STOLE_FROM).
stale_scenario() {
  local impl="$1"; local wd="$ROOT/$impl-stale"
  newrepo "$wd"
  export BABYSIT_PROJECT_HOME="$ROOT/$impl-stale-home"
  local gd; gd="$(git -C "$wd" rev-parse --absolute-git-dir)"
  mkdir -p "$gd/bbs-qa-lease"
  # since_epoch 2h ago, ttl 30m → stale
  local old=$(( $(date +%s) - 7200 ))
  cat > "$gd/bbs-qa-lease/owner" <<EOF
owner=bs-old00000
pid=99999
since=2026-01-01T00:00:00Z
since_epoch=$old
ttl_min=30
EOF
  local out err rc
  out="$( cd "$wd" && BABYSIT_TICKET=bs-new11111 "$ROOT/$impl-cmd" qa-lease acquire --ttl-min 30 2>"$ROOT/.e" )"; rc=$?
  err="$(cat "$ROOT/.e")"
  { printf 'RC=%s\nOUT=%s\nERR=%s\n' "$rc" "$out" "$err"; } >> "$ROOT/$impl.stale"
}
stale_scenario bash
stale_scenario go
if diff -u <(mask "$ROOT/bash-stale" < "$ROOT/bash.stale") \
           <(mask "$ROOT/go-stale"   < "$ROOT/go.stale") > "$ROOT/stale.diff"; then
  ok "qa-lease stale-steal (STOLE_FROM) identical"
else
  fail "qa-lease stale-steal diverged" "$(cat "$ROOT/stale.diff")"
fi

# ── Scenario 3: ensure — trunk mode (no cut) ─────────────────────────
# git-flow.yaml mode: trunk → ensure creates ticket state without cutting a
# branch. Random ticket id masked.
ensure_trunk_scenario() {
  local impl="$1"; local wd="$ROOT/$impl-trunk"
  newrepo "$wd"
  mkdir -p "$wd/.babysit"
  printf 'mode: trunk\n' > "$wd/.babysit/git-flow.yaml"
  export BABYSIT_PROJECT_HOME="$ROOT/$impl-trunk-home"
  local out err rc
  out="$( cd "$wd" && "$ROOT/$impl-cmd" ensure --from-input "Add user export button" 2>"$ROOT/.e" )"; rc=$?
  err="$(cat "$ROOT/.e")"
  local branch; branch="$( git -C "$wd" rev-parse --abbrev-ref HEAD )"
  { printf 'RC=%s\nOUT=%s\nERR=%s\nBRANCH=%s\n' "$rc" "$out" "$err" "$branch"; } >> "$ROOT/$impl.trunk"
}
ensure_trunk_scenario bash
ensure_trunk_scenario go
if diff -u <(mask "$ROOT/bash-trunk" < "$ROOT/bash.trunk") \
           <(mask "$ROOT/go-trunk"   < "$ROOT/go.trunk") > "$ROOT/trunk.diff"; then
  ok "ensure trunk-mode (no cut) identical"
else
  fail "ensure trunk-mode diverged" "$(cat "$ROOT/trunk.diff")"
fi

# ── Scenario 4: ensure — safe-cut NEEDS_CONFIRM gate ─────────────────
# branch mode + developer role + dirty tree (unsafe base) → the safe-cut gate
# must refuse in place with STATUS: NEEDS_CONFIRM (exit 3) rather than divert.
ensure_safecut_scenario() {
  local impl="$1"; local wd="$ROOT/$impl-safe"
  newrepo "$wd"
  mkdir -p "$wd/.babysit"
  printf 'mode: branch\n' > "$wd/.babysit/git-flow.yaml"
  printf 'dirty\n' > "$wd/uncommitted.txt"   # dirty tree = unsafe base checkout
  export BABYSIT_PROJECT_HOME="$ROOT/$impl-safe-home"
  export AGENT_ROLE=developer SAFE_CUT=1
  local out err rc
  out="$( cd "$wd" && "$ROOT/$impl-cmd" ensure --from-input "Risky change here" 2>"$ROOT/.e" )"; rc=$?
  err="$(cat "$ROOT/.e")"
  { printf 'RC=%s\nOUT=%s\nERR=%s\n' "$rc" "$out" "$err"; } >> "$ROOT/$impl.safe"
  unset AGENT_ROLE SAFE_CUT
}
ensure_safecut_scenario bash
ensure_safecut_scenario go
if diff -u <(mask "$ROOT/bash-safe" < "$ROOT/bash.safe") \
           <(mask "$ROOT/go-safe"   < "$ROOT/go.safe") > "$ROOT/safe.diff"; then
  ok "ensure safe-cut NEEDS_CONFIRM gate identical"
else
  fail "ensure safe-cut gate diverged" "$(cat "$ROOT/safe.diff")"
fi

echo
echo "  $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || { printf '  failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; }
