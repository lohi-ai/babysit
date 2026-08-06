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

### Which coding agent runs the work

`foreman` dispatches workers as coding-agent CLI sessions. Two keys pick which
CLI, in `~/.babysit/config.yaml` above or in a repo's committed
`.babysit/config.yaml` (the global file wins, so a machine can opt out without
editing tracked state):

| Key | Selects | Default |
|-----|---------|---------|
| `worker_agent` | the per-ticket workers | `claude` |
| `foreman_agent` | the foreman session itself | `claude` |

Supported: `claude`, `grok`. `BABYSIT_AGENT=<name>` overrides both for one run;
`bbs foreman spawn --agent <name>` overrides everything.

The two keys do not inherit from each other on purpose. A foreman reviews design
gates and QA evidence from its workers, so moving workers to another agent is a
throughput choice that must not silently relocate that audit — `worker_agent:
grok` alone leaves the foreman on Claude Code.

Adding an agent is a registry entry in `internal/agent`, which owns the binary
name and the flag that suppresses tool approval. Two things it does not own:

- **Skills must be installed for that agent.** Non-Claude CLIs have their own
  plugin stores — for grok, `grok plugin install
  https://github.com/lohi-ai/babysit`. Without it a worker launches fine and then
  cannot resolve `/bbs:autopilot`. `bbs foreman worker-command` preflights the
  binary on PATH; the plugin install is on the operator.
- **A foreman's session is pinned to the agent that minted it.** `spawn` records
  it and reuses it on resume, because a session uuid means nothing to a different
  CLI. Changing `foreman_agent` takes effect on the next *new* foreman, not on a
  resume; `bbs foreman spawn <id> --agent <other>` refuses rather than guess.

**grok needs the directory trusted first.** grok keeps a per-folder trust record
in `~/.grok/trusted_folders.toml`, and it is *separate* from `permission_mode` —
with `always-approve` set globally, a first run in an unlisted directory still
stops on "Do you trust the contents of this directory?", which `--always-approve`
does not answer. An unattended worker parked on that prompt reads as a hung
ticket. Both spawn paths preflight it and refuse with the fix named, so the
failure is loud instead of silent. Grant it once per repo:

```bash
cd <repo> && grok      # answer the trust prompt, then quit
```

Workers launch with `--cwd <repo>`, so this is one decision per repo, not per
worktree.

## Telemetry

Skill runs append JSON Lines to `~/.babysit/analytics/skill-usage.jsonl`. Because babysit runs unattended, telemetry is the *primary* feedback channel — treat it as load-bearing, not decoration. Local-only by default; nothing leaves the machine.

Nothing summarizes that file on demand — the reader is the `/bbs:analytics-review` skill. Dispatch it by hand (`/bbs:analytics-review`) when you want a report; to look at the raw rows, read the JSONL directly.

The Auto-Decision Framework's audit trail is the companion file, `~/.babysit/analytics/decisions.jsonl` — one line per Taste/Mechanical decision. `investigate` can read it for prior-learnings context; grep or `jq` it directly.

## Auto-update

`bbs upgrade check` compares the local `VERSION` against `main` on GitHub, with cache-friendly TTLs (60 min when up-to-date, 12 h when an upgrade is pending). Typical preamble wiring:

```bash
UPD="$(bbs upgrade check 2>/dev/null || true)"
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

## Watching a foreman

A foreman drives its batch from its own terminal, so its worst failure is the
quiet one: the session finishes a thought, prints nothing more, and sits at an
idle prompt while its workers wait for a design gate. Nothing detects that
today — the record's heartbeat is written by the foreman itself, so a foreman
that stopped working also stopped reporting that it stopped.

`bbs foreman watch` is the outside observer. It captures the last N lines of the
foreman's cmux pane on an interval; if those bytes are identical for longer than
`--idle`, it types a nudge into the pane — the same "check status" a human would
send — and if the nudges stop landing, it says so and gives up rather than
poking forever.

```bash
bbs foreman watch                       # every foreman with an open workspace
bbs foreman watch fm-acme               # just this one
bbs foreman watch --idle 300 --nudge "check status and report the board"
bbs foreman watch --once                # one pass, for cron
```

| flag | default | what it does |
|---|---|---|
| `--interval <sec>` | 60 | how often to capture the pane |
| `--idle <sec>` | 600 | unchanged for this long → nudge |
| `--lines <n>` | 40 | how much of the pane forms the fingerprint |
| `--nudge <text>` | `check status` | what gets typed in |
| `--max-nudges <n>` | 3 | budget before it reports `STALLED` and stops |
| `--once` | off | single pass then exit; prints a line per foreman |

It is a foreground loop, not a daemon: it holds no lock, writes only its own
clock under `~/.babysit/watch/`, and nothing in babysit depends on it running.
Output is events only — a foreman that is working produces no output at all.

```text
NUDGED fm-acme after 12m (1/3) — sent "check status"
STALLED fm-acme — 3 nudges, no change in 41m; open "bbs foreman"
GONE fm-acme — workspace "bbs foreman" is closed
```

Two behaviours worth knowing. It selects foremen by **open cmux workspace, not
by liveness** — a foreman wedged long enough to need a nudge is exactly the one
whose heartbeat has gone stale, so selecting on `Live()` would drop every
foreman this exists to catch. And the nudge's own echo in the pane does not
refund the budget: real progress changes the pane on more than one tick, which
is what keeps `--max-nudges` binding on a dead session.

## Health checks

There is no health-check command. `./bin/setup-skills` reports what it linked and warns when `~/.local/bin` is missing from your `PATH`; beyond that, the preamble is the live check — it emits `BBS_DEGRADED` on stderr at the top of every skill run when no working `bbs` is reachable, which is the failure that actually matters.

To verify an install by hand:

```bash
bbs ticket --help     # exit 0 = the binary is present and serving subcommands
bbs config list       # reads ~/.babysit/config.yaml
```
