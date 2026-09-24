#!/usr/bin/env bash
# tests/test_autopilot_v2_readiness.sh — the compiled gate honors the v2
# readiness contract end-to-end: enforced checkpoints deny on missing/stale
# typed evidence and pass on accepted current evidence, never consulting
# legacy verdict prose.
#
# Drives the real `bbs hooks pre-tool-gate` against a real v2 ticket produced
# by `bbs autopilot checkpoint` + `bbs autopilot verification` — no stubs:
# the gate resolves identity and readiness in-process now, so a fake
# bbs-ticket could never intercept it.
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

BBS="$TMP/bbs"
( cd "$ROOT" && go build -o "$BBS" ./cmd/bbs ) || { echo "FAIL: go build" >&2; exit 1; }

export HOME="$TMP/home" BABYSIT_HOME="$TMP/state" BABYSIT_PROJECT_HOME="$TMP/state/projects/repo"
export BABYSIT_TICKET=v2-ticket BABYSIT_SKIP_CHECKPOINT_VALIDATION=1
mkdir -p "$HOME" "$BABYSIT_PROJECT_HOME"
unset BBS_TICKET CODEX_SESSION_ID CODEX_THREAD_ID GROK_SESSION_ID GROK_HOOK_EVENT CLAUDE_CODE_SESSION_ID

REPO="$TMP/repo"; mkdir -p "$REPO"
git -C "$REPO" init -qb main
git -C "$REPO" config user.email t@t; git -C "$REPO" config user.name t
echo x > "$REPO/f"; git -C "$REPO" add f; git -C "$REPO" commit -qm init

TICKET_HOME="$BABYSIT_PROJECT_HOME/tickets/v2-ticket"
mkdir -p "$TICKET_HOME"
# requirement/plan feed the evidence subject digests — the producer rejects
# evidence whose subject lacks them.
echo requirement > "$TICKET_HOME/requirement.md"
echo plan > "$TICKET_HOME/plan.md"

# ticket init seeds index.json, which the verification producer requires;
# the v2 checkpoint then makes readiness enforced.
( cd "$REPO" && "$BBS" ticket init >/dev/null 2>&1 || true )
( cd "$REPO" && "$BBS" autopilot checkpoint --ticket v2-ticket --workflow builder \
    --step run --status in_progress --contract-version 2 >/dev/null ) \
  || { echo "FAIL: checkpoint"; exit 1; }

input() {
  jq -cn --arg command "$1" --arg cwd "$REPO" '{tool_input:{command:$command,workdir:$cwd},cwd:$cwd}'
}

fail=0
check() { # $1=name $2=want-decision-or-empty $3=stdout
  if [ "$2" = "pass" ]; then
    [ -z "$3" ] && { echo "ok $1"; return; }
    echo "FAIL $1: expected silence, got: $3"; fail=1; return
  fi
  if printf '%s' "$3" | jq -e --arg d "$2" '.hookSpecificOutput.permissionDecision == $d' >/dev/null 2>&1; then
    echo "ok $1"
  else
    echo "FAIL $1: wanted $2, got: $3"; fail=1
  fi
}

# Enforced, no accepted evidence → deny naming the missing gates.
out="$(cd "$REPO" && "$BBS" hooks pre-tool-gate <<<"$(input 'gh pr create --fill')")"
check v2-missing-denies deny "$out"
printf '%s' "$out" | grep -q missing || { echo "FAIL v2-missing-denies: reason lacks 'missing': $out"; fail=1; }

# Mint real accepted evidence for both gates via the producer.
for gate in review-pr qa; do
  attempt="$(cd "$REPO" && "$BBS" autopilot verification begin --gate "$gate" \
      --owner test --handle test --transport test --harness test | jq -r '.data.id')" \
    || { echo "FAIL: verification begin $gate"; exit 1; }
  log="$TICKET_HOME/evidence/$gate.log"; mkdir -p "$(dirname "$log")"; echo ok > "$log"
  cat > "$TMP/result.json" <<EOF
{"checks":[{"argv":["go","test","./internal/cmd"],"cwd":"$REPO","exit_code":0,"log_path":"$log"}],
 "unresolved_findings":[],"limitations":[]}
EOF
  ( cd "$REPO" && "$BBS" autopilot verification record --attempt "$attempt" \
      --file "$TMP/result.json" >/dev/null ) \
    || { echo "FAIL: verification record $gate"; exit 1; }
done

# Accepted current evidence → pass, even with a BLOCKED legacy verdict file.
mkdir -p "$TICKET_HOME/verdicts"
printf 'STATUS: BLOCKED\n' > "$TICKET_HOME/verdicts/review-pr.md"
out="$(cd "$REPO" && "$BBS" hooks pre-tool-gate <<<"$(input 'gh pr create --fill')")"
check v2-ready-passes pass "$out"

# A commit after acceptance stales the evidence → deny.
git -C "$REPO" commit -qm 'new revision' --allow-empty
out="$(cd "$REPO" && "$BBS" hooks pre-tool-gate <<<"$(input 'gh pr create --fill')")"
check v2-stale-denies deny "$out"

exit "$fail"
