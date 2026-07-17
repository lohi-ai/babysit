#!/usr/bin/env bash
# tests/test_bbs_secrets_differential.sh — differential guard for the bbs-secrets Go port.
#
# `bbs secrets` replaced bin/bbs-secrets, the project-local dotenv auto-loader
# (load / seed / ensure-gitignore). The bin only ever touches the filesystem, so
# the sandbox is a throwaway tree per case: a work dir the impl runs from (load
# walks up for .babysit/) and a repo-root it seeds into. No real credentials,
# no real ~/.babysit — everything lives under a mktemp root wiped on exit.
#
# For each case the frozen pre-port bash (tests/fixtures/bbs-secrets.reference)
# and the Go binary run under an identical environment and cwd; we then diff all
# three channels — stdout, stderr, exit — plus any file the case produces
# (the seeded .env, the .gitignore). Absolute paths in `created:/exists:` output
# are root-normalized so only structural differences remain. `help` bytes are
# NOT compared: the bash renders its header through a sed pipeline whose `\?`
# differs between BSD (macOS) and GNU (Linux) sed, so its output is
# platform-dependent; help is non-load-bearing, so we assert exit 0 + the three
# subcommand names appear, on both impls.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-secrets.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

# mk_env <impl> — a private sandbox with an entrypoint, a work/ dir the impl is
# run from, and a repo/ dir cases seed into. The bash oracle also needs the
# lib/load-env-file.sh it sources (SCRIPT_DIR/../lib); the Go side gets a
# bbs-secrets symlink to the multicall — the same argv[0]-dispatch shape we ship.
mk_env() {
  local impl="$1"
  local r="$T/$CASE/$impl"
  mkdir -p "$r/bin" "$r/lib" "$r/work" "$r/repo"
  if [ "$impl" = bash ]; then
    cp "$REFERENCE" "$r/bin/bbs-secrets"
    chmod +x "$r/bin/bbs-secrets"
    cp "$REPO/lib/load-env-file.sh" "$r/lib/load-env-file.sh"
  else
    cp "$BIN" "$r/bin/bbs"
    ln -s bbs "$r/bin/bbs-secrets"
  fi
  printf '%s' "$r"
}

