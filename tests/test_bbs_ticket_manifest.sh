#!/usr/bin/env bash
# tests/test_bbs_ticket_manifest.sh — coverage for `bbs-ticket` manifest.yaml
# integration: init seeds it (single-mode), set-branch updates one repo,
# get-manifest reads back JSON, created_at is preserved across writes.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"
[ -x "$BBS_TICKET_BIN" ] || { echo "FAIL: bin not executable" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

mk_repo() {
  local d="$1" branch="$2"
  git init -q "$d"
  git -C "$d" -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git -C "$d" checkout -q -b "$branch"
}

# ── init-seeds-manifest-yaml-single-mode ───────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  mk_repo "$T/r" "feat/bs-mfst1_demo"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  "$BBS_TICKET_BIN" init >/dev/null 2>&1 || { echo "init failed"; exit 1; }
  TH="$("$SCRIPT_DIR/bin/bbs-slug" ticket-home)"
  M="$TH/manifest.yaml"
  [ -f "$M" ] || { echo "no manifest at $M"; exit 1; }
  grep -q "^version: 1$"        "$M" || { echo "missing version"; cat "$M"; exit 1; }
  grep -q "^ticket: bs-mfst1$"  "$M" || { echo "missing ticket id"; cat "$M"; exit 1; }
  grep -q "^repos:"             "$M" || { echo "missing repos:"; cat "$M"; exit 1; }
  grep -qE "^  - name: "        "$M" || { echo "missing repo entry"; cat "$M"; exit 1; }
) && ok "init-seeds-manifest-yaml-single-mode" || fail "init-seeds-manifest-yaml-single-mode"
rm -rf "$T"

# ── set-branch-updates-one-repo ────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  mk_repo "$T/r" "feat/bs-mfst2_demo"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  "$BBS_TICKET_BIN" init >/dev/null 2>&1 || exit 1
  TH="$("$SCRIPT_DIR/bin/bbs-slug" ticket-home)"
  M="$TH/manifest.yaml"
  # Read existing repo name out of the manifest
  REPO_NAME="$(awk '/^  - name:/ {sub(/^  - name:[[:space:]]*/, ""); print; exit}' "$M")"
  CREATED_BEFORE="$(awk '/^created_at:/ {print; exit}' "$M")"
  sleep 1
  "$BBS_TICKET_BIN" set-branch "bs-mfst2" "$REPO_NAME" "feat/new-branch" >/dev/null 2>&1 \
    || { echo "set-branch failed"; cat "$M"; exit 1; }
  grep -q "branch: feat/new-branch" "$M" || { echo "branch not updated"; cat "$M"; exit 1; }
  CREATED_AFTER="$(awk '/^created_at:/ {print; exit}' "$M")"
  [ "$CREATED_BEFORE" = "$CREATED_AFTER" ] || { echo "created_at changed: $CREATED_BEFORE → $CREATED_AFTER"; exit 1; }
  grep -q "^updated_at:" "$M" || { echo "missing updated_at"; cat "$M"; exit 1; }
) && ok "set-branch-updates-one-repo" || fail "set-branch-updates-one-repo"
rm -rf "$T"

# ── get-manifest-emits-json ────────────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  mk_repo "$T/r" "feat/bs-mfst3_demo"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  "$BBS_TICKET_BIN" init >/dev/null 2>&1 || exit 1
  out="$("$BBS_TICKET_BIN" get-manifest "bs-mfst3" 2>/dev/null)" || { echo "get-manifest failed"; exit 1; }
  printf '%s' "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["ticket"]=="bs-mfst3", d; assert isinstance(d["repos"], list) and d["repos"], d' \
    || { echo "json malformed: $out"; exit 1; }
) && ok "get-manifest-emits-json" || fail "get-manifest-emits-json"
rm -rf "$T"

# ── set-branch-unknown-repo-errors ─────────────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  mk_repo "$T/r" "feat/bs-mfst4_demo"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"
  "$BBS_TICKET_BIN" init >/dev/null 2>&1 || exit 1
  err="$("$BBS_TICKET_BIN" set-branch "bs-mfst4" "no-such-repo" "main" 2>&1 1>/dev/null)"; rc=$?
  [ "$rc" -ne 0 ] || { echo "expected non-zero rc, got 0; err=$err"; exit 1; }
) && ok "set-branch-unknown-repo-errors" || fail "set-branch-unknown-repo-errors"
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
