---
name: browse
description: Use the browser for focused web-app checks: open a URL, inspect state, click through a flow, capture screenshots, read console errors, or verify a frontend fix. Prefer this over a full QA workflow.
---

# browse

Small browser checks only. Do not turn this into a product review unless asked.

## Use When

- The user asks to open, inspect, click, type, screenshot, or verify a URL.
- A local frontend change needs a quick smoke test.
- `autopilot` or `implement` needs proof that a touched UI renders.

## Rules

- Use the real target URL when known. For local apps, prefer the running dev server; start one only if the task needs it.
- Capture the smallest useful proof: current page state, console errors, screenshot path, and one or two exercised interactions.
- Report failures with reproduction steps and the exact error. Fix only if the user asked for a fix or the calling workflow owns the code change.
- Keep screenshots and logs under the ticket or repo temp area when possible.

## Engine

Browsing runs on [`agent-browser`](https://github.com/vercel-labs/agent-browser) — `agent-browser open <url>`, then `snapshot -i` for refs, `click @e<n>` / `type @e<n> <text>` / `fill` / `select` to interact (the `@ref` is an argument to a verb, never a bare command), plus `eval`, `screenshot`, `errors`, `vitals`. One-time: `npm install -g agent-browser cloakbrowser`.

**One Claude Code session = one browser.** The session name is the browser identity: the same name reattaches to that browser's daemon (cookies, tabs, window); a different name — or *no* name, which silently falls through to the shared `default` session — launches another instance. So never derive the name from the target URL, task, or an ad-hoc label (that's how one run leaks N windows and loses the setup done in the previous one), and never run a bare `agent-browser` command before this export (that's the phantom second window at startup):

```bash
export AGENT_BROWSER_SESSION="cc-${CLAUDE_CODE_SESSION_ID:0:8}"
# outside Claude Code: export AGENT_BROWSER_SESSION="$(agent-browser session id --scope worktree)"
```

Every Bash call in this Claude Code session then reuses one window; another Claude Code window gets its own. Refs (`@e<n>`) are per-session: snapshot the session you're about to act on. If parallel subagents each need their own window, give each `export AGENT_BROWSER_NAMESPACE=<agent-id>` on top.

When the check is done, `agent-browser close` your session. A crashed run leaves its browser behind — on macOS even headless ones keep a Chrome-for-Testing icon in the Dock — so if `agent-browser session list` shows names you don't recognize, `agent-browser close --all` (and per `--namespace <ns>` for anything under `~/.agent-browser/namespaces/`).

Sessions are isolated browsers, so login state doesn't carry across Claude Code windows by itself. To share one "profile", add `--restore bbs-profile` to `open` (and `close`): every session loads/saves the same cookies+localStorage bundle under `~/.agent-browser/sessions/`, so a login done in one window is there for the next. Don't point concurrent sessions at one `--profile` dir instead — Chromium locks the user-data-dir per instance.

**Default: headed + cloakbrowser stealth.** Run agent-browser through [`cloakbrowser`](https://github.com/CloakHQ/cloakbrowser)'s patched Chromium in a visible window — survives Cloudflare Turnstile / FingerprintJS / DataDome, and a watching human sees the flow. agent-browser spawns the binary itself (it can't attach over CDP), so set these once per shell before any `agent-browser` call:

```bash
CB="$(npm root -g)/cloakbrowser/dist/index.js"   # cloakbrowser is ESM-only; import by abs path (NODE_PATH won't resolve it)
export AGENT_BROWSER_HEADED=1
export AGENT_BROWSER_EXECUTABLE_PATH="$(node --input-type=module -e "import('$CB').then(m=>m.ensureBinary()).then(p=>process.stdout.write(p))" 2>/dev/null)"
# Deterministic --fingerprint seeded from the session name (NOT cwd — a cd between Bash
# calls would change a cwd seed). getDefaultStealthArgs() randomizes --fingerprint on every
# call; that flag feeds agent-browser's per-session launchHash, so a changed value makes it
# tear down and relaunch the browser — a new window, tabs and logins gone.
export AGENT_BROWSER_ARGS="$(node --input-type=module -e "import('$CB').then(m=>{const s=process.env.AGENT_BROWSER_SESSION||'bbs';let h=0;for(const c of s)h=(h*31+c.charCodeAt(0))>>>0;const fp=10000+(h%90000);process.stdout.write(m.getDefaultStealthArgs().map(a=>a.startsWith('--fingerprint=')?'--fingerprint='+fp:a).join(','))})" 2>/dev/null)"
```

`ensureBinary()` auto-downloads the ~140MB Chromium on first run (cached at `~/.cloakbrowser/`); `2>/dev/null` keeps the download banner out of the captured path. All `agent-browser` commands then work unchanged. Verify with `agent-browser eval "navigator.webdriver"` → `false`. Use only for legitimate access checks, never to evade authorization.

Each Bash call is a fresh shell — these exports don't survive to the next call. Re-running the blocks per call is safe (session name and fingerprint are deterministic, so the launchHash stays constant and the daemon is reused), but to skip the repeated `node` startup cost, write all four exports (`AGENT_BROWSER_SESSION` + the three stealth vars) once to `env.sh` in the scratchpad and `source` it at the top of every later call. Don't stash the command in a var hoping word-splitting expands it: zsh doesn't split unquoted vars, so `$CMD click @e1` fails — call `agent-browser …` directly.

Drop to plain headless (unset the three stealth vars, or `--headless`; keep `AGENT_BROWSER_SESSION`) only when there's no display — CI, a remote box without X — or the caller asks.

## Output

End with:

```text
STATUS: DONE | DONE_WITH_CONCERNS | BLOCKED
SUMMARY: <what was checked, what passed, what failed>
EVIDENCE: <url, screenshot/log paths, or "none">
```
