# Babysit hooks

Babysit keeps two runtime hooks: a release check and session tracking.
The repository's Git pre-commit hook remains separate.

| Hook | When | Purpose |
| --- | --- | --- |
| `pre-tool-gate` | Before shell tools | Check ticket review/QA artifacts before push, PR creation, or PR merge |
| `session-writer` | Session start and after shell tools | Refresh dashboard session identity, throttled to once per minute |

## Installation and agent contracts

Install the companion `bbs` CLI — the hooks are compiled into it as
`bbs hooks pre-tool-gate` and `bbs hooks session-writer`, so neither bash nor
jq is required on any OS. Plugins ship no compiled `bbs`; use
`bin/setup-skills` (POSIX) or `bin/setup-skills.ps1` (PowerShell) from a
checkout, or the documented Homebrew install. The manifest commands invoke
`bbs` by name, so it must be on PATH (setup-skills links it into
`~/.local/bin`).

| Agent | Wiring | Payload / decision |
| --- | --- | --- |
| Claude Code | Plugin auto-discovers `hooks/hooks.json` | snake_case input; native deny/ask JSON |
| Codex | Plugin auto-discovers `hooks/hooks.json` | snake_case input; native deny/ask JSON |
| Grok Build | Plugin loads `hooks/hooks.json` | camelCase input; native deny JSON |
| OMP | Load `hooks/omp.ts` as an extension | `tool_call` / `tool_result` / `session_start`; native block result |

The command manifest calls `bbs hooks <name>` directly — no shell syntax, so
the same manifest works under POSIX shells, PowerShell, and cmd. The
`bin/hooks/*` scripts remain as thin `exec bbs hooks <name>` shims for
installations that still invoke them by path.

For OMP, skills configuration alone does **not** activate these hooks:

```sh
omp --extension "/absolute/path/to/babysit/hooks/omp.ts"
```

For persistent discovery, put a symlink to that file in
`~/.omp/agent/extensions/babysit.ts`. The adapter resolves its real file
location, so the symlink doesn't break script lookup. Avoid loading it twice.
Restart the agent after updating its installed plugin/extension; editing this
checkout does not update an existing marketplace cache.

## Release behavior

Only recognized push / PR-create / PR-merge shell commands pay the cost of
ticket resolution. Other commands return silently. This is a workflow check,
not a shell security sandbox: aliases, scripts, dynamically constructed
commands, and tools outside the host's hook coverage can bypass classification.
Run releases through Babysit's workflows; `bbs ticket land` independently
checks its persisted verdicts.

- No ticket: no objection.
- Ticket identity conflict or unavailable companion binary: deny with a reason.
- Push: a blocked review denies; a missing review requests the review.
- PR creation / merge: check both review and QA, including the QA evidence body.
  Contradictory evidence denies; missing or thin evidence requests the check.
- No objection means **exit 0 with no output**. Never emit `allow` (which
  could override the host's own permission checks) or `defer` (which can
  suspend Claude Code's headless execution).

Claude Code and Codex can present their native `ask` decision. Grok and OMP
return a denial/block with the missing check's reason, so an unattended agent
can perform the check and retry. No custom prompt or automatic approval is
introduced. OMP also blocks process failures, timeouts, and malformed
decision responses. Host-native timeout/error handling otherwise applies;
this is not a universal fail-closed boundary.

The gate uses the payload's working directory (`tool_input.workdir` when
provided, otherwise `cwd`). Shell-internal directory changes and `git -C`
aren't parsed; invoke release tools from the target repository.

## Session tracking

Both snake_case and Grok's camelCase session IDs are supported. Files use
`cc-`, `cx-`, `grok-`, or `omp-` prefixes under
`${BABYSIT_HOME:-$HOME/.babysit}/sessions`. Codex is identified by its
session/thread environment or turn payload; OMP supplies its identity explicitly.
Session IDs containing path separators are rejected. Tracking is advisory;
an unwritable state directory never blocks tool execution.

## Removed audits

- `verify-skill-output`: the Skill tool loads instructions before the model
  writes its verdict; inspecting its output does not validate the final verdict.
- `clean-handoff-check`: a turn ending with working-tree changes is normal for
  directly invoked skills, so the warning incorrectly encouraged commits/stashes.
- `qa-evidence-audit`: duplicated the evidence check at the release boundary.

Their scripts and registrations were removed. Existing telemetry rows remain
available for historical analysis; skill telemetry and persisted verdicts remain.
The gate has one registration instead of five host-specific `if` filters.

## Verification

```sh
go test ./internal/cmd -run 'TestGate|TestRunPreToolGate|TestSessionWriter|TestClassifyGateStage'
bash tests/test_autopilot_v2_readiness.sh
bash tests/test_hook_session_writer.sh
python3 tests/test_hooks_portability.py
bun test tests/test_hooks_omp.test.ts
```

The Go tests cover the gate's deny/ask/pass matrix (including the enforced v2
readiness path) and the session writer; the portability suite executes the
shipped command manifest against real ticket state; the OMP tests exercise
the adapter contract. These tests make no model requests and never execute
the proposed release command.

Contracts checked against [Claude Code hooks](https://code.claude.com/docs/en/hooks),
[Codex hooks](https://developers.openai.com/codex/hooks),
[Grok Build hooks](https://docs.x.ai/build/features/hooks), and
[OMP extensions](https://github.com/can1357/oh-my-pi/blob/main/docs/extensions.md).
