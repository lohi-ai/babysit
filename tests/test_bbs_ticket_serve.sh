#!/usr/bin/env bash
# tests/test_bbs_ticket_serve.sh — coverage for bin/bbs-ticket § serve.
#
# serve <ticket> = the human-review lever: long qa-lease (240 min default)
# + switch, in this repo and in each linked sibling repo, so the human can
# review the running feature while parallel agents keep working their own
# worktrees. Reentrant — re-run after each fix. serve --release frees the
# leases without touching the surface.
#
# Scenarios:
#   serve-acquires-and-switches   serve A → lease OWNER=A ttl 240, primary has
#                                 A's file; re-run is reentrant (rc 0, owner kept)
#   serve-blocked-by-other-lease  B holds the lease → rc 2 BLOCKED, owner
#                                 unchanged, primary untouched
#   serve-release-keeps-surface   --release frees the lease; surface still
#                                 serves A (no reset-base)
#   serve-switch-block-lease-hygiene
#                                 dirty primary → rc 2; a lease minted by this
#                                 serve is rolled back, a pre-existing (refreshed)
#                                 lease is kept
#   serve-sibling-fanout          sibling repo resolvable → both repos served,
#                                 both leases held; --release frees both
#   serve-sibling-unresolved      sibling path unset → NEEDS_CONTEXT rc 2, but
#                                 the primary side stays served
#   serve-multi-composes          serve A B → surface = base + both tickets,
#                                 one lease; reordered re-run keeps the owner
#   serve-bare-finished-batch     bare serve = every open ticket with qa +
#                                 review-pr DONE: nothing finished → note +
#                                 rc 0; A finished → A only; both → composed;
#                                 bare --release frees the lease

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

# Sibling fixture: a second worktree-mode repo at $1/repo2 with its own bare
# remote and one worktree ticket. Sets TK_S/WT_S. Leaves cwd unchanged.
build_sibling_repo() {
  local t="$1" out here; here="$(pwd)"
  git init -q --bare "$t/remote2.git"
  git init -q "$t/repo2"
  (
    cd "$t/repo2"
    git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
    git branch -M main
    git remote add origin "$t/remote2.git"
    mkdir -p .babysit
    echo "mode: worktree" > .babysit/git-flow.yaml
    git add .babysit/git-flow.yaml
    git -c user.email=t@t -c user.name=t commit -q -m "git-flow config"
    git push -q origin main
  )
  cd "$t/repo2"
  out="$("$BBS_TICKET_BIN" ensure --slug-hint sib-a --type feat 2>/dev/null)" || { cd "$here"; return 1; }
  TK_S="$(printf '%s\n' "$out" | sed -n 's|^TICKET=||p')"
  WT_S="$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')"
  [ -n "$WT_S" ] || { cd "$here"; return 1; }
  ( cd "$WT_S" && echo "sib work" > s.txt && git add s.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m "sibling ticket" ) || { cd "$here"; return 1; }
  cd "$here"
}

# ── serve-acquires-and-switches ───────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" serve "$TK_A" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "serve failed rc=$rc: $(cat "$T/err")"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing on primary after serve"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVED: repo $TK_A$" \
    || { echo "expected SERVED: repo $TK_A; out: $out"; exit 1; }
  st="$("$BBS_TICKET_BIN" qa-lease status)"
  printf '%s\n' "$st" | grep -q "^OWNER=$TK_A$" || { echo "lease owner wrong: $st"; exit 1; }
  printf '%s\n' "$st" | grep -q "^TTL_MIN=240$" || { echo "expected ttl 240: $st"; exit 1; }
  # Reentrant: the review-fix loop re-runs serve after each fix.
  "$BBS_TICKET_BIN" serve "$TK_A" >/dev/null 2>"$T/err" || { echo "re-serve failed: $(cat "$T/err")"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_A$" \
    || { echo "owner lost on re-serve"; exit 1; }
) && ok "serve-acquires-and-switches" || fail "serve-acquires-and-switches"
rm -rf "$T"

