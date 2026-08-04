#!/usr/bin/env bash
# tests/test_bbs_parallel_tickets.sh — git-flow + qa flow with several tickets
# in flight at once. foreman dispatches a batch of workers against one primary
# checkout, so "two runs hit the same surface at the same moment" is the
# designed usage rather than an edge case; this suite covers the interactions
# no single-ticket test can see.
#
# Scenarios:
#   ensure-race-distinct-tickets    N concurrent ensure → N distinct ids/worktrees
#   ensure-serializes-worktree-add  the cut waits on the add-lock rather than
#                                   running `git worktree add` alongside it
#   merge-base-race-all-land        N worktrees land at once → all merged, serving
#                                   lists every ticket
#   land-while-a-peer-merges        a peer mid-merge does not read as the
#                                   operator's own uncommitted work
#   merge-base-vs-reset-base        landing races a reset → serving never claims a
#                                   ticket whose commit is not in HEAD
#   switch-race-serving-truth       the merge lock is never free between a
#                                   switch's reset and its merge landing
#   lease-blocks-parallel-ops       lease held → a burst of surface-movers all
#                                   BLOCK and the primary does not move
#   lease-not-granted-mid-merge     acquire cannot take the lease while a merge is
#                                   already in flight (see the note on the test)
#   force-release-vs-acquire        release --force racing acquire → at most one
#                                   owner, never a lease dir without an owner file
#   no-lock-leaked                  no code path leaves a lock dir behind (one
#                                   leak wedges every surface op for good)
#   mixed-op-stress                 randomized concurrent traffic → all of the
#                                   above invariants hold together

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Bare origin + clone on mode: worktree, main pushed.
build_repo() {
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
}

# build_tickets <tmpdir> <n> — n worktree tickets, each with one distinct
# committed file f<i>.txt so they never conflict with each other. Fills TKS/WTS.
build_tickets() {
  local t="$1" n="$2" i out
  TKS=(); WTS=()
  cd "$t/repo"
  for i in $(seq 1 "$n"); do
    out="$("$BBS_TICKET_BIN" ensure --slug-hint "tick-$i" --type feat 2>/dev/null)" || return 1
    TKS+=("$(printf '%s\n' "$out" | sed -n 's|^TICKET=||p')")
    WTS+=("$(printf '%s\n' "$out" | sed -n 's|^WORKTREE=||p')")
  done
  for i in $(seq 1 "$n"); do
    ( cd "${WTS[$((i-1))]}" && echo "work $i" > "f$i.txt" && git add "f$i.txt" \
        && git -c user.email=t@t -c user.name=t commit -q -m "ticket $i" ) || return 1
  done
}

serving_of() { tr -d '\n' < "$1/.git/bbs-serving" 2>/dev/null; }

# ── ensure-race-distinct-tickets ──────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; cd "$T/repo"

  # 16 and not 6: `git worktree add` is not concurrency-safe, and at 6 workers
  # the build without the add-lock still passed most runs (measured 0/25). At 16
  # it fails ~30% of trials, which is a signal a repeated CI run will surface.
  # The deterministic half of this coverage is ensure-serializes-worktree-add.
  rm -f "$T/go"
  for i in $(seq 1 16); do
    ( while [ ! -f "$T/go" ]; do :; done      # start barrier: collide, don't queue
      "$BBS_TICKET_BIN" ensure --slug-hint "para-$i" --type feat >"$T/e$i" 2>&1 ) &
  done
  : > "$T/go"; wait

  n_ok=$(grep -l '^TICKET=' "$T"/e* 2>/dev/null | wc -l | tr -d ' ')
  [ "$n_ok" -eq 16 ] || { echo "only $n_ok/16 ensures produced a ticket: $(cat "$T"/e*)"; exit 1; }
  uniq_t=$(cat "$T"/e* | sed -n 's|^TICKET=||p' | sort -u | wc -l | tr -d ' ')
  [ "$uniq_t" -eq 16 ] || { echo "ticket ids collided: $(cat "$T"/e* | sed -n 's|^TICKET=||p' | sort | uniq -d)"; exit 1; }
  uniq_w=$(cat "$T"/e* | sed -n 's|^WORKTREE=||p' | sort -u | wc -l | tr -d ' ')
  [ "$uniq_w" -eq 16 ] || { echo "worktree paths collided"; exit 1; }
  while read -r w; do [ -d "$w" ] || { echo "worktree missing: $w"; exit 1; }; done \
    < <(cat "$T"/e* | sed -n 's|^WORKTREE=||p')
  [ "$(git branch --show-current)" = "main" ] || { echo "primary moved off main"; exit 1; }
) && ok "ensure-race-distinct-tickets" || fail "ensure-race-distinct-tickets"
rm -rf "$T"

