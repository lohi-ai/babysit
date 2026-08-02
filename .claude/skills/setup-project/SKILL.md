---
name: setup-project
description: Configure the current repo for babysit/autopilot. Use when the user asks to set up a project, initialize babysit config, or make autopilot understand branch and QA defaults.
---
# setup-project
Set up only the config the repo needs. Re-running should be safe.
## Create Or Update
- `.babysit/git-flow.yaml`: minimal `base_branch`, `branch_prefix`, `push`, and `mode`.
- `.babysit/qa.yaml`: minimal local `url`, `start`, `check`, and `flows`.
- `.babysit/config.yaml`: committed — which workspace this repo belongs to, plus optional `name`, `description`, `repo_type`. Written via `bbs workspace config set`; only when the repo joins a workspace.
- `.babysit/.env`: gitignored machine-local values (credentials; related repo paths only when there is no workspace entry).
- `.gitignore`: include `.babysit/.env` if missing.
- `AGENTS.md` or `CLAUDE.md`: add/update only a tiny Babysit pointer section.
## Rules
- Detect defaults from the repo before asking: remote, default branch, run
  commands (package scripts, compose, Makefile), app URL hints.
- Ask once: **how will babysit work in this repo?** (see `references/git-flow.md § Profiles`) — the answer picks the git-flow preset; never ask about `mode`/`land`/`push` directly:
  - Pair-programming assistant — a human watches every run → `trunk`: ride the current branch, no cuts, no PRs.
  - Background worker — one ticket at a time (freelance / client repo) → `branch-pr`: cut per ticket, straight PR — the client-facing work trail.
  - Background worker — parallel tickets, composed local review (small team, or solo + foreman) → `worktree-review`: parallel worktree tickets, review the composed surface on local dev (`serve`), then `create-pr`.
  - Background worker — parallel tickets, straight PRs (big team / enterprise) → `worktree-pr`: parallel worktree tickets, straight per-ticket PRs; review lives on GitHub, browser-test any PR locally on demand (`bbs ticket serve <ticket>`).
  Write the resulting knobs (not a `profile:` key) into `git-flow.yaml`, with the profile name as a comment.
- Re-run on a configured repo = switch: read the current profile from `git-flow.yaml`, ask the same question with the current answer marked as current, and on change rewrite only the git-flow knobs and walk the user through the transition steps (`references/git-flow.md § Profiles`, "Switching later"). Leave `qa.yaml`, `.env`, and the landing doc untouched unless they're missing.
- Prefer the simple top-level `qa.yaml` shape with a localhost `url`; hosted
  URLs are secondary, never a substitute for local QA. If the project cannot
  run locally, record the blocker and closest harness in the landing doc.
- Do not invent credentials or hosted URLs; keep committed config free of
  secrets (values go in ignored files or env vars).
- Verify by parsing config and probing the local app target; not clean if QA
  would only know a happy path.
- Prefer `AGENTS.md` when both exist; otherwise update whichever exists, or
  create `AGENTS.md`. Don't duplicate git-flow/QA rules there — link the
  config files.