# ── serve-blocked-by-other-lease ──────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease acquire --ticket "$TK_B" >/dev/null 2>&1 || { echo "seed lease failed"; exit 1; }
  pre="$(git rev-parse HEAD)"

  "$BBS_TICKET_BIN" serve "$TK_A" >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 while B holds lease, got $rc"; exit 1; }
  grep -q "qa-lease held by" "$T/err" || { echo "expected lease-held reason: $(cat "$T/err")"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_B$" \
    || { echo "B's lease disturbed"; exit 1; }
  [ "$(git rev-parse HEAD)" = "$pre" ] || { echo "primary changed despite BLOCK"; exit 1; }
  [ ! -f a.txt ] || { echo "A merged despite BLOCK"; exit 1; }
) && ok "serve-blocked-by-other-lease" || fail "serve-blocked-by-other-lease"
rm -rf "$T"

# ── serve-release-keeps-surface ───────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  "$BBS_TICKET_BIN" serve "$TK_A" >/dev/null 2>&1 || { echo "serve failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" serve --release "$TK_A" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "release failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^RELEASED: repo $TK_A$" \
    || { echo "expected RELEASED line; out: $out"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] || { echo "lease not freed"; exit 1; }
  # --release never reset-bases: the surface still serves A.
  [ -f a.txt ] || { echo "release reset the surface"; exit 1; }
) && ok "serve-release-keeps-surface" || fail "serve-release-keeps-surface"
rm -rf "$T"

# ── serve-switch-block-lease-hygiene ──────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  # Fresh lease minted by a serve whose switch BLOCKs → rolled back.
  echo "uncommitted" > dirty.txt
  "$BBS_TICKET_BIN" serve "$TK_A" >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on dirty primary, got $rc"; exit 1; }
  grep -q "uncommitted changes" "$T/err" || { echo "expected dirty reason: $(cat "$T/err")"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] \
    || { echo "stray lease left by failed serve"; exit 1; }

  # Pre-existing lease (this serve only refreshed it) → kept on failure.
  rm dirty.txt
  "$BBS_TICKET_BIN" serve "$TK_A" >/dev/null 2>&1 || { echo "clean serve failed"; exit 1; }
  echo "uncommitted again" > dirty.txt
  "$BBS_TICKET_BIN" serve "$TK_A" >/dev/null 2>&1; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on re-serve over dirty, got $rc"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_A$" \
    || { echo "refreshed lease wrongly released"; exit 1; }
) && ok "serve-switch-block-lease-hygiene" || fail "serve-switch-block-lease-hygiene"
rm -rf "$T"