# ── ensure-serializes-worktree-add ────────────────────────────────────
# The probabilistic scenario above only fails ~30% of the time, so pin the
# mechanism too: hold the add-lock and the cut must wait for it rather than
# running a second `git worktree add` alongside whoever holds it.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; cd "$T/repo"

  mkdir "$T/repo/.git/bbs-worktree-add.lock"      # stand in for an add in flight
  "$BBS_TICKET_BIN" ensure --slug-hint held --type feat >"$T/out" 2>&1 &
  pid=$!
  sleep 2
  kill -0 "$pid" 2>/dev/null || { echo "ensure finished while the add-lock was held"; exit 1; }
  n=$(find "$T/repo/.babysit/worktrees" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
  [ "$n" -eq 0 ] || { echo "worktree was cut while the add-lock was held ($n dirs)"; exit 1; }

  rmdir "$T/repo/.git/bbs-worktree-add.lock"
  wait "$pid" || { echo "ensure failed after the lock was released: $(cat "$T/out")"; exit 1; }
  grep -q '^TICKET=' "$T/out" || { echo "no ticket emitted: $(cat "$T/out")"; exit 1; }
  w="$(sed -n 's|^WORKTREE=||p' "$T/out")"
  [ -d "$w" ] || { echo "worktree missing after release: $w"; exit 1; }
  [ -d "$T/repo/.git/bbs-worktree-add.lock" ] && { echo "add-lock left behind"; exit 1; }
  :
) && ok "ensure-serializes-worktree-add" || fail "ensure-serializes-worktree-add"
rm -rf "$T"

# ── merge-base-race-all-land ──────────────────────────────────────────
# A batch finishing together lands on the one primary; the merge lock has to
# serialize them or a merge reads a tree another merge is still writing.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 5 || { echo "fixture failed"; exit 1; }

  rm -f "$T/go"
  for i in 1 2 3 4 5; do
    ( while [ ! -f "$T/go" ]; do :; done
      cd "${WTS[$((i-1))]}" && "$BBS_TICKET_BIN" merge-base >"$T/m$i" 2>&1 ) &
  done
  : > "$T/go"; wait

  for i in 1 2 3 4 5; do
    grep -q '^MERGED=1' "$T/m$i" || { echo "ticket $i did not land: $(cat "$T/m$i")"; exit 1; }
    [ -f "$T/repo/f$i.txt" ] || { echo "f$i.txt missing from the primary"; exit 1; }
  done
  sv="$(serving_of "$T/repo")"
  for i in 1 2 3 4 5; do
    case ",$sv," in *",${TKS[$((i-1))]},"*) : ;;
      *) echo "serving list lost ticket ${TKS[$((i-1))]}: '$sv'"; exit 1 ;; esac
  done
) && ok "merge-base-race-all-land" || fail "merge-base-race-all-land"
rm -rf "$T"

