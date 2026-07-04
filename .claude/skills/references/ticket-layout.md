# Ticket Layout (Layout C)

How babysit stores per-ticket state on disk. This is the authoritative spec —
everything in [handoff-contracts.md](handoff-contracts.md) points here for file
paths and the file ownership matrix lives at the bottom of this doc.

## The big idea

A ticket is a folder. Open it, read it, understand the whole story. The
structure is modelled on Linear's Issue schema — single `status` enum, typed
relations, parent/child as a tree — adapted for filesystem use by an autonomous
agent that must answer *what is this ticket, what state is it in, and what
does the last skill want me to do next* with bounded reads.

Three kinds of files live here, never mixed:

| Kind | Format | Mutation | Examples |
|------|--------|----------|----------|
| **Identity** | YAML (`manifest.yaml`) | rewrite via `bbs-ticket` | `ticket`, `repos[]`, branch/canonical/worktree per repo |
| **Metadata** | JSON (`index.json`) | lock-protected mutate | `status`, `phase`, relations, siblings, pointers |
| **Content** | Markdown snapshots | overwrite by owner skill | `plan.md`, `design.md`, `requirement.md`, `manifest.md` |
| **Log** | Append-only JSONL / numbered MD | append only | `history.jsonl`, `handoffs/<NNN>-*.md` |
| **Evidence** | Blobs | per-skill dir, overwrite per run | `evidence/<skill>/*.png`, `*.json` |

`manifest.yaml` is the **identity anchor** — it pins which repos and branches
this ticket spans. Tickets get a one-row manifest auto-seeded by
`bbs-ticket init`. Schema lives in [docs/identity.md](../../../docs/identity.md).
Resolution ladder (env `BABYSIT_TICKET` → `manifest.yaml` cwd-match → branch
regex) — there is exactly one resolver.

## Why JSON, not YAML

`index.json` is programmatically mutated by many skills. YAML needs an external
parser (`yq` or PyYAML) that we can't assume is installed. Python3's stdlib
parses JSON; humans read it fine; readers can use `jq` when available. The
file is pretty-printed and sorted-keys for diff-friendliness.

## Directory shape

```
~/.babysit/projects/<slug>/tickets/<ticket>/
├── index.json                       authoritative metadata + relations + pointers
├── requirement.md                   verbatim user intent, seeded by the first entry-point skill
├── design.md                        office-hours snapshot (optional)
├── plan.md                          plan-draft output (optional)
├── manifest.md                      decomposition output (optional, only if split)
├── handoffs/
│   ├── 001-plan-draft-done.md     append-only, zero-padded monotonic
│   ├── 002-implement-done.md
│   └── LATEST                       plain text file holding latest filename
├── verdicts/
│   └── <skill>.md                   per-skill latest (overwrite, name-partitioned)
├── reviews/
│   └── human-review.md
├── history.jsonl                    append-only timeline (events, not prose)
├── evidence/
│   └── <skill>/
│       └── <date>-<label>.{png,json,md}
├── sub-tickets/                     only exists on decomposed tickets, pre-promotion
│   └── <N>-<slug>.md                seed for each child sub-ticket
├── checkpoint.json                  bbs-autopilot workflow-step state (orthogonal)
└── .index.lock/                     mkdir-based lock for index.json mutations
```

