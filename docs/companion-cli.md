# Companion CLI

`setup-skills` installs the bins below as symlinks into `~/.claude/`. Run `<bin> --help` for full usage.

Most of these are now the compiled Go `bbs` multicall binary rather than
standalone bash: each `bbs-<name>` is a symlink to `bin/bbs`, which dispatches
on `argv[0]` (`bbs-config` → `bbs config`, etc.). The bins ported to Go —
`bbs-config`, `bbs-env`, `bbs-slug`, `bbs-ticket`, `bbs-update-check`,
`bbs-upgrade`, `bbs-learnings-log`, `bbs-learnings-search`, `bbs-qa-config`,
`bbs-telemetry-log`, `bbs-codex-competitive`, `bbs-analytics-cron`,
`bbs-secrets`, `bbs-design`, `bbs-dashboard`, `bbs-autopilot`, `bbs-ticket` —
behave identically to the bash they replaced (guarded by the differential
harnesses in `tests/`). `bbs-ticket` was the last strangler: every subcommand
now runs natively in Go — identity/verdict/session/board, the index.json
state-accessors (`get`/`set-*`/`add-*`/`ensure-size`/`append-history`/`env`),
the file-only manifest.yaml ops (`init`/`get-manifest`/`set-branch`), the
git-mutating base-ops (`merge-base`/`refresh`/`reset-base`/`switch`/`serve`/
`qa-lease`), `ensure`, and `path`/`list`/`reconcile`/`find-similar`. No bash
remains in production; a frozen byte-identical copy of the old script lives at
`tests/fixtures/bbs-ticket.reference` purely as the differential oracle.

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
| `bbs-secrets` | `load` (emit `export KEY='…'` for `.babysit/.env` keys not already in shell env) / `seed` / `ensure-gitignore` — project-local credential auto-loader for the qa skill |
| `bbs-design` | `tokens` (DESIGN.md frontmatter → JSON, `--field` for a leaf) / `suggest --product <type>` / `components` / `ux-check` — design-intelligence broker for the design-ui skill |