# ── land-while-a-peer-merges ──────────────────────────────────────────
# The state checks (base branch, clean tree) have to run under the merge lock.
# A peer's merge leaves the primary mid-write for as long as it runs, and a
# check outside the lock reads that as the operator's own uncommitted work: the
# ticket does not land and the operator is told to stash changes that are not
# theirs. This is the flake behind an intermittent merge-base-race failure, so
# the hook makes the peer's merge slow enough to hit it every time.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 2 || { echo "fixture failed"; exit 1; }
  cd "$T/repo"; echo trunk > trunk.txt; git add trunk.txt   # non-FF: hooks run
  git -c user.email=t@t -c user.name=t commit -q -m "trunk moves"

  cat > "$T/repo/.git/hooks/prepare-commit-msg" <<EOF
#!/bin/sh
touch "$T/in_merge"
sleep 3
exit 0
EOF
  chmod +x "$T/repo/.git/hooks/prepare-commit-msg"

  ( cd "${WTS[0]}" && "$BBS_TICKET_BIN" merge-base >"$T/m1" 2>&1 ) &
  for _ in $(seq 1 400); do [ -f "$T/in_merge" ] && break; sleep 0.05; done
  [ -f "$T/in_merge" ] || { echo "setup: the peer merge never started: $(cat "$T/m1")"; exit 1; }

  ( cd "${WTS[1]}" && "$BBS_TICKET_BIN" merge-base >"$T/m2" 2>&1 )
  wait
  grep -q '^MERGED=1' "$T/m2" \
    || { echo "landing during a peer's merge was refused:"; cat "$T/m2"; exit 1; }
  grep -q '^MERGED=1' "$T/m1" || { echo "the peer merge itself failed: $(cat "$T/m1")"; exit 1; }
  for i in 1 2; do
    [ -f "$T/repo/f$i.txt" ] || { echo "f$i.txt missing from the primary"; exit 1; }
  done
) && ok "land-while-a-peer-merges" || fail "land-while-a-peer-merges"
rm -rf "$T"

# ── merge-base-vs-reset-base ──────────────────────────────────────────
# One worker lands while another resets the surface. Either order is legal —
# the invariant is that serving never claims a ticket whose commit is not in
# HEAD, because a QA run reads serving to know what it is testing.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 2 || { echo "fixture failed"; exit 1; }

  for trial in 1 2 3 4 5 6; do
    (cd "$T/repo" && git reset -q --hard origin/main && rm -f .git/bbs-serving)
    rm -f "$T/go"
    ( while [ ! -f "$T/go" ]; do :; done
      cd "${WTS[0]}" && "$BBS_TICKET_BIN" merge-base >"$T/mb" 2>&1 ) &
    ( while [ ! -f "$T/go" ]; do :; done
      cd "$T/repo" && "$BBS_TICKET_BIN" reset-base >"$T/rb" 2>&1 ) &
    : > "$T/go"; wait

    sv="$(serving_of "$T/repo")"
    has_f1=no; [ -f "$T/repo/f1.txt" ] && has_f1=yes
    case ",$sv," in *",${TKS[0]},"*) claims=yes ;; *) claims=no ;; esac
    [ "$claims" = "$has_f1" ] || {
      echo "trial $trial: serving='$sv' claims=$claims but f1.txt present=$has_f1"
      echo "  merge-base: $(cat "$T/mb")"; echo "  reset-base: $(cat "$T/rb")"; exit 1; }
  done
) && ok "merge-base-vs-reset-base" || fail "merge-base-vs-reset-base"
rm -rf "$T"

