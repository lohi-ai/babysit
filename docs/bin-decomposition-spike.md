# Spike: decomposing `bbs-ticket` / `bbs-product` into `lib/`

> **2026-07-03:** `bbs-product` (and product mode) has since been removed;
> the shared-code inventory below reflects the repo as of the spike.

*Design ticket `bs-bin-decomposition-spike` — decision doc only, no extraction
in this change. Written 2026-07-02 against v1.48.x.*

## Problem

`bin/bbs-ticket` (2,565 lines) and `bin/bbs-product` (2,340 lines) hold most of
the pack's load-bearing bash, and every feature lands as more lines in the same
two files. The roadmap flags the next size doubling as the deadline for a
decision. Constraint from the roadmap: **subtraction and extraction only** — no
rewrite-in-X.

## What actually overlaps (measured)

Literal duplication is smaller than the file sizes suggest — exactly one
function name (`_release_lock`) is defined in both bins. The real extraction
surface is *conceptual* seams, each currently reachable from only one bin but
needed by both (or by hooks/`bbs-dashboard`) as features grow:

| Seam | Today lives in | Also wanted by |
|------|----------------|----------------|
| Identity ladder (`BABYSIT_TICKET` → `manifest.yaml` → branch regex) | `bbs-ticket resolve` | `bbs-product`, hooks, `bbs-autopilot` (all shell out to `bbs-ticket` today) |
| `manifest.yaml` IO (read/write repo rows) | `bbs-ticket` (~51 references) | `bbs-dashboard` (reimplements a YAML→JSON parse), `bbs-product` |
| YAML scalar/list helpers (`_yaml_scalar`, `_yaml_repos`, `_yaml_nested_scalar`, …) | `bbs-product` | `bbs-ticket` manifest IO, `bbs-dashboard` |
| Session writing (`~/.babysit/sessions/<id>.yaml`) | `bbs-product cmd_session` + preamble hook | any bin that wants to stamp session context |
| Locking (`_release_lock` + acquire logic) | duplicated in both bins | any future writer |
| JSONL audit-line append (decisions/skill-usage) | inlined in several places (rubric block, hooks, `bbs-learnings-log`) | everything that logs |

## Proposed shape

```
bin/
  bbs-ticket            # thin: arg parsing + dispatch, sources lib/
  bbs-product
  lib/
    identity.sh         # resolve ladder + conflict BLOCK (one codepath, as today)
    manifest.sh         # manifest.yaml read/write + YAML helpers
    session.sh          # session file write/list/attach payload
    lock.sh             # acquire/release (the one literal duplicate)
    log.sh              # JSONL append honoring BABYSIT_ANALYTICS_DIR
```

`lib/*.sh` are *sourced modules*: function definitions only, no side effects at
source time, no `set -e` changes, everything namespaced `bbs_<module>_*`.

### The symlink constraint (load-bearing)

`setup-skills` installs the bins as **symlinks** into `~/.claude/`, so
`$(dirname "$0")` points at `~/.claude/`, not the repo `bin/`. Every bin that
sources `lib/` must resolve its real path first, portably (macOS `readlink -f`
can't be assumed):

```bash
SELF="$0"
while [ -L "$SELF" ]; do SELF="$(readlink "$SELF")"; done
BBS_LIB="${BBS_LIB:-$(cd "$(dirname "$SELF")/lib" && pwd)}"
. "$BBS_LIB/identity.sh"
```

`BBS_LIB` override keeps tests hermetic and lets the plugin-installed copy
(`~/.claude/skills/babysit/bin/lib/`) work unchanged. `setup-skills` must also
symlink (or the plugin must ship) the `lib/` directory — that is the one
install-path change this design requires.

## Extraction order (safety first)

Existing tests (~40 shell/python suites over the bins) are the safety net; each
step is one PR that moves code without changing behavior, verified by the full
suite before and after.

1. **`lock.sh`** — smallest, literally duplicated, trivially testable. Also
   proves the symlink-resolution + `BBS_LIB` mechanics end to end.
2. **`log.sh`** — unifies the JSONL append currently copy-pasted across hooks,
   the resize rubric block, and `bbs-learnings-log`.
3. **`manifest.sh`** — move `bbs-product`'s `_yaml_*` helpers + `bbs-ticket`'s
   manifest IO; port `bbs-dashboard`'s private YAML parse onto it last.
4. **`session.sh`**, then **`identity.sh`** — identity goes last because it is
   the most load-bearing and best-tested; by then the mechanics are boring.

Stop condition: if any step needs behavior changes to extract cleanly, stop and
file it — this spike's mandate is extraction, not repair.

## What we are explicitly not doing

- No rewrite in Bun/Python/Rust. The bash is tested and deployed; the problem
  is layout, not language.
- No new abstractions: modules are cut along the seams above, not along
  hypothetical future needs.
- No behavior change, including error messages that tests pin.

## Decision requested

Approve the `lib/` layout + extraction order above. First implementation
ticket: `lock.sh` + the `BBS_LIB` resolution preamble in both bins +
`setup-skills` shipping `lib/`.