# run_impl <root> <cwd-rel> [env=VAL ...] -- [args...] — run the entrypoint from
# <root>/<cwd-rel>, injecting any KEY=VAL pairs before `--`, capturing channels.
run_impl() {
  local r="$1" cwd="$2"; shift 2
  local envs=()
  while [ $# -gt 0 ] && [ "$1" != "--" ]; do envs+=("$1"); shift; done
  shift # drop --
  ( cd "$r/$cwd" && env "${envs[@]}" "$r/bin/bbs-secrets" "$@" ) > "$r/out" 2> "$r/err"
  echo "$?" > "$r/rc"
}

# norm <root> <file> — strip the per-case sandbox root so the two impls' paths
# (they run under sibling dirs) don't register as differences.
norm() { sed -E -e "s#$1#ROOT#g" "$2" 2>/dev/null; }

# diff_channels <name> <bash-root> <go-root> — stdout, stderr, exit.
diff_channels() {
  local name="$1" b="$2" g="$3" why=""
  [ "$(cat "$b/rc")" = "$(cat "$g/rc")" ] || why="exit ${name}: bash=$(cat "$b/rc") go=$(cat "$g/rc")"
  [ -z "$why" ] && ! diff <(norm "$b" "$b/out") <(norm "$g" "$g/out") >/dev/null && why="stdout differs"
  [ -z "$why" ] && ! diff <(norm "$b" "$b/err") <(norm "$g" "$g/err") >/dev/null && why="stderr differs"
  echo "$why"
}

# diff_file <bash-file> <go-file> — a produced artifact. Both absent is a match.
diff_file() {
  local bf="$1" gf="$2" ba="absent" ga="absent"
  [ -f "$bf" ] && ba="$(cat "$bf")"
  [ -f "$gf" ] && ga="$(cat "$gf")"
  [ "$ba" = "$ga" ]
}

# Sample .env exercising every parse branch: comment, double/single quotes,
# empty-quoted, inline ` #` comment, ${...} placeholder (skipped), duplicate key
# (first wins), and a key pre-exported in the shell (shell wins → not emitted).
write_sample_env() {
  cat > "$1" <<'EOF'
# a comment
QA_USER=alice
QA_PASS="p@ss w'ord"
EMPTYQUOTED=''
INLINE=value # trailing comment
PLACEHOLDER=${SOMETHING}
DUP=first
DUP=second
ALREADY_SET=fromfile
EOF
}

# ─── Case 1: load — mixed .env, one shell-shadowed key ───────────────────────
CASE=load_mixed
BR="$(mk_env bash)"; GR="$(mk_env go)"
mkdir -p "$BR/work/.babysit" "$GR/work/.babysit"
write_sample_env "$BR/work/.babysit/.env"
write_sample_env "$GR/work/.babysit/.env"
run_impl "$BR" work ALREADY_SET=fromshell -- load
run_impl "$GR" work ALREADY_SET=fromshell -- load
why="$(diff_channels load_mixed "$BR" "$GR")"
# Positive assertion so a both-empty regression can't pass: the single-quote
# escaping and shadow-skip must actually appear.
[ -z "$why" ] && grep -q "export QA_PASS='p@ss w'\\\\''ord'" "$GR/out" || why="${why:-go missing escaped QA_PASS}"
[ -z "$why" ] && ! grep -q ALREADY_SET "$GR/out" || why="${why:-shell-set key leaked into output}"
[ -z "$why" ] && ok "load: mixed .env, quotes/escape/dup/placeholder/shadow parity" || fail "load mixed" "$why"

# ─── Case 2: load — nested cwd walks up to .babysit ancestor ─────────────────
CASE=load_walkup
BR="$(mk_env bash)"; GR="$(mk_env go)"
mkdir -p "$BR/work/.babysit" "$GR/work/.babysit" "$BR/work/a/b/c" "$GR/work/a/b/c"
printf 'FOO=bar\n' > "$BR/work/.babysit/.env"
printf 'FOO=bar\n' > "$GR/work/.babysit/.env"
run_impl "$BR" work/a/b/c -- load
run_impl "$GR" work/a/b/c -- load
why="$(diff_channels load_walkup "$BR" "$GR")"
[ -z "$why" ] && grep -q "export FOO='bar'" "$GR/out" || why="${why:-go did not walk up to .babysit}"
[ -z "$why" ] && ok "load: walks up from nested cwd to nearest .babysit" || fail "load walkup" "$why"

# ─── Case 3: load — no .babysit ancestor → silent no-op ──────────────────────
CASE=load_no_repo
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR" work -- load
run_impl "$GR" work -- load
why="$(diff_channels load_no_repo "$BR" "$GR")"
[ -z "$why" ] && [ ! -s "$GR/out" ] && [ "$(cat "$GR/rc")" = 0 ] || why="${why:-expected empty stdout, exit 0}"
[ -z "$why" ] && ok "load: no repo root → empty output, exit 0" || fail "load no-repo" "$why"

# ─── Case 4: load — repo present but no .env file → no-op ─────────────────────
CASE=load_no_env
BR="$(mk_env bash)"; GR="$(mk_env go)"
mkdir -p "$BR/work/.babysit" "$GR/work/.babysit"
run_impl "$BR" work -- load
run_impl "$GR" work -- load
why="$(diff_channels load_no_env "$BR" "$GR")"
[ -z "$why" ] && [ ! -s "$GR/out" ] || why="${why:-expected empty stdout}"
[ -z "$why" ] && ok "load: .babysit but no .env → empty output" || fail "load no-env" "$why"

# ─── Case 5: seed — fresh repo (creates .env + .gitignore) ───────────────────
CASE=seed_fresh
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR" . -- seed --repo-root "$BR/repo" QA_USER QA_PASS
run_impl "$GR" . -- seed --repo-root "$GR/repo" QA_USER QA_PASS
why="$(diff_channels seed_fresh "$BR" "$GR")"
[ -z "$why" ] && ! diff_file "$BR/repo/.babysit/.env" "$GR/repo/.babysit/.env" && why="seeded .env differs"
[ -z "$why" ] && ! diff_file "$BR/repo/.gitignore" "$GR/repo/.gitignore" && why=".gitignore differs"
[ -z "$why" ] && grep -q "^created: " "$GR/out" && grep -q "# QA_USER=" "$GR/repo/.babysit/.env" || why="${why:-go missing created line or placeholders}"
[ -z "$why" ] && ok "seed: fresh → .env placeholders + gitignore + 'created:' parity" || fail "seed fresh" "$why"

# ─── Case 6: seed — existing .env is left untouched ──────────────────────────
CASE=seed_exists
BR="$(mk_env bash)"; GR="$(mk_env go)"
mkdir -p "$BR/repo/.babysit" "$GR/repo/.babysit"
printf 'PRE=existing\n' > "$BR/repo/.babysit/.env"
printf 'PRE=existing\n' > "$GR/repo/.babysit/.env"
run_impl "$BR" . -- seed --repo-root "$BR/repo" NEWVAR
run_impl "$GR" . -- seed --repo-root "$GR/repo" NEWVAR
why="$(diff_channels seed_exists "$BR" "$GR")"
[ -z "$why" ] && ! diff_file "$BR/repo/.babysit/.env" "$GR/repo/.babysit/.env" && why=".env should be untouched"
[ -z "$why" ] && grep -q "^exists: " "$GR/out" && [ "$(cat "$GR/repo/.babysit/.env")" = "PRE=existing" ] || why="${why:-expected 'exists:' + unchanged file}"
[ -z "$why" ] && ok "seed: existing .env → 'exists:', file untouched" || fail "seed exists" "$why"

# ─── Case 7: seed — missing --repo-root → exit 2 ─────────────────────────────
CASE=seed_no_root
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR" . -- seed QA_USER
run_impl "$GR" . -- seed QA_USER
why="$(diff_channels seed_no_root "$BR" "$GR")"
[ -z "$why" ] && [ "$(cat "$GR/rc")" = 2 ] && grep -q "required" "$GR/err" || why="${why:-expected exit 2 + 'required'}"
[ -z "$why" ] && ok "seed: missing --repo-root → exit 2, stderr parity" || fail "seed no-root" "$why"

# ─── Case 8: seed — --repo-root not a directory → exit 2 ─────────────────────
CASE=seed_not_dir
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR" . -- seed --repo-root "$BR/repo/nope" QA_USER
run_impl "$GR" . -- seed --repo-root "$GR/repo/nope" QA_USER
why="$(diff_channels seed_not_dir "$BR" "$GR")"
[ -z "$why" ] && [ "$(cat "$GR/rc")" = 2 ] && grep -q "not a directory" "$GR/err" || why="${why:-expected exit 2 + 'not a directory'}"
[ -z "$why" ] && ok "seed: --repo-root non-dir → exit 2, stderr parity" || fail "seed not-dir" "$why"

# ─── Case 9: ensure-gitignore — fresh → 'added' ──────────────────────────────
CASE=gi_fresh
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR" . -- ensure-gitignore --repo-root "$BR/repo"
run_impl "$GR" . -- ensure-gitignore --repo-root "$GR/repo"
why="$(diff_channels gi_fresh "$BR" "$GR")"
[ -z "$why" ] && ! diff_file "$BR/repo/.gitignore" "$GR/repo/.gitignore" && why=".gitignore differs"
[ -z "$why" ] && [ "$(cat "$GR/out")" = "added" ] && [ "$(cat "$GR/repo/.gitignore")" = ".babysit/.env" ] || why="${why:-expected 'added' + gitignore line}"
[ -z "$why" ] && ok "ensure-gitignore: fresh → 'added', line appended" || fail "gi fresh" "$why"

# ─── Case 10: ensure-gitignore — already present → 'present', idempotent ──────
CASE=gi_present
BR="$(mk_env bash)"; GR="$(mk_env go)"
printf 'node_modules\n.babysit/.env\n' > "$BR/repo/.gitignore"
printf 'node_modules\n.babysit/.env\n' > "$GR/repo/.gitignore"
run_impl "$BR" . -- ensure-gitignore --repo-root "$BR/repo"
run_impl "$GR" . -- ensure-gitignore --repo-root "$GR/repo"
why="$(diff_channels gi_present "$BR" "$GR")"
[ -z "$why" ] && ! diff_file "$BR/repo/.gitignore" "$GR/repo/.gitignore" && why=".gitignore mutated"
[ -z "$why" ] && [ "$(cat "$GR/out")" = "present" ] && [ "$(cat "$GR/repo/.gitignore")" = "$(printf 'node_modules\n.babysit/.env')" ] || why="${why:-expected 'present' + untouched file}"
[ -z "$why" ] && ok "ensure-gitignore: present → 'present', file untouched" || fail "gi present" "$why"

# ─── Case 11: unknown subcommand → exit 2 ────────────────────────────────────
CASE=unknown
BR="$(mk_env bash)"; GR="$(mk_env go)"
run_impl "$BR" . -- frobnicate
run_impl "$GR" . -- frobnicate
why="$(diff_channels unknown "$BR" "$GR")"
[ -z "$why" ] && [ "$(cat "$GR/rc")" = 2 ] && grep -q "unknown subcommand: frobnicate" "$GR/err" || why="${why:-expected exit 2 + unknown subcommand}"
[ -z "$why" ] && ok "unknown subcommand → exit 2, stderr parity" || fail "unknown" "$why"

# ─── Case 12: help — exit 0 + subcommand names (bytes NOT compared) ───────────
# The bash help renders through a `\?`-sensitive sed pipeline (BSD vs GNU), so
# byte parity is impossible cross-platform; assert structure on both impls.
CASE=help
GR="$(mk_env go)"; BR="$(mk_env bash)"
help_ok() {
  local r="$1"
  run_impl "$r" . -- help
  [ "$(cat "$r/rc")" = 0 ] || { echo "exit $(cat "$r/rc")"; return; }
  for sub in load seed ensure-gitignore; do
    grep -q "$sub" "$r/out" || { echo "help missing '$sub'"; return; }
  done
  echo ""
}
why="$(help_ok "$GR")"; [ -z "$why" ] && why="$(help_ok "$BR")"
[ -z "$why" ] && ok "help: exit 0 + names present on both impls (bytes platform-free)" || fail "help" "$why"

# ─── Summary ─────────────────────────────────────────────────────────────────
echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d/%d bbs-secrets differential cases\n' "$PASS" "$((PASS + FAIL))"
else
  printf '\033[0;31mFAIL\033[0m  %d/%d passed; failures: %s\n' "$PASS" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
  exit 1
fi
