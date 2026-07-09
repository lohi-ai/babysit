---
name: Ticket size rubric
description: Canonical S/M/L signals used by plan-draft to classify tickets and by `bbs-ticket ensure-size` to self-estimate when the `ticket_size` pointer is absent.
---
# Ticket size rubric
The `ticket_size` pointer (`XS`|`S`|`M`|`L`) controls depth inside heavy
skills. `plan-draft` sets it at Step 1 against the expected footprint;
downstream skills read it via `bbs-ticket ensure-size` (returns the pointer,
or estimates from the PR diff, persists, and prints — never re-estimate
inline). When you change thresholds here, update `bbs-ticket`'s `ensure-size`
in the same commit.
Signals measured against the PR diff (`git diff <base>...HEAD`): **files**
(name-only count), **loc** (insertions+deletions), **modules** (distinct
top-level dirs), **migrations** (paths matching `migrations?/`, `alembic/`,
`schema.`, `*.sql`), **deps** (manifest/lockfile paths).
A ticket is the **largest size any row matches**:

| | XS | S | M | L |
|---|---|---|---|---|
| files | 1 | ≤ 3 | ≤ 10 | > 10 |
| loc | ≤ 20 | ≤ 50 | ≤ 300 | > 300 |
| modules | 1 | 1 | ≤ 3 | > 3 |
| migrations | 0 | 0 | additive only | any destructive / any L-row hit |
| deps | 0 | 0 | ≤ 1 (justified) | > 1 or new runtime service |
| new symbols / API change | none | none | additive | any breaking |
| ticket type | one-liner (typo, dep bump, null guard, list extension) | pattern-extend/modify | pattern-extend/modify/new-capability (single subsystem) | new-capability / cross-cutting |
**Default when ambiguous: M.** XS requires a row to affirmatively match the
XS column — absent confident XS signals, fall back to S or higher. This file
wins over any skill-local rubric copy.
## Downgrade triggers (scope contraction)
Real execution sometimes contracts the planned scope mid-flight; two hooks
downgrade the pointer one tier (`L→M`, `M→S`, `S→XS`; never upgrade, never
below XS): **A. Plan deferral** (`plan-draft` before handoff — ≥40% of
in-scope items deferred to follow-ups) and **B. Files-Modified collapse**
(`implement` before editing — ≤3 expected files, all trivial doc/comment
work).
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
`$TRIGGER` is `deferral_ratio>=40%` (A) or `files_modified<=3_trivial` (B).
Subsequent skills pick up the new value via `bbs-ticket ensure-size`.