# ── switch-race-serving-truth ─────────────────────────────────────────
# switch resets the base and then merges. Those must be one step under one
# lock: with two acquisitions, a second switch resets in between and its merge
# lands on a tree still carrying the first ticket, while serving names only its
# own — QA then measures another ticket's code without knowing it.
#
# Racing two switches and checking the outcome is a poor test for it: the window
# is the few milliseconds between the two acquisitions, and a rival that polls
# every 100ms lands in it about 5% of trials idle (56% on a loaded machine) — a
# trial count tuned on one machine passes by luck on another.
# So test the property that causes it instead: the lease is never free between
# the reset and the merge landing. A reference-transaction hook parks the switch
# inside its `git reset --hard`, and a thief spinning flat-out on the lease dir
# takes it the instant it is free. Whenever the thief gets in, the surface must
# already be finished — reset undone and the ticket merged. A build that drops
# the lease mid-switch lets the thief in while the tree is bare.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 1 || { echo "fixture failed"; exit 1; }

  # Put main behind origin/main so the reset actually moves the ref — a no-op
  # reset opens no ref transaction and the hook below would never fire.
  ( cd "$T/repo" && echo x > ahead.txt && git add ahead.txt \
      && git -c user.email=t@t -c user.name=t commit -q -m ahead \
      && git push -q origin main && git reset -q --hard HEAD~1 )

  # Fires on every ref update; only the reset of main while `hold` exists parks.
  # The merge updates main too, so `hold` is gone by the time it runs.
  cat > "$T/repo/.git/hooks/reference-transaction" <<HOOK
#!/bin/sh
[ "\$1" = prepared ] || exit 0
read -r old new ref || exit 0
[ "\$ref" = refs/heads/main ] || exit 0
[ -f "$T/hold" ] || exit 0
: > "$T/parked"
while [ -f "$T/hold" ]; do sleep 0.1; done
HOOK
  chmod +x "$T/repo/.git/hooks/reference-transaction"

  LOCK="$T/repo/.git/bbs-qa-lease"
  : > "$T/hold"
  ( cd "${WTS[0]}" && "$BBS_TICKET_BIN" switch "${TKS[0]}" >"$T/w1" 2>&1 ) &
  for _ in $(seq 1 200); do [ -f "$T/parked" ] && break; sleep 0.05; done
  [ -f "$T/parked" ] || { echo "switch never reached its reset"; exit 1; }
  [ -d "$LOCK" ] || { echo "switch is resetting without holding the surface lease"; exit 1; }

  # Steal the lease the moment it opens, and photograph the surface right then.
  # mkdir is the same window the real publish races for: it can only win once
  # the holder has removed the directory.
  ( while ! mkdir "$LOCK" 2>/dev/null; do :; done
    { [ -f "$T/repo/f1.txt" ] && echo "merged=yes" || echo "merged=no"
      echo "serving=$(serving_of "$T/repo")"; } > "$T/stolen"
    rmdir "$LOCK" ) &
  thief=$!
  sleep 1                       # the thief is provably spinning by now
  rm -f "$T/hold"
  wait "$thief"; wait

  grep -q '^merged=yes' "$T/stolen" || {
    echo "surface lease went free mid-switch — surface at that moment: $(tr '\n' ' ' < "$T/stolen")"
    echo "  w1: $(cat "$T/w1")"; exit 1; }
  sv="$(serving_of "$T/repo")"
  [ "$sv" = "${TKS[0]}" ] || { echo "serving='$sv' want '${TKS[0]}'"; exit 1; }
  [ -f "$T/repo/f1.txt" ] || { echo "ticket file missing from the surface"; exit 1; }
) && ok "switch-race-serving-truth" || fail "switch-race-serving-truth"
rm -rf "$T"

# ── lease-blocks-parallel-ops ─────────────────────────────────────────
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 4 || { echo "fixture failed"; exit 1; }

  (cd "${WTS[0]}" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "acquire failed"; exit 1; }
  head_before="$(cd "$T/repo" && git rev-parse HEAD)"

  rm -f "$T/go"
  for i in 2 3 4; do
    ( while [ ! -f "$T/go" ]; do :; done
      cd "${WTS[$((i-1))]}" && "$BBS_TICKET_BIN" merge-base >"$T/q$i" 2>&1 ) &
  done
  ( while [ ! -f "$T/go" ]; do :; done
    cd "$T/repo" && "$BBS_TICKET_BIN" reset-base >"$T/qr" 2>&1 ) &
  : > "$T/go"; wait

  for i in 2 3 4; do
    grep -q 'STATUS: BLOCKED' "$T/q$i" || { echo "merge-base $i ran under a held lease: $(cat "$T/q$i")"; exit 1; }
    [ ! -f "$T/repo/f$i.txt" ] || { echo "f$i.txt landed under a held lease"; exit 1; }
  done
  grep -q 'STATUS: BLOCKED' "$T/qr" || { echo "reset-base ran under a held lease: $(cat "$T/qr")"; exit 1; }
  [ "$head_before" = "$(cd "$T/repo" && git rev-parse HEAD)" ] \
    || { echo "primary HEAD moved under a held lease"; exit 1; }
) && ok "lease-blocks-parallel-ops" || fail "lease-blocks-parallel-ops"
rm -rf "$T"

