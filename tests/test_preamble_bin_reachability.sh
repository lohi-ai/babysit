#!/usr/bin/env bash
# tests/test_preamble_bin_reachability.sh — guards the preamble's bin contract.
#
# Skills invoke the CLI as `bbs <sub>` (never a hyphenated alias — a brew-only
# install ships only two of them). That makes exactly one thing load-bearing:
# a *working* `bbs` must be reachable from the preamble's shell. The preamble
# guarantees it by prepending the absolute install dirs to PATH, then probing
# once with `bbs ticket --help`.
#
# Replaces test_preamble_bbs_resolve.sh, which guarded the per-subcommand
# _bbs_resolve this contract retired. Two lessons carried over from it:
#
#   1. EXECUTE, don't just resolve. That test was written after a resolver
#      handed back a bare `bbs-<sub>` name that looked resolved and could not
#      run. So these cases run the real preamble block and assert on observable
#      output (SLUG resolved / BBS_DEGRADED emitted), never on a path string.
#   2. SHELL MATRIX. The preamble runs under whatever shell drives the skill —
#      zsh on a stock macOS box, not just bash. zsh differs on unquoted-$PATH
#      splitting and command hashing, so every case runs under both.
#
# Cases cover the three install shapes plus the two failure modes.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
PREAMBLE="$REPO/.claude/skills/references/preamble.md"
[ -f "$PREAMBLE" ] || { echo "FAIL: missing $PREAMBLE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

# Extract the preamble block and name the skill. Select it by content, not by
# position: preamble.md grew an earlier ```bash fence (the AGENT_ROLE=dashboard
# approval snippet), and "first fence" silently started matching that one —
# which tripped the guard below and skipped every case in this file.
python3 - "$PREAMBLE" > "$T/preamble.sh" <<'PY'
import re, sys
src = open(sys.argv[1]).read()
blocks = [m.group(1) for m in re.finditer(r'```bash\n(.*?)```', src, re.S)]
block = next((b for b in blocks if 'Bin reachability' in b), blocks[0])
print(block.replace('_SKILL_NAME="SKILL_NAME"', '_SKILL_NAME="test"', 1))
PY
grep -q 'Bin reachability' "$T/preamble.sh" \
  || { echo "FAIL: extracted block has no reachability section — preamble restructured?" >&2; exit 1; }

SHELLS=(bash)
command -v zsh >/dev/null 2>&1 && SHELLS+=(zsh)

# A PATH with no bbs on it at all — the shape cron/tmux workers get.
BARE_PATH="/usr/bin:/bin:/usr/sbin:/sbin"

# run_case <shell> <home> <path> — run the preamble, capture stdout+stderr.
run_case() {
  env -i HOME="$2" PATH="$3" TERM=dumb "$1" "$T/preamble.sh" 2>&1
}

# expect <yes|no> <desc> <shell> <home> <path> — assert on BBS_DEGRADED presence.
expect_degraded() {
  local want="$1" desc="$2" sh="$3" home="$4" path="$5" out
  out="$(cd "$REPO" && run_case "$sh" "$home" "$path")"
  case "$out" in
    *BBS_DEGRADED*) got=yes ;;
    *) got=no ;;
  esac
  if [ "$got" = "$want" ]; then ok "[$sh] $desc"
  else fail "[$sh] $desc" "want BBS_DEGRADED=$want got=$got"; fi
  LAST_OUT="$out"
}

echo "preamble bin reachability"

