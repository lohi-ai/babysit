#!/usr/bin/env bash
# tests/test_archetype_workflows.sh — regression guards for the five
# archetype workflows introduced in v1.47.0.
#
# The workflows are prose, so refactors can silently drop load-bearing
# lines (this happened to the readiness gate in v1.47.0). Pin:
#   1. All five workflow files exist and pass `bbs-autopilot lint-workflow`.
#   2. Each has a **Final status** block with a STATUS line, a VERDICT line
#      carrying that archetype's verdict vocabulary, and a NEXT line.
#   3. Builder keeps its mode table (child/orchestrate/implement/build/verify)
#      and the two verbatim CHILD_BRANCH constructions.
#   4. Code-touching workflows (builder, sweeper, maintainer) persist the QA
#      verdict with `bbs-ticket set-verdict --skill qa` — the PR gate reads it.
#   5. `bbs-autopilot explain` routes a committed non-base branch to builder
#      (verify mode) — needs a real origin, which the eval-set fixtures lack.

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WF_DIR="$SCRIPT_DIR/.claude/skills/autopilot/workflows"
BBS_AUTOPILOT="$SCRIPT_DIR/bin/bbs-autopilot"
[ -x "$BBS_AUTOPILOT" ] || { echo "FAIL: $BBS_AUTOPILOT not executable" >&2; exit 1; }

PASS=0
FAIL=0
FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# ── existence + lint + final-status skeleton, per archetype ─────────

check_workflow() {  # $1=name $2=verdict-vocab regex (matched on the VERDICT line)
  local name="$1" vocab="$2" f="$WF_DIR/$1.md"
  [ -f "$f" ] || { fail "$name-exists" "missing $f"; return; }
  ok "$name-exists"

  if "$BBS_AUTOPILOT" lint-workflow "$f" >/dev/null 2>&1; then
    ok "$name-lint"
  else
    fail "$name-lint" "$("$BBS_AUTOPILOT" lint-workflow "$f" 2>&1 | head -3)"
  fi

  if grep -q '^\*\*Final status\*\*' "$f" \
     && grep -q '^STATUS: ' "$f" \
     && grep -q '^NEXT: ' "$f"; then
    ok "$name-final-status-block"
  else
    fail "$name-final-status-block"
  fi

  if grep -q "^VERDICT: $vocab\$" "$f"; then
    ok "$name-verdict-vocabulary"
  else
    fail "$name-verdict-vocabulary" "expected 'VERDICT: $vocab', got: $(grep '^VERDICT:' "$f" | head -1)"
  fi
}

check_workflow prototyper 'VALIDATED | INVALIDATED | INCONCLUSIVE'
check_workflow builder    'BUILT'
check_workflow sweeper    'SWEPT'
check_workflow grower     'RANKED | SCAFFOLDED'
check_workflow maintainer 'AUDITED | HARDENED | FIXED'

# ── builder mode table + verbatim branch constructions ──────────────

B="$WF_DIR/builder.md"
if grep -q '^| \*\*child\*\*' "$B" \
   && grep -q '^| \*\*orchestrate\*\*' "$B" \
   && grep -q '^| \*\*implement\*\*' "$B" \
   && grep -q '^| \*\*build\*\*' "$B" \
   && grep -q '^| \*\*verify\*\*' "$B"; then
  ok "builder-mode-table"
else
  fail "builder-mode-table"
fi

# Cross-repo tasks: siblings resolve via RELATED_* in .babysit/.env, and the
# handoff must name every touched repo. The only cross-repo handoff surface
# in the pack — keep it pinned.
if grep -q "RELATED_" "$B" \
   && grep -q 'every touched repo' "$B"; then
  ok "builder-cross-repo"
else
  fail "builder-cross-repo"
fi

if grep -qF 'CHILD_BRANCH="feat/${TICKET}/${POS}_${CHILD}_${SLUG}"' "$B" \
   && grep -qF 'CHILD_BRANCH="feat/${PARENT_ID}/${POS}_${TICKET}_${SLUG}"' "$B"; then
  ok "builder-child-branch-verbatim"
