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
