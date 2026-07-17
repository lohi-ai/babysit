#!/usr/bin/env bash
# tests/test_bbs_ticket_refresh.sh — coverage for bin/bbs-ticket § refresh.
#
# refresh is the one sanctioned way to pull latest base into a ticket branch:
# fetch + merge origin/<base>. It must never reference local <base> — under
# worktree mode local base is a pile of other tickets' integration merges.
#
# Scenarios:
#   refresh-noop            branch already contains origin/main → UPDATED=0
#   refresh-pulls-latest    origin/main advanced → UPDATED=1, new file present
#   refresh-ignores-local-pile  local main ahead (integration merge), origin
#                           unchanged → UPDATED=0, pile NOT merged in
#   refresh-conflict-BLOCK  conflicting origin change → BLOCKED, merge aborted,
#                           branch and tree untouched
#   refresh-dirty-BLOCK     uncommitted changes → BLOCKED
#   refresh-on-base-refuses on the base branch → exit 2

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Fixture: bare remote, clone `repo` on a ticket branch cut from origin/main,
# and a second clone `other` for advancing origin.
build_repo() {
  local t="$1"
  git init -q --bare "$t/remote.git"
  git init -q "$t/repo"
  (
    cd "$t/repo"
    echo "base" > base.txt
    git add base.txt
    git -c user.email=t@t -c user.name=t commit -q -m init
    git branch -M main
    git remote add origin "$t/remote.git"
    git push -q origin main
    git checkout -q --no-track -b feat/bs-test_x origin/main
  )
  git -C "$t/remote.git" symbolic-ref HEAD refs/heads/main
  git clone -q "$t/remote.git" "$t/other"
}

advance_origin() {  # $1=tmpdir $2=file $3=content
  (
    cd "$1/other"
    echo "$3" > "$2"
    git add "$2"
    git -c user.email=o@o -c user.name=o commit -q -m "origin advance: $2"
    git push -q origin main
  )
}

# ── refresh-noop ──────────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  out="$("$BBS_TICKET_BIN" refresh 2>"$T/err")" || {
    echo "refresh failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^UPDATED=0$' \
    || { echo "expected UPDATED=0; out: $out"; exit 1; }
) && ok "refresh-noop" || fail "refresh-noop"
rm -rf "$T"

# ── refresh-pulls-latest ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  advance_origin "$T" upstream.txt "new upstream work"
  cd "$T/repo"
  echo "ticket work" > ticket.txt
  git add ticket.txt && git -c user.email=t@t -c user.name=t commit -q -m "ticket work"

  out="$("$BBS_TICKET_BIN" refresh 2>"$T/err")" || {
    echo "refresh failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^UPDATED=1$' \
    || { echo "expected UPDATED=1; out: $out"; exit 1; }
  [ -f upstream.txt ] || { echo "upstream.txt missing after refresh"; exit 1; }
  [ -f ticket.txt ] || { echo "ticket.txt lost by refresh"; exit 1; }
  [ "$(git branch --show-current)" = "feat/bs-test_x" ] \
    || { echo "refresh moved the checkout"; exit 1; }
) && ok "refresh-pulls-latest" || fail "refresh-pulls-latest"
rm -rf "$T"

# ── refresh-ignores-local-pile ────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  # Local main gains an integration merge (another ticket landed locally);
  # origin/main is unchanged.
  git checkout -q main
  git checkout -q -b feat/pile
  echo "pile" > pile.txt
  git add pile.txt && git -c user.email=t@t -c user.name=t commit -q -m "pile ticket"
  git checkout -q main
  git -c user.email=t@t -c user.name=t merge -q --no-ff feat/pile -m "integrate pile"
  git checkout -q feat/bs-test_x

  out="$("$BBS_TICKET_BIN" refresh 2>"$T/err")" || {
    echo "refresh failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^UPDATED=0$' \
    || { echo "expected UPDATED=0 (origin unchanged); out: $out"; exit 1; }
  [ ! -e pile.txt ] || { echo "refresh merged the local pile in"; exit 1; }
) && ok "refresh-ignores-local-pile" || fail "refresh-ignores-local-pile"
rm -rf "$T"

# ── refresh-conflict-BLOCK ────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  advance_origin "$T" base.txt "origin version"
  cd "$T/repo"
  echo "ticket version" > base.txt
  git add base.txt && git -c user.email=t@t -c user.name=t commit -q -m "ticket edit"
  pre="$(git rev-parse HEAD)"

  out="$("$BBS_TICKET_BIN" refresh 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q 'STATUS: BLOCKED' \
    || { echo "expected BLOCKED; got: $out"; exit 1; }
  printf '%s\n' "$out" | grep -qi 'conflict' \
    || { echo "expected conflict reason; got: $out"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$pre" ] || { echo "branch moved despite abort"; exit 1; }
  [ -z "$(git status --porcelain)" ] || { echo "tree dirty after abort"; exit 1; }
) && ok "refresh-conflict-BLOCK" || fail "refresh-conflict-BLOCK"
rm -rf "$T"

# ── refresh-dirty-BLOCK ───────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  echo "uncommitted" > dirty.txt

  out="$("$BBS_TICKET_BIN" refresh 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -qi 'uncommitted' \
    || { echo "expected uncommitted reason; got: $out"; exit 1; }
) && ok "refresh-dirty-BLOCK" || fail "refresh-dirty-BLOCK"
rm -rf "$T"

# ── refresh-on-base-refuses ───────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  build_repo "$T"
  cd "$T/repo"
  git checkout -q main

  out="$("$BBS_TICKET_BIN" refresh 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on base branch, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -qi 'base branch' \
    || { echo "expected on-base refusal; got: $out"; exit 1; }
) && ok "refresh-on-base-refuses" || fail "refresh-on-base-refuses"
rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
