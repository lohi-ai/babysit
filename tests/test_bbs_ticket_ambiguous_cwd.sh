#!/usr/bin/env bash
# tests/test_bbs_ticket_ambiguous_cwd.sh — two manifests claim the same cwd.
#
# The identity ladder (env → manifest cwd-match → branch) must abort on
# inferred identity, but project-only and explicit-ticket commands must not
# be blocked by an ambiguity they never needed.
#
# Scenarios:
#   resolve-aborts          `ticket resolve` exits 2 naming both candidates
#   board-succeeds          project-only command unaffected
#   get-manifest-explicit   explicit <ticket> unaffected
#   env-wins                BABYSIT_TICKET short-circuits the ambiguity

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS="$SCRIPT_DIR/bin/bbs"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

git init -q "$T/repo"
(
  cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git branch -M main
)

PH="$T/home/projects/repo"
for tid in bs-aaaa1111 bs-bbbb2222; do
  mkdir -p "$PH/tickets/$tid"
  cat > "$PH/tickets/$tid/index.json" <<EOF
{"id":"$tid","status":"in_progress"}
EOF
  cat > "$PH/tickets/$tid/manifest.yaml" <<EOF
version: 1
ticket: $tid
title: t
repos:
  - name: repo
    branch: main
    canonical: .
    worktree: $T/repo
    base: main
    pushed: "false"
EOF
done

cd "$T/repo"
export BABYSIT_PROJECT_HOME="$PH"
unset BABYSIT_TICKET BBS_TICKET 2>/dev/null || true

# resolve must abort with both candidates.
out=$("$BBS" ticket resolve 2>&1); rc=$?
if [ "$rc" -eq 2 ] && printf '%s' "$out" | grep -q 'bs-aaaa1111' && printf '%s' "$out" | grep -q 'bs-bbbb2222'; then
  ok resolve-aborts
else
  fail resolve-aborts "rc=$rc out=$out"
fi

# board is project-only — ambiguity must not block it.
if "$BBS" ticket board >/dev/null 2>&1; then
  ok board-succeeds
else
  fail board-succeeds "board exited $?"
fi

# get-manifest with an explicit ticket is unaffected.
if "$BBS" ticket get-manifest bs-aaaa1111 2>/dev/null | grep -q 'bs-aaaa1111'; then
  ok get-manifest-explicit
else
  fail get-manifest-explicit
fi

# env short-circuits the ambiguity entirely.
out=$(BABYSIT_TICKET=bs-aaaa1111 "$BBS" ticket resolve 2>/dev/null); rc=$?
if [ "$rc" -eq 0 ] && [ "$out" = "bs-aaaa1111" ]; then
  ok env-wins
else
  fail env-wins "rc=$rc out=$out"
fi

echo
if [ "$FAIL" -eq 0 ]; then
  printf 'PASS %d scenario(s)\n' "$PASS"
else
  printf 'FAIL %d/%d: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