- Suggest **cmux** once, never block on it: on a `worktree-*` profile, if
  `command -v cmux` misses, tell the human it is the recommended terminal for
  this shape of work (https://cmux.com) — worker-per-workspace, status pills,
  a real diff viewer, a browser split beside the dev server — and that
  `foreman` requires it outright. It is a machine preference, so it goes in
  the message, not in committed config or the landing doc.
- Related repos (FE/BE counterpart, shared schemas) feed planning and API-contract checks. Their paths belong in the **workspace registry** — `bbs workspace add-repo <ws> --git-url <url> --path <dir> --role <fe|be|shared>` — which is the authority babysit reads. `RELATED_*_REPO` in `.babysit/.env` still resolves for repos that never joined a workspace, but it is a fallback: when both name a role and disagree, babysit blocks instead of picking. Meaning (what each repo is *for*) still goes in `AGENTS.md`.
- Record the harness version once the config is written: `bbs workspace config stamp`. It is what makes "this repo was set up by an older babysit" visible in `bbs workspace show`. A repo with no `harness_version` is not a problem — that is every repo configured before this existed — so never warn about it.
## QA Harness Notes
Prefer this committed shape:
```yaml
# .babysit/git-flow.yaml
# profile: branch-pr — see references/git-flow.md § Profiles
base_branch: main
branch_prefix: feat
push: true
mode: branch
land: pr
```
```yaml
# .babysit/qa.yaml
version: 1
url: http://localhost:5173
start: npm run dev
check: npm test
prepare: npm i && npm run db:migrate   # include only if QA needs install/migrate (idempotent)
revert: npm run db:rollback            # include only if migrations must be undone after QA
flows: login validation, empty state, error state, mobile layout
credentials:            # include only if the app needs a login
  username_env: QA_USER
  password_env: QA_PASS
```
Capture the minimum future agents need:

- local start command and expected port/URL
- health check or page that proves the app booted
- login credentials via the **standard** env-var names `QA_USER` / `QA_PASS`
  (names only in `qa.yaml`; values seeded into `.babysit/.env`)
- 3-5 critical flows, including validation/error/empty-state cases
- commands for the narrowest useful test or lint check
When the app has a login, seed the credential placeholders into the gitignored
`.babysit/.env` (idempotent — never overwrites existing values):
```bash
bbs secrets seed --repo-root "$(git rev-parse --show-toplevel)" QA_USER QA_PASS
```
On machines with multiple GitHub accounts, also seed `GH_ACCOUNT=<login>` into
`.babysit/.env` — `create-pr` and `fix-pr` run `gh auth switch -u "$GH_ACCOUNT"`
before pushing, so the wrong active account can't fail the push.
## Landing Doc Section
Add or update exactly one concise section in `AGENTS.md` or `CLAUDE.md`:
```md
## Babysit

This repo is configured for babysit autonomous runs.

- Git policy: `.babysit/git-flow.yaml`
- QA harness: `.babysit/qa.yaml`
- Browser: for any UI check — open a URL, click a flow, read console errors, screenshot — invoke `/bbs:browse` (or `/bbs:qa` for a full loop). These drive a real Chromium via `agent-browser`; there is no separate browser *tool* to look for, and `WebFetch` is not a substitute. One-time: `npm install -g agent-browser cloakbrowser`.
- Default run: `/goal "STATUS: DONE or STATUS: BLOCKED appears" /bbs:autopilot "<task>"`

QA must prove the local target or name the blocker, and must include at least one non-happy-path case before PASS.
```
If a `## Babysit` section already exists, replace only that section. Do not
rewrite unrelated project instructions.
When related repos exist or the user provides them, also add or update this
section:
```md
## Related Repos

Use these repos for investigation and planning when a task crosses FE/BE,
API contracts, generated types, or shared schemas. Local paths are machine
specific: they live in the workspace registry (`bbs workspace show`), which
is the authority. `$RELATED_*_REPO` in `.babysit/.env` is a fallback for
repos outside a workspace.

- Backend API: role `be`
- Frontend app: role `fe`
- Shared package: role `shared`
```
Include only repos that apply. If a `## Related Repos` section already exists,
replace only that section. Do not commit absolute local paths to `AGENTS.md` or
`CLAUDE.md`.
Register each related repo the human names:
```bash
bbs workspace create <workspace>          # idempotent
bbs workspace add-repo <workspace> --git-url <origin-url> --path <local-dir> --role be
bbs workspace config set workspace <workspace>   # writes this repo's .babysit/config.yaml
```
The registry lives in `~/.babysit/workspaces/` — machine-local, never
committed, which is what keeps absolute paths out of git.
On a repo that is not joining a workspace, seed `.babysit/.env` instead, after
ensuring it is gitignored:
```bash
# .babysit/.env  (gitignored) — fallback when there is no workspace entry
RELATED_BACKEND_REPO=../api
RELATED_FRONTEND_REPO=../web
RELATED_SHARED_REPO=../shared
```
Don't seed both for the same role. Do not fail setup when a related repo path
is absent — record where the path is expected to come from and leave it unset.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
CONFIG: <files created/updated, including AGENTS.md/CLAUDE.md and .babysit/.env when related repos are configured>
VERIFY: <config parse + local app probe/check, or named blocker>
NEXT: /bbs:autopilot "<feature>"
```
