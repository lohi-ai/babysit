#!/usr/bin/env bash
# tests/test_bbs_ticket_surface_compose.sh — coverage for
# bin/bbs-ticket § surface compose.
#
# Single-repo trunk flow: the dev server runs in the primary checkout (on the
# base branch), tickets are implemented in linked worktrees, and before QA the
# ticket branch is composed onto the primary checkout so the server serves the
# change. compose is EXACT — it reverts the primary to origin/<base> and then
# merges exactly the named ticket branches — and it must BLOCK on any unsafe
# position instead of guessing.
#
# Scenarios:
#   happy-compose             worktree commit lands on primary main; SERVING=<t>
#   re-compose-idempotent     second run with no new commits → same HEAD, rc 0
#   fix-then-re-compose       new worktree commit after first compose lands too
#   conflict-BLOCK            stray non-merge commit on local base → rc 2,
#                             primary HEAD unchanged, nothing merged
#   worktree-dirty-BLOCK      uncommitted worktree change → rc 2
#   primary-dirty-BLOCK       uncommitted primary change → rc 2
#   primary-off-base-BLOCK    primary checked out elsewhere → rc 2
#   from-primary-BLOCK        bare compose from the primary with no ticket in
#                             scope → usage error, rc 2

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Fixture: clone on main + a linked ticket worktree with one committed change.
# Prints nothing; layout is $t/repo (primary, on main) and $t/repo/.babysit/
# worktrees/<name> (linked, on feat/bs-<name>_test).
build_repo_with_worktree() {
  local t="$1" name="$2"
  git init -q --bare "$t/remote.git"
  git init -q "$t/repo"
  (
    cd "$t/repo"
    echo base > base.txt
    git add base.txt
    git -c user.email=t@t -c user.name=t commit -q -m init
    git branch -M main
    git remote add origin "$t/remote.git"
    git push -q origin main
    mkdir -p .babysit/worktrees
    # ensure's safe-cut divert excludes the in-repo worktree dir; mirror that.
    echo '.babysit/worktrees/' >> .git/info/exclude
    git worktree add -q --no-track -b "feat/bs-${name}_test" ".babysit/worktrees/$name" main
  )
  (
    cd "$t/repo/.babysit/worktrees/$name"
    echo change > "change-$name.txt"
    git add "change-$name.txt"
    git -c user.email=t@t -c user.name=t commit -q -m "feat: $name change"
  )
}

test_env() {
  export PATH="$SCRIPT_DIR/bin:$PATH"
  export HOME="$1/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t
  export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
}

# ── happy-compose ─────────────────────────────────────────────────────
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mba
  cd "$T/repo/.babysit/worktrees/mba"

  out="$("$BBS_TICKET_BIN" surface compose 2>"$T/err")"; rc=$?
  [ "$rc" -eq 0 ] || { echo "expected rc=0, got $rc: $(cat "$T/err")"; exit 1; }
  printf '%s\n' "$out" | grep -q '^SERVING=bs-mba$' \
    || { echo "expected SERVING=bs-mba; out: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q '^BASE=main$' \
    || { echo "expected BASE=main; out: $out"; exit 1; }
  # The primary checkout now contains the worktree commit.
  git -C "$T/repo" log --oneline | grep -q 'mba change' \
    || { echo "primary main missing worktree commit"; exit 1; }
  [ "$(git -C "$T/repo" branch --show-current)" = "main" ] \
    || { echo "primary moved off main"; exit 1; }
  # The worktree itself is untouched (still on the ticket branch).
  [ "$(git branch --show-current)" = "feat/bs-mba_test" ] \
    || { echo "worktree moved off ticket branch"; exit 1; }
) && ok "happy-compose" || fail "happy-compose"
rm -rf "$T"

