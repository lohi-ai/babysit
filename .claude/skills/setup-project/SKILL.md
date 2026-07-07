---
name: setup-project
description: Configure the current repo for babysit/autopilot. Use when the user asks to set up a project, initialize babysit config, or make autopilot understand branch and QA defaults.
---

# setup-project

Set up only the config the repo needs. Re-running should be safe.

## Create Or Update

- `.babysit/git-flow.yaml`: minimal `base_branch`, `branch_prefix`, `push`, and `mode`.
- `.babysit/qa.yaml`: minimal local `url`, `start`, `check`, and `flows`.
- `.babysit/.env`: gitignored machine-local values, including related repo paths.
- `.gitignore`: include `.babysit/.env` if missing.
- `AGENTS.md` or `CLAUDE.md`: add/update only a tiny Babysit pointer section.

## Rules

- Detect defaults from git before asking: remote, default branch, package scripts, app URL hints.
- Ask once for the git-flow `mode` (see `references/git-flow.md § mode:`): `trunk` = tickets share the current branch, no cuts; `branch` (default) = cut in place when safe, worktree when not; `worktree` = every ticket in its own worktree, primary checkout pinned to base as the shared test surface. Rule of thumb: solo/hobby repos → `trunk` or `branch`; one-ticket-per-PR team repos → `worktree`.
- Detect how to run the project locally: package scripts, compose files, Procfile, Makefile, framework defaults, or existing docs.
- Prefer the simple top-level `qa.yaml` shape with a localhost `url`. Hosted/dev URLs are secondary, not a substitute for local QA.
- If the project cannot run locally, record the blocker and the closest available harness in `CLAUDE.md` / `AGENTS.md`.
- Do not invent credentials or hosted URLs.
- Keep committed config free of secrets. Put local secrets in ignored files or env vars.
- Verify by parsing config and, when possible, starting or probing the local app target.
- Do not mark setup clean if QA would only know how to test a happy path.
- Prefer `AGENTS.md` when both `AGENTS.md` and `CLAUDE.md` exist; otherwise update whichever exists, or create `AGENTS.md`.
- Do not duplicate git-flow or QA rules in the landing doc. Link to config files and keep detailed behavior in bbs skills/docs.
- Related repos are important planning/investigation context for FE/BE tasks, API contract checks, generated types, and shared schemas.
- Put related repo meaning in `AGENTS.md`, but put machine-specific paths in `.babysit/.env` because checkout paths differ by user and machine.
- Use stable env-var names for related repos, for example `RELATED_BACKEND_REPO`, `RELATED_FRONTEND_REPO`, and `RELATED_SHARED_REPO`.

## QA Harness Notes

Prefer this committed shape:

```yaml
# .babysit/git-flow.yaml
base_branch: main
branch_prefix: feat
push: true
mode: branch   # trunk | branch | worktree — see references/git-flow.md
```

```yaml
# .babysit/qa.yaml
version: 1
url: http://localhost:5173
start: npm run dev
check: npm test
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
bbs-secrets seed --repo-root "$(git rev-parse --show-toplevel)" QA_USER QA_PASS
```

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
specific and live in `.babysit/.env`.

- Backend API: `$RELATED_BACKEND_REPO`
- Frontend app: `$RELATED_FRONTEND_REPO`
- Shared package: `$RELATED_SHARED_REPO`
```

Include only repos that apply. If a `## Related Repos` section already exists,
replace only that section. Do not commit absolute local paths to `AGENTS.md` or
`CLAUDE.md`.

Seed `.babysit/.env` with commented placeholders or detected local paths after
ensuring it is gitignored:

```bash
# .babysit/.env  (gitignored)
RELATED_BACKEND_REPO=../api
RELATED_FRONTEND_REPO=../web
RELATED_SHARED_REPO=../shared
```

Do not fail setup when a related repo path is absent. Record the env-var name in
the landing doc and leave the `.babysit/.env` value blank or commented so each
developer can fill their local path.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
CONFIG: <files created/updated, including AGENTS.md/CLAUDE.md and .babysit/.env when related repos are configured>
VERIFY: <config parse + local app probe/check, or named blocker>
NEXT: /bbs:autopilot "<feature>"
```
