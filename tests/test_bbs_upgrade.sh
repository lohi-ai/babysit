#!/usr/bin/env bash
# tests/test_bbs_upgrade.sh — differential guard for the bbs-upgrade Go port.
#
# `bbs upgrade` replaced the bin/bbs-upgrade bash script, and the session-start
# hooks depend on its exact stdout/stderr/exit contract. Rather than assert
# hand-written goldens, every case runs the frozen pre-port bash
# (tests/fixtures/bbs-upgrade.reference) and the Go binary side by side under an
# identical environment and diffs all three channels — so any drift from the
# original is a failure, not a judgement call. Same shape as test_bbs_env.sh.
#
# Three exemptions are deliberate divergences, asserted against the Go side
# alone (each marked below): `--help`/`-h` (the bash upgraded anyway), the
# no-checkout install (the bash said "reinstall manually"; the port names the
# brew / `claude plugin` commands), and a branch with no upstream (the bash's
# bare `git pull` failed and blamed conflicts that don't exist).
#
# Both implementations are staged into their own throwaway project root (bin/ +
# VERSION + a setup-skills stub) so BABYSIT_DIR resolves the same way for each.
# The git cases clone ONE prepared remote twice: independently *built* repos
# would commit at different timestamps, and `git pull` echoes the SHA range
# ("Updating 3e7acfc..af9b8d1"), which would make the stdout diff flaky. Cloning
# a single remote makes every SHA — and therefore every byte of git's own output
# — identical on both sides.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-upgrade.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }
command -v git >/dev/null 2>&1 || { echo "SKIP: git not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

HOME_DIR="$T/home"; mkdir -p "$HOME_DIR"
RUNPATH="$PATH"

# A PATH with everything the bash needs except git, to exercise the
# `command -v git` guard. Dropping PATH entirely would break the oracle's
# `#!/usr/bin/env bash` shebang before it could reach the guard.
NOGIT="$T/nogit"; mkdir -p "$NOGIT"
for c in env bash sh dirname mkdir awk date cat tr rm; do
  p="$(command -v "$c" 2>/dev/null)" && ln -sf "$p" "$NOGIT/$c"
done
if env -i PATH="$NOGIT" sh -c 'command -v git' >/dev/null 2>&1; then
  echo "FAIL: git still reachable on the git-less PATH" >&2; exit 1
fi

N=0
# stage_root <dir> [setup_exit] — a project root serving one implementation.
stage_root() {
  local r="$1" rc="${2:-0}"
  mkdir -p "$r/bin"
  cp "$REFERENCE" "$r/bin/bbs-upgrade-oracle"; chmod +x "$r/bin/bbs-upgrade-oracle"
  cp "$BIN" "$r/bin/bbs"
  ln -sf bbs "$r/bin/bbs-upgrade"   # multicall: basename bbs-upgrade -> `upgrade`
  # stdout must be swallowed by the caller (`setup-skills >/dev/null`); stderr
  # must survive.
  printf '#!/bin/sh\necho "relink stdout noise"\necho "relink stderr noise" >&2\nexit %s\n' "$rc" \
    > "$r/bin/setup-skills"
  chmod +x "$r/bin/setup-skills"
}

# Per-case scratch: two roots + two state dirs.
AD=""; BD=""; S1=""; S2=""
new_case() {
  N=$((N + 1))
  AD="$T/c$N/a"; BD="$T/c$N/b"; S1="$T/c$N/s1"; S2="$T/c$N/s2"
  mkdir -p "$AD" "$BD" "$S1" "$S2"
  stage_root "$AD" "${1:-0}"; stage_root "$BD" "${1:-0}"
}

CMP_MSG=""
# cmp_run [args...] — run both impls under identical env, diff all channels.
# Set CASE_PATH / CASE_DIR_ENV to vary PATH / whether BABYSIT_DIR is exported.
CASE_PATH=""; CASE_DIR_ENV=1; CASE_ERR_FILTER=""
cmp_run() {
  local orc grc
  CMP_MSG=""
  local p="${CASE_PATH:-$RUNPATH}"
  if [ "$CASE_DIR_ENV" = 1 ]; then
    ( cd "$AD" && env -i PATH="$p" HOME="$HOME_DIR" BABYSIT_DIR="$AD" BABYSIT_STATE_DIR="$S1" \
        "$AD/bin/bbs-upgrade-oracle" "$@" >"$T/o.out" 2>"$T/o.err" ); orc=$?
    ( cd "$BD" && env -i PATH="$p" HOME="$HOME_DIR" BABYSIT_DIR="$BD" BABYSIT_STATE_DIR="$S2" \
        "$BD/bin/bbs-upgrade" "$@" >"$T/g.out" 2>"$T/g.err" ); grc=$?
  else
    # BABYSIT_DIR unset: each side must derive it from dirname($0)/.. itself.
    ( cd "$AD" && env -i PATH="$p" HOME="$HOME_DIR" BABYSIT_STATE_DIR="$S1" \
        "$AD/bin/bbs-upgrade-oracle" "$@" >"$T/o.out" 2>"$T/o.err" ); orc=$?
    ( cd "$BD" && env -i PATH="$p" HOME="$HOME_DIR" BABYSIT_STATE_DIR="$S2" \
        "$BD/bin/bbs-upgrade" "$@" >"$T/g.out" 2>"$T/g.err" ); grc=$?
  fi
  # CASE_ERR_FILTER drops matching stderr lines from BOTH sides, for the one
  # message class the port deliberately does not reproduce: text bash emits
  # itself rather than routing from the program (its "Terminated: 15" notice for
  # a signal-killed child; its `rm:` diagnostic). Nothing consumes those strings,
  # and matching them would mean hardcoding the shell's wording. Exit code,
  # stdout, and every other stderr line are still compared. Off by default.
  if [ -n "$CASE_ERR_FILTER" ]; then
    grep -v "$CASE_ERR_FILTER" "$T/o.err" > "$T/o.err.f" || :; mv "$T/o.err.f" "$T/o.err"
    grep -v "$CASE_ERR_FILTER" "$T/g.err" > "$T/g.err.f" || :; mv "$T/g.err.f" "$T/g.err"
  fi
  [ "$orc" = "$grc" ] || CMP_MSG="exit bash=$orc go=$grc;"
  cmp -s "$T/o.out" "$T/g.out" || CMP_MSG="$CMP_MSG stdout[$(diff "$T/o.out" "$T/g.out" | tr '\n' '|')]"
  cmp -s "$T/o.err" "$T/g.err" || CMP_MSG="$CMP_MSG stderr[$(diff "$T/o.err" "$T/g.err" | tr '\n' '|')]"
}

# same_state <name> — the state file must match (or be absent) on both sides.
# The snooze line ends in `date +%s`, which can straddle a second boundary; pin
# the epoch to a shape rather than a value.
same_state() {
  local f="$1" a="$S1/$1" b="$S2/$1"
  if [ -f "$a" ] && [ -f "$b" ]; then
    local na nb
    na="$(sed -E 's/ [0-9]{9,}$/ <epoch>/' "$a")"
    nb="$(sed -E 's/ [0-9]{9,}$/ <epoch>/' "$b")"
    [ "$na" = "$nb" ] || { CMP_MSG="$CMP_MSG $f[bash='$na' go='$nb']"; return 1; }
    return 0
  fi
  [ ! -f "$a" ] && [ ! -f "$b" ] && return 0
  CMP_MSG="$CMP_MSG $f[exists bash=$([ -f "$a" ] && echo y || echo n) go=$([ -f "$b" ] && echo y || echo n)]"
  return 1
}

report() { if [ -z "$CMP_MSG" ]; then ok "$1"; else fail "$1" "$CMP_MSG"; fi; }

# ── Snooze ─────────────────────────────────────────────────────────────
# No git, no VERSION needed: snooze returns before any of that.
echo "snooze:"

new_case; cmp_run --snooze 1; same_state update-snoozed
report "snooze-no-cache-file-exits-1"

seed_cache() { printf '%b' "$1" > "$S1/last-update-check"; printf '%b' "$1" > "$S2/last-update-check"; }

new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-level-1"
new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze 2; same_state update-snoozed
report "snooze-level-2"
new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze 3; same_state update-snoozed
report "snooze-level-3"
new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze; same_state update-snoozed
report "snooze-omitted-level-defaults-to-1"
new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze ""; same_state update-snoozed
report "snooze-empty-level-defaults-to-1"   # ${2:-1} treats empty as unset

for bad in 0 4 9 abc -1 " " 1.0; do
  new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze "$bad"; same_state update-snoozed
  report "snooze-rejects-level-[$bad]"
done

new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze 2 extra args; same_state update-snoozed
report "snooze-ignores-trailing-args"

# awk field/record edges — the parse the port had to reimplement natively.
new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-cache-line-missing-field-3-exits-1"
new_case; seed_cache 'UPGRADE_AVAILABLE\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-cache-bare-keyword-exits-1"
new_case; seed_cache 'UP_TO_DATE 1.0.0 2.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-cache-non-matching-line-exits-1"
new_case; seed_cache ''; cmp_run --snooze 1; same_state update-snoozed
report "snooze-empty-cache-exits-1"
new_case; seed_cache 'UPGRADE_AVAILABLE  1.0.0\t2.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-blank-runs-are-one-separator"
new_case; seed_cache '   UPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-leading-space-breaks-the-anchor"   # /^UPGRADE_AVAILABLE/ is anchored
new_case; seed_cache 'UPGRADE_AVAILABLE_X 1.0.0 2.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-prefix-match-is-not-word-anchored"
new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\nUPGRADE_AVAILABLE 2.0.0 3.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-multiple-matches-concatenate"
new_case; seed_cache 'noise\nUPGRADE_AVAILABLE 1.0.0 2.0.0\n'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-skips-leading-noise-line"
new_case; seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0'; cmp_run --snooze 1; same_state update-snoozed
report "snooze-cache-without-trailing-newline"
new_case; mkdir -p "$S1/last-update-check" "$S2/last-update-check"; cmp_run --snooze 1; same_state update-snoozed
report "snooze-cache-is-a-directory-exits-1"      # `[ -f ]` guard

# ── Upgrade ────────────────────────────────────────────────────────────
echo "upgrade:"

# prep_clones <old_ver> <new_ver> [bump] — one remote, cloned into both roots,
# so every SHA git prints is identical on both sides.
prep_clones() {
  local old="$1" new="$2" bump="${3:-1}"
  local remote="$T/c$N/remote"
  git init -q -b main "$remote"
  ( cd "$remote" && git config user.email t@example.com && git config user.name test \
      && printf '%b' "$old" > VERSION && git add -A && git commit -qm v-old ) >/dev/null
  git clone -q "$remote" "$T/c$N/ca"; git clone -q "$remote" "$T/c$N/cb"
  if [ "$bump" = 1 ]; then
    ( cd "$remote" && printf '%b' "$new" > VERSION && echo changed > other.txt \
        && git add -A && git commit -qm v-new ) >/dev/null
  fi
  # Move the clone's .git + worktree into the staged roots.
  cp -R "$T/c$N/ca/.git" "$AD/.git"; cp "$T/c$N/ca/VERSION" "$AD/VERSION"
  cp -R "$T/c$N/cb/.git" "$BD/.git"; cp "$T/c$N/cb/VERSION" "$BD/VERSION"
}

new_case
CASE_PATH="$NOGIT"; cmp_run; CASE_PATH=""
report "upgrade-without-git-on-PATH-exits-1"

# Deliberate divergence (see the header). The bash said "reinstall manually"
# and printed a repo URL. A brew or plugin install does have an upgrade path —
# it just isn't a pull — so the port names those commands instead. Asserted
# against the Go side alone. $HOME is scratch here, so neither plugin dir
# exists and the marketplace branch of upgradeHints is the deterministic one.
new_case
CMP_MSG=""
( cd "$BD" && env -i PATH="$RUNPATH" HOME="$HOME_DIR" BABYSIT_DIR="$BD" BABYSIT_STATE_DIR="$S2" \
    "$BD/bin/bbs-upgrade" >"$T/g.out" 2>"$T/g.err" ); grc=$?
[ "$grc" = 1 ] || CMP_MSG="exit=$grc want 1;"
grep -q 'not installed via git clone' "$T/g.err" || CMP_MSG="$CMP_MSG no-shape-line;"
grep -q 'brew upgrade bbs' "$T/g.err" || CMP_MSG="$CMP_MSG no-cli-hint;"
grep -q 'claude plugin update' "$T/g.err" || CMP_MSG="$CMP_MSG no-plugin-hint;"
grep -q 'restart Claude Code' "$T/g.err" || CMP_MSG="$CMP_MSG no-restart-line;"
[ -f "$S2/just-upgraded-from" ] && CMP_MSG="$CMP_MSG marker-written;"
report "upgrade-outside-a-git-clone-names-the-real-upgrade-path"

# The brew shape, found live on 2026-08-03. /opt/homebrew is itself a git clone
# and the Cellar sits inside it, so a bare `git rev-parse --git-dir` said "yes,
# a checkout" and `bbs upgrade` ran `git pull` against HOMEBREW's repository.
# The install dir must be the repo TOPLEVEL, not merely inside a repo. Go side
# alone: the oracle has the bug this guards.
new_case
ENCL="$T/c$N/enclosing"; mkdir -p "$ENCL"
git init -q -b main "$ENCL"
( cd "$ENCL" && git config user.email t@e.com && git config user.name t \
    && echo x > f && git add -A && git commit -qm seed ) >/dev/null
NESTED="$ENCL/Cellar/bbs/1.0.0"; mkdir -p "$NESTED"
cp -R "$BD/bin" "$NESTED/bin"; rm -f "$NESTED/bin/setup-skills"
CMP_MSG=""
( cd "$NESTED" && env -i PATH="$RUNPATH" HOME="$HOME_DIR" BABYSIT_DIR="$NESTED" BABYSIT_STATE_DIR="$S2" \
    "$NESTED/bin/bbs-upgrade" >"$T/g.out" 2>"$T/g.err" ); grc=$?
[ "$grc" = 1 ] || CMP_MSG="exit=$grc want 1;"
grep -q 'not installed via git clone' "$T/g.err" || CMP_MSG="$CMP_MSG treated-as-checkout[$(head -1 "$T/g.err")];"
grep -q 'Pulling latest' "$T/g.out" && CMP_MSG="$CMP_MSG PULLED-THE-ENCLOSING-REPO;"
report "upgrade-nested-in-someone-elses-repo-does-not-pull-it"

new_case; prep_clones "1.0.0\n" "2.0.0\n"
cmp_run; same_state just-upgraded-from; same_state last-update-check; same_state update-snoozed
report "upgrade-success-writes-marker-on-version-change"

new_case; prep_clones "1.0.0\n" "1.0.0\n"
cmp_run; same_state just-upgraded-from
report "upgrade-success-no-marker-when-version-unchanged"

new_case; prep_clones "1.0.0\n" "2.0.0\n"
seed_cache 'UPGRADE_AVAILABLE 1.0.0 2.0.0\n'
printf 'stale\n' > "$S1/update-snoozed"; printf 'stale\n' > "$S2/update-snoozed"
cmp_run; same_state last-update-check; same_state update-snoozed; same_state just-upgraded-from
report "upgrade-success-clears-cache-and-snooze"

new_case; prep_clones "" "" 0; rm -f "$AD/VERSION" "$BD/VERSION"
( cd "$AD" && git rm -q --cached VERSION >/dev/null 2>&1 ); ( cd "$BD" && git rm -q --cached VERSION >/dev/null 2>&1 )
cmp_run; same_state just-upgraded-from
report "upgrade-with-no-VERSION-file-omits-the-suffix"

# `tr -d '[:space:]'` strips whitespace everywhere, not just at the ends.
new_case; prep_clones "  1. 0. 0  \n" "2.0.0\n"
cmp_run; same_state just-upgraded-from
report "upgrade-VERSION-inner-whitespace-is-stripped"

new_case; prep_clones "1.0.0\n\n\n" "1.0.0\n"
cmp_run; same_state just-upgraded-from
report "upgrade-VERSION-trailing-newlines-equal-no-marker"

# BABYSIT_DIR unset: each side derives it from dirname($0)/.. — the argv[0]
# fallback the port reproduces bug-for-bug.
new_case; prep_clones "1.0.0\n" "2.0.0\n"
CASE_DIR_ENV=0; cmp_run; CASE_DIR_ENV=1
same_state just-upgraded-from
report "upgrade-derives-BABYSIT_DIR-from-argv0"

# --ff-only refuses a diverged branch; the failure message must match.
new_case; prep_clones "1.0.0\n" "2.0.0\n"
for d in "$AD" "$BD"; do
  ( cd "$d" && git config user.email t@example.com && git config user.name test \
      && echo local > local.txt && git add -A && git commit -qm local ) >/dev/null
done
cmp_run; same_state just-upgraded-from
report "upgrade-pull-not-fast-forwardable-exits-1"

# `set -e` exits with setup-skills' own status, not a flattened 1.
for rc in 1 2 3 42 127; do
  new_case "$rc"; prep_clones "1.0.0\n" "2.0.0\n"
  cmp_run; same_state just-upgraded-from
  report "upgrade-propagates-setup-skills-exit-$rc"
done

# A signal-killed setup-skills exits 128+N, not 1.
new_case; prep_clones "1.0.0\n" "2.0.0\n"
for d in "$AD" "$BD"; do printf '#!/bin/sh\nkill -TERM $$\n' > "$d/bin/setup-skills"; done
CASE_ERR_FILTER='^Terminated: '; cmp_run; CASE_ERR_FILTER=""
same_state just-upgraded-from
report "upgrade-setup-skills-killed-by-SIGTERM-exits-143"

# `rm -f` forgives only a missing file; a directory is a hard error under set -e.
new_case; prep_clones "1.0.0\n" "2.0.0\n"
mkdir -p "$S1/last-update-check" "$S2/last-update-check"
CASE_ERR_FILTER='last-update-check'; cmp_run; CASE_ERR_FILTER=""
same_state just-upgraded-from
report "upgrade-cache-is-a-directory-fails-the-rm"

new_case 0; prep_clones "1.0.0\n" "2.0.0\n"
cmp_run --snooze-typo junk
report "upgrade-unknown-args-fall-through-to-upgrade"

# Deliberate divergence (see the header). A branch with no upstream fails the
# bash's bare `git pull --ff-only`, and the error it prints blames conflicts
# that don't exist — the shape reported on 2026-08-02. The port pulls
# origin/<branch> explicitly. Go side alone: the oracle cannot pass this.
new_case 0; prep_clones "1.0.0\n" "2.0.0\n"
CMP_MSG=""
( cd "$BD" && git branch --unset-upstream >/dev/null 2>&1 )
( cd "$BD" && env -i PATH="$RUNPATH" HOME="$HOME_DIR" BABYSIT_DIR="$BD" BABYSIT_STATE_DIR="$S2" \
    "$BD/bin/bbs-upgrade" >"$T/g.out" 2>"$T/g.err" ); grc=$?
[ "$grc" = 0 ] || CMP_MSG="exit=$grc want 0; stderr[$(head -2 "$T/g.err" | tr '\n' '|')]"
grep -q 'no upstream' "$T/g.out" || CMP_MSG="$CMP_MSG no-fallback-notice;"
[ "$(cat "$BD/VERSION")" = "2.0.0" ] || CMP_MSG="$CMP_MSG not-pulled[VERSION=$(cat "$BD/VERSION")];"
[ -f "$S2/just-upgraded-from" ] || CMP_MSG="$CMP_MSG no-marker;"
report "upgrade-branch-without-upstream-pulls-origin-explicitly"

# ── Deliberate divergence from the oracle ──────────────────────────────
# The bash treated --help as junk and upgraded anyway. The port intercepts it:
# --help is the one flag whose entire purpose is "tell me what this does
# without doing it", and here doing it means pulling and relinking. This case
# is therefore asserted against the Go side alone, not diffed against bash.
new_case 0; prep_clones "1.0.0\n" "2.0.0\n"
for flag in --help -h; do
  CMP_MSG=""
  ( cd "$BD" && env -i PATH="$RUNPATH" HOME="$HOME_DIR" BABYSIT_DIR="$BD" BABYSIT_STATE_DIR="$S2" \
      "$BD/bin/bbs-upgrade" "$flag" >"$T/g.out" 2>"$T/g.err" ); grc=$?
  [ "$grc" = 0 ] || CMP_MSG="exit=$grc want 0;"
  grep -q '^Usage:' "$T/g.out" || CMP_MSG="$CMP_MSG no-usage-on-stdout[$(head -1 "$T/g.out")];"
  [ -s "$T/g.err" ] && CMP_MSG="$CMP_MSG stderr[$(head -1 "$T/g.err")];"
  # The upgrade must not have run: VERSION still pre-pull, no marker written.
  [ "$(cat "$BD/VERSION")" = "1.0.0" ] || CMP_MSG="$CMP_MSG pulled[VERSION=$(cat "$BD/VERSION")];"
  [ -f "$S2/just-upgraded-from" ] && CMP_MSG="$CMP_MSG marker-written;"
  report "upgrade-$flag-prints-usage-and-does-not-upgrade"
done

# ── Summary ────────────────────────────────────────────────────────────
echo ""
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d/%d cases match bin/bbs-upgrade exactly\n' "$PASS" "$((PASS + FAIL))"
  exit 0
fi
printf '\033[0;31mFAIL\033[0m  %d/%d failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
exit 1