# Install shapes, per shell.
for SH in "${SHELLS[@]}"; do
  # 1. brew-shaped: bbs on PATH, no ~/.claude at all.
  H="$T/home-brew-$SH"; mkdir -p "$H" "$T/brewbin"
  cp "$BIN" "$T/brewbin/bbs"
  expect_degraded no "brew-shaped: bbs already on PATH" "$SH" "$H" "$T/brewbin:$BARE_PATH"
  case "$LAST_OUT" in
    *"SLUG: "*) ok "[$SH] brew-shaped: slug resolves through the space form" ;;
    *) fail "[$SH] brew-shaped: slug resolves through the space form" "no SLUG line" ;;
  esac

  # 2. clone install, hostile PATH: heal finds ~/.claude/skills/babysit/bin.
  H="$T/home-clone-$SH"; mkdir -p "$H/.claude/skills/babysit/bin"
  cp "$BIN" "$H/.claude/skills/babysit/bin/bbs"
  expect_degraded no "clone install: heal recovers a stripped PATH" "$SH" "$H" "$BARE_PATH"

  # 3. setup-skills install: ~/.local/bin/bbs, stripped PATH.
  H="$T/home-local-$SH"; mkdir -p "$H/.local/bin"
  cp "$BIN" "$H/.local/bin/bbs"
  expect_degraded no "setup-skills install: ~/.local/bin healed onto PATH" "$SH" "$H" "$BARE_PATH"

  # 4. no binary anywhere: fail loud, but don't abort the skill.
  H="$T/home-empty-$SH"; mkdir -p "$H"
  expect_degraded yes "no binary: reports BBS_DEGRADED" "$SH" "$H" "$BARE_PATH"
  case "$LAST_OUT" in
    *"SKILL: test"*) ok "[$SH] no binary: preamble still completes (degraded, not fatal)" ;;
    *) fail "[$SH] no binary: preamble still completes (degraded, not fatal)" "no SKILL line" ;;
  esac

  # 5. stale binary serving no `ticket` subcommand — the silent-exit-1 trap the
  #    probe exists for. A path check alone would call this healthy.
  H="$T/home-stale-$SH"; mkdir -p "$H/.local/bin"
  printf '#!/bin/sh\nexit 1\n' > "$H/.local/bin/bbs"; chmod +x "$H/.local/bin/bbs"
  expect_degraded yes "stale binary: silent exit 1 caught by the probe" "$SH" "$H" "$BARE_PATH"
done

# 6. Nothing readers act on may call a hyphenated alias brew does not ship.
#    Three kinds of hyphenated mention stay legitimate, so they're filtered
#    rather than fixed:
#      - lines *about* the aliases (argv0 / alias / symlink), e.g. docs/install.md
#      - filesystem paths (bin/bbs-ticket, tests/fixtures/bbs-*.reference)
#      - bbs-ticket-test / bbs-ticket-lint, which are separate scripts, not symlinks
#    Excluded outright: CHANGELOG.md and blogs/ (dated records — rewriting them
#    would falsify history) and docs/bin-decomposition-spike.md (a bash-era
#    spike whose subject *is* the standalone scripts).
ALIAS_RE='(^|[^/[:alnum:]_-])bbs-(ticket|design|secrets|qa-config|autopilot|config|slug|env|upgrade|update-check|dashboard)([^[:alnum:]_-]|$)'
STRAY="$(grep -rnoE "$ALIAS_RE" \
    "$REPO/.claude/skills" "$REPO/docs" "$REPO/README.md" "$REPO/README.vi.md" "$REPO/CLAUDE.md" \
    --include='*.md' 2>/dev/null \
  | grep -v 'bin-decomposition-spike' \
  | grep -vE 'bbs-ticket-(test|lint)' || true)"
# Re-check each hit against its full source line — the -o match alone can't see
# whether the line is discussing the alias or invoking it.
STRAY="$(echo "$STRAY" | while IFS=: read -r file line _; do
  [ -n "${file:-}" ] || continue
  src="$(sed -n "${line}p" "$file")"
  case "$src" in
    *argv*|*alias*|*symlink*|*bin/*) ;;
    *) echo "$file:$line: $src" ;;
  esac
done)"
if [ -z "$STRAY" ]; then
  ok "no hyphenated alias calls left in skills or docs"
else
  fail "no hyphenated alias calls left in skills or docs" "$(echo "$STRAY" | head -3)"
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32m%d passed\033[0m\n' "$PASS"
  exit 0
fi
printf '\033[0;31m%d failed\033[0m, %d passed: %s\n' "$FAIL" "$PASS" "${FAIL_NAMES[*]}"
exit 1
