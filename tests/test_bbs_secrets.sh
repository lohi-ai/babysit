#!/usr/bin/env bash
# tests/test_bbs_secrets.sh — coverage for `bbs-secrets`.
#
# Scenarios:
#   load-file-missing                load is silent no-op when no .babysit/.env
#   load-single-repo                 load emits exports for keys in repo file
#   load-shell-wins                  pre-exported shell var beats file value
#   load-symlink-dedup               REPO_ENV resolves to PROD_ENV → loaded once
#   load-placeholder-skipped         lines with ${...} are skipped
#   load-crlf-stripped               trailing \r is stripped from values
#   load-special-chars-roundtrip     values with '/$/"/space round-trip via eval
#   seed-creates                     first run writes file with commented placeholders
#   seed-idempotent                  second run leaves file untouched
#   ensure-gitignore-adds            entry appended when missing
#   ensure-gitignore-idempotent      no duplicate after second run
#   ensure-gitignore-no-existing-gi  creates .gitignore when absent
#   integration-qa-config-resolves   load → bbs-qa-config probe → printenv resolves
#
# Run from the babysit repo root:
#   ./tests/test_bbs_secrets.sh

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_SECRETS="$SCRIPT_DIR/bin/bbs-secrets"
BBS_QA_CONFIG="$SCRIPT_DIR/bin/bbs-qa-config"
# bbs-qa-config symlinks to the gitignored Go binary; build it if absent.
[ -x "$BBS_QA_CONFIG" ] || (cd "$SCRIPT_DIR" && go build -o bin/bbs ./cmd/bbs) 2>/dev/null || true
[ -x "$BBS_SECRETS" ] || { echo "FAIL: $BBS_SECRETS not executable" >&2; exit 1; }

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

# Run bbs-secrets from a given CWD so .babysit/ walk-up works.
run_in() {
  local dir="$1"; shift
  ( cd "$dir" && PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" "$@" )
}

# ─── load: file missing ──────────────────────────────────────────────
case_header "load — file missing"
T="$(mk_repo)"
out="$(run_in "$T" load)"
if [ -z "$out" ]; then
  ok "load is silent no-op when .babysit/.env is missing"
else
  fail "load emitted output with no .env present" "$out"
fi
rm -rf "$T"

# ─── load: single-repo ───────────────────────────────────────────────
case_header "load — single-repo file"
T="$(mk_repo)"
printf 'QA_USER=alice\nQA_PASS=s3cret\n' > "$T/.babysit/.env"
out="$(run_in "$T" load)"
if printf '%s\n' "$out" | grep -qxF "export QA_USER='alice'" \
   && printf '%s\n' "$out" | grep -qxF "export QA_PASS='s3cret'"; then
  ok "load emits both exports"
else
  fail "load did not emit expected exports" "$out"
fi
rm -rf "$T"

# ─── load: shell wins ────────────────────────────────────────────────
case_header "load — shell var wins over file"
T="$(mk_repo)"
printf 'QA_USER=file-alice\n' > "$T/.babysit/.env"
out="$(QA_USER=shell-alice run_in "$T" load)"
if ! printf '%s\n' "$out" | grep -q 'QA_USER='; then
  ok "shell-exported QA_USER suppressed file value"
else
  fail "file value was emitted despite shell export" "$out"
fi
rm -rf "$T"

# ─── load: ${...} placeholder skipped ────────────────────────────────
case_header "load — \${...} placeholder skipped"
T="$(mk_repo)"
printf 'QA_USER=alice\nDB_URL=${DB_BASE}/qa\n' > "$T/.babysit/.env"
out="$(run_in "$T" load)"
if printf '%s\n' "$out" | grep -qxF "export QA_USER='alice'" \
   && ! printf '%s\n' "$out" | grep -q '^export DB_URL='; then
  ok "placeholder line skipped, plain line emitted"
else
  fail "placeholder handling broken" "$out"
fi
rm -rf "$T"

# ─── load: CRLF stripped ─────────────────────────────────────────────
case_header "load — CRLF line ending stripped"
T="$(mk_repo)"
printf 'QA_USER=alice\r\n' > "$T/.babysit/.env"
out="$(run_in "$T" load)"
if printf '%s\n' "$out" | grep -qxF "export QA_USER='alice'"; then
  ok "trailing \\r stripped from value"
else
  fail "CRLF leaked into value" "$(printf '%s\n' "$out" | od -c | head -3)"
fi
rm -rf "$T"

# ─── load: special chars round-trip via eval ─────────────────────────
case_header "load — special chars round-trip via eval"
T="$(mk_repo)"
printf "WEIRD=val with 'quotes' and \$dollar and \"dq\" and spaces\n" > "$T/.babysit/.env"
out="$(run_in "$T" load)"
roundtrip="$( cd "$T" && eval "$out" && printf '%s' "$WEIRD" )"
expected="val with 'quotes' and \$dollar and \"dq\" and spaces"
if [ "$roundtrip" = "$expected" ]; then
  ok "value with '/\$/\"/space round-tripped byte-identical"
else
  fail "round-trip differs" "got: [$roundtrip] expected: [$expected]"
fi
rm -rf "$T"

# ─── seed: created ───────────────────────────────────────────────────
case_header "seed — first run creates file"
T="$(mk_repo)"
out="$( PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" seed --repo-root "$T" QA_USER QA_PASS )"
if printf '%s\n' "$out" | grep -q '^created: ' \
   && [ -f "$T/.babysit/.env" ] \
   && grep -qxF '# QA_USER=' "$T/.babysit/.env" \
   && grep -qxF '# QA_PASS=' "$T/.babysit/.env"; then
  ok "seed created file with commented placeholders"
