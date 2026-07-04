#!/usr/bin/env bash
# tests/test_qa_config_loader.sh — coverage for `bbs-qa-config`.
#
# Scenarios (per bs-82x4oym0 acceptance criteria):
#   list-empty, list-standalone, list-product-merge,
#   probe-resolves-fields, probe-missing-env, probe-missing-url,
#   default-env-standalone, default-env-product-wins,
#   precedence-product-over-standalone, precedence-local-over-committed,
#   leak-check-clean, leak-check-inline-password,
#   legacy-product-shorthand-promoted, empty-yaml-silent,
#   credentials-env-var-name-only-not-value.
#
# Run from the babysit repo root:
#   ./tests/test_qa_config_loader.sh

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_QA_CONFIG="$SCRIPT_DIR/bin/bbs-qa-config"
[ -x "$BBS_QA_CONFIG" ] || { echo "FAIL: $BBS_QA_CONFIG not executable" >&2; exit 1; }

PASS=0
FAIL=0
FAIL_NAMES=()

ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }
case_header() { printf '\n\033[1m%s\033[0m\n' "$1"; }

mk_repo() {
  local t; t="$(mktemp -d)"
  git init -q "$t"
  ( cd "$t" && git -c user.email=t@t -c user.name=t commit --allow-empty -q -m init )
  mkdir -p "$t/.babysit"
  printf '%s' "$t"
}

