---
name: Ticket size rubric
description: Canonical S/M/L signals used by plan-draft to classify tickets and by `bbs-ticket ensure-size` to self-estimate when the `ticket_size` pointer is absent.
---

# Ticket size rubric

The `ticket_size` pointer (`S` | `M` | `L`) is the single knob that controls
depth inside heavy skills. `plan-draft` sets it at Step 1. Downstream workflows
read it via
`bbs-ticket ensure-size`, which returns the pointer if set and otherwise
estimates from the PR diff, persists the result, and prints it. So later
skills never re-estimate — the first one to hit an absent pointer fixes it.

## Signals

All thresholds are measured against the PR diff (`git diff <base>...HEAD`) or,
for `plan-draft`, against the expected change footprint.

| Signal | How it's measured |
|--------|-------------------|
| **files** | `git diff --name-only <base>...HEAD \| wc -l` |
| **loc** | insertions + deletions from `git diff --shortstat` |
| **modules** | distinct top-level dirs (`awk -F/ 'NF>1{print $1}' \| sort -u \| wc -l`) |
| **migrations** | count of diff paths matching `migrations?/`, `alembic/`, `schema.`, or `*.sql` |
| **deps** | count of diff paths matching `package.json`, `package-lock*`, `requirements.txt`, `go.mod`/`go.sum`, `Gemfile*`, `pyproject.toml`, `Cargo.toml`/`Cargo.lock` |

## Classification

A ticket is the **largest size any row matches** — thresholds are inclusive
upper bounds for the given size.

| | XS | S | M | L |
|---|---|---|---|---|
| files | 1 | ≤ 3 | ≤ 10 | > 10 |
| loc | ≤ 20 | ≤ 50 | ≤ 300 | > 300 |
| modules | 1 | 1 | ≤ 3 | > 3 |
| migrations | 0 | 0 | additive only | any destructive / any L-row hit |
| deps | 0 | 0 | ≤ 1 (justified) | > 1 or new runtime service |
| new symbols / API change | none | none | additive | any breaking |
| ticket type | one-liner (typo, dep bump, null guard, list extension) | pattern-extend/modify | pattern-extend/modify/new-capability (single subsystem) | new-capability / cross-cutting |

**Default when ambiguous:** M. Review cheaper than a missed concern, but L is
expensive enough that we don't want to pay it by accident — pick M unless a
signal lands squarely in L territory. XS is the only tier with a positive
requirement — a row must affirmatively match the XS column; absent confident
XS signals, fall back to S or higher.

Skills consuming this rubric (`plan-draft`, `implement`) maintain their own
copy with skill-specific commentary at
`<skill>/references/ticket-size-rubric.md`. The skill-local rubrics treat
this one as the source of truth for thresholds; if a skill-local rubric
disagrees, this file wins.

## Downgrade triggers (scope contraction)

`plan-draft` sets `ticket_size` against the *expected*
footprint. Real execution sometimes contracts that scope mid-flight — the
plan defers half its scope to follow-up tickets, or `Files Modified` collapses
to a few doc-only fixes after an audit. When this happens, the higher-tier
ceremony in `implement` keeps running on what is now S-shaped work.

Two hooks downgrade the pointer when these signals fire. Each downgrade is
one tier (`L → M`, `M → S`, `S → XS`); the rubric never upgrades and never
downgrades below `XS`.

| Hook | Where | Trigger |
|------|-------|---------|
| **A. Plan deferral check** | `plan-draft` before handoff | ≥40% of the plan's in-scope items have been deferred to follow-up tickets. |
| **B. Files-Modified collapse** | `implement` before editing | Expected file list has ≤3 entries and every entry is trivial documentation or comment-only work. |

When either trigger fires, downgrade the pointer and append one line to the
decision audit trail:

```bash
OLD="$("${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" get pointers.ticket_size)"
case "$OLD" in
  L) NEW=M ;;
  M) NEW=S ;;
  S) NEW=XS ;;
  XS) NEW=XS ;;  # already minimum — no-op, no log entry
esac
if [ "$NEW" != "$OLD" ]; then
  "${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" set-pointer ticket_size "$NEW"
  _ADIR="${BABYSIT_ANALYTICS_DIR:-$HOME/.babysit/analytics}"
  mkdir -p "$_ADIR"
  printf '{"ts":"%s","skill":"%s","ticket":"%s","kind":"resize","from":"%s","to":"%s","trigger":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$SKILL_NAME" "$TICKET" "$OLD" "$NEW" "$TRIGGER" \
    >> "$_ADIR/decisions.jsonl"
fi
```

`$TRIGGER` is one of: `deferral_ratio>=40%` (Hook A) or `files_modified<=3_trivial`
(Hook B). Subsequent skills read the new value via `bbs-ticket ensure-size`
and adjust ceremony accordingly — no in-flight phase re-runs needed.

## Implementation

- `bbs-ticket ensure-size` — executes the diff-based rubric. Single source of
  truth. Skills call it; don't inline the signals bash.
- `plan-draft` Step 1 — applies the same rubric against expected footprint
  (no diff yet) and writes the pointer directly via `bbs-ticket set-pointer`.
  If Step 6b self-review revises the classification, rewrite the pointer
  before the final handoff.

When you change the thresholds above, update `bbs-ticket`'s `ensure-size`
case in the same commit.