# ── lease-not-granted-mid-merge ───────────────────────────────────────
# A surface op checks the lease before it takes the merge lock, so there is a
# window where a lease published in between covers a surface that is already
# moving — QA would measure a tree that changes under it.
#
# Racing the two processes and hoping for that interleaving is worthless: the
# acquire is a far shorter codepath and wins every time (measured 30/30), so
# the test passes without ever reaching the window. Force the order instead —
# a merge hook holds the merge open, and the lease is requested while it is
# demonstrably in flight. The lease may only be granted once the merge lands.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 2 || { echo "fixture failed"; exit 1; }
  # Put a commit on main the ticket branch lacks, so landing it is a real merge
  # commit — a fast-forward runs no hooks and gives us nothing to hold open.
  cd "$T/repo"; echo trunk > trunk.txt; git add trunk.txt
  git -c user.email=t@t -c user.name=t commit -q -m "trunk moves"

  cat > "$T/repo/.git/hooks/prepare-commit-msg" <<EOF
#!/bin/sh
touch "$T/merge_start"
sleep 2
touch "$T/merge_end"
exit 0
EOF
  chmod +x "$T/repo/.git/hooks/prepare-commit-msg"

  ( cd "${WTS[0]}" && "$BBS_TICKET_BIN" merge-base >"$T/mb" 2>&1 ) &
  mb_pid=$!
  # Generous: a loaded CI runner takes its time getting through the git calls
  # that precede the merge, and a short wait here would read as a failure.
  for _ in $(seq 1 400); do [ -f "$T/merge_start" ] && break; sleep 0.05; done
  [ -f "$T/merge_start" ] || { echo "setup: merge never reached the hook: $(cat "$T/mb")"; exit 1; }

  out="$(cd "${WTS[1]}" && "$BBS_TICKET_BIN" qa-lease acquire 2>&1)"
  landed=no; [ -f "$T/merge_end" ] && landed=yes
  wait $mb_pid 2>/dev/null

  if printf '%s' "$out" | grep -q 'ACQUIRED=1' && [ "$landed" = no ]; then
    echo "lease granted while the merge was still in flight:"; printf '%s\n' "$out"; exit 1
  fi
  # And the wait must resolve, not fail: once the merge lands the lease is takeable.
  grep -q '^MERGED=1' "$T/mb" || { echo "merge-base did not land: $(cat "$T/mb")"; exit 1; }
  printf '%s' "$out" | grep -q 'ACQUIRED=1' \
    || { echo "acquire never got the lease after the merge landed: $out"; exit 1; }
) && ok "lease-not-granted-mid-merge" || fail "lease-not-granted-mid-merge"
rm -rf "$T"

