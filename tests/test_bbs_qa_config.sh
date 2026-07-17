#!/usr/bin/env bash
# tests/test_bbs_qa_config.sh — differential guard for the bbs-qa-config Go port.
#
# `bbs qa-config` replaced the bin/bbs-qa-config bash script, and the qa skill
# depends on its exact stdout/stderr/exit contract. Every case runs the frozen
# pre-port bash (tests/fixtures/bbs-qa-config.reference) and the Go binary side
# by side under an identical environment and diffs all three channels — any
# drift from the original is a failure, not a judgement call.
#
# HOME and BABYSIT_STATE_DIR are pinned to a throwaway dir in every case, and
# each fixture is its own git repo under mktemp, so no case can read or write
# the real ~/.babysit or the babysit checkout's own .babysit/.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-qa-config.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
HOMEDIR="$T/home"; STATE="$T/state"
mkdir -p "$HOMEDIR" "$STATE" "$T/gobin"

BIN="$T/gobin/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }
ln -sf bbs "$T/gobin/bbs-qa-config"   # multicall: basename bbs-qa-config -> `qa-config`
ORACLE="$T/gobin/bbs-qa-config-oracle"
cp "$REFERENCE" "$ORACLE"; chmod +x "$ORACLE"

# diff_case <desc> <args...> — run oracle and Go under CASE_ENV/CASE_CWD with
# HOME + BABYSIT_STATE_DIR pinned, diff stdout, stderr, and exit code.
CASE_ENV=(); CASE_CWD="$T"
diff_case() {
  local desc="$1"; shift
  local orc grc msg=""
  ( cd "$CASE_CWD" && env -i PATH="$PATH" HOME="$HOMEDIR" BABYSIT_STATE_DIR="$STATE" \
      ${CASE_ENV[@]+"${CASE_ENV[@]}"} \
      "$ORACLE" "$@" >"$T/o.out" 2>"$T/o.err" ); orc=$?
  ( cd "$CASE_CWD" && env -i PATH="$PATH" HOME="$HOMEDIR" BABYSIT_STATE_DIR="$STATE" \
      ${CASE_ENV[@]+"${CASE_ENV[@]}"} \
      "$T/gobin/bbs-qa-config" "$@" >"$T/g.out" 2>"$T/g.err" ); grc=$?
  [ "$orc" = "$grc" ] || msg="exit bash=$orc go=$grc;"
  cmp -s "$T/o.out" "$T/g.out" || msg="$msg stdout[$(diff "$T/o.out" "$T/g.out" | tr '\n' '|')]"
  cmp -s "$T/o.err" "$T/g.err" || msg="$msg stderr[$(diff "$T/o.err" "$T/g.err" | tr '\n' '|')]"
  if [ -z "$msg" ]; then ok "$desc"; else fail "$desc" "$msg"; fi
}

mk_repo() { git -c init.defaultBranch=main init -q "$1" && mkdir -p "$1/.babysit"; }

# ── Dispatch / help ─────────────────────────────────────────────────────
mk_repo "$T/empty"
CASE_CWD="$T/empty"; CASE_ENV=()
echo "dispatch:"
diff_case "no-args-usage"
diff_case "help-sub" help
diff_case "unknown-sub-usage" wat
diff_case "empty-string-sub" ""

# ── No config present ───────────────────────────────────────────────────
echo "no config:"
diff_case "list-empty" list
diff_case "default-env-empty-exit1" default-env
diff_case "probe-no-config" probe --env local
diff_case "check-no-config" check

# ── Simple schema ───────────────────────────────────────────────────────
mk_repo "$T/simple"
cat > "$T/simple/.babysit/qa.yaml" <<'EOF'
version: 1
url: http://localhost:5173   # dev server
start: npm run dev
check: npm test
flows: login validation, empty state
runtime: chromium
credentials:
  username_env: QA_USER
  password_env: QA_PASS
EOF
CASE_CWD="$T/simple"
echo "simple schema:"
diff_case "simple-probe-local" probe --env local
diff_case "simple-default-env-implicit" default-env
diff_case "simple-list" list
diff_case "simple-check-clean" check
diff_case "simple-probe-other-miss" probe --env staging

mk_repo "$T/simple2"
cat > "$T/simple2/.babysit/qa.yaml" <<'EOF'
url: "http://a:3000"
guideline: 'skip marketing'
prepare: npm i && npm run db:migrate
revert: npm run db:rollback
default_env: local
EOF
CASE_CWD="$T/simple2"
diff_case "simple-quoted-values" probe --env local
diff_case "simple-prepare-revert" probe --env local
diff_case "simple-explicit-default-env" default-env

mk_repo "$T/quirks"
cat > "$T/quirks/.babysit/qa.yaml" <<'EOF'
url: http://x/it's got 'quotes' and a | pipe   # and a comment
guideline: |
flows: a, b
EOF
CASE_CWD="$T/quirks"
echo "parser quirks:"
diff_case "quirk-shquote-pipe-comment" probe --env local
diff_case "quirk-guideline-block-scalar" probe --env local