else
  fail "builder-child-branch-verbatim"
fi

# ── code-touching workflows persist the qa verdict ──────────────────

for name in builder sweeper maintainer; do
  if grep -q 'set-verdict --skill qa' "$WF_DIR/$name.md"; then
    ok "$name-persists-qa-verdict"
  else
    fail "$name-persists-qa-verdict"
  fi
done

# ── explain routes commits-ahead to builder (verify mode) ────────────
# Needs origin/<base> to exist: build a repo with a bare origin, push main,
# then commit on a feat branch without pushing.

T="$(mktemp -d)"
(
  export PATH="$SCRIPT_DIR/bin:$PATH"
  # Isolate from the developer's ~/.babysit config (base_branch override
  # would shadow the origin/HEAD → main fallback under test).
  export HOME="$T/home"; mkdir -p "$HOME"
  git init -q --bare "$T/origin.git"
  git init -q "$T/repo"; cd "$T/repo"
  git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
  git branch -M main
  git remote add origin "$T/origin.git"
  git push -q origin main
  git checkout -q -b feat/bs-verify-1_scratch
  echo x > f.txt; git add f.txt
  git -c user.email=t@t -c user.name=t commit -q -m "feat: work"
  out="$("$BBS_AUTOPILOT" explain 2>/dev/null)"
  printf '%s\n' "$out" | grep -q 'builder (verify mode)' \
    || { echo "no verify-mode route; got:"; printf '%s\n' "$out" | head -20; exit 1; }
) && ok "explain-routes-verify-mode" || fail "explain-routes-verify-mode"
rm -rf "$T"

# ── explain routes each builder mode (child/orchestrate/implement/build) ──
# The verify-mode route above was the only builder mode with explain coverage;
# the other four routed silently, which is how v1.47.0 shipped a sub_ticket
# (child-mode) routing regression unnoticed. Routing precedence is
# sub_ticket > manifest.md > plan.md > requirement.md, so seeding exactly one
# signal makes each mode's route unambiguous. The child case is the explicit
# regression guard for sub-ticket branch shape.
route_mode_test() {
  local label="$1" ticket="$2" expect="$3"; shift 3
  local seed="$1"
  local D; D="$(mktemp -d)"
  (
    export PATH="$SCRIPT_DIR/bin:$PATH"
    export HOME="$D/home"; mkdir -p "$HOME"
    git init -q "$D/repo"; cd "$D/repo"
    git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init
    git checkout -q -b "feat/${ticket}_scratch"
    "$seed"
    out="$(bbs-autopilot explain 2>/dev/null)"
    printf '%s\n' "$out" | grep -q "$expect" \
      || { echo "expected '$expect'; got:"; printf '%s\n' "$out" | grep -A1 'recommended workflow'; exit 1; }
  ) && ok "explain-routes-$label" || fail "explain-routes-$label"
  rm -rf "$D"
}
seed_sub_ticket()  { bbs-ticket init --origin-type sub_ticket >/dev/null 2>&1; }
seed_manifest()    { bbs-ticket init >/dev/null 2>&1; local m; m="$(bbs-ticket path manifest --write 2>/dev/null)"; [ -n "$m" ] && echo "# manifest" > "$m"; }
seed_plan()        { bbs-ticket init >/dev/null 2>&1; local p; p="$(bbs-ticket path plan --write 2>/dev/null)"; [ -n "$p" ] && echo "# plan" > "$p"; }
seed_requirement() { bbs-ticket init >/dev/null 2>&1; local r; r="$(bbs-ticket path requirement --write 2>/dev/null)"; [ -n "$r" ] && echo "# req" > "$r"; }

route_mode_test child       bs-child-1  'builder (child mode)'       seed_sub_ticket
route_mode_test orchestrate bs-orch-1   'builder (orchestrate mode)' seed_manifest
route_mode_test implement   bs-impl-1   'builder (implement mode)'   seed_plan
route_mode_test build       bs-build-1  'builder (build mode)'       seed_requirement

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d checks\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m  %d/%d checks failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