# ── force-release-vs-acquire ──────────────────────────────────────────
# `release --force` is the "that run is dead" escape hatch; a racer acquiring
# at the same moment must not end up co-owner or leave a half-built lease.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 3 || { echo "fixture failed"; exit 1; }

  for trial in 1 2 3 4 5 6 7 8; do
    rm -rf "$T/repo/.git/bbs-qa-lease"
    (cd "${WTS[0]}" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1) || { echo "seed acquire failed"; exit 1; }
    rm -f "$T/go"
    ( while [ ! -f "$T/go" ]; do :; done
      cd "$T/repo" && "$BBS_TICKET_BIN" qa-lease release --force >"$T/fr" 2>&1 ) &
    for i in 2 3; do
      ( while [ ! -f "$T/go" ]; do :; done
        cd "${WTS[$((i-1))]}" && "$BBS_TICKET_BIN" qa-lease acquire >"$T/a$i" 2>&1 ) &
    done
    : > "$T/go"; wait

    won=$(grep -l 'ACQUIRED=1' "$T"/a[23] 2>/dev/null | wc -l | tr -d ' ')
    [ "$won" -le 1 ] || { echo "trial $trial: $won owners after a forced release: $(cat "$T"/a[23])"; exit 1; }
    if [ -d "$T/repo/.git/bbs-qa-lease" ]; then
      [ -f "$T/repo/.git/bbs-qa-lease/owner" ] || { echo "trial $trial: lease dir left with no owner file"; exit 1; }
    fi
    if [ "$won" -eq 1 ]; then
      claimed=$(grep -l 'ACQUIRED=1' "$T"/a[23] | head -1)
      want=$(sed -n 's|^OWNER=||p' "$claimed" | head -1)
      got=$(cd "$T/repo" && "$BBS_TICKET_BIN" qa-lease status 2>/dev/null | sed -n 's|^OWNER=||p')
      [ -n "$got" ] && [ "$want" = "$got" ] \
        || { echo "trial $trial: winner '$want' but the lease on disk says '$got'"; exit 1; }
    fi
  done
) && ok "force-release-vs-acquire" || fail "force-release-vs-acquire"
rm -rf "$T"

# ── no-lock-leaked ────────────────────────────────────────────────────
# A leak is worse than the races above: a lease left standing blocks every
# surface op until its ttl runs out. The short lease a surface op takes must
# never outlive the op — only a QA session's long lease stays up between ops.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 2 || { echo "fixture failed"; exit 1; }
  R="$T/repo"
  no_locks() {
    [ ! -d "$R/.git/bbs-qa-lease.steal" ] || { echo "$1: bbs-qa-lease.steal left behind"; exit 1; }
    [ ! -d "$R/.git/bbs-worktree-add.lock" ] || { echo "$1: bbs-worktree-add.lock left behind"; exit 1; }
    [ -z "$(find "$R/.git" -maxdepth 1 -name 'bbs-qa-lease.tmp-*' 2>/dev/null)" ] \
      || { echo "$1: orphaned lease temp dir"; exit 1; }
    ! grep -qs '^kind=short' "$R/.git/bbs-qa-lease/owner" \
      || { echo "$1: a surface op left its lease standing"; exit 1; }
  }
  (cd "${WTS[0]}" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1); no_locks "acquire"
  (cd "${WTS[0]}" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1); no_locks "reentrant refresh"
  (cd "${WTS[1]}" && "$BBS_TICKET_BIN" qa-lease acquire >/dev/null 2>&1); no_locks "blocked acquire"
  (cd "${WTS[1]}" && "$BBS_TICKET_BIN" merge-base >/dev/null 2>&1);       no_locks "lease-blocked merge-base"
  (cd "${WTS[1]}" && "$BBS_TICKET_BIN" qa-lease release >/dev/null 2>&1); no_locks "refused release"
  # stale steal: the one path that nests the steal lock inside the merge lock
  ownerf="$R/.git/bbs-qa-lease/owner"; aged=$(( $(date +%s) - 3900 ))
  sed "s/^since_epoch=.*/since_epoch=$aged/" "$ownerf" > "$ownerf.t" && mv "$ownerf.t" "$ownerf"
  out="$(cd "${WTS[1]}" && "$BBS_TICKET_BIN" qa-lease acquire 2>&1)"
  printf '%s' "$out" | grep -q 'STOLE_FROM=' || { echo "expected a steal, got: $out"; exit 1; }
  no_locks "stale steal"
  (cd "$R" && "$BBS_TICKET_BIN" qa-lease release --force >/dev/null 2>&1); no_locks "forced release"
  (cd "${WTS[0]}" && "$BBS_TICKET_BIN" merge-base >/dev/null 2>&1);        no_locks "successful merge-base"
  (cd "${WTS[0]}" && "$BBS_TICKET_BIN" switch "${TKS[0]}" >/dev/null 2>&1); no_locks "successful switch"
  # and the surface is still usable — a wedged lock only shows up on the next op
  (cd "${WTS[1]}" && "$BBS_TICKET_BIN" merge-base 2>&1) | grep -q '^MERGED=' \
    || { echo "merge-base wedged after the lease/switch cycle"; exit 1; }
) && ok "no-lock-leaked" || fail "no-lock-leaked"
rm -rf "$T"

