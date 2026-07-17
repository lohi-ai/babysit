#!/usr/bin/env bash
# tests/test_bbs_codex_competitive_differential.sh — differential guard for the
# bbs-codex-competitive Go port.
#
# `bbs codex-competitive` replaced the bin/bbs-codex-competitive bash script.
# Rather than assert hand-written goldens, every case runs the frozen pre-port
# bash (tests/fixtures/bbs-codex-competitive.reference) and the Go binary side
# by side under an identical environment and diffs all three channels — so any
# drift from the original is a failure, not a judgement call.
#
# The bin writes symlinks into $HOME and --root, and its apply path does an
# unconditional `rm -rf` of AGENTS.md and .agents/skills. Every case therefore
# pins HOME, CODEX_HOME and --root inside a mktemp -d sandbox, and every
# destructive path is gated on sandboxed() below. env -i is NOT the safety
# mechanism — the path assertion is.
#
# The bin makes no network calls (symlink syscalls only), so there is nothing
# to stub: no test here reaches the network.

set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO/tests/fixtures/bbs-codex-competitive.reference"
[ -f "$REFERENCE" ] || { echo "FAIL: missing oracle $REFERENCE" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "SKIP: go not installed" >&2; exit 0; }

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

# ── Blast-radius guard ─────────────────────────────────────────────────
# The bin deletes whatever sits at <root>/AGENTS.md and <root>/.agents/skills,
# and writes into <codex_home>/skills. A typo or an unset var in a case is the
# difference between a green suite and losing the developer's real ~/.codex or
# AGENTS.md. Assert containment before any case that can write or delete;
# abort the whole run — not just the case — if a path escapes the sandbox.
sandboxed() {
  local p
  for p in "$@"; do
    case "$p" in
      "$T"/*) ;;
      *) printf '\033[0;31mABORT\033[0m  refusing to run: path escapes sandbox %s: [%s]\n' "$T" "$p" >&2
         exit 99 ;;
    esac
  done
  # A sandbox that isn't a real temp dir means mktemp -d failed or T got clobbered.
  [ -n "$T" ] && [ -d "$T" ] || {
    printf '\033[0;31mABORT\033[0m  sandbox root missing: [%s]\n' "$T" >&2; exit 99; }
}
# Self-test the guard: it must reject the very paths this suite exists to protect.
( sandboxed "$HOME/.codex" ) 2>/dev/null && { echo "FAIL: guard let \$HOME/.codex through" >&2; exit 1; }
( sandboxed "$T/ok" "/etc/passwd" ) 2>/dev/null && { echo "FAIL: guard let /etc/passwd through" >&2; exit 1; }
sandboxed "$T/ok" || { echo "FAIL: guard rejected a sandboxed path" >&2; exit 1; }

BIN="$T/bbs"
(cd "$REPO" && go build -o "$BIN" ./cmd/bbs) || { echo "FAIL: go build" >&2; exit 1; }

ORACLE="$T/oracle/bbs-codex-competitive"
mkdir -p "$T/oracle" "$T/go"
cp "$REFERENCE" "$ORACLE"; chmod +x "$ORACLE"
cp "$BIN" "$T/go/bbs"
ln -sf bbs "$T/go/bbs-codex-competitive"   # multicall: basename -> `codex-competitive`
GOBIN="$T/go/bbs-codex-competitive"

# ── Case harness ───────────────────────────────────────────────────────
# Each case builds TWO identical sandboxes — one per implementation — so the
# side effects of one never leak into the other's view of the world.
#
# mk_root <dir> [skill...] — a project root the bin will accept.
mk_root() {
  local r="$1"; shift
  sandboxed "$r"
  mkdir -p "$r/.claude/skills"
  printf '# CLAUDE.md\n' > "$r/CLAUDE.md"
  local s
  for s in "$@"; do mkdir -p "$r/.claude/skills/$s"; done
}

# run_one <impl-bin> <root> <home> <codex_home> <args...>
# CODEX_HOME is passed through only when non-empty, so a case can exercise the
# $HOME/.codex + $HOME/.Codex branch.
run_one() {
  local bin="$1" root="$2" home="$3" codex="$4"; shift 4
  sandboxed "$root" "$home"
  [ -z "$codex" ] || sandboxed "$codex"
  local -a envv=(PATH="$PATH" HOME="$home")
  [ -z "$codex" ] || envv+=(CODEX_HOME="$codex")
  ( cd "$root" && env -i "${envv[@]}" "$bin" --root "$root" "$@" )
}

# diff_case <desc> <setup-fn> <args...>
# setup-fn is called as `setup-fn <root> <home>` for each sandbox in turn.
CASE_CODEX=""
diff_case() {
  local desc="$1" setup="$2"; shift 2
  local base="$T/case/$RANDOM$RANDOM"
  local oroot="$base/o/root" ohome="$base/o/home" groot="$base/g/root" ghome="$base/g/home"
  sandboxed "$oroot" "$ohome" "$groot" "$ghome"
  mkdir -p "$oroot" "$ohome" "$groot" "$ghome"

  local ocodex="" gcodex=""
  if [ -n "$CASE_CODEX" ]; then ocodex="$base/o/$CASE_CODEX"; gcodex="$base/g/$CASE_CODEX"; fi

  "$setup" "$oroot" "$ohome"
  "$setup" "$groot" "$ghome"

  local orc grc msg=""
  run_one "$ORACLE" "$oroot" "$ohome" "$ocodex" "$@" >"$base/o.out" 2>"$base/o.err"; orc=$?
  run_one "$GOBIN"  "$groot" "$ghome" "$gcodex" "$@" >"$base/g.out" 2>"$base/g.err"; grc=$?

  # Roots differ by path (o/ vs g/), so normalize before diffing: the bin prints
  # absolute paths and we care about the shape, not the prefix.
  sed "s|$base/o|ROOT|g" "$base/o.out" > "$base/o.out.n"
  sed "s|$base/g|ROOT|g" "$base/g.out" > "$base/g.out.n"
  sed "s|$base/o|ROOT|g" "$base/o.err" > "$base/o.err.n"
  sed "s|$base/g|ROOT|g" "$base/g.err" > "$base/g.err.n"

  [ "$orc" = "$grc" ] || msg="exit bash=$orc go=$grc;"
  cmp -s "$base/o.out.n" "$base/g.out.n" || msg="$msg stdout[$(diff "$base/o.out.n" "$base/g.out.n" | tr '\n' '|')]"
  cmp -s "$base/o.err.n" "$base/g.err.n" || msg="$msg stderr[$(diff "$base/o.err.n" "$base/g.err.n" | tr '\n' '|')]"

  # Filesystem parity: the tree each implementation left behind must match,
  # symlink targets included. This is what catches an apply path that prints
  # the right words and links the wrong thing.
  ( cd "$base/o" && find . -exec sh -c 'for f; do printf "%s %s\n" "$f" "$(readlink "$f" 2>/dev/null)"; done' _ {} + | sed "s|$base/o|ROOT|g" | sort ) > "$base/o.tree"
  ( cd "$base/g" && find . -exec sh -c 'for f; do printf "%s %s\n" "$f" "$(readlink "$f" 2>/dev/null)"; done' _ {} + | sed "s|$base/g|ROOT|g" | sort ) > "$base/g.tree"
  cmp -s "$base/o.tree" "$base/g.tree" || msg="$msg tree[$(diff "$base/o.tree" "$base/g.tree" | tr '\n' '|')]"

  if [ -z "$msg" ]; then ok "$desc"; else fail "$desc" "$msg"; fi
}

setup_plain()   { mk_root "$1" alpha beta; }
setup_empty()   { mk_root "$1"; }
setup_nodoc()   { sandboxed "$1"; mkdir -p "$1/.claude/skills"; }
setup_noskills(){ sandboxed "$1"; mkdir -p "$1"; printf '# CLAUDE.md\n' > "$1/CLAUDE.md"; }
setup_filtered(){ mk_root "$1" alpha references shared; mkdir -p "$1/.claude/skills/.hidden"; }
setup_notdir()  { mk_root "$1" alpha; : > "$1/.claude/skills/loose-file"; }

# ── Detection / would-link output ──────────────────────────────────────
echo "check / dry-run / apply:"
CASE_CODEX="codexhome"
diff_case "check-all-missing-is-stale"       setup_plain --check
diff_case "dry-run-all-missing"              setup_plain --dry-run
diff_case "apply-creates-links"              setup_plain
diff_case "apply-then-check-is-current"      setup_plain
diff_case "project-only"                     setup_plain --project-only
diff_case "global-only"                      setup_plain --global-only
diff_case "project-only-check"               setup_plain --project-only --check
diff_case "global-only-check"                setup_plain --global-only --check
diff_case "project-only-dry-run"             setup_plain --project-only --dry-run
diff_case "empty-skills-dir"                 setup_empty
diff_case "empty-skills-dir-check"           setup_empty --check
diff_case "filters-references-shared-dotted" setup_filtered
diff_case "skips-non-directory-entries"      setup_notdir

# ── Missing sources ────────────────────────────────────────────────────
echo "missing sources:"
diff_case "missing-CLAUDE.md"        setup_nodoc
diff_case "missing-skills-dir"       setup_noskills
diff_case "missing-CLAUDE.md-check"  setup_nodoc --check

# ── Flag grammar ───────────────────────────────────────────────────────
echo "flag grammar:"
diff_case "help-long"              setup_plain --help
diff_case "help-short"             setup_plain -h
diff_case "help-outruns-mutex"     setup_plain --project-only --global-only --help
diff_case "unknown-option"         setup_plain --bogus
diff_case "unknown-option-outruns-mutex" setup_plain --project-only --global-only --bogus
diff_case "mutually-exclusive"     setup_plain --project-only --global-only
diff_case "check-and-dry-run-check-wins" setup_plain --check --dry-run
diff_case "repeated-flags"         setup_plain --check --check

# ── $HOME/.codex + $HOME/.Codex branch (CODEX_HOME unset) ──────────────
# Bug-for-bug B1: the bash's guard is a string compare that never matches, so
# BOTH homes are always used and every global link is emitted twice.
echo "codex_homes (CODEX_HOME unset):"
CASE_CODEX=""
diff_case "home-codex-and-Codex-both-linked" setup_plain
diff_case "home-codex-and-Codex-check"       setup_plain --check
diff_case "home-codex-and-Codex-dry-run"     setup_plain --dry-run
CASE_CODEX="codexhome"

# ── Idempotency / repair ───────────────────────────────────────────────
echo "idempotency:"
(
  base="$T/idem"; oroot="$base/o/root"; ohome="$base/o/home"; groot="$base/g/root"; ghome="$base/g/home"
  sandboxed "$oroot" "$ohome" "$groot" "$ghome"
  mkdir -p "$oroot" "$ohome" "$groot" "$ghome"
  mk_root "$oroot" alpha; mk_root "$groot" alpha
  run_one "$ORACLE" "$oroot" "$ohome" "$base/o/codexhome" >/dev/null 2>&1
  run_one "$GOBIN"  "$groot" "$ghome" "$base/g/codexhome" >/dev/null 2>&1
  o2="$(run_one "$ORACLE" "$oroot" "$ohome" "$base/o/codexhome" --check 2>&1)"; orc=$?
  g2="$(run_one "$GOBIN"  "$groot" "$ghome" "$base/g/codexhome" --check 2>&1)"; grc=$?
  [ "$orc" = 0 ] && [ "$grc" = 0 ] || { echo "second --check: bash=$orc go=$grc"; exit 1; }
  [ "$o2" = "$g2" ] || { echo "bash=[$o2] go=[$g2]"; exit 1; }
  [ "$g2" = "Codex symlinks are current" ] || { echo "unexpected: [$g2]"; exit 1; }
) && ok "apply-is-idempotent-and-check-clean" || fail "apply-is-idempotent-and-check-clean"

# Re-apply over an existing symlink takes the `ln -sfn` branch (not `ln -s`,
# which would fail on an existing path).
diff_case "reapply-over-existing-links" setup_plain
echo "repair:"
setup_stale_doc() {
  mk_root "$1" alpha
  ln -s WRONG.md "$1/AGENTS.md"
  ln -s ../wrong "$1/.agents-tmp" 2>/dev/null || true
}
diff_case "repairs-wrong-doc-symlink"       setup_stale_doc
diff_case "repairs-wrong-doc-symlink-check" setup_stale_doc --check

setup_stale_global() {
  local root="$1" home="$2"
  mk_root "$root" alpha
  sandboxed "$home"
  mkdir -p "$home/codexhome/skills"
  ln -s /nowhere "$home/codexhome/skills/bbs:alpha"
}
# Pins the CODEX_HOME branch by passing it to run_one directly, so only the one
# home is linked and the repair is unambiguous.
(
  base="$T/staleglobal"; groot="$base/g/root"; ghome="$base/g/home"; oroot="$base/o/root"; ohome="$base/o/home"
  sandboxed "$oroot" "$ohome" "$groot" "$ghome"
  mkdir -p "$oroot" "$ohome" "$groot" "$ghome"
  setup_stale_global "$oroot" "$ohome"; setup_stale_global "$groot" "$ghome"
  o="$(run_one "$ORACLE" "$oroot" "$ohome" "$ohome/codexhome" 2>&1)"; orc=$?
  g="$(run_one "$GOBIN"  "$groot" "$ghome" "$ghome/codexhome" 2>&1)"; grc=$?
  [ "$orc" = "$grc" ] || { echo "exit bash=$orc go=$grc"; exit 1; }
  ot="$(readlink "$ohome/codexhome/skills/bbs:alpha")"; gt="$(readlink "$ghome/codexhome/skills/bbs:alpha")"
  [ "$ot" = "$oroot/.claude/skills/alpha" ] || { echo "bash didn't repair: $ot"; exit 1; }
  [ "$gt" = "$groot/.claude/skills/alpha" ] || { echo "go didn't repair: $gt"; exit 1; }
) && ok "repairs-stale-global-symlink (ln -sfn branch)" || fail "repairs-stale-global-symlink (ln -sfn branch)"

# `exists and not symlink` — the script's own error, so it must match verbatim.
setup_blocked_global() {
  local root="$1" home="$2"
  mk_root "$root" alpha
  sandboxed "$home"
  mkdir -p "$home/codexhome/skills"
  : > "$home/codexhome/skills/bbs:alpha"   # a real file where a link belongs
}
(
  base="$T/blocked"; groot="$base/g/root"; ghome="$base/g/home"; oroot="$base/o/root"; ohome="$base/o/home"
  sandboxed "$oroot" "$ohome" "$groot" "$ghome"
  mkdir -p "$oroot" "$ohome" "$groot" "$ghome"
  setup_blocked_global "$oroot" "$ohome"; setup_blocked_global "$groot" "$ghome"
  run_one "$ORACLE" "$oroot" "$ohome" "$ohome/codexhome" >"$base/o.out" 2>"$base/o.err"; orc=$?
  run_one "$GOBIN"  "$groot" "$ghome" "$ghome/codexhome" >"$base/g.out" 2>"$base/g.err"; grc=$?
  [ "$orc" = 1 ] && [ "$grc" = 1 ] || { echo "exit bash=$orc go=$grc want 1"; exit 1; }
  oe="$(sed "s|$ohome|HOME|g" "$base/o.err")"; ge="$(sed "s|$ghome|HOME|g" "$base/g.err")"
  [ "$oe" = "$ge" ] || { echo "stderr bash=[$oe] go=[$ge]"; exit 1; }
  [ "$ge" = "exists and not symlink: HOME/codexhome/skills/bbs:alpha" ] || { echo "unexpected stderr: [$ge]"; exit 1; }
) && ok "global-dst-exists-not-symlink-errors" || fail "global-dst-exists-not-symlink-errors"

# ── B4: the unconditional rm -rf (data loss, replicated on purpose) ─────
echo "B4 — unconditional rm -rf (bug-for-bug):"
setup_real_doc() {
  mk_root "$1" alpha
  rm -f "$1/AGENTS.md"
  printf 'hand-written notes\n' > "$1/AGENTS.md"      # a real file, not a link
  mkdir -p "$1/.agents/skills/precious"
  printf 'user data\n' > "$1/.agents/skills/precious/keep.md"
}
diff_case "rm-rf-destroys-real-AGENTS.md-and-.agents-skills" setup_real_doc
(
  base="$T/b4"; root="$base/root"; home="$base/home"
  sandboxed "$root" "$home"
  mkdir -p "$root" "$home"
  setup_real_doc "$root"
  run_one "$GOBIN" "$root" "$home" "$base/codexhome" >/dev/null 2>&1 || { echo "apply failed"; exit 1; }
  # Pin the bug: the port must destroy exactly what the bash destroys.
  [ ! -e "$root/.agents/skills/precious/keep.md" ] || { echo "port spared the file — that would be a FIX, not parity"; exit 1; }
  [ -L "$root/AGENTS.md" ] || { echo "AGENTS.md not replaced by a symlink"; exit 1; }
  [ "$(readlink "$root/AGENTS.md")" = "CLAUDE.md" ] || { echo "wrong target"; exit 1; }
) && ok "B4-port-replicates-the-data-loss (not fixed)" || fail "B4-port-replicates-the-data-loss (not fixed)"

# ── --root resolution ──────────────────────────────────────────────────
echo "--root resolution:"
(
  base="$T/rootres"; sandboxed "$base"; mkdir -p "$base/o" "$base/g"
  mk_root "$base/o" alpha; mk_root "$base/g" alpha
  # --root as the final arg: `shift 2` fails under set -e → exit 1, silent.
  o="$(env -i PATH="$PATH" HOME="$base/o" "$ORACLE" --root 2>&1)"; orc=$?
  g="$(env -i PATH="$PATH" HOME="$base/g" "$GOBIN"  --root 2>&1)"; grc=$?
  [ "$orc" = "$grc" ] && [ "$grc" = 1 ] || { echo "exit bash=$orc go=$grc want 1"; exit 1; }
  [ "$o" = "$g" ] && [ -z "$g" ] || { echo "output bash=[$o] go=[$g] want empty"; exit 1; }
) && ok "root-as-final-arg-exits-1-silently (B6)" || fail "root-as-final-arg-exits-1-silently (B6)"

# Relative --root, resolved against cwd by `cd … && pwd`.
(
  base="$T/relroot"; sandboxed "$base"; mkdir -p "$base/o/nest" "$base/g/nest"
  mk_root "$base/o/nest" alpha; mk_root "$base/g/nest" alpha
  o="$(cd "$base/o" && env -i PATH="$PATH" HOME="$base/o" CODEX_HOME="$base/o/ch" "$ORACLE" --root nest --check 2>&1)"; orc=$?
  g="$(cd "$base/g" && env -i PATH="$PATH" HOME="$base/g" CODEX_HOME="$base/g/ch" "$GOBIN"  --root nest --check 2>&1)"; grc=$?
  [ "$orc" = "$grc" ] || { echo "exit bash=$orc go=$grc"; exit 1; }
  on="$(printf '%s' "$o" | sed "s|$base/o|R|g")"; gn="$(printf '%s' "$g" | sed "s|$base/g|R|g")"
  [ "$on" = "$gn" ] || { echo "bash=[$on] go=[$gn]"; exit 1; }
  # The absolute path in the output proves the relative root was expanded.
  case "$gn" in *"R/nest/.claude/skills/alpha"*) ;; *) echo "relative root not expanded: [$gn]"; exit 1 ;; esac
) && ok "relative-root-expanded-against-cwd" || fail "relative-root-expanded-against-cwd"

# Deliberate divergence D1 — the bash's own `cd` failure diagnostic names the
# script path and a bash line number ("…/bbs-codex-competitive: line 56: cd:
# /nope: No such file or directory"). That text has no native equivalent, so the
# port reproduces the contract (exit 1, nothing on stdout) and stays quiet.
(
  base="$T/badroot"; sandboxed "$base"; mkdir -p "$base"
  o="$(env -i PATH="$PATH" HOME="$base" "$ORACLE" --root "$base/nope" 2>/dev/null)"; orc=$?
  g="$(env -i PATH="$PATH" HOME="$base" "$GOBIN"  --root "$base/nope" 2>"$base/g.err")"; grc=$?
  [ "$orc" = "$grc" ] && [ "$grc" = 1 ] || { echo "exit bash=$orc go=$grc want 1"; exit 1; }
  [ -z "$o" ] && [ -z "$g" ] || { echo "stdout bash=[$o] go=[$g] want empty"; exit 1; }
  [ ! -s "$base/g.err" ] || { echo "port should stay silent, got: $(cat "$base/g.err")"; exit 1; }
  # The oracle agrees on the contract and differs only by that diagnostic.
  env -i PATH="$PATH" HOME="$base" "$ORACLE" --root "$base/nope" 2>"$base/o.err" >/dev/null
  grep -q 'No such file or directory' "$base/o.err" || { echo "oracle contract drifted"; exit 1; }
) && ok "nonexistent-root-exit-1 (contract only; bash leaks a cd diagnostic)" \
  || fail "nonexistent-root-exit-1 (contract only; bash leaks a cd diagnostic)"

# A --root that is a file, not a directory — same cd failure path.
(
  base="$T/fileroot"; sandboxed "$base"; mkdir -p "$base"; : > "$base/afile"
  env -i PATH="$PATH" HOME="$base" "$ORACLE" --root "$base/afile" >/dev/null 2>&1; orc=$?
  env -i PATH="$PATH" HOME="$base" "$GOBIN"  --root "$base/afile" >/dev/null 2>&1; grc=$?
  [ "$orc" = "$grc" ] && [ "$grc" = 1 ] || { echo "exit bash=$orc go=$grc want 1"; exit 1; }
) && ok "file-as-root-exits-1" || fail "file-as-root-exits-1"

# Empty --root value falls through to the git/pwd default, as `[ -z "$ROOT" ]` says.
(
  base="$T/emptyroot"; sandboxed "$base"; mkdir -p "$base/o" "$base/g"
  mk_root "$base/o" alpha; mk_root "$base/g" alpha
  o="$(cd "$base/o" && env -i PATH="$PATH" HOME="$base/o" CODEX_HOME="$base/o/ch" "$ORACLE" --root "" --check 2>&1)"; orc=$?
  g="$(cd "$base/g" && env -i PATH="$PATH" HOME="$base/g" CODEX_HOME="$base/g/ch" "$GOBIN"  --root "" --check 2>&1)"; grc=$?
  [ "$orc" = "$grc" ] || { echo "exit bash=$orc go=$grc"; exit 1; }
  on="$(printf '%s' "$o" | sed "s|$base/o|R|g")"; gn="$(printf '%s' "$g" | sed "s|$base/g|R|g")"
  [ "$on" = "$gn" ] || { echo "bash=[$on] go=[$gn]"; exit 1; }
) && ok "empty-root-value-falls-through-to-default" || fail "empty-root-value-falls-through-to-default"

# Default root outside a git repo = pwd (git rev-parse fails → `|| pwd`).
(
  base="$T/norepo"; sandboxed "$base"; mkdir -p "$base/o" "$base/g"
  mk_root "$base/o" alpha; mk_root "$base/g" alpha
  # GIT_CEILING_DIRECTORIES stops rev-parse escaping the sandbox into the repo
  # this suite runs from, which would otherwise make the default root the repo.
  o="$(cd "$base/o" && env -i PATH="$PATH" HOME="$base/o" CODEX_HOME="$base/o/ch" GIT_CEILING_DIRECTORIES="$base" "$ORACLE" --check 2>&1)"; orc=$?
  g="$(cd "$base/g" && env -i PATH="$PATH" HOME="$base/g" CODEX_HOME="$base/g/ch" GIT_CEILING_DIRECTORIES="$base" "$GOBIN"  --check 2>&1)"; grc=$?
  [ "$orc" = "$grc" ] || { echo "exit bash=$orc go=$grc"; exit 1; }
  on="$(printf '%s' "$o" | sed "s|$base/o|R|g")"; gn="$(printf '%s' "$g" | sed "s|$base/g|R|g")"
  [ "$on" = "$gn" ] || { echo "bash=[$on] go=[$gn]"; exit 1; }
  case "$gn" in *"R/.claude/skills/alpha"*) ;; *) echo "default root != pwd: [$gn]"; exit 1 ;; esac
) && ok "default-root-is-pwd-outside-a-git-repo" || fail "default-root-is-pwd-outside-a-git-repo"

# Default root inside a git repo = `git rev-parse --show-toplevel`, proving the
# shell-out is wired: run from a subdir, the root must climb to the repo top.
command -v git >/dev/null 2>&1 && (
  base="$T/inrepo"; sandboxed "$base"
  for side in o g; do
    r="$base/$side/repo"; mkdir -p "$r/sub"; mk_root "$r" alpha
    ( cd "$r" && git init -q . && git config user.email t@t && git config user.name t ) >/dev/null 2>&1
  done
  o="$(cd "$base/o/repo/sub" && env -i PATH="$PATH" HOME="$base/o" CODEX_HOME="$base/o/ch" "$ORACLE" --check 2>&1)"; orc=$?
  g="$(cd "$base/g/repo/sub" && env -i PATH="$PATH" HOME="$base/g" CODEX_HOME="$base/g/ch" "$GOBIN"  --check 2>&1)"; grc=$?
  [ "$orc" = "$grc" ] || { echo "exit bash=$orc go=$grc"; exit 1; }
  on="$(printf '%s' "$o" | sed "s|$base/o|R|g")"; gn="$(printf '%s' "$g" | sed "s|$base/g|R|g")"
  [ "$on" = "$gn" ] || { echo "bash=[$on] go=[$gn]"; exit 1; }
  case "$gn" in *"R/repo/.claude/skills/alpha"*) ;; *) echo "root not git toplevel: [$gn]"; exit 1 ;; esac
) && ok "default-root-is-git-toplevel-from-a-subdir" || fail "default-root-is-git-toplevel-from-a-subdir"

# ── Summary ────────────────────────────────────────────────────────────
echo ""
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m  %d/%d cases match bin/bbs-codex-competitive exactly\n' "$PASS" "$((PASS + FAIL))"
  exit 0
fi
printf '\033[0;31mFAIL\033[0m  %d/%d failed: %s\n' "$FAIL" "$((PASS + FAIL))" "${FAIL_NAMES[*]}"
exit 1
