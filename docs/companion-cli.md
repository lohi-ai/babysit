# Companion CLI

`setup-skills` installs the bins below as symlinks into `~/.claude/`. Run `<bin> --help` for full usage.

| Bin | Purpose |
|-----|---------|
| `bbs-autopilot` | State helpers the `/bbs:autopilot` skill uses, also runnable by hand for debugging: `probe` (dump probed state), `explain` (show recommended workflow; add `--details` for the per-workflow PASS/FAIL table), `base-branch` (resolve with per-project override), `lint-workflow <path>` (authoring-time `needs-state:` lint), plus the checkpoint surface `read` / `checkpoint` / `timeline` / `recover` / `clear` / `current` |
| `bbs-ticket` | Ticket-layout broker and state-probe surface. `path <kind>` resolves Layout C file paths; `verdict-status --skill <n>` reads the latest verdict for a sub-skill (used by autopilot's Probe and Verify-post) |
| `bbs-learnings-log` | `decision --type mechanical\|taste …` appends routing/taste decisions to `~/.babysit/analytics/decisions.jsonl` — autopilot's Dispatch phase logs every route here |
| `bbs-slug` | Derives `<slug>` / `<ticket>` / `<branch>` from git remote + current branch — the branch-as-anchor mechanism `/bbs:autopilot` relies on for resume |
| `bbs-env` | `resolve` / `is-set` / `list-prefix` / `prompt` — env resolution with `.env.base` auto-load |
| `bbs-db` | `snapshot` / `restore` / `list` — postgres snapshots per rig |
| `bbs-config` | `get` / `set` / `list` in `~/.babysit/config.yaml` |
| `bbs-update-check` | Prints `UPGRADE_AVAILABLE <old> <new>` when a new release exists (cached) |
| `bbs-upgrade` | `git pull` + `setup-skills`; writes a `JUST_UPGRADED` marker |
