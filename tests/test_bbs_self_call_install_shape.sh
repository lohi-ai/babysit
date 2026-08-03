#!/usr/bin/env bash
# tests/test_bbs_self_call_install_shape.sh — the binary must reach itself.
#
# bbs is a multicall binary, and several subcommands need another one's answer
# (ticket needs the slug, ticket base-ops needs autopilot's base-branch, design
# needs the ticket's design pointer). Those used to be subprocess calls through
# `bbs-<sub>` argv0 aliases resolved off PATH — and Formula/bbs.rb ships exactly
# two of them, bbs-config and bbs-env. None of the three that were needed.
#
# The failure was silent, which is why it survived: identity.Resolve() treated a
# missing bbs-slug as "no answer" and fell through to SLUG=unknown, so every
# ticket path pointed at ~/.babysit/projects/unknown and `board` listed a
# different (empty) project. autopilot probe reported every artifact absent.
#
# So these cases run against a *brew-shaped* install — only bbs, bbs-config and
# bbs-env on PATH, nothing under ~/.claude — and assert the answers match a
# dev-shaped install that has every alias. A regression re-introducing an argv0
# self-call fails here rather than in a user's terminal six weeks later.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

BIN="$T/build/bbs"
mkdir -p "$T/build"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

# A git repo with an origin remote and a ticket branch, so the slug and the
# derived ticket are both non-trivial — "unknown" must not be able to pass.
WORK="$T/work"
mkdir -p "$WORK"
(
  cd "$WORK" || exit 1
  git init -q .
  git remote add origin git@github.com:acme/widget.git
  git config user.email t@t; git config user.name t
  git commit -q --allow-empty -m init
  git checkout -q -b feat/bs-abc12345_self-call
) >/dev/null 2>&1

# Two install shapes on disk. Both get the identical binary; they differ only
# in which argv0 aliases sit next to it.
BREW="$T/brew"; mkdir -p "$BREW"
cp "$BIN" "$BREW/bbs"
ln -sf bbs "$BREW/bbs-config"          # exactly what Formula/bbs.rb installs
ln -sf bbs "$BREW/bbs-env"

DEV="$T/dev"; mkdir -p "$DEV"
cp "$BIN" "$DEV/bbs"
for a in config env slug ticket autopilot design dashboard secrets qa-config upgrade; do
  ln -sf bbs "$DEV/bbs-$a"
done

BARE_PATH="/usr/bin:/bin:/usr/sbin:/sbin"

# run <bindir> <args...> — a clean env with an isolated HOME, so nothing under
# the real ~/.claude can supply an alias the shape is supposed to lack.
run() {
  local bindir="$1"; shift
  ( cd "$WORK" && env -i HOME="$T/home" PATH="$bindir:$BARE_PATH" TERM=dumb \
      "$bindir/bbs" "$@" 2>&1 )
}

# same <desc> <args...> — the brew shape must answer exactly like the dev shape.
same() {
  local desc="$1"; shift
  local b d
  b="$(run "$BREW" "$@")"
  d="$(run "$DEV" "$@")"
  if [ "$b" = "$d" ]; then ok "$desc"
  else fail "$desc" "brew: ${b:-<empty>} | dev: ${d:-<empty>}"; fi
  LAST="$b"
}

mkdir -p "$T/home"
echo "bbs self-call across install shapes"

# 1. The slug bootstrap — the one that silently degraded.
same "ticket resolve --explain agrees across shapes" ticket resolve --explain
case "$LAST" in
  *SLUG=acme-widget*) ok "brew shape derives the real slug, not 'unknown'" ;;
  *) fail "brew shape derives the real slug, not 'unknown'" "$LAST" ;;
esac

# 2. The ticket id itself, derived from the branch regex through that slug.
same "ticket resolve agrees across shapes" ticket resolve
[ "$LAST" = "bs-abc12345" ] \
  && ok "brew shape resolves the branch ticket" \
  || fail "brew shape resolves the branch ticket" "got '${LAST:-<empty>}'"

# 3. base-branch — was a fork to bbs-autopilot, plus a fork to bbs-config for
#    the global-config rung.
same "autopilot base-branch agrees across shapes" autopilot base-branch

# 4. autopilot probe — its artifact counts came from forked bbs-ticket calls
#    behind an isExecutable() guard, so on brew every artifact read as absent.
same "autopilot probe agrees across shapes" autopilot probe

# 5. board — reads the project home the slug bootstrap picked.
same "ticket board agrees across shapes" ticket board

# 6. No source file may reintroduce an argv0 self-call. The binary reaching
#    itself is `<self> <sub>`; anything spawning a hyphenated alias is the bug.
STRAY="$(grep -rnE 'exec\.(Command|LookPath)\("bbs-' "$REPO/internal" "$REPO/cmd" \
          --include='*.go' 2>/dev/null | grep -v '_test\.go' || true)"
if [ -z "$STRAY" ]; then
  ok "no argv0 self-calls in Go sources"
else
  fail "no argv0 self-calls in Go sources" "$(echo "$STRAY" | head -3)"
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32m%d passed\033[0m\n' "$PASS"
  exit 0
fi
printf '\033[0;31m%d failed\033[0m, %d passed: %s\n' "$FAIL" "$PASS" "${FAIL_NAMES[*]}"
exit 1
