#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOK="$ROOT/bin/hooks/pre-tool-gate"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/plugin/bin" "$TMP/work"

cat >"$TMP/plugin/bin/bbs-ticket" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  resolve) echo v2-ticket ;;
  readiness)
    case "${V2_CASE:-stale}" in
      stale) printf '%s\n' '{"schema_version":2,"ok":true,"data":{"enforced":true,"ready":false,"reason_codes":["qa:stale_tree_digest"]}}' ;;
      ready) printf '%s\n' '{"schema_version":2,"ok":true,"data":{"enforced":true,"ready":true,"reason_codes":[]}}' ;;
      legacy) printf '%s\n' '{"schema_version":2,"ok":true,"data":{"enforced":false,"ready":false,"reason_codes":["qa:legacy_verdict_blocked"]}}' ;;
    esac
    ;;
  verdict-status) echo "${LEGACY_STATUS:-BLOCKED}" ;;
  qa-evidence) echo ok ;;
esac
SH
chmod +x "$TMP/plugin/bin/bbs-ticket"

input() {
  jq -cn --arg command "$1" --arg cwd "$TMP/work" '{tool_input:{command:$command,workdir:$cwd}}'
}

fail=0
out="$(V2_CASE=stale LEGACY_STATUS=DONE CLAUDE_PLUGIN_ROOT="$TMP/plugin" "$HOOK" <<<"$(input 'gh pr create --fill')")"
if printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision == "deny" and (.hookSpecificOutput.permissionDecisionReason | contains("stale_tree_digest"))' >/dev/null; then
  echo "ok v2-stale-denies"
else
  echo "FAIL v2-stale-denies: $out"
  fail=1
fi

out="$(V2_CASE=ready LEGACY_STATUS=BLOCKED CLAUDE_PLUGIN_ROOT="$TMP/plugin" "$HOOK" <<<"$(input 'gh pr create --fill')")"
if [ -z "$out" ]; then
  echo "ok v2-ready-is-authoritative"
else
  echo "FAIL v2-ready-is-authoritative: $out"
  fail=1
fi

out="$(V2_CASE=legacy LEGACY_STATUS=BLOCKED CLAUDE_PLUGIN_ROOT="$TMP/plugin" "$HOOK" <<<"$(input 'git push origin HEAD')")"
if printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision == "deny" and (.hookSpecificOutput.permissionDecisionReason | contains("review-pr verdict is BLOCKED"))' >/dev/null; then
  echo "ok legacy-falls-through"
else
  echo "FAIL legacy-falls-through: $out"
  fail=1
fi

exit "$fail"
