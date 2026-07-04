# Handoff Contracts

How babysit skills hand off to each other (and to humans). Kept deliberately
minimal: babysit is a pack of skills, not an orchestrator. A run's state lives
in git and the filesystem, not in a ticket system.

**Per-ticket file layout is specified in [ticket-layout.md](ticket-layout.md)** —
that's the authoritative spec for what lives under `tickets/<ticket>/`. This
file covers the *stdout* handoff surface (status block, verdict vocabulary);
the *file* handoff surface (plan.md, handoffs/, verdicts/, reviews/, history.jsonl)
is owned by ticket-layout.md.

## The handoff surface

When a skill finishes, the next actor — a human, another skill, or a parent
orchestrator — reads four things:

| Surface | What lives there |
|---------|------------------|
| **Stdout status block** | `STATUS` + `VERDICT` + `SUMMARY` lines (see below) |
| **Git state** | Branch, commits, diff against base — the code itself |
| **Project home** | `~/.babysit/projects/<slug>/tickets/<ticket>/` — `index.json` metadata, ticket-root canonicals (`plan.md`, `design.md`, `manifest.md`), `handoffs/<NNN>-<skill>.md`, `verdicts/<skill>.md`, `reviews/<skill>.md`, `history.jsonl`, `evidence/…` |
| **Conversation context** | `INVOKER=developer` only — implicit, no extra work needed |

Everything a downstream actor needs must be reachable from one of those four
surfaces. If something only lives in the skill's head, it's not handed off.