# simple parser skips the whole rest of file after `environments:` (bash bug
# kept for parity) — the url below the header must vanish.
mk_repo "$T/after-envs"
cat > "$T/after-envs/.babysit/qa.yaml" <<'EOF'
environments:
  - name: a
    url: http://a
url: http://simple-should-be-ignored
EOF
CASE_CWD="$T/after-envs"
diff_case "quirk-simple-dead-after-environments" probe --env local
diff_case "quirk-simple-dead-after-environments-list" list

# simple-shape keys are column-0 only: an indented url under qa: is invisible.
mk_repo "$T/qa-indented"
cat > "$T/qa-indented/.babysit/qa.yaml" <<'EOF'
qa:
  url: http://indented
EOF
CASE_CWD="$T/qa-indented"
diff_case "quirk-qa-parent-simple-invisible" probe --env local
diff_case "quirk-qa-parent-simple-default-env" default-env

# ── Named-environments schema ───────────────────────────────────────────
mk_repo "$T/envs"
cat > "$T/envs/.babysit/qa.yaml" <<'EOF'
version: 1
default_env: staging
environments:
  - name: local
    url: http://localhost:5173
    runtime: chromium
    guideline: Skip the marketing pages.
    credentials:
      username_env: QA_USER
      password_env: QA_PASS
  - name: "staging"
    url: 'https://staging.example.com'
    credentials:
      username_env: STG_USER
EOF
CASE_CWD="$T/envs"
echo "environments schema:"
diff_case "envs-probe-local" probe --env local
diff_case "envs-probe-staging-quoted-name" probe --env staging
diff_case "envs-default-env" default-env
diff_case "envs-list" list
diff_case "envs-probe-missing" probe --env prod
diff_case "envs-check-clean" check

mk_repo "$T/envs-qa-parent"
cat > "$T/envs-qa-parent/.babysit/qa.yaml" <<'EOF'
qa:
  default_env: dev
  environments:
    - name: dev
      url: http://dev:8080
EOF
CASE_CWD="$T/envs-qa-parent"
diff_case "envs-under-qa-parent" probe --env dev
diff_case "envs-under-qa-parent-default" default-env

mk_repo "$T/envs-edge"
cat > "$T/envs-edge/.babysit/qa.yaml" <<'EOF'
environments:
  - name: dup
    url: http://first
  - name: dup
    url: http://second
    runtime: firefox
  - name: creds-interrupted
    url: http://ci
    credentials:
      username_env: U1
    url: http://ci2
    password_env: STRAY
  - name: nourle
    runtime: chromium
EOF
CASE_CWD="$T/envs-edge"
diff_case "envs-duplicate-name-last-wins" probe --env dup
diff_case "envs-creds-reset-by-url" probe --env creds-interrupted
diff_case "envs-cred-outside-creds-ignored" probe --env creds-interrupted
diff_case "envs-list-dedup" list
diff_case "envs-check-missing-url" check
diff_case "envs-probe-no-url-not-found" probe --env nourle

# second `environments:` header discards the pending env without flushing.
mk_repo "$T/envs-twice"
cat > "$T/envs-twice/.babysit/qa.yaml" <<'EOF'
environments:
  - name: lost
    url: http://lost
environments:
  - name: kept
    url: http://kept
EOF
CASE_CWD="$T/envs-twice"
diff_case "envs-second-header-discards-pending" list
diff_case "envs-second-header-probe-lost" probe --env lost

# ── Precedence across qa.yaml / qa.local.yaml ───────────────────────────
mk_repo "$T/prec"
cat > "$T/prec/.babysit/qa.yaml" <<'EOF'
default_env: local
prepare: npm run prep:committed
environments:
  - name: local
    url: http://committed:3000
    runtime: chromium
EOF
cat > "$T/prec/.babysit/qa.local.yaml" <<'EOF'
default_env: overridden
revert: npm run revert:local
environments:
  - name: local
    url: http://local-override:3000
  - name: extra
    guideline: only in local file
EOF
CASE_CWD="$T/prec"
echo "precedence:"
diff_case "prec-url-local-overrides" probe --env local
diff_case "prec-default-env-local-wins" default-env
diff_case "prec-prepare-from-qa-revert-from-local" probe --env local
diff_case "prec-list-union" list
diff_case "prec-check-extra-missing-url" check

# field mixing: url only in qa.yaml, runtime only in qa.local.yaml — SOURCE
# still names qa.yaml (bash bug kept for parity).
mk_repo "$T/mix"
cat > "$T/mix/.babysit/qa.yaml" <<'EOF'
environments:
  - name: mixed
    url: http://from-committed
EOF
cat > "$T/mix/.babysit/qa.local.yaml" <<'EOF'
environments:
  - name: mixed
    runtime: firefox
EOF
CASE_CWD="$T/mix"
diff_case "prec-field-mixing-source-bug" probe --env mixed

# simple shape overrides environments shape for env "local" within one file.
mk_repo "$T/shape-prec"
cat > "$T/shape-prec/.babysit/qa.yaml" <<'EOF'
url: http://simple-wins
environments:
  - name: local
    url: http://envs-block
