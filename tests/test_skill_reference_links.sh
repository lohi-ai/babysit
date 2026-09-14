#!/usr/bin/env bash
# tests/test_skill_reference_links.sh — relative references in the pack resolve,
# and the skills that escape their own directory say how to read them.
#
# The pack keeps its shared references in `.claude/skills/references/` — a
# *sibling* of every skill directory — and reaches them with `../references/
# <file>.md`. That works when a harness resolves a relative path against the
# skill's directory (Claude Code, Codex, grok). It cannot work through OMP's
# `skill://` protocol, which addresses one skill directory: URL normalization
# strips `..` first, so `skill://qa/../references/worktrees.md` silently
# becomes `skill://qa/references/worktrees.md` and dies as `File not found` —
# which is how an autopilot run and a QA run each lost their first read. So a
# SKILL.md that names anything outside its own directory has to also say these
# are filesystem paths, not `skill://` targets; otherwise the model repeats the
# dead read.
#
# Two contracts, both observable:
#   1. every relative reference a skill file names resolves — renaming or
#      moving a reference is how these rot silently (`setup-project` spent
#      months pointing `references/git-flow.md` at a directory that never
#      held it);
#   2. every SKILL.md that names something outside its directory carries the
#      read-by-path note.
#
# The convention is the one the Codex wrappers state: a relative reference
# resolves against the skill's own directory, whichever file it appears in —
# that is why `autopilot/workflows/builder.md` writes `../references/` and not
# `../../references/`. Paths into the *target repo* (`.babysit/qa.yaml`,
# `verdicts/qa.md`) are not pack references and are not checked. Fenced code
# blocks are skipped: a path inside a fence (a yaml comment in a template, a
# bash comment) is illustration, not something the model resolves.
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
command -v python3 >/dev/null 2>&1 || { echo "SKIP: python3 not installed" >&2; exit 0; }

python3 - "$ROOT" <<'PY'
import os, re, sys

root = sys.argv[1]
skills_root = os.path.join(root, ".claude", "skills")
NOTE = "filesystem paths beside this skill's directory, so read them by path, not as `skill://`"
FENCE = re.compile(r"^\s*(```|~~~)")
ESCAPE = re.compile(r"^(?:\.\./)+[A-Za-z0-9._/-]+$")          # ../references/x, ../../../docs/x
SKILL_LOCAL = re.compile(r"^references/[A-Za-z0-9._/-]+$")   # a skill's own references/x
EXTRACT = re.compile(r"\]\(([^)\s]+)\)|`([^`]+)`")

fails = []


def strip_blocks(text):
    """Text with fenced code blocks removed."""
    out, fenced = [], False
    for line in text.splitlines():
        if FENCE.match(line):
            fenced = not fenced
            continue
        if not fenced:
            out.append(line)
    return "\n".join(out)


def refs(text, skill_dir):
    """(token, hops, target) for every relative pack reference a file names.

    `target` applies the pack convention: walk `hops` levels up from the
    *skill* directory (not the file's own directory), then append the tail.
    """
    for md_target, backticked in EXTRACT.findall(strip_blocks(text)):
        raw = (md_target or backticked).split("#")[0].strip()
        for tok in (t.strip() for t in raw.split()):
            if ESCAPE.match(tok):
                hops = tok.count("../")
                yield tok, hops, os.path.normpath(os.path.join(skill_dir, *[".."] * hops, tok[3 * hops:]))
            elif SKILL_LOCAL.match(tok):
                yield tok, 0, os.path.normpath(os.path.join(skill_dir, tok))


for dirpath, _dirs, files in os.walk(skills_root):
    for fname in sorted(files):
        if not fname.endswith(".md"):
            continue
        path = os.path.join(dirpath, fname)
        rel = os.path.relpath(path, root)
        skill_dir = os.path.join(skills_root, os.path.relpath(path, skills_root).split(os.sep)[0])
        text = open(path, encoding="utf-8").read()
        found = list(refs(text, skill_dir))
        for tok, _hops, target in found:
            if not os.path.exists(target):
                fails.append(f"{rel}: `{tok}` does not resolve ({os.path.relpath(target, root)})")
        escaping = [t for t, h, _x in found if h >= 1]
        if escaping and fname == "SKILL.md" and NOTE not in " ".join(text.split()):
            fails.append(
                f"{rel}: names {escaping[0]} outside its own directory but never says "
                f"these are filesystem paths ({NOTE!r}) — on OMP the read dies as File not found"
            )

if fails:
    print(f"FAIL: {len(fails)} reference problem(s)")
    for f in fails:
        print(f"  {f}")
    sys.exit(1)

print("OK: every relative reference resolves, and every skill that leaves its own directory says how to read it")
PY
status=$?
exit "$status"