# ── mixed-op-stress ───────────────────────────────────────────────────
# Randomized concurrent traffic of the shape a foreman batch produces. Holds
# the invariants together: one lease owner at most, no leaked locks, serving
# never claims a ticket absent from HEAD, repo still usable at the end.
T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"; export HOME="$T/home"; mkdir -p "$HOME"
  export AGENT_ROLE=mayor
  build_repo "$T"; build_tickets "$T" 5 || { echo "fixture failed"; exit 1; }
  R="$T/repo"

  for round in $(seq 1 10); do
    rm -f "$T/go"
    for i in 1 2 3 4 5; do
      wt="${WTS[$((i-1))]}"; tk="${TKS[$((i-1))]}"
      case $(( (round + i) % 4 )) in
        0) op=(merge-base) ;;
        1) op=(qa-lease acquire) ;;
        2) op=(switch "$tk") ;;
        3) op=(qa-lease release) ;;
      esac
      ( while [ ! -f "$T/go" ]; do :; done
        cd "$wt" && "$BBS_TICKET_BIN" "${op[@]}" >"$T/o$i" 2>&1 ) &
    done
    ( while [ ! -f "$T/go" ]; do :; done
      cd "$R" && "$BBS_TICKET_BIN" reset-base >"$T/o6" 2>&1 ) &
    : > "$T/go"; wait

    [ ! -d "$R/.git/bbs-qa-lease.steal" ] || { echo "round $round: steal lock leaked"; exit 1; }
    ! grep -qs '^kind=short' "$R/.git/bbs-qa-lease/owner" \
      || { echo "round $round: a surface op left its lease standing"; exit 1; }
    n_owner=$(grep -l 'ACQUIRED=1' "$T"/o[1-5] 2>/dev/null | wc -l | tr -d ' ')
    [ "$n_owner" -le 1 ] || { echo "round $round: $n_owner lease owners: $(cat "$T"/o[1-5])"; exit 1; }
    sv=""; [ -f "$R/.git/bbs-serving" ] && sv="$(tr -d '\n' < "$R/.git/bbs-serving")"
    for i in 1 2 3 4 5; do
      case ",$sv," in *",${TKS[$((i-1))]},"*)
        [ -f "$R/f$i.txt" ] \
          || { echo "round $round: serving='$sv' claims ${TKS[$((i-1))]} but f$i.txt is absent"; exit 1; } ;;
      esac
    done
  done
  (cd "$R" && "$BBS_TICKET_BIN" qa-lease release --force >/dev/null 2>&1)
  (cd "$R" && "$BBS_TICKET_BIN" reset-base >/dev/null 2>&1) || { echo "reset-base wedged at the end"; exit 1; }
  (cd "${WTS[0]}" && "$BBS_TICKET_BIN" merge-base 2>&1) | grep -q '^MERGED=' \
    || { echo "merge-base wedged at the end"; exit 1; }
) && ok "mixed-op-stress" || fail "mixed-op-stress"
rm -rf "$T"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
else
  printf '\033[0;31mFAIL\033[0m %d of %d: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