# ── re-compose-idempotent + fix-then-re-compose ───────────────────────
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mbb
  cd "$T/repo/.babysit/worktrees/mbb"

  "$BBS_TICKET_BIN" surface compose >/dev/null 2>&1 || { echo "first compose failed"; exit 1; }
  head1="$(git -C "$T/repo" rev-parse HEAD)"
  out="$("$BBS_TICKET_BIN" surface compose 2>/dev/null)"; rc=$?
  [ "$rc" -eq 0 ] || { echo "re-compose expected rc=0, got $rc"; exit 1; }
  printf '%s\n' "$out" | grep -q '^SERVING=bs-mbb$' \
    || { echo "expected SERVING=bs-mbb on re-compose; out: $out"; exit 1; }
  # Exact compose: revert to origin/main then merge the same branch lands the
  # same HEAD — no duplicate commit, no drift.
  [ "$(git -C "$T/repo" rev-parse HEAD)" = "$head1" ] \
    || { echo "re-compose moved HEAD"; exit 1; }

  # QA found a bug: fix in the worktree, commit, re-compose.
  echo fix > fix.txt
  git add fix.txt && git commit -q -m "fix: qa finding"
  out="$("$BBS_TICKET_BIN" surface compose 2>/dev/null)"; rc=$?
  [ "$rc" -eq 0 ] || { echo "fix re-compose expected rc=0, got $rc"; exit 1; }
  printf '%s\n' "$out" | grep -q '^SERVING=bs-mbb$' \
    || { echo "expected SERVING=bs-mbb after fix; out: $out"; exit 1; }
  git -C "$T/repo" log --oneline | grep -q 'qa finding' \
    || { echo "primary missing the fix commit"; exit 1; }
) && ok "re-compose-idempotent+fix" || fail "re-compose-idempotent+fix"
rm -rf "$T"

# ── conflict-BLOCK ────────────────────────────────────────────────────
# The conflicting commit lives on the primary's LOCAL base (unpushed, a plain
# non-merge commit). compose's revert step refuses to discard real work, so it
# BLOCKs on the stray commit before any merge is attempted.
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mbc
  # Conflicting edit on primary main: same file, different content.
  (
    cd "$T/repo/.babysit/worktrees/mbc"
    echo worktree-side > conflict.txt
    git add conflict.txt && git commit -q -m "feat: worktree side"
  )
  (
    cd "$T/repo"
    echo primary-side > conflict.txt
    git add conflict.txt && git commit -q -m "chore: primary side"
  )
  pre="$(git -C "$T/repo" rev-parse HEAD)"
  cd "$T/repo/.babysit/worktrees/mbc"

  out="$("$BBS_TICKET_BIN" surface compose 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q '^STATUS: BLOCKED$' \
    || { echo "no STATUS: BLOCKED; got: $out"; exit 1; }
  printf '%s\n' "$out" | grep -qi 'commits no other branch' \
    || { echo "no stray-commit reason; got: $out"; exit 1; }
  # Nothing moved: HEAD unchanged, no in-progress merge state, tree clean.
  [ "$(git -C "$T/repo" rev-parse HEAD)" = "$pre" ] \
    || { echo "primary HEAD moved despite BLOCK"; exit 1; }
  git -C "$T/repo" rev-parse -q --verify MERGE_HEAD >/dev/null 2>&1 \
    && { echo "MERGE_HEAD left behind"; exit 1; }
  [ -z "$(git -C "$T/repo" status --porcelain)" ] \
    || { echo "primary left dirty after BLOCK"; exit 1; }
) && ok "conflict-BLOCK" || fail "conflict-BLOCK"
rm -rf "$T"

# ── worktree-dirty-BLOCK ──────────────────────────────────────────────
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mbd
  cd "$T/repo/.babysit/worktrees/mbd"
  echo uncommitted > dirty.txt

  out="$("$BBS_TICKET_BIN" surface compose 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -qi 'uncommitted changes' \
    || { echo "no uncommitted-changes reason; got: $out"; exit 1; }
) && ok "worktree-dirty-BLOCK" || fail "worktree-dirty-BLOCK"
rm -rf "$T"

# ── primary-dirty-BLOCK ───────────────────────────────────────────────
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mbe
  echo uncommitted > "$T/repo/dirty.txt"
  cd "$T/repo/.babysit/worktrees/mbe"

  out="$("$BBS_TICKET_BIN" surface compose 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -qi 'primary checkout.*uncommitted' \
    || { echo "no primary-dirty reason; got: $out"; exit 1; }
) && ok "primary-dirty-BLOCK" || fail "primary-dirty-BLOCK"
rm -rf "$T"

