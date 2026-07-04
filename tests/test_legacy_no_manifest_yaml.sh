#!/usr/bin/env bash
# tests/test_legacy_no_manifest_yaml.sh — backward-compat guard.
#
# Pre-existing ticket directories (created before manifest.yaml became the
# identity anchor) only have index.json + per-skill artifacts. `resolve`
# must still recover the ticket id via the branch regex — the branch
# fallback is the safety net that keeps existing trees readable.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"
BBS_SLUG_BIN="$SCRIPT_DIR/bin/bbs-slug"
[ -x "$BBS_TICKET_BIN" ] && [ -x "$BBS_SLUG_BIN" ] || { echo "FAIL" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# ── legacy-ticket-dir-resolves-via-branch ──────────────────────────────
T="$(mktemp -d)"
(
  HOME="$T/h"; export HOME; mkdir -p "$HOME"
  unset BABYSIT_TICKET BBS_TICKET
  git init -q "$T/r"
  git -C "$T/r" -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git -C "$T/r" checkout -q -b "feat/bs-legacy_topic"
  cd "$T/r"
  PATH="$SCRIPT_DIR/bin:$PATH"

  SLUG="$("$BBS_SLUG_BIN" slug)"
  TH="$HOME/.babysit/projects/$SLUG/tickets/bs-legacy"
  mkdir -p "$TH/handoffs"
  # Mimic legacy artifacts — no manifest.yaml.
  echo '{"id": "bs-legacy"}' > "$TH/index.json"
  touch "$TH/plan.md"

  out="$("$BBS_TICKET_BIN" resolve)"
  [ "$out" = "bs-legacy" ] || { echo "got: $out"; exit 1; }

  # And get-manifest must error cleanly (rc=1, not crash).
  err="$("$BBS_TICKET_BIN" get-manifest "bs-legacy" 2>&1 1>/dev/null)"; rc=$?
  [ "$rc" = "1" ] || { echo "expected rc=1, got rc=$rc; err=$err"; exit 1; }
  printf '%s' "$err" | grep -q "no manifest" || { echo "unexpected error: $err"; exit 1; }
) && ok "legacy-ticket-dir-resolves-via-branch" || fail "legacy-ticket-dir-resolves-via-branch"
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