**Why outside the repo:** artifacts live under `~/.babysit/projects/<slug>/`
so they survive `git clean -fdx`, don't pollute the working tree, and are
visible from every worktree of the same project. `<slug>` is derived by
`bbs-slug` from the git remote (cached per project root), and `<ticket>` is
re-derived from the branch name (`feat/<ticket>_<name>` etc.) every run —
never from conversation memory. This is the branch-as-oracle invariant; see
[preamble.md § Ticket consistency](preamble.md#ticket-consistency--the-four-layer-invariant).

## Status codes

Defined in [preamble.md § Completion Status Protocol](preamble.md#completion-status-protocol).
Every skill ends with exactly one:

| Status | Meaning |
|--------|---------|
| `DONE` | Completed successfully |
| `DONE_WITH_CONCERNS` | Completed, but the caller should read the concerns |
| `BLOCKED` | Cannot proceed — broken tool, missing access, unresolvable error |
| `NEEDS_CONTEXT` | Missing information — ambiguous requirements, unclear scope |

Non-`DONE` statuses must include `REASON`, `ATTEMPTED`, and `RECOMMENDATION`
lines so the next actor can act without re-investigating.

## Status block format

Print this as the last thing the skill emits — orchestrators and humans both
parse it:

```
STATUS: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
VERDICT: <skill-specific verdict, see below>
SUMMARY: <1–2 sentences of what happened>
```

For `BLOCKED` / `NEEDS_CONTEXT`, add:

```
REASON: <why this can't proceed>
ATTEMPTED: <what was tried>
RECOMMENDATION: <what should happen next — include a question with labeled options if human input is needed>
```

Keep it machine-parseable: one field per line, no nested bullets, no markdown
formatting inside field values.

## Verdicts per skill

A verdict is a short, fixed-shape string that summarises the outcome. Skills
define their own verdict vocabulary; these are the ones currently in the pack:

| Skill | Verdict shape | Meaning |
|-------|--------------|---------|
| `autopilot` | `PLANNED` \| `BUILT` \| `FIXED` \| `HANDOFF` | Workflow reached its named checkpoint |
| `browse` | `CHECKED` | Browser target inspected; evidence captured |
| `implement` | `BUILT` \| `FIXED` \| `CHANGED` | Code change completed and verified |
| `investigate` | `FIXED` \| `INVESTIGATED` | Root cause found; fix either applied or deferred |
| `plan-draft` | `PLANNED(<XS\|S\|M\|L>)` | Plan written or XS skip recorded |
| `plan-draft` | `DECOMPOSED(<N>)` | L work split into N child tickets |
| `qa` | `PASS` \| `FIXED(<N>)` \| `FAIL` | Application verification result |
| `review-pr` | `PASS` \| `FINDINGS(<N>)` \| `FIXED(<N>)` | Pre-landing review result |
| `create-pr` | `PR_CREATED` | Pull request pushed and opened |
| `conversion-fix` | `FIXED` \| `AUDITED` | Conversion issue fixed or audit produced |
| `copy-rewrite` | `REWRITTEN` | Marketing copy updated |
| `design-ui` | `DESIGNED` | Feature design written |
| `growth-experiment` | `RANKED` \| `SCAFFOLDED` | Growth experiment ranked or scaffolded |
| `office-hours` | `DESIGNED` \| `NOT_READY` | Idea stress-tested |
| `recon` | `STEAL(<approach>)` \| `PASS` | External adoption recommendation |
| `social-content` | `SCRIPTS` | Short-form scripts produced |
| `setup-project` | `CONFIGURED` | Repo config created or updated |

When adding a new skill, pick a verdict shape that fits on one line and
communicates the outcome without requiring the reader to open the workspace
directory. Document it in the skill's own `SKILL.md` — this table is a summary.

## CHANGE_BRIEF — the primary file artifact

The Change Brief is what one skill leaves for the next (or for a human reviewer)
so they don't have to re-derive intent from the diff.

**Path:** `~/.babysit/projects/<slug>/tickets/<ticket>/handoffs/<NNN>-<skill>.md`
(Layout C — append-only, one entry per handoff). See
[ticket-layout.md](ticket-layout.md) for the full ticket folder spec.

Don't write the file directly. Build the brief in a tmp file and hand it to
`bbs-ticket`, which appends it with a monotonic position prefix and logs a
`handoff_added` event to `history.jsonl`:

```bash
eval "$("${BBS_SLUG_BIN:-$HOME/.claude/bbs-slug}" env)"
cat > /tmp/<skill>-brief.md <<EOF
SUMMARY: <1–3 sentences: what changed and why>
FILES: <comma-separated changed files>
APPROACH: <one-line implementation approach>
BLAST_RADIUS: <what existing behavior could be affected>
EOF
"${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" add-handoff <skill> /tmp/<skill>-brief.md
```

**Format:**

```
SUMMARY: <1–3 sentences: what changed and why>
FILES: <comma-separated changed files>
APPROACH: <one-line implementation approach>
BLAST_RADIUS: <what existing behavior could be affected>
```

Single-line fields, no markdown headers, so downstream skills can grep them
directly. If a field doesn't apply (`BLAST_RADIUS` for a docs-only change),
write `none` rather than omitting the field.

## Evidence paths

Any skill that produces artifacts beyond the code diff writes them under
`~/.babysit/projects/<slug>/tickets/<ticket>/` following Layout C:

| Path | Contents | Writer |
|------|----------|--------|
| `handoffs/<NNN>-<skill>.md` | Append-only change briefs | `bbs-ticket add-handoff` |
| `verdicts/<skill>.md` | Latest status block per skill (overwritten) | `bbs-ticket set-verdict` |
| `reviews/<skill>.md` | Latest review per skill (overwritten) | `bbs-ticket set-review` |
| `plan.md`, `design.md`, `manifest.md` | Ticket-root canonicals | skill writes file + `bbs-ticket set-pointer` |
| `evidence/*.png` | Screenshots, visual diffs | skill writes directly |
| `evidence/*.json` | Structured outputs (test results, metrics) | skill writes directly |
| `report.md` | Human-readable summary when the skill has many files | skill writes directly |

All metadata mutations go through `bbs-ticket` so `index.json` and
`history.jsonl` stay consistent. Skills should not write inside the repo
working tree. If you need scratch space, use the ticket's project-home
directory. This keeps the diff clean and lets `git clean` stay safe.

**No gitignore needed:** everything lives outside the repo. Cross-ticket
coordination state (`current.txt`, `timeline.jsonl`) lives one level up at
`~/.babysit/projects/<slug>/`.

### Typed evidence artifacts (the mid-tier gate)

Three evidence kinds are *typed* — written through `bbs-ticket set-evidence`
(validated on write) and read back with a categorical `evidence-status`
({`none`|`valid`|`malformed`}), the same hook-checkable shape as
`verdict-status`. Canonical path: `evidence/<kind>/result.json`. These persist
analysis that skills already do in-flight, so the artifact-gated hook *can*
check them. (The hard-stage gate still keys on `verdicts/` — see
[artifact-gated-approval](../../../docs/artifact-gated-approval.md). Typed
evidence is audited, not yet a deny condition.)

| Kind | Owner skill | Required fields | Optional fields |
|------|-------------|-----------------|-----------------|
| `verification` | `implement`, `browse`, `investigate` | `result` (`PASS`\|`FAIL`) | `checks:[{cmd,result}]`, `before`, `after` |

```bash
# write (exit 2 on malformed → producer retries once, then escalates)
bbs-ticket set-evidence --kind verification \
  --json '{"result":"PASS","checks":[{"cmd":"eslint changed","result":"pass"}],"before":"3 errs","after":"0"}'
# read (hook / audit surface)
bbs-ticket evidence-status --kind verification   # → none | valid | malformed
```

Score-free by construction: the status is presence + structure, never a number
— a model can't self-grade its way past the gate.

## Branch-as-oracle invariant

Every artifact path includes `<ticket>`, and `<ticket>` comes from the branch
name — not conversation memory. A skill that writes to the wrong ticket's
directory is a correctness bug, not a cosmetic one, because a later skill
reading that directory will act on the wrong change.

Rules:

1. **Always derive ticket from branch** via `bbs-slug` / `bbs-autopilot`. Never
   accept a ticket id from prior conversation state without cross-checking
   the current branch.
2. **If branch doesn't encode a ticket**, either accept standalone scope (no
   ticket-scoped artifacts — write to a tmp dir or skip) or emit
   `NEEDS_CONTEXT` asking the operator to switch to a `feat/<ticket>_*`
   branch. Never fabricate a ticket id.
3. **Checkpoint.json carries the branch it was written from.** Before acting
   on a checkpoint, cross-check `checkpoint.json.branch` against
   `git rev-parse --abbrev-ref HEAD`. On mismatch, emit `BLOCKED` — the state
   you'd read is for a different branch.

Full rules and the divergence status-block shape live in
[preamble.md § Ticket consistency](preamble.md#ticket-consistency--the-four-layer-invariant).

## Git state conventions

The diff is the primary deliverable — most handoffs are "read the branch." A
few conventions help the next reader:

- **One logical change per commit.** Atomic commits let review-oriented skills
  bisect and comment at the right granularity.
- **Commit messages reference intent, not mechanics.** `Fix retry count race`
  beats `Update invoice.service.ts`.
- **Branch name = feature intent.** `feat/<slug>`, `fix/<slug>`, `chore/<slug>`
  — whatever the project's convention is, stay consistent with existing history.
- **Don't force-push shared branches.** See top-level CLAUDE.md on reversibility.

If the orchestrator (babysit-office, gastown, cron) has more specific
conventions (base branches, PR structure), they layer on top of these — the
skills themselves only need to produce a clean branch with clean commits.

## Escalation delivery

Skills always run autonomously and produce the same surfaces (status block,
change brief, evidence). The only thing that varies between runs is how a
`NEEDS_CONTEXT` reaches a human:

- **`INVOKER=developer`** — render the `NEEDS_CONTEXT` as a single
  `AskUserQuestion`; the human is at the terminal. A second one in the same
  run means you're steering — stop and emit the structured block instead.
- **`INVOKER=mayor|general|scanner|...`** — emit the structured `NEEDS_CONTEXT`
  block with the question in `RECOMMENDATION`. Never call `AskUserQuestion`
  (it hangs the run); the orchestrator relays via its own channel.

See [preamble.md § One mode, two escalation channels](preamble.md#one-mode-two-escalation-channels)
for the full rules and shape.

## What this file deliberately does NOT define

Orchestrators that sit above babysit (babysit-office, gastown, any future
pipeline) may add their own conventions on top — ticket IDs, review cards,
auto-merge labels, cross-run state. Those belong in the orchestrator's docs,
not here. A babysit skill must work when invoked standalone with nothing but a
git repo and a working directory.

If an orchestrator needs a field that isn't on the surfaces listed above, the
orchestrator is responsible for extracting or synthesising it from the status
block, change brief, evidence, and git state. Skills should not grow
orchestrator-specific outputs.
