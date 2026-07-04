# lint-test-fixtures

Self-test fixture for `bin/bbs-ticket-lint`. The linter scans markdown for
direct ticket-path constructions inside ` ```bash ` code blocks.

`bbs-ticket-test` scans this file with the linter and asserts **exactly 5**
unapproved hits (the 5 lines below inside the bash block, minus the one with
the `# lint:allow-direct-path` marker).

## Known-bad lines (must be flagged)

```bash
# 5 direct-path hits the linter MUST flag.
PLAN="$TH/plan.md"
VERDICT="$TICKET_HOME/verdicts/plan-draft.md"
REQ="$BABYSIT_PROJECT_HOME/tickets/$TICKET/requirement.md"
DESIGN="$TH/design.md"
HISTORY="$TH/history.jsonl"
```

## Allow-marker line (must NOT be flagged)

```bash
# This one is intentionally direct — exempted by the marker.
LEGACY="$TH/legacy/cache.bin"  # lint:allow-direct-path
```

## Known-good lines (must NOT be flagged — outside bash blocks)

These appear in prose and inline backticks, not in fenced code blocks. The
linter is scoped to ```` ```bash ```` blocks only.

- `$TH/plan.md` is the canonical plan path under Layout C.
- A handoff lives at `$TH/handoffs/<NNN>-<skill>-<status>.md`.
- The verdict file is `$TICKET_HOME/verdicts/<skill>.md`.
- `$BABYSIT_PROJECT_HOME/tickets/$TICKET/` is the ticket home.
- The full path is `$TH/evidence/<skill>/<name>` for evidence.

## Other-language fenced blocks (must NOT be flagged)

```sh
# Not a `bash` fence — should be ignored.
PLAN="$TH/plan.md"
```

```
# Unfenced (no language) — should be ignored.
PLAN="$TH/plan.md"
```
