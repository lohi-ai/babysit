# QA config

Keep QA config small. The goal is to let `/goal` run `/bbs:autopilot`, and let
autopilot run `/bbs:qa` without asking how to start or test the project.

## Minimal `.babysit/qa.yaml`

```yaml
version: 1
url: http://localhost:5173
start: npm run dev
check: npm test
flows: login validation, empty state, error state, mobile layout
credentials:            # optional — omit if the app needs no login
  username_env: QA_USER
  password_env: QA_PASS
```

That is enough for the default path:

```text
/goal "STATUS: DONE or STATUS: BLOCKED appears" /bbs:autopilot "<task>"
```

`url` is the only required field. The other fields make QA meaningful:

| Field | Purpose |
|-------|---------|
| `url` | Local target QA must boot or probe before PASS. |
| `start` | Command a future agent should use to run the app locally. |
| `check` | Narrow useful check before or after browser QA. |
| `flows` | Critical cases, including at least one non-happy-path case. |
| `credentials` | Standard login env-var **names** — `QA_USER` / `QA_PASS` — whose values live in `.babysit/.env`. Never the secrets. |

QA must not return `PASS` if it only tests a happy path, or if it never proves
a local target is running and does not name a local-run blocker.

## Secrets

Do not put secrets in `qa.yaml` — it names env vars, never their values. The
committed `credentials:` block above points at two **standard** login env vars,
`QA_USER` (account) and `QA_PASS` (password). Put the real values in the
gitignored `.babysit/.env`:

```bash
# .babysit/.env  (gitignored)
QA_USER=alice@example.com
QA_PASS='...'
```

`/bbs:qa` loads them at runtime with `eval "$(bbs-secrets load)"` and resolves
the names via `bbs-qa-config probe`. Shell-exported vars win over the file, so
CI can inject `QA_USER` / `QA_PASS` without touching `.babysit/.env`.

## Advanced Named Envs

The older named-env shape still works when a project needs staging or multiple
targets:

```yaml
version: 1
default_env: local
environments:
  - name: local
    url: http://localhost:5173
    guideline: "Start with npm run dev; test validation and empty states."
  - name: staging
    url: https://staging.example.com
    credentials:
      username_env: QA_USER
      password_env: QA_PASS
```

Use the simple top-level shape unless the project truly has multiple QA targets.

## Checks

```bash
bbs-qa-config default-env
bbs-qa-config probe --env local
bbs-qa-config check
```