EOF
CASE_CWD="$T/shape-prec"
diff_case "prec-simple-overrides-envs-same-file" probe --env local

# ── probe argument quirks ───────────────────────────────────────────────
CASE_CWD="$T/envs"
echo "probe args:"
diff_case "probe-no-args-exit2" probe
diff_case "probe-empty-env-exit2" probe --env ""
diff_case "probe-dangling-env-silent-exit1" probe --env
diff_case "probe-dangling-repo-silent-exit1" probe --env local --repo
diff_case "probe-repo-value-consumed" probe --repo somewhere --env local
diff_case "probe-unknown-args-ignored" probe -x whatever --env local
diff_case "probe-env-twice-last-wins" probe --env local --env staging

# ── leak-check ──────────────────────────────────────────────────────────
mk_repo "$T/leak"
cat > "$T/leak/.babysit/qa.yaml" <<'EOF'
url: http://x
password: hunter2
  token: abc
api_key : k
secretish: not-a-hit
credentials:
  username_env: QA_USER
EOF
printf 'password: real\ntoken: real\n' > "$T/leak/.babysit/qa.local.yaml"
printf 'password: x\n' > "$T/leak/foo-qa.local.yaml"
CASE_CWD="$T/leak"
echo "leak-check:"
diff_case "leak-no-arg-exit2" leak-check
diff_case "leak-missing-file-exit2" leak-check nope.yaml
diff_case "leak-dir-exit2" leak-check .babysit
diff_case "leak-hits-with-line-numbers" leak-check .babysit/qa.yaml
diff_case "leak-local-suffix-skipped" leak-check .babysit/qa.local.yaml
diff_case "leak-any-local-suffix-skipped" leak-check foo-qa.local.yaml
diff_case "leak-clean-file" leak-check .git/HEAD
diff_case "check-accumulates-leak-and-local-skip" check

mk_repo "$T/leak2"
cat > "$T/leak2/.babysit/qa.yaml" <<'EOF'
environments:
  - name: a
    runtime: chromium
token: oops
EOF
CASE_CWD="$T/leak2"
diff_case "check-missing-url-plus-leak" check

# ── Repo-toplevel resolution (a–e) ──────────────────────────────────────
# The bash resolves config via `git rev-parse --show-toplevel`; the port must
# agree in every shape babysit actually runs in — most importantly linked
# worktrees (.git is a FILE there) and env-var overrides.
echo "toplevel resolution:"

# (a) linked worktree
mk_repo "$T/wt-main"
git -C "$T/wt-main" -c user.name=t -c user.email=t@e commit -q --allow-empty -m init
git -C "$T/wt-main" worktree add -q "$T/wt-linked" >/dev/null 2>&1
mkdir -p "$T/wt-linked/.babysit"
printf 'url: http://from-worktree\n' > "$T/wt-linked/.babysit/qa.yaml"
CASE_CWD="$T/wt-linked"
diff_case "toplevel-linked-worktree" probe --env local

# (b) symlinked path to the repo (PWD carries the logical path). The config
# also declares an env without a url so `check` emits a diagnostic embedding
# the resolved toplevel path — git prints the physical path there, and a
# resolver that returns the logical PWD path would diverge visibly.
mk_repo "$T/real-repo"
cat > "$T/real-repo/.babysit/qa.yaml" <<'EOF'
url: http://via-symlink
environments:
  - name: no-url-env
    runtime: chromium
EOF
ln -s "$T/real-repo" "$T/repo-link"
CASE_CWD="$T/repo-link"; CASE_ENV=(PWD="$T/repo-link")
diff_case "toplevel-symlinked-path" probe --env local
diff_case "toplevel-symlinked-path-check" check
CASE_ENV=()

# (c) GIT_DIR / GIT_WORK_TREE from an unrelated cwd
mk_repo "$T/gwt-repo"
printf 'url: http://via-gitdir\n' > "$T/gwt-repo/.babysit/qa.yaml"
mkdir -p "$T/plain"
CASE_CWD="$T/plain"; CASE_ENV=(GIT_DIR="$T/gwt-repo/.git" GIT_WORK_TREE="$T/gwt-repo")
diff_case "toplevel-git-env-overrides" probe --env local
CASE_ENV=()

# (d) nested subdir deep in the tree
mk_repo "$T/deep"
printf 'url: http://from-root\n' > "$T/deep/.babysit/qa.yaml"
mkdir -p "$T/deep/a/b/c"
CASE_CWD="$T/deep/a/b/c"
diff_case "toplevel-nested-subdir" probe --env local
diff_case "toplevel-nested-subdir-default-env" default-env

# (e) outside any repo
mkdir -p "$T/norepo"
CASE_CWD="$T/norepo"
diff_case "toplevel-norepo-list" list
diff_case "toplevel-norepo-default-env" default-env
diff_case "toplevel-norepo-check" check
diff_case "toplevel-norepo-probe" probe --env local
diff_case "toplevel-norepo-leak-check-relative" leak-check missing.yaml

# ── Summary ─────────────────────────────────────────────────────────────
echo
echo "pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'failed: %s\n' "${FAIL_NAMES[*]}"
  exit 1
fi
exit 0
