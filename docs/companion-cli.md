# Companion CLI

There is **one binary**: `bbs`. `setup-skills` builds it and symlinks it onto
your `PATH` at `~/.local/bin/bbs` (`brew install bbs` gets you the same binary
with no checkout). Every command below is a subcommand of it — call them as
`bbs <sub>`. Run `bbs <sub> --help` for full usage.

`bbs` is a multicall binary: it also dispatches on `argv[0]`, so the
`bbs-<name>` symlinks `setup-skills` drops into `~/.claude/` still work
(`bbs-config` → `bbs config`). Those are **legacy aliases only**, and a
Homebrew install ships just two of those aliases (`bbs-config`, `bbs-env`) —
which is why skills and docs always use the space form.

Every subcommand is now native Go, behaving identically to the bash it
replaced (guarded by the differential harnesses in `tests/`). `ticket` was the
last strangler: identity/verdict/session/board, the index.json state-accessors
(`get`/`set-*`/`add-*`/`ensure-size`/`append-history`/`env`), the file-only
manifest.yaml ops (`init`/`get-manifest`/`set-branch`), the git-mutating
base-ops (`merge-base`/`refresh`/`reset-base`/`switch`/`serve`/`qa-lease`),
`ensure`, and `path`/`list`/`reconcile`/`find-similar`. No bash remains in
production; a frozen byte-identical copy of the old script lives at
`tests/fixtures/bbs-ticket.reference` purely as the differential oracle.

| Command | Purpose |
|---------|---------|
| `bbs autopilot` | State helpers the `/bbs:autopilot` skill uses, also runnable by hand for debugging: `probe` (dump probed state), `explain` (show recommended workflow; add `--details` for the per-workflow PASS/FAIL table), `base-branch` (resolve with per-project override), `lint-workflow <path>` (authoring-time `needs-state:` lint), plus the checkpoint surface `read` / `checkpoint` / `timeline` / `recover` / `clear` / `current` |
| `bbs ticket` | Ticket-layout broker and state-probe surface. `env` derives `SLUG`/`BRANCH`/`TICKET`/`BABYSIT_PROJECT_HOME` from git remote + current branch — the branch-as-anchor mechanism `/bbs:autopilot` relies on for resume, and what every skill preamble evals; `path <kind>` resolves Layout C file paths; `verdict-status --skill <n>` reads the latest verdict for a sub-skill (used by autopilot's Probe and Verify-post) |
| `bbs config` | `get` / `set` / `list` in `~/.babysit/config.yaml` |
| `bbs upgrade` | `git pull` + `setup-skills`; writes a `JUST_UPGRADED` marker. `bbs upgrade check` is the cached probe — prints `UPGRADE_AVAILABLE <old> <new>` when a new release exists |
| `bbs secrets` | Everything a skill reads to reach a running app. `load` (emit `export KEY='…'` for `.babysit/.env` keys not already in shell env) / `seed` / `ensure-gitignore` — project-local credential auto-loader; `resolve` / `is-set` / `list-prefix` / `prompt` — env resolution with `.env.base` auto-load; `qa <probe\|list\|default-env\|check\|leak-check>` — named-environment fields (`url`, `start`, `check`, `flows`, `prepare`/`revert`) from `.babysit/qa.yaml` |
| `bbs design` | `tokens` (DESIGN.md frontmatter → JSON, `--field` for a leaf) / `suggest --product <type>` / `components` / `ux-check` — design-intelligence broker for the design-ui skill |
| `bbs dashboard` | Serves the dashboard + JSON API on `127.0.0.1` and opens it. The SPA is embedded in released binaries, so a brew-only install needs no checkout and no npm; a checkout's own `web/dist` wins when it exists. `--snapshot` writes `web/dist/data.js` and opens the `file://` build instead, `build` rebuilds `web/`, `--no-open` for CI, `--dev` for vite + HMR |
