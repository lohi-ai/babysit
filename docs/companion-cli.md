# Companion CLI

`setup-skills` installs the bins below as symlinks into `~/.claude/`. Run `<bin> --help` for full usage.

Most of these are now the compiled Go `bbs` multicall binary rather than
standalone bash: each `bbs-<name>` is a symlink to `bin/bbs`, which dispatches
on `argv[0]` (`bbs-config` → `bbs config`, etc.). The bins ported to Go —
`bbs-config`, `bbs-env`, `bbs-slug`, `bbs-ticket`, `bbs-update-check`,
`bbs-upgrade`, `bbs-learnings-log`, `bbs-learnings-search`, `bbs-qa-config`,
`bbs-telemetry-log`, `bbs-codex-competitive`, `bbs-analytics-cron` — behave
identically to the bash they replaced (guarded by the differential harnesses in
`tests/`). `bbs-ticket` is a strangler: its Go core owns identity/verdict/
session/board and delegates the remaining subcommands to a `bbs-ticket.bash`
sibling. Still pure bash: `bbs-autopilot`, `bbs-dashboard`, `bbs-design`,
`bbs-secrets`.

| Bin | Purpose |
|-----|---------|
| `bbs-autopilot` | State helpers the `/bbs:autopilot` skill uses, also runnable by hand for debugging: `probe` (dump probed state), `explain` (show recommended workflow; add `--details` for the per-workflow PASS/FAIL table), `base-branch` (resolve with per-project override), `lint-workflow <path>` (authoring-time `needs-state:` lint), plus the checkpoint surface `read` / `checkpoint` / `timeline` / `recover` / `clear` / `current` |
| `bbs-ticket` | Ticket-layout broker and state-probe surface. `path <kind>` resolves Layout C file paths; `verdict-status --skill <n>` reads the latest verdict for a sub-skill (used by autopilot's Probe and Verify-post) |
| `bbs-learnings-log` | `decision --type mechanical\|taste …` appends routing/taste decisions to `~/.babysit/analytics/decisions.jsonl` — autopilot's Dispatch phase logs every route here |
| `bbs-slug` | Derives `<slug>` / `<ticket>` / `<branch>` from git remote + current branch — the branch-as-anchor mechanism `/bbs:autopilot` relies on for resume |
| `bbs-env` | `resolve` / `is-set` / `list-prefix` / `prompt` — env resolution with `.env.base` auto-load |
| `bbs-config` | `get` / `set` / `list` in `~/.babysit/config.yaml` |
| `bbs-update-check` | Prints `UPGRADE_AVAILABLE <old> <new>` when a new release exists (cached) |
| `bbs-upgrade` | `git pull` + `setup-skills`; writes a `JUST_UPGRADED` marker |
| `bbs-qa-config` | Reads `.babysit/qa.yaml` fields (`url`, `start`, `check`, `flows`, `prepare`/`revert`) for the qa skill |
| `bbs-telemetry-log` | Appends skill-usage rows to `~/.babysit/analytics/skill-usage.jsonl` |
| `bbs-codex-competitive` | Competitive-analysis helper the analytics skills call |
| `bbs-analytics-cron` | `--install` / `--uninstall` / `--dry-run` the weekly `/bbs:analytics-review` schedule (launchd on macOS, cron on Linux) |