# ── primary-off-base-BLOCK ────────────────────────────────────────────
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mbf
  git -C "$T/repo" checkout -q -b elsewhere
  cd "$T/repo/.babysit/worktrees/mbf"

  out="$("$BBS_TICKET_BIN" surface compose 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -q "is on 'elsewhere', not base 'main'" \
    || { echo "no off-base reason; got: $out"; exit 1; }
) && ok "primary-off-base-BLOCK" || fail "primary-off-base-BLOCK"
rm -rf "$T"

# ── from-primary-BLOCK ────────────────────────────────────────────────
# Bare compose needs a ticket in scope; from the primary checkout there is
# none, so this is a usage error, not a surface op.
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mbg
  cd "$T/repo"

  out="$("$BBS_TICKET_BIN" surface compose 2>&1)"; rc=$?
  [ "$rc" -eq 2 ] || { echo "expected rc=2, got $rc: $out"; exit 1; }
  printf '%s\n' "$out" | grep -qi 'usage:.*surface compose' \
    || { echo "no usage error; got: $out"; exit 1; }
) && ok "from-primary-BLOCK" || fail "from-primary-BLOCK"
rm -rf "$T"

# ── serving-set ───────────────────────────────────────────────────────
# compose records what it serves in <gitdir>/bbs-serving with set semantics:
# exactly the named tickets, replacing whatever was there. A bare compose from
# a worktree names only its own ticket; composing both names both.
T="$(mktemp -d)"
(
  test_env "$T"
  build_repo_with_worktree "$T" mbh
  (
    cd "$T/repo"
    git worktree add -q --no-track -b "feat/bs-mbi_test" .babysit/worktrees/mbi main
    cd .babysit/worktrees/mbi
    echo change > change-mbi.txt
    git add change-mbi.txt
    git commit -q -m "feat: mbi change"
  )
  GD="$(git -C "$T/repo" rev-parse --absolute-git-dir)"

  ( cd "$T/repo/.babysit/worktrees/mbh" && "$BBS_TICKET_BIN" surface compose >/dev/null 2>&1 ) \
    || { echo "compose mbh failed"; exit 1; }
  [ "$(cat "$GD/bbs-serving")" = "bs-mbh" ] \
    || { echo "expected serving=bs-mbh: $(cat "$GD/bbs-serving")"; exit 1; }
  # A bare compose from mbi's worktree replaces the surface, not appends.
  ( cd "$T/repo/.babysit/worktrees/mbi" && "$BBS_TICKET_BIN" surface compose >/dev/null 2>&1 ) \
    || { echo "compose mbi failed"; exit 1; }
  [ "$(cat "$GD/bbs-serving")" = "bs-mbi" ] \
    || { echo "expected serving=bs-mbi (set, not append): $(cat "$GD/bbs-serving")"; exit 1; }
  # Composing both tickets by name serves both.
  ( cd "$T/repo" && "$BBS_TICKET_BIN" surface compose bs-mbh bs-mbi >/dev/null 2>&1 ) \
    || { echo "compose mbh mbi failed"; exit 1; }
  [ "$(cat "$GD/bbs-serving")" = "bs-mbh,bs-mbi" ] \
    || { echo "expected serving=bs-mbh,bs-mbi: $(cat "$GD/bbs-serving")"; exit 1; }
  # Re-compose of a single ticket narrows the surface back to it.
  ( cd "$T/repo/.babysit/worktrees/mbh" && "$BBS_TICKET_BIN" surface compose >/dev/null 2>&1 ) \
    || { echo "re-compose mbh failed"; exit 1; }
  [ "$(cat "$GD/bbs-serving")" = "bs-mbh" ] \
    || { echo "expected serving=bs-mbh after re-compose: $(cat "$GD/bbs-serving")"; exit 1; }
) && ok "serving-set" || fail "serving-set"
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
