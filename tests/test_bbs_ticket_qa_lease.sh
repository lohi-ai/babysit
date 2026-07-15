#!/usr/bin/env bash
# tests/test_bbs_ticket_qa_lease.sh — coverage for bin/bbs-ticket § qa-lease.
#
# qa-lease = exclusive QA-session lease on the shared test surface. While a
# ticket holds it, merge-base / switch / reset-base from any OTHER ticket
# BLOCK (qa_lease_guard), so a QA verdict always describes the surface it was
# measured on. Reentrant for the owner; stale leases (age > ttl) are stolen.
#
# Scenarios:
#   lease-acquire-status       acquire from a worktree → ACQUIRED=1; status
#                              shows OWNER/AGE_MIN/TTL_MIN; FREE after release
#   lease-reentrant            second acquire by the same owner → REFRESHED=1
#   lease-blocked-other-owner  B acquires while A holds → BLOCKED exit 2
#   lease-stale-steal          A's lease older than its ttl → B steals it,
#                              STOLE_FROM=A
#   lease-release-owner-only   B can't release A's lease (exit 2), --force
#                              can; release when free → FREE=1 exit 0
#   lease-blocks-merge-base    A holds → B's merge-base BLOCKs naming A;
#                              after A releases, the same merge-base lands
#   lease-blocks-surface-ops   A holds → switch/reset-base as B BLOCK; switch
#                              as A (own lease) passes
#   lease-stale-guard-clears   stale lease → B's merge-base clears it with a
#                              warning and proceeds

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Fixture: bare origin + clone with mode: worktree committed and pushed, then
# two worktree tickets A and B, each with one committed file. Sets globals
# WT_A/WT_B (worktree paths) and TK_A/TK_B (ticket ids). Runs in $1/repo.
build_two_tickets() {
  local t="$1"
  git init -q --bare "$t/remote.git"
  git init -q "$t/repo"
  (
    cd "$t/repo"
    git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
    git branch -M main
    git remote add origin "$t/remote.git"
    mkdir -p .babysit
    echo "mode: worktree" > .babysit/git-flow.yaml
    git add .babysit/git-flow.yaml
    git -c user.email=t@t -c user.name=t commit -q -m "git-flow config"
    git push -q origin main
  )
  cd "$t/repo"
  local out
  out="$("$BBS_TICKET_BIN" ensure --slug-hint tick-a --type feat 2>/dev/null)" || return 1
  TK_A="$(printf '%s\n' "$out" | sed -n 's|^TICKET=||p')"
  WT_A="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  out="$("$BBS_TICKET_BIN" ensure --slug-hint tick-b --type feat 2>/dev/null)" || return 1
  TK_B="$(printf '%s\n' "$out" | sed -n 's|^TICKET=||p')"
  WT_B="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$WT_A" ] && [ -n "$WT_B" ] || return 1
  ( cd "$WT_A" && echo "work a" > a.txt && git add a.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m "ticket a" ) || return 1
  ( cd "$WT_B" && echo "work b" > b.txt && git add b.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m "ticket b" ) || return 1
}

# Backdate the lease so it reads as stale: rewrite since_epoch in the owner
# file inside the shared git dir ($1 = repo path, $2 = seconds to age it by).
backdate_lease() {
  local ownerf="$1/.git/bbs-qa-lease/owner" aged
  aged=$(( $(date +%s) - $2 ))
  sed "s/^since_epoch=.*/since_epoch=$aged/" "$ownerf" > "$ownerf.tmp" \
    && mv "$ownerf.tmp" "$ownerf"
}

# ── lease-acquire-status ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  out="$(cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "acquire failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^ACQUIRED=1$" || { echo "no ACQUIRED=1; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^OWNER=$TK_A$" || { echo "expected OWNER=$TK_A; out: $out"; exit 1; }

  out="$("$BBS_TICKET_BIN" qa-lease status)"
  printf '%s\n' "$out" | grep -q "^OWNER=$TK_A$" || { echo "status lacks owner; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^TTL_MIN=60$" || { echo "status lacks default ttl; out: $out"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease release >/dev/null) || { echo "release failed"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] || { echo "expected FREE after release"; exit 1; }
) && ok "lease-acquire-status" || fail "lease-acquire-status"
rm -rf "$T"

# ── lease-reentrant ───────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "first acquire failed"; exit 1; }
  out="$(cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "re-acquire failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^REFRESHED=1$" || { echo "expected REFRESHED=1; out: $out"; exit 1; }
) && ok "lease-reentrant" || fail "lease-reentrant"
rm -rf "$T"

# ── lease-blocked-other-owner ─────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "A acquire failed"; exit 1; }
  (cd "$WT_B" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>"$T/err"); rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 for B, got $rc"; exit 1; }
  grep -q "held by '$TK_A'" "$T/err" || { echo "expected reason naming $TK_A: $(cat "$T/err")"; exit 1; }
  # A still owns it.
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_A$" || { echo "A lost the lease"; exit 1; }
) && ok "lease-blocked-other-owner" || fail "lease-blocked-other-owner"
rm -rf "$T"

