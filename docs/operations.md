# Operations

Day-2 configuration, telemetry, and upgrade handling.

## Configuration

```bash
bbs config set telemetry local       # off | local
bbs config set update_check true     # false silences upgrade notifications
bbs config set auto_upgrade false    # true runs bbs upgrade on session start
bbs config set proactive true        # false = only run skills typed explicitly
bbs config list                      # show all keys + annotated docs
```

## Telemetry

Skill runs append JSON Lines to `~/.babysit/analytics/skill-usage.jsonl`. Because babysit runs unattended, telemetry is the *primary* feedback channel — treat it as load-bearing, not decoration. Local-only by default; nothing leaves the machine.

Nothing summarizes that file on demand — the reader is the `/bbs:analytics-review` skill, dispatched on the [weekly cadence](#weekly-review-cadence) below. To look at the raw rows, read the JSONL directly.

### Decision search

```bash
bbs learnings-search                    # last 10 decisions, current project
bbs learnings-search "autopilot" --limit 5
bbs learnings-search --cross-project    # across all projects
```

Query the Auto-Decision Framework's audit trail in `~/.babysit/analytics/decisions.jsonl`. `investigate` can use this for prior-learnings context.

### Weekly review cadence

Telemetry is only load-bearing if something reads it. `bbs analytics-cron` is the cadence: once a week it headlessly dispatches `/bbs:analytics-review` and writes the ticket-ready report to `~/.babysit/analytics/reviews/<date>.md`. The run is read-only (the skill never edits the pack), scoped to read-only tools, with `AGENT_ROLE` cleared so it renders a plain report instead of an orchestrator relay block.

```bash
bbs analytics-cron --dry-run   # show what it would run
bbs analytics-cron             # run the review now
bbs analytics-cron --install   # register weekly (macOS launchd / cron, Mondays 09:00)
bbs analytics-cron --uninstall
```

`--install` writes a launchd agent (`~/Library/LaunchAgents/dev.babysit.analytics.plist`) on macOS, or appends a crontab line elsewhere. Both point at the resolved script path, so they survive the `setup-skills` symlink. Reports accumulate under `analytics/reviews/`; file the top finding of a report as a `sweep`/`maintain` ticket.

## Auto-update

`bbs update-check` compares the local `VERSION` against `main` on GitHub, with cache-friendly TTLs (60 min when up-to-date, 12 h when an upgrade is pending). Typical preamble wiring:

```bash
UPD="$(bbs update-check 2>/dev/null || true)"
case "$UPD" in
  "UPGRADE_AVAILABLE "*) echo "babysit upgrade available — run bbs upgrade";;
  "JUST_UPGRADED "*)     echo "babysit upgraded: $UPD";;
esac
```

Snooze a pending upgrade: `bbs upgrade --snooze 1` (24 h), `2` (48 h), `3` (7 d).

## Workflow linting

Every workflow file must declare `needs-state:` frontmatter so the autopilot orchestrator can route mechanically. `bbs autopilot lint-workflow <path>` validates this and checks for missing `> produces:` directives.

```bash
# Lint a single workflow
bbs autopilot lint-workflow .claude/skills/autopilot/workflows/builder.md

# Lint all workflows
for wf in .claude/skills/autopilot/workflows/*.md .claude/workflows/*.md; do
  [ -f "$wf" ] && bbs autopilot lint-workflow "$wf"
done
```

### Pre-commit hook

`setup-skills` installs a pre-commit hook that auto-lints staged workflow files. To install or reinstall:

```bash
./bin/setup-skills
```

### CI

The `Lint Workflows` GitHub Action runs on pushes and PRs that touch workflow `.md` files. See `.github/workflows/lint-workflows.yml`.

## Health checks

There is no health-check command. `./bin/setup-skills` reports what it linked and warns when `~/.local/bin` is missing from your `PATH`; beyond that, the preamble is the live check — it emits `BBS_DEGRADED` on stderr at the top of every skill run when no working `bbs` is reachable, which is the failure that actually matters.

To verify an install by hand:

```bash
bbs ticket --help     # exit 0 = the binary is present and serving subcommands
bbs config list       # reads ~/.babysit/config.yaml
```
