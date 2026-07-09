# Ticket Layout (Layout C)
A ticket is a folder under `~/.babysit/projects/<slug>/tickets/<ticket>/` —
open it and answer *what is this ticket, what state, what next* with bounded
reads. `manifest.yaml` is the identity anchor (schema:
[docs/identity.md](../../../docs/identity.md)); resolution ladder: env
`BABYSIT_TICKET` → `manifest.yaml` cwd-match → branch regex.
```
tickets/<ticket>/
├── index.json                       authoritative metadata + relations + pointers
├── requirement.md                   verbatim user intent (write-once anchor)
├── design.md / plan.md / manifest.md   owner-skill snapshots (optional)
├── handoffs/                        append-only change briefs, 001-…; LATEST holds current filename
├── verdicts/<skill>.md              worker's own status block (overwrite per run)
├── reviews/<skill>.md               a gate's opinion on someone else's work
├── history.jsonl                    append-only event timeline (`tail -20` = what happened)
├── evidence/<skill>/…               per-skill blobs
├── sub-tickets/<N>-<slug>.md        decomposition seeds (only if split)
├── checkpoint.json                  bbs-autopilot workflow-step state
└── .index.lock/                     mkdir lock for index.json mutations
```
Every writable file is name-partitioned by skill, append-only, or
lock-protected; all metadata mutations go through `bbs-ticket` — never edit
`index.json` by hand.
## index.json
All fields optional except `id`; unknown fields preserved. Key fields:
`status`, `phase`, `parent`/`children`, `relations` (`blocks`, `blocked_by`,
`duplicate_of`, `related`), `siblings` (`{role, repo, ticket}` for cross-repo),
`labels`, `assignee`, `origin`, `pointers` (`branch`, `pr`, `plan`, `design`,
`requirement`, `manifest`).
**Status enum** (set via `bbs-ticket set-status`, never derived from
verdicts): `triage` → `backlog` → `planned` / `decomposed` → `in_progress` →
`in_review` → `done`; plus `blocked`, `cancelled`, `duplicate`.
**`origin.type`**: `standalone` (default) | `sub_ticket` (has `parent`,
`seed`, `plan`, `position`) | `hotfix` | `design-initiated` (has `design_doc`).
## Bootstrap: how tickets come into being
Tickets exist when the branch matches
`(feat|fix|chore|bug|refactor)/<id>_<slug>` — the preamble derives `$TICKET`
and runs `bbs-ticket init`. Entry-point skills invoked without a ticket (e.g.
from `main`) run the universal entry hook early instead of failing:
```bash
eval "$("${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" ensure \
  --from-input "$USER_REQUEST" \
  --type feat \
  --reason <skill-name>-entry)"
# Safe-cut divert: ensure only cuts in place from a clean base-branch
# checkout. When it printed WORKTREE=, all work happens there.
[ -n "${WORKTREE:-}" ] && cd "$WORKTREE"
```
`ensure` is idempotent. Fast-path (already on a ticket branch): no-op, prints
`CREATED=0` + ticket env. Slow-path: generates `bs-<8hex>`, cuts
`feat/<id>_<slug>` through the **safe-cut gate** (in place only from a clean
base checkout; otherwise diverts to a worktree and prints `WORKTREE=<path>` —
cd there; see [git-flow.md](git-flow.md)), seeds `requirement.md`, prints
`CREATED=1` + ticket env. Under `mode: trunk` the slow-path skips the cut and
prints `export BABYSIT_TICKET=<id>` as the identity carrier.
**Exit 3 = `NEEDS_CONFIRM`** — in developer mode the slow-path never cuts a
branch in place silently: without `--cut-branch` it exits 3. Render one
`AskUserQuestion` (cut the branch vs stay + `BABYSIT_TICKET`) and re-run with
`--cut-branch` or `--no-branch`. Worktree diverts proceed without asking;
autonomous roles never see exit 3.
## Accessing ticket files
**Construct paths via `bbs-ticket path` / `bbs-ticket list` — never by
concatenating `$TH/`, `$TICKET_HOME/`, etc.** The broker resolves canonical →
legacy (pre-Layout-C tickets keep working) and validates selectors.
Kinds: `home`; single files `index requirement design plan manifest history
checkpoint`; `handoff` (`--skill`, `--seq N` or `--latest`); `verdict` /
`review` (`--skill`); `evidence` (`--skill`, `--name`); `sub-ticket`
(`--seq`, `--slug` for write). `--read` returns the first existing candidate;
`--write` returns the canonical target and mkdir-ps the parent; append-only
kinds reject `--write` — use `add-handoff` / `set-verdict` / `set-review`.
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
(Don't use `X="$(…)" || X=""` — it swallows exit 2/3 into "not found".)
`bin/bbs-ticket-lint` flags raw `$TH/` constructions in bash fences; a
genuinely needed bypass takes a trailing `# lint:allow-direct-path <why>`.
## Typical skill usage
```bash
bbs-ticket init                                  # startup — idempotent

cp "$PLAN_DRAFT" "$(bbs-ticket path plan --write)"   # primary artifact
bbs-ticket set-pointer plan plan.md
bbs-ticket set-status planned
bbs-ticket set-phase plan-draft

bbs-ticket add-handoff --skill plan-draft --status DONE --body-file /tmp/brief.md
bbs-ticket set-verdict  --skill plan-draft --body-file /tmp/verdict.md

ORIGIN_TYPE="$(bbs-ticket get origin.type)"      # reads, e.g. sub-ticket check
```
Custom timeline events: `bbs-ticket append-history --event <e> --extra-json
'{…}'` (core fields `ts ticket branch event actor` are protected).
`evidence/quality/` is shared between the `quality` workflow and `bbs:qa` —
grep by filename, don't assume one writer.