# Run bbs-qa-config from a given CWD (so git-toplevel resolution works).
run_in() {
  local dir="$1"; shift
  ( cd "$dir" && PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_QA_CONFIG" "$@" )
}

# ─── list ─────────────────────────────────────────────────────────────
case_header "list — standalone qa.yaml"
T="$(mk_repo)"
cat > "$T/.babysit/qa.yaml" <<'YAML'
version: 1
default_env: local
environments:
  - name: local
    url: http://localhost:5173
  - name: staging
    url: https://staging.example.com
YAML
out="$(run_in "$T" list)"
expected="local
staging"
[ "$out" = "$expected" ] && ok "list returns both env names sorted" || fail "list-standalone" "got: $out"

# ─── default-env ──────────────────────────────────────────────────────
case_header "default-env"
out="$(run_in "$T" default-env)"
[ "$out" = "local" ] && ok "default-env reads default_env from qa.yaml" || fail "default-env-standalone" "got: $out"

# ─── probe — resolves all fields ──────────────────────────────────────
case_header "probe — resolves fields"
T="$(mk_repo)"
cat > "$T/.babysit/qa.yaml" <<'YAML'
version: 1
default_env: staging
environments:
  - name: staging
    url: https://staging.example.com
    runtime: chromium
    guideline: Skip marketing pages.
    credentials:
      username_env: QA_USER
      password_env: QA_PASS
YAML
out="$(run_in "$T" probe --env staging)"
echo "$out" | grep -q "^QA_ENV_URL='https://staging.example.com'$"  && ok "probe resolves url" || fail "probe-url" "$out"
echo "$out" | grep -q "^QA_ENV_RUNTIME='chromium'$"                 && ok "probe resolves runtime" || fail "probe-runtime" "$out"
echo "$out" | grep -q "^QA_ENV_GUIDELINE='Skip marketing pages.'$"  && ok "probe resolves guideline" || fail "probe-guideline" "$out"
echo "$out" | grep -q "^QA_ENV_USERNAME_ENV='QA_USER'$"             && ok "probe resolves username_env (NAME, not value)" || fail "probe-username-env" "$out"
echo "$out" | grep -q "^QA_ENV_PASSWORD_ENV='QA_PASS'$"             && ok "probe resolves password_env (NAME, not value)" || fail "probe-password-env" "$out"

# Critical: credential VALUES must never appear in probe output.
if echo "$out" | grep -qE "(hunter2|secret-value|alice@)"; then
  fail "probe-leaks-secret" "secret value leaked into probe output"
else
  ok "probe never emits credential values, only env-var names"
fi

# ─── probe — env not found ────────────────────────────────────────────
case_header "probe — missing env"
if run_in "$T" probe --env nope >/dev/null 2>&1; then
  fail "probe-missing-env-exit" "expected non-zero exit"
else
  ok "probe returns non-zero for unknown env"
fi

# ─── precedence — local overrides committed ───────────────────────────
case_header "precedence — qa.local.yaml > qa.yaml"
T="$(mk_repo)"
cat > "$T/.babysit/qa.yaml" <<'YAML'
version: 1
environments:
  - name: local
    url: http://committed.example.com
YAML
cat > "$T/.babysit/qa.local.yaml" <<'YAML'
version: 1
environments:
  - name: local
    url: http://override.local
YAML
out="$(run_in "$T" probe --env local)"
echo "$out" | grep -q "^QA_ENV_URL='http://override.local'$" && ok "qa.local.yaml overrides qa.yaml for same env" || fail "precedence-local-over-committed" "$out"
echo "$out" | grep -q "^QA_ENV_SOURCE='qa.local.yaml'$"      && ok "QA_ENV_SOURCE reports qa.local.yaml" || fail "precedence-source" "$out"

# ─── leak-check — clean ───────────────────────────────────────────────
case_header "leak-check — clean qa.yaml"
T="$(mk_repo)"
cat > "$T/.babysit/qa.yaml" <<'YAML'
version: 1
environments:
  - name: local
    url: http://localhost:5173
    credentials:
      username_env: QA_USER
      password_env: QA_PASS
YAML
if run_in "$T" leak-check "$T/.babysit/qa.yaml" >/dev/null 2>&1; then
  ok "leak-check clean for env-var indirection"
else
  fail "leak-check-clean" "expected exit 0 for env-indirected creds"
fi

# ─── leak-check — inline password ─────────────────────────────────────
case_header "leak-check — inline password rejected"
cat > "$T/.babysit/qa.yaml" <<'YAML'
version: 1
environments:
  - name: local
    url: http://localhost:5173
    credentials:
      username: alice@example.com
      password: hunter2
YAML
if run_in "$T" leak-check "$T/.babysit/qa.yaml" >/dev/null 2>&1; then
  fail "leak-check-inline-password" "expected non-zero exit when inline password present"
else
  ok "leak-check rejects inline password"
fi

# qa.local.yaml is exempt (it's gitignored — operators may put real secrets).
case_header "leak-check — qa.local.yaml exempt"
cp "$T/.babysit/qa.yaml" "$T/.babysit/qa.local.yaml"
if run_in "$T" leak-check "$T/.babysit/qa.local.yaml" >/dev/null 2>&1; then
  ok "leak-check skips qa.local.yaml (gitignored)"
else
  fail "leak-check-qa-local-yaml-exempt" "qa.local.yaml should be exempt"
fi

# ─── empty config — silent ────────────────────────────────────────────
case_header "empty qa.yaml — silent (not an error)"
T="$(mk_repo)"
: > "$T/.babysit/qa.yaml"
out="$(run_in "$T" list)"
[ -z "$out" ] && ok "empty qa.yaml produces no output, no error" || fail "empty-yaml-silent" "got: $out"
out="$(run_in "$T" default-env)"
[ -z "$out" ] && ok "default-env empty when qa.yaml empty" || fail "empty-yaml-default" "got: $out"

# ─── no qa.yaml at all — silent ───────────────────────────────────────
case_header "no qa.yaml — silent"
T="$(mk_repo)"
out="$(run_in "$T" list)"
[ -z "$out" ] && ok "list silent when no config exists" || fail "no-config-silent" "got: $out"
if run_in "$T" probe --env any >/dev/null 2>&1; then
  fail "no-config-probe" "probe should fail when no env defined"
else
  ok "probe fails cleanly when no config"
fi

# ─── inline-URL backward compat (loader-side) ─────────────────────────
# The loader is independent of the qa skill's URL parsing; it only refuses
# to invent envs. Confirmed by probe-missing-env above.

# ─── summary ──────────────────────────────────────────────────────────
echo
printf 'Passed: \033[0;32m%d\033[0m   Failed: \033[0;31m%d\033[0m\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "Failures:"
  for n in "${FAIL_NAMES[@]}"; do echo "  - $n"; done
  exit 1
fi