Anything outside this tree (analytics JSONL, timeline.jsonl, current.txt,
caches) is project-scoped and documented in
[handoff-contracts.md § Evidence paths](handoff-contracts.md#evidence-paths).

## index.json schema

All fields optional except `id`. Unknown fields are preserved (don't rewrite
the file by hand — use `bbs-ticket`).

```json
{
  "id": "BBS-142",
  "title": "Add rig-scoped postgres snapshot rotation",
  "status": "in_progress",
  "phase": "implement",
  "parent": "BBS-140",
  "children": ["BBS-143", "BBS-144"],
  "relations": {
    "blocks": ["BBS-150"],
    "blocked_by": ["BBS-139"],
    "duplicate_of": null,
    "related": ["BBS-120"]
  },
  "siblings": [
    {"role": "be", "repo": "lohi/babysit-backend", "ticket": "BE-44"}
  ],
  "labels": ["db", "infra"],
  "assignee": "mayor",
  "origin": {
    "type": "standalone",
    "parent": null,
    "seed": null,
    "plan": null,
    "position": null,
    "design_doc": null
  },
  "pointers": {
    "branch": "feat/BBS-142_rotation",
    "pr": "https://github.com/lohi/babysit/pull/321",
    "plan": "plan.md",
    "design": "design.md",
    "requirement": "requirement.md",
    "manifest": "manifest.md"
  },
  "created_at": "2026-04-10T09:00:00Z",
  "updated_at": "2026-04-19T14:30:00Z"
}
```

### Status enum

Single authoritative enum. Don't derive status from verdicts — update it
explicitly via `bbs-ticket set-status` so transitions are logged to
`history.jsonl`.

| Status | Meaning | Typical entry skill |
|--------|---------|---------------------|
| `triage` | Just created, not yet planned | init |
| `backlog` | Ready to plan | office-hours |
| `planned` | Plan written, awaiting build/decomposition | plan-draft |
| `decomposed` | Split into sub-tickets, parent is a coordinator | plan-draft |
| `in_progress` | Implementation in flight | implement |
| `in_review` | Built, awaiting review/QA | review-pr, qa |
| `blocked` | Can't proceed — dependency, ambiguity, or broken tool | any |
| `done` | Completed | create-pr |
| `cancelled` | Abandoned | any |
| `duplicate` | Superseded by another ticket — see `relations.duplicate_of` | any |

### Origin types

`origin.type` records *how* the ticket came to exist, which informs downstream
routing. `standalone` is the default.

| Type | Meaning | Populated fields |
|------|---------|------------------|
| `standalone` | Created directly by a human or orchestrator | — |
| `sub_ticket` | Child of a decomposed parent | `parent`, `seed`, `plan`, `position` |
| `hotfix` | Emergency fix branched from production | `parent` (optional) |
| `design-initiated` | Spawned from a design doc | `design_doc` |

## Relations taxonomy

Four typed edges, no more:

| Edge | Direction | Cardinality | Use |
|------|-----------|-------------|-----|
| `blocks` | outgoing | many | This ticket blocks the listed ones |
| `blocked_by` | incoming | many | This ticket is blocked by the listed ones |
| `duplicate_of` | outgoing | 1 or null | Close this; work lives on the target |
| `related` | undirected | many | Weak context hint |

`parent` / `children` live at the top level (they form a tree, not a typed
relation). Cross-repo siblings live under `siblings` with `role/repo/ticket`.

Jira's `clones` and `causes` are intentionally omitted — they don't pay rent in
an agent workflow.

## handoffs/ — append-only change-briefs

Each skill that finishes meaningful work writes one file here via
`bbs-ticket add-handoff`. The helper computes the next three-digit sequence
number, writes `<NNN>-<skill>-<status>.md`, and updates `handoffs/LATEST` under
the index lock.

**Why numbered + LATEST, not a mutable `change-brief.md`:** two skills running
concurrently (e.g. `qa` + `review` after `implement`) never collide. Each
claims its own `<NNN>`. Downstream readers open `handoffs/LATEST` to get the
single current handoff, no mtime picking.

**LATEST is a plain file, not a symlink.** Some filesystems + Windows/WSL
handle symlinks poorly; a text file containing the filename is universal.

## verdicts/ vs reviews/

Distinct on purpose:

- **`verdicts/<skill>.md`** — the worker's status block. `plan-draft`,
  `implement`, `investigate`, and `browse` write these. One file per skill,
  overwritten on re-run.
- **`reviews/<skill>.md`** — the *gate's* opinion on someone else's work.
  Human or external review tools may write these. Same format, different
  conceptual role.

Agents reading the ticket can answer "did implementation pass?" by reading
`verdicts/implement.md` — no scanning handoffs.

## history.jsonl — the timeline

One JSON object per line, append-only. Append is atomic for writes under
`PIPE_BUF` (4 KB on macOS, 64 KB on Linux) — a single-line JSON object
comfortably fits. Modelled on GitHub's timeline event list.

Event types emitted by `bbs-ticket`:

| Event | Fields |
|-------|--------|
| `ticket_initialized` | — |
| `requirement_seeded` | — (written once by `bbs-ticket ensure`) |
| `status_changed` | `from`, `to` |
| `phase_changed` | `phase` |
| `parent_set` | `parent` |
| `child_added` | `child` |
| `relation_added` | `type`, `target` |
| `sibling_added` | `role`, `repo` |
| `handoff` | `status`, `file` |
| `verdict` | — (actor is the skill) |
| `review` | — (actor is the skill) |

Custom events via `bbs-ticket append-history --event <e> --extra-json '{...}'`.
Core fields (`ts`, `ticket`, `branch`, `event`, `actor`) are protected —
callers can't override them via `--extra-json`.

Answering "what happened on this ticket?" is `tail -n 20 history.jsonl`.
No prose parsing.

## File ownership matrix

Every writable file is either **name-partitioned by skill**, **append-only**,
or **lock-protected**. No exceptions.

| File | Writers | Concurrency model |
|------|---------|-------------------|
| `index.json` | any skill + orchestrator | `.index.lock/` (mkdir) |
| `requirement.md` | any entry-point skill via `bbs-ticket ensure` | write-once (skipped if present) |
| `design.md` | `design-ui`, planning/design entrypoints | last writer wins |
| `plan.md` | `plan-draft` | single-writer skill |
| `manifest.md` | `plan-draft` (decomposition) | single-writer skill |
| `handoffs/<NNN>-*` | any skill | unique NNN per writer |
| `handoffs/LATEST` | any skill | under `.index.lock/` |
| `verdicts/<skill>.md` | that skill only | name-partitioned |
| `reviews/<skill>.md` | that skill only | name-partitioned |
| `history.jsonl` | any skill | POSIX atomic append |
| `evidence/<skill>/*` | that skill only | name-partitioned |
| `sub-tickets/<N>-*.md` | `plan-draft` (decomposition) | single-writer skill |
| `checkpoint.json` | `bbs-autopilot` | orthogonal — workflow-step state |

**The lock:** `.index.lock/` is a directory created atomically via `mkdir`.
`bbs-ticket` holds it for the duration of any `index.json` mutation, then
releases via an EXIT trap. Never edit `index.json` by hand during a skill run.

## Bootstrap: how tickets come into being

Tickets exist when the checked-out branch matches
`(feat|fix|chore|bug|refactor)/<id>_<slug>` — `bbs-slug` derives `$TICKET` /
`$TICKET_HOME` from there, and preamble auto-runs `bbs-ticket init`.

But users may invoke any entry-point skill directly from `main`
(`/bbs:implement "fix the bug"`, `/bbs:office-hours "I want to add X"`) —
no branch, no ticket. Entry-point skills must not fail, and they must not
silently produce orphan artifacts that downstream skills can't trace.

**Universal entry hook** — each entry-point skill runs this early:

```bash
eval "$("${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" ensure \
  --from-input "$USER_REQUEST" \
  --type feat \
  --reason <skill-name>-entry)"
# Safe-cut divert: ensure only cuts in place from a clean base-branch
# checkout. When it printed WORKTREE=, all work happens there.
[ -n "${WORKTREE:-}" ] && cd "$WORKTREE"
```

`ensure` is idempotent:

- **Fast-path** (already on a ticket branch) — no-op; optionally seeds
  `requirement.md` if `--from-input` is given and no requirement exists yet;
  prints `CREATED=0` + the existing ticket env.
- **Slow-path** (no ticket in the branch) — generates `bs-<8hex>`,
  derives a slug from `--slug-hint` or the first few words of the input,
  cuts `feat/<id>_<slug>`, inits the ticket, seeds
  `requirement.md`, and prints `CREATED=1` + the new ticket env so the
  caller's `eval` picks up `TICKET` / `BRANCH` / `TICKET_HOME` / `REQUIREMENT`.
  The cut runs through the **safe-cut gate**: `git checkout -b` in place only
  from a clean checkout of the base branch. On any other branch, or with
  uncommitted changes, the cut diverts to a worktree forked from base
  (`<repo>/.babysit/worktrees/<id>_<slug>/`, git-excluded locally) and the output adds `WORKTREE=<path>`
  — cd there; the invoking checkout is never touched (see
  [git-flow.md](git-flow.md)).
  With `ticket_branch: optional` in `.babysit/git-flow.yaml` the slow-path
  skips the branch cut: the ticket rides the current branch and the output
  adds `export BABYSIT_TICKET=<id>` as the identity carrier (see
  [git-flow.md](git-flow.md)).

**Exit 3 = `NEEDS_CONFIRM`** — in developer mode (`AGENT_ROLE` unset or
`developer`) the slow-path never cuts a branch **in place** silently: without
an explicit `--cut-branch` it prints a `NEEDS_CONFIRM` block and exits 3.
Handle it as a single `AskUserQuestion` ("cut `feat/<id>_<slug>` off
`<branch>`?" vs "stay on `<branch>`, identity via `BABYSIT_TICKET`") and
re-run `ensure` with the chosen flag (`--cut-branch` or `--no-branch`).
Worktree diverts never move the checkout, so they proceed without asking, and
autonomous roles (`mayor`, `general`, …) never see exit 3.

Entry-point skills that run the hook: `office-hours`, `plan-draft`,
`implement`, and `investigate`. `browse` can run standalone without a ticket.

**Why `requirement.md` is write-once:** it captures the verbatim user intent
at ticket birth. If the ask evolves, the plan and design sections of the
ticket evolve with it — the original requirement stays as the anchor that
lets downstream skills answer "what was this ticket really about?"
without parsing prose out of a chain of handoffs.

## Accessing ticket files

**Every skill or workflow constructs ticket file paths via `bbs-ticket path`
or `bbs-ticket list` — never by string-concatenating `$TH/`,
`$TICKET_HOME/`, or `$BABYSIT_PROJECT_HOME/tickets/$TICKET/`.** The path
broker walks canonical → legacy fallbacks, validates selectors against
traversal, and emits telemetry on legacy hits so we can drive the layout
forward without breaking pre-Layout-C tickets.

```
bbs-ticket path  <kind> [selectors] [--read|--write]
bbs-ticket list  <kind> [selectors]
```

`--read` resolves canonical → legacy, returns the first that exists, exits
`1` if none. `--write` returns the canonical write target and `mkdir -p`s
the parent. Append-only kinds (handoff, verdict, review) reject `--write`
— go through the dedicated subcommands (`add-handoff`, `set-verdict`).
Kinds + selectors:

| Kind | Selectors | Notes |
|------|-----------|-------|
| `home` | — | ticket directory |
| `index`, `requirement`, `design`, `plan`, `manifest`, `history`, `checkpoint` | — | single canonical file |
| `handoff` | `--skill <s>` `--seq <N>` or `--latest` | numbered, append-only |
| `verdict`, `review` | `--skill <s>` | name-partitioned, append-only |
| `evidence` | `--skill <s>` `--name <file>` | per-skill blob namespace |
| `sub-ticket` | `--seq <N>` (`--slug <s>` for `--write`) | seed file |

**Caller recipe** (read paths often legitimately don't exist yet — but
preserve `exit 3` from the broker so traversal attempts fail loud):

```bash
# Exit codes from `bbs-ticket path … --read`:
#   0 — found (canonical or legacy hit)
#   1 — not found (canonical AND every legacy candidate missing)
#   2 — usage / missing required selector
#   3 — security (selector failed _safe_path_component)
# Treat 0/1 as data signals; let 2/3 propagate.
PLAN="$(bbs-ticket path plan --read 2>/dev/null)"; rc=$?
case $rc in
  0) ;;             # found — $PLAN is the path
  1) PLAN="" ;;     # absent — fall back / skip
  *) exit "$rc" ;;  # usage / security — surface the failure
esac
[ -n "$PLAN" ] && cat "$PLAN"
```

The older one-liner `X="$(... 2>/dev/null)" || X=""` swallows exit 2 and 3
into the same bucket as "not found", so a poisoned selector silently degrades
to a noop instead of failing the run. Don't use it.

**Discovery:**

```bash
bbs-ticket list handoff                         # newest-first by seq
bbs-ticket list evidence --skill quality        # bare names, alpha
```

**Lint:** `bin/bbs-ticket-lint --mode discovery|enforce` flags raw `$TH/`,
`$TICKET_HOME/`, `$BABYSIT_PROJECT_HOME/tickets/` patterns inside ` ```bash `
fenced blocks. Suppress a single line with a trailing `# lint:allow-direct-path`
comment when bypassing the broker is genuinely needed (and add the
justification in the comment).

**`evidence/quality/` is shared.** Both the `quality` workflow and `bbs:qa`
write under this namespace, but to disjoint filenames (`quality-summary.md`
vs `browser-check-*.json`). The skill name in the path is a *namespace*, not an
ownership claim — readers should grep by filename, not assume one writer.

## Using bbs-ticket from a skill

The helper is installed at `"${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}"` (via `bin/setup-skills`).
Typical skill usage:

```bash
# During skill startup — idempotent; seeds index.json if missing.
bbs-ticket init

# At the step where the skill's primary artifact is written:
cp "$PLAN_DRAFT" "$(bbs-ticket path plan --write)"
bbs-ticket set-pointer plan plan.md
bbs-ticket set-status planned
bbs-ticket set-phase plan-draft

# At skill completion:
bbs-ticket add-handoff --skill plan-draft --status DONE --body-file /tmp/brief.md
bbs-ticket set-verdict  --skill plan-draft --body-file /tmp/verdict.md
```

For reads — e.g. `implement` checking whether this is a sub-ticket:

```bash
ORIGIN_TYPE="$(bbs-ticket get origin.type)"
if [ "$ORIGIN_TYPE" = "sub_ticket" ]; then
  SEED="$(bbs-ticket get origin.seed)"
  PARENT_PLAN="$(bbs-ticket get origin.plan)"
  # read both as primary context
fi
```

## Deprecated layout (pre-Layout C)

Older skills wrote to `tickets/<ticket>/<skill>/change-brief.md` +
`<skill>/verdict.md` + `<skill>/report.md`. During migration both coexist:
skills already cut over use Layout C; the rest still write to per-skill
subdirs. Once all skills are migrated the per-skill subdirs get archived — see
the migration tracker in this repo's CLAUDE.md.

A downstream skill reading a ticket should prefer Layout C paths
(`verdicts/<skill>.md`) and fall back to legacy (`<skill>/verdict.md`) only
if the new path is absent.