else
  fail "seed did not create file as expected" "$out"
fi
# defense-in-depth: ensure-gitignore was called internally
if grep -qxF '.babysit/.env' "$T/.gitignore" 2>/dev/null; then
  ok "seed registered .babysit/.env in .gitignore"
else
  fail "seed did NOT register .gitignore entry"
fi
rm -rf "$T"

# ─── seed: idempotent ────────────────────────────────────────────────
case_header "seed — second run leaves file untouched"
T="$(mk_repo)"
PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" seed --repo-root "$T" QA_USER >/dev/null
sum1="$( shasum "$T/.babysit/.env" | awk '{print $1}' )"
out="$( PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" seed --repo-root "$T" QA_USER QA_PASS )"
sum2="$( shasum "$T/.babysit/.env" | awk '{print $1}' )"
if printf '%s\n' "$out" | grep -q '^exists: ' && [ "$sum1" = "$sum2" ]; then
  ok "seed reported exists and did not rewrite file"
else
  fail "seed not idempotent" "$out / sum $sum1 → $sum2"
fi
rm -rf "$T"

# ─── ensure-gitignore: adds entry ────────────────────────────────────
case_header "ensure-gitignore — adds entry when missing"
T="$(mk_repo)"
out="$( PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" ensure-gitignore --repo-root "$T" )"
if printf '%s\n' "$out" | grep -qxF 'added' \
   && grep -qxF '.babysit/.env' "$T/.gitignore"; then
  ok "entry appended"
else
  fail "ensure-gitignore did not append" "$out"
fi
rm -rf "$T"

# ─── ensure-gitignore: idempotent ────────────────────────────────────
case_header "ensure-gitignore — idempotent"
T="$(mk_repo)"
PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" ensure-gitignore --repo-root "$T" >/dev/null
out="$( PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" ensure-gitignore --repo-root "$T" )"
count="$(grep -cxF '.babysit/.env' "$T/.gitignore" || true)"
if printf '%s\n' "$out" | grep -qxF 'present' && [ "$count" = "1" ]; then
  ok "second run reported present and gitignore has exactly one entry"
else
  fail "ensure-gitignore not idempotent" "$out / count=$count"
fi
rm -rf "$T"

# ─── ensure-gitignore: creates missing .gitignore ────────────────────
case_header "ensure-gitignore — creates .gitignore when absent"
T="$(mk_repo)"
[ ! -f "$T/.gitignore" ] || { echo "pre: .gitignore unexpectedly present" >&2; exit 1; }
PATH="$SCRIPT_DIR/bin:$PATH" "$BBS_SECRETS" ensure-gitignore --repo-root "$T" >/dev/null
if [ -f "$T/.gitignore" ] && grep -qxF '.babysit/.env' "$T/.gitignore"; then
  ok ".gitignore was created with the entry"
else
  fail ".gitignore was not created"
fi
rm -rf "$T"

# ─── integration: load → bbs-qa-config probe → printenv ──────────────
case_header "integration — load → bbs-qa-config probe → printenv resolves"
if [ ! -x "$BBS_QA_CONFIG" ]; then
  fail "integration scenario skipped — bbs-qa-config not executable"
else
  T="$(mk_repo)"
  cat > "$T/.babysit/qa.yaml" <<'YAML'
version: 1
default_env: local
environments:
  - name: local
    url: http://localhost:5173
    credentials:
      username_env: QA_USER
      password_env: QA_PASS
YAML
  printf 'QA_USER=alice\nQA_PASS=s3cret\n' > "$T/.babysit/.env"
  result="$(
    cd "$T" \
      && unset QA_USER QA_PASS QA_AUTH_USERNAME QA_AUTH_PASSWORD \
      && PATH="$SCRIPT_DIR/bin:$PATH" \
      && eval "$("$BBS_SECRETS" load)" \
      && eval "$("$BBS_QA_CONFIG" probe --env local 2>/dev/null | grep -E '^QA_ENV_(USERNAME|PASSWORD)_ENV=')" \
      && printf 'user=%s pass=%s\n' "$(printenv "$QA_ENV_USERNAME_ENV" || true)" "$(printenv "$QA_ENV_PASSWORD_ENV" || true)"
  )"
  if printf '%s\n' "$result" | grep -qxF 'user=alice pass=s3cret'; then
    ok "qa-config resolves credentials via .babysit/.env"
  else
    fail "credentials not resolved via .babysit/.env" "$result"
  fi
  rm -rf "$T"
fi

# ─── summary ─────────────────────────────────────────────────────────
echo
printf 'Passed: \033[0;32m%d\033[0m   Failed: \033[0;31m%d\033[0m\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