# ── lease-stale-steal ─────────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "A acquire failed"; exit 1; }
  backdate_lease "$T/repo" 3900   # 65min old > 60min ttl
  out="$(cd "$WT_B" && "$BBS_TICKET_BIN" qa-lease acquire 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "steal failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^STOLE_FROM=$TK_A$" || { echo "expected STOLE_FROM=$TK_A; out: $out"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_B$" || { echo "B not the owner after steal"; exit 1; }
) && ok "lease-stale-steal" || fail "lease-stale-steal"
rm -rf "$T"

# ── lease-release-owner-only ──────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  # Release when free is a no-op, not an error.
  out="$("$BBS_TICKET_BIN" qa-lease release)"; rc=$?
  [ "$rc" -eq 0 ] && printf '%s\n' "$out" | grep -q "^FREE=1$" \
    || { echo "release-when-free should FREE=1 rc=0; rc=$rc out: $out"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "A acquire failed"; exit 1; }
  (cd "$WT_B" && "$BBS_TICKET_BIN" qa-lease release >/dev/null 2>"$T/err"); rc=$?
  [ "$rc" -eq 2 ] || { echo "B released A's lease (rc=$rc)"; exit 1; }
  grep -q "belongs to '$TK_A'" "$T/err" || { echo "expected ownership reason: $(cat "$T/err")"; exit 1; }
  (cd "$WT_B" && "$BBS_TICKET_BIN" qa-lease release --force >/dev/null 2>&1) || { echo "--force release failed"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] || { echo "lease survived --force release"; exit 1; }
) && ok "lease-release-owner-only" || fail "lease-release-owner-only"
rm -rf "$T"

# ── lease-blocks-merge-base ───────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "A acquire failed"; exit 1; }
  (cd "$WT_B" && "$BBS_TICKET_BIN" merge-base >/dev/null 2>"$T/err"); rc=$?
  [ "$rc" -eq 2 ] || { echo "B's merge-base should BLOCK, rc=$rc"; exit 1; }
  grep -q "qa-leased by '$TK_A'" "$T/err" || { echo "expected lease reason: $(cat "$T/err")"; exit 1; }
  [ ! -f b.txt ] || { echo "b.txt landed on primary despite lease"; exit 1; }

  # Owner's own merge-base passes through the guard.
  (cd "$WT_A" && "$BBS_TICKET_BIN" merge-base >/dev/null 2>"$T/err") || {
    echo "A's own merge-base failed under its lease: $(cat "$T/err")"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing after A's merge-base"; exit 1; }

  # Release, then B lands cleanly.
  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease release >/dev/null) || { echo "release failed"; exit 1; }
  (cd "$WT_B" && "$BBS_TICKET_BIN" merge-base >/dev/null 2>"$T/err") || {
    echo "B's merge-base failed after release: $(cat "$T/err")"; exit 1; }
  [ -f b.txt ] || { echo "b.txt missing after B's merge-base"; exit 1; }
) && ok "lease-blocks-merge-base" || fail "lease-blocks-merge-base"
rm -rf "$T"

# ── lease-blocks-surface-ops ──────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "A acquire failed"; exit 1; }
  pre="$(git rev-parse HEAD)"

  BABYSIT_TICKET="$TK_B" "$BBS_TICKET_BIN" switch "$TK_B" >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "switch as B should BLOCK, rc=$rc"; exit 1; }
  grep -q "qa-leased by '$TK_A'" "$T/err" || { echo "expected lease reason: $(cat "$T/err")"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$pre" ] || { echo "surface moved despite BLOCK"; exit 1; }

  BABYSIT_TICKET="$TK_B" "$BBS_TICKET_BIN" reset-base >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "reset-base as B should BLOCK, rc=$rc"; exit 1; }

  # The owner may re-point the surface under its own lease.
  out="$(BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" switch "$TK_A" 2>"$T/err")" || {
    echo "switch as owner failed: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVING=$TK_A$" || { echo "expected SERVING=$TK_A; out: $out"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing after owner switch"; exit 1; }
) && ok "lease-blocks-surface-ops" || fail "lease-blocks-surface-ops"
rm -rf "$T"

# ── lease-stale-guard-clears ──────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  (cd "$WT_A" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "A acquire failed"; exit 1; }
  backdate_lease "$T/repo" 3900
  (cd "$WT_B" && "$BBS_TICKET_BIN" merge-base >/dev/null 2>"$T/err") || {
    echo "B's merge-base should clear the stale lease and land: $(cat "$T/err")"; exit 1; }
  grep -q "stale qa-lease" "$T/err" || { echo "expected stale-clear warning: $(cat "$T/err")"; exit 1; }
  [ -f b.txt ] || { echo "b.txt missing after stale-clear merge-base"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] || { echo "stale lease not cleared"; exit 1; }
) && ok "lease-stale-guard-clears" || fail "lease-stale-guard-clears"
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