# ── serve-sibling-fanout ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  build_sibling_repo "$T" || { echo "sibling fixture failed"; exit 1; }
  echo "RELATED_BACKEND_REPO=$T/repo2" >> .babysit/.env
  echo ".babysit/.env" >> .git/info/exclude   # real repos gitignore it (setup-project)
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-sibling \
    --role be --repo repo2 --ticket "$TK_S" >/dev/null 2>&1 || { echo "set-sibling failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" serve "$TK_A" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "serve failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVED: repo $TK_A$" || { echo "primary not served; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVED: repo2 $TK_S$" || { echo "sibling not served; out: $out"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing on primary"; exit 1; }
  [ -f "$T/repo2/s.txt" ] || { echo "s.txt missing on sibling primary"; exit 1; }
  ( cd "$T/repo2" && "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_S$" ) \
    || { echo "sibling lease not held"; exit 1; }

  out="$("$BBS_TICKET_BIN" serve --release "$TK_A" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "release failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^RELEASED: repo2 $TK_S$" || { echo "sibling not released; out: $out"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] || { echo "primary lease not freed"; exit 1; }
  [ "$(cd "$T/repo2" && "$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] \
    || { echo "sibling lease not freed"; exit 1; }
) && ok "serve-sibling-fanout" || fail "serve-sibling-fanout"
rm -rf "$T"

# ── serve-sibling-unresolved ──────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-sibling \
    --role be --repo ghost-api --ticket bs-ghost000 >/dev/null 2>&1 || { echo "set-sibling failed"; exit 1; }

  "$BBS_TICKET_BIN" serve "$TK_A" >/dev/null 2>"$T/err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2 on unresolved sibling, got $rc"; exit 1; }
  grep -q "STATUS: NEEDS_CONTEXT" "$T/err" || { echo "expected NEEDS_CONTEXT: $(cat "$T/err")"; exit 1; }
  # Partial success: the primary side is served and its lease held.
  [ -f a.txt ] || { echo "primary not served despite partial success"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_A$" \
    || { echo "primary lease missing"; exit 1; }
) && ok "serve-sibling-unresolved" || fail "serve-sibling-unresolved"
rm -rf "$T"

# ── serve-multi-composes ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  out="$("$BBS_TICKET_BIN" serve "$TK_A" "$TK_B" 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "serve A B failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVED: repo $TK_A,$TK_B$" \
    || { echo "expected SERVED: repo $TK_A,$TK_B; out: $out"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing after composed serve"; exit 1; }
  [ -f b.txt ] || { echo "b.txt missing after composed serve"; exit 1; }
  st="$("$BBS_TICKET_BIN" qa-lease status)"
  printf '%s\n' "$st" | grep -q "^OWNER=$TK_A$" || { echo "expected first ticket to own: $st"; exit 1; }
  printf '%s\n' "$st" | grep -q "^TTL_MIN=240$" || { echo "expected ttl 240: $st"; exit 1; }
  # Reordered re-serve stays reentrant: the live owner is in the set → kept.
  "$BBS_TICKET_BIN" serve "$TK_B" "$TK_A" >/dev/null 2>"$T/err" \
    || { echo "reordered re-serve failed: $(cat "$T/err")"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_A$" \
    || { echo "owner changed on reordered re-serve"; exit 1; }
  [ -f a.txt ] && [ -f b.txt ] || { echo "surface lost a ticket on re-serve"; exit 1; }
) && ok "serve-multi-composes" || fail "serve-multi-composes"
rm -rf "$T"

# ── serve-bare-finished-batch ─────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_two_tickets "$T" || { echo "fixture failed"; exit 1; }

  # Nothing finished yet → note on stderr, rc 0, no lease taken.
  out="$("$BBS_TICKET_BIN" serve 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "bare serve with nothing finished rc=$rc"; exit 1; }
  grep -q "nothing finished" "$T/err" || { echo "expected nothing-finished note: $(cat "$T/err")"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] || { echo "lease taken with nothing to serve"; exit 1; }

  # A finished (qa + review-pr DONE), B only qa → bare serve = A alone.
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-verdict --skill qa --body "STATUS: DONE" >/dev/null
  BABYSIT_TICKET="$TK_A" "$BBS_TICKET_BIN" set-verdict --skill review-pr --body "STATUS: DONE" >/dev/null
  BABYSIT_TICKET="$TK_B" "$BBS_TICKET_BIN" set-verdict --skill qa --body "STATUS: DONE" >/dev/null
  out="$("$BBS_TICKET_BIN" serve 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "bare serve failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVED: repo $TK_A$" \
    || { echo "expected SERVED: repo $TK_A; out: $out"; exit 1; }
  [ -f a.txt ] || { echo "a.txt missing"; exit 1; }
  [ ! -f b.txt ] || { echo "unfinished B served"; exit 1; }

  # B finishes → bare re-serve composes both; owner stays A (reentrant).
  BABYSIT_TICKET="$TK_B" "$BBS_TICKET_BIN" set-verdict --skill review-pr --body "STATUS: DONE_WITH_CONCERNS" >/dev/null
  out="$("$BBS_TICKET_BIN" serve 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "bare re-serve failed rc=$rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q "^SERVED: repo " || { echo "no SERVED line; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep "^SERVED: repo " | grep -q "$TK_A" || { echo "A missing from batch; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep "^SERVED: repo " | grep -q "$TK_B" || { echo "B missing from batch; out: $out"; exit 1; }
  [ -f a.txt ] && [ -f b.txt ] || { echo "composed surface incomplete"; exit 1; }
  "$BBS_TICKET_BIN" qa-lease status | grep -q "^OWNER=$TK_A$" \
    || { echo "owner changed when batch grew"; exit 1; }

  # Bare release frees the lease and leaves the surface alone.
  "$BBS_TICKET_BIN" serve --release >/dev/null 2>"$T/err" || { echo "bare release failed: $(cat "$T/err")"; exit 1; }
  [ "$("$BBS_TICKET_BIN" qa-lease status)" = "FREE" ] || { echo "lease not freed"; exit 1; }
  [ -f a.txt ] && [ -f b.txt ] || { echo "release reset the surface"; exit 1; }
) && ok "serve-bare-finished-batch" || fail "serve-bare-finished-batch"
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
