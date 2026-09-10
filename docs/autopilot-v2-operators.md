# Autopilot v2 compatibility

Autopilot v2 is additive. A plugin update can arrive before the local `bbs`
binary does, so skills must capability-check the binary instead of inferring
support from the plugin version.

Run this read-only check from the target repository:

```bash
bbs autopilot snapshot --json | jq -e '.schema_version == 2 and .ok == true'
```

On success, callers may use the returned identity, policy provenance, artifact
index, gate projections, and obligations as a shared state packet. `ok: true`
only says the read succeeded; it is not permission to push, land, or open a
PR. Older or unsupported binaries fall back to the existing text commands and
their historical exit behavior.

The plugin does not ship a compiled `bbs` binary. Install or rebuild it with
`bin/setup-skills --full` from a checkout, or install the published formula.
After updating a plugin, restart/reload the agent session so it reads the
matching skill files. Do not write a policy file, migrate ticket state, or
reinterpret a legacy Markdown verdict merely to make a version check pass.

When support is incomplete, record the binary capability as unavailable and
continue through the legacy path. This is a compatibility limitation, not a
successful v2 readiness result.

## Release and dashboard readers

For an opted-in run, the release decision is the typed evaluator, not a
successful snapshot read or a `STATUS: DONE` Markdown verdict:

```bash
bbs ticket readiness --json --action pr \
  | jq -e '.schema_version == 2 and .ok == true and .data.enforced == true and .data.ready == true'
```

`ready:false` still exits zero because it is a valid observation; report its
`data.reason_codes` and re-run the stale or missing gate. Legacy runs report
`enforced:false` and keep their existing verdict-based behavior. `pre-tool-gate`
and `ticket land` use this same evaluator when enforcement is active.

The served dashboard reads this evaluator for a ticket's PR readiness and
shows its reason codes. A `--snapshot` dashboard is intentionally read-only
and labels live readiness unavailable: it cannot establish current worktree
freshness. Skill-event rows display provider usage as `unavailable` unless
the provider supplied observed token counts; no usage is inferred.

Version-2 checkpoints are opt-in. Migrate an eligible ticket deliberately,
then retain a compatible binary for all later writers:

```bash
bbs autopilot checkpoint --ticket <id> --workflow <workflow> --step <step> \
  --status in_progress --contract-version=2
```

The first migration creates `checkpoint.v1.backup.json`; do not delete it or
downgrade a v2 checkpoint to bypass a readiness failure. Context cache entries
under `tickets/<id>/cache/context` are disposable read projections, never
evidence or release authorization.
