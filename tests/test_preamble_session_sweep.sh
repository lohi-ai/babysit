#!/usr/bin/env bash
# tests/test_preamble_session_sweep.sh — the preamble's session sweep must be
# portable: no `find` (Windows find.exe shadows it on PATH — audit
# bs-b3m7rnkw #9), stale files (>120 min) removed, fresh files counted.
set -u

REPO="$(cd "$(dirname "$0")/.." && pwd)"
PREAMBLE="$REPO/.claude/skills/references/preamble.md"
[ -f "$PREAMBLE" ] || { echo "FAIL: missing $PREAMBLE" >&2; exit 1; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

# ── static: no `find` in the session-tracking section ──────────────────
# The section is bounded by the "Session tracking" comment and the
# "Session-writer hook" comment that follows it.
SWEEP="$(awk '/^# Session tracking/,/^# Session-writer hook/' "$PREAMBLE")"
[ -n "$SWEEP" ] || { echo "FAIL: could not extract sweep section" >&2; exit 1; }
if printf '%s' "$SWEEP" | grep -qE '(^|[;|&]|\$\()\s*find\b'; then
  fail "sweep-uses-no-find" "find invocation present in sweep section"
else
  ok "sweep-uses-no-find"
fi

# ── dynamic: run the real preamble block, assert sweep behavior ────────
python3 - "$PREAMBLE" > "$T/preamble.sh" <<'PY'
import re, sys
src = open(sys.argv[1]).read()
blocks = [m.group(1) for m in re.finditer(r'```bash\n(.*?)```', src, re.S)]
block = next((b for b in blocks if 'Bin reachability' in b), blocks[0])
print(block.replace('_SKILL_NAME="SKILL_NAME"', '_SKILL_NAME="test"', 1))
PY
grep -q 'Session tracking' "$T/preamble.sh" \
  || { echo "FAIL: extracted block has no sweep — preamble restructured?" >&2; exit 1; }

SHELLS=(bash)
command -v zsh >/dev/null 2>&1 && SHELLS+=(zsh)

for SH in "${SHELLS[@]}"; do
  H="$T/home-$SH"; mkdir -p "$H/.babysit/sessions"
  # One stale file (fixed old mtime — always >120 min), one fresh.
  echo stale > "$H/.babysit/sessions/cc-old.yaml"
  touch -t 200001010000.00 "$H/.babysit/sessions/cc-old.yaml"
  echo fresh > "$H/.babysit/sessions/cc-new.yaml"

  out="$(env -i HOME="$H" PATH="$PATH" "$SH" -c ". '$T/preamble.sh'" 2>/dev/null)"

  if [ ! -f "$H/.babysit/sessions/cc-old.yaml" ]; then
    ok "sweep-removes-stale-$SH"
  else
    fail "sweep-removes-stale-$SH" "stale file survived"
  fi
  if [ -f "$H/.babysit/sessions/cc-new.yaml" ]; then
    ok "sweep-keeps-fresh-$SH"
  else
    fail "sweep-keeps-fresh-$SH" "fresh file removed"
  fi
  n="$(printf '%s\n' "$out" | sed -n 's/^SESSIONS_ACTIVE: //p')"
  if [ -n "$n" ] && [ "$n" -ge 1 ] 2>/dev/null; then
    ok "sessions-counted-$SH"
  else
    fail "sessions-counted-$SH" "SESSIONS_ACTIVE='$n' (output: $(printf '%s' "$out" | head -5 | tr '\n' '|'))"
  fi
done

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || { printf 'failed: %s\n' "${FAIL_NAMES[*]}"; exit 1; }
