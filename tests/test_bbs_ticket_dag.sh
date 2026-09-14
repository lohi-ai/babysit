#!/usr/bin/env bash
# tests/test_bbs_ticket_dag.sh — coverage for `bbs ticket dag`.
#
# dag = read-only project graph: a parent's declared `children`, layered into
# waves by `relations.blocked_by`/`blocks`, with the blockers that live outside
# the subtree resolved one hop and dangling child ids shown rather than dropped.
#
# Scenarios:
#   dag-waves-and-states   the fixture graph layers into 4 waves with the right
#                          ready/waiting/running/done tally, and a settled
#                          blocker makes its dependent READY, not waiting
#   dag-outside-and-gone   a blocker outside the subtree gets its own lane with
#                          its real status; a declared child with no record is
#                          NOT FOUND and its dependent stays WAITING
#   dag-cycle              two tickets blocking each other are reported as a
#                          cycle, the model terminates, and exit stays 0
#   dag-nested-parent      a child that is itself a parent contributes its own
#                          children as members and is labelled `N nested`
#   dag-verdicts           qa/review-pr statuses ride on the rows that can still
#                          move, and a settled row stays compact (no verdicts)
#   dag-mermaid            a fenced flowchart with one subgraph per wave, a
#                          sanitized edge, and classDefs
#   dag-json               the model round-trips: node count, wave ids and each
#                          node's own `wave` field agree
#   dag-readonly           nothing under the project home changes
#   dag-bare-and-leaves    bare lists every DAG; an explicit leaf ticket says so;
#                          an unknown id is a BLOCKED exit 2

set -u
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BBS_TICKET_BIN="$SCRIPT_DIR/bin/bbs-ticket"

PASS=0; FAIL=0; FAIL_NAMES=()
ok()   { PASS=$((PASS + 1)); printf '  \033[0;32mok\033[0m  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAIL_NAMES+=("$1"); printf '  \033[0;31mFAIL\033[0m  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# The fixture is the shape the real graphs are: layered dependencies, a nested
# parent, a blocker from outside the subtree, a child id with no record, and a
# cycle. One parent (bs-p1) with nine children:
#
#   bs-c1 done ──► bs-c2 planned ──► bs-c3 planned
#   bs-c4 in_progress                       (no deps)
#   bs-c5 planned ◄── bs-out in_progress    (outside the subtree)
#   bs-c6 planned ── { bs-g1 planned, bs-g2 done }   (nested parent)
#   bs-c7 declared, no index.json           (dangling)
#   bs-c8 ⇄ bs-c8b planned                  (cycle)
build_dag_fixture() { # $1 = home dir
  local H="$1"
  mkdir -p "$H/projects/demo/tickets"
  local KIDS='["bs-c1","bs-c2","bs-c3","bs-c4","bs-c5","bs-c6","bs-c7","bs-c8","bs-c8b"]'
  mk "$H" bs-p1  ""    null decomposed  "$KIDS"
  mk "$H" bs-c1  bs-p1 1    done        '[]'
  mk "$H" bs-c2  bs-p1 2    planned     '[]' '["bs-c1"]'
  mk "$H" bs-c3  bs-p1 3    planned     '[]' '["bs-c2"]'
  mk "$H" bs-c4  bs-p1 4    in_progress '[]'
  mk "$H" bs-c5  bs-p1 5    planned     '[]' '["bs-out"]'
  mk "$H" bs-c6  bs-p1 6    planned     '["bs-g1","bs-g2"]'
  mk "$H" bs-c8  bs-p1 8    planned     '[]' '["bs-c8b"]'
  mk "$H" bs-c8b bs-p1 9    planned     '[]' '["bs-c8"]'
  mk "$H" bs-g1  bs-c6 1    planned     '[]'
  mk "$H" bs-g2  bs-c6 2    done        '[]'
  # bs-out is a real ticket outside the subtree: bs-c5 waits on it, and its real
  # status is what decides whether bs-c5 is merely waiting or actually ready.
  # Its `blocks` names bs-c5 from the other side, the way real records do.
  mk "$H" bs-out bs-p1 null in_progress '[]' '[]' '["bs-c5"]'

  local TICKETS="$H/projects/demo/tickets"
  mkdir -p "$TICKETS/bs-c1/verdicts" "$TICKETS/bs-c6/verdicts"
  printf 'STATUS: DONE\n' > "$TICKETS/bs-c1/verdicts/qa.md"
  printf 'STATUS: DONE\n' > "$TICKETS/bs-c1/verdicts/review-pr.md"
  # A DONE verdict on a ticket that can still move is what proves the verdict
  # columns are read at all — a settled row deliberately carries none.
  printf 'STATUS: DONE\n' > "$TICKETS/bs-c6/verdicts/qa.md"
}

# mk writes one index.json: identity, status, fan-out, and the varying relations
# tail. `bs-c7` is deliberately never written — it is the dangling child.
mk() { # $1 home, $2 id, $3 parent, $4 position, $5 status, $6 children, $7 blocked_by, $8 blocks
  local dir="$1/projects/demo/tickets/$2"
  mkdir -p "$dir"
  local parent=null
  [ -n "$3" ] && parent="\"$3\""
  printf '{"id":"%s","status":"%s","parent":%s,"children":%s,"relations":{"blocks":%s,"blocked_by":%s},"origin":{"position":%s}}\n' \
    "$2" "$5" "$parent" "$6" "${8:-[]}" "${7:-[]}" "$4" > "$dir/index.json"
}

# dag_run sets up an isolated HOME + project and runs the subcommand. It must
# run outside a git repository: slug resolution in one would override
# BABYSIT_PROJECT_HOME with the repo it found.
dag_run() { # $1 fixture root, rest = dag args
  local root="$1"; shift
  ( cd "$root" && HOME="$root/home" BABYSIT_PROJECT_HOME="$root/home/projects/demo" \
      "$BBS_TICKET_BIN" dag "$@" 2>&1 )
}

# ─── dag-waves-and-states ────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1)"
  echo "$OUT" | head -1 | grep -q "^DAG bs-p1 (demo) — 11 tickets, 4 waves: 3 ready, 4 waiting, 1 running, 2 done, 1 outside, 1 not found$" \
    || { echo "header: $(echo "$OUT" | head -1)"; exit 1; }
  # A settled blocker admits its dependent into wave 1 and reads READY.
  echo "$OUT" | grep -q "^WAVE 1  (1)$" || { echo "no wave 1"; exit 1; }
  echo "$OUT" | sed -n '/^WAVE 1/,/^$/p' | grep -q "READY     bs-c2" \
    || { echo "bs-c2 not READY in wave 1"; exit 1; }
  # Pulled two layers down: bs-c3 waits on bs-c2, which has not started.
  echo "$OUT" | sed -n '/^WAVE 2/,/^$/p' | grep -q "WAITING   bs-c3.*blocked by bs-c2 (planned)" \
    || { echo "bs-c3 not waiting on bs-c2"; exit 1; }
  # A done ticket's row is compact: no verdict column, no blocker list.
  ! echo "$OUT" | grep "^  DONE      bs-c1" | grep -q "qa:" \
    || { echo "settled row is not compact"; exit 1; }
) && ok "dag-waves-and-states" || fail "dag-waves-and-states"

# ─── dag-outside-and-gone ────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1)"
  echo "$OUT" | sed -n '/^OUTSIDE THIS SUBTREE/,$p' | grep -q "bs-out.*status in_progress" \
    || { echo "outside blocker missing"; exit 1; }
  # The outside ticket must NOT also appear as its own in-wave card (it is
  # legitimately named inside bs-c5's blocker list, which is not a card).
  echo "$OUT" | sed -n '/^WAVE/,/^OUTSIDE/p' | grep -qE '^  [A-Z_ ]{9} bs-out ' \
    && { echo "outside blocker rendered as a wave card"; exit 1; }
  # A foreign blocker that is not done keeps bs-c5 waiting.
  echo "$OUT" | grep -q "WAITING   bs-c5.*blocked by bs-out (in_progress)" \
    || { echo "bs-c5 not waiting on bs-out"; exit 1; }
  # A declared child with no record shows as NOT FOUND rather than vanishing.
  echo "$OUT" | grep -q "NOT FOUND bs-c7" || { echo "dangling child missing"; exit 1; }
  exit 0
) && ok "dag-outside-and-gone" || fail "dag-outside-and-gone"

# ─── dag-cycle ───────────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1)"; RC=$?
  [ "$RC" -eq 0 ] || { echo "cycle exited $RC"; exit 1; }
  echo "$OUT" | grep -q "^CYCLE$" || { echo "no CYCLE section"; exit 1; }
  echo "$OUT" | grep -q "bs-c8 → bs-c8b → bs-c8 — these tickets block each other" \
    || { echo "cycle path missing"; exit 1; }
  # Both members of the cycle land in the same final lane; neither is dropped.
  echo "$OUT" | sed -n '/^WAVE 3/,/^$/p' | grep -q "bs-c8" || { echo "bs-c8 unplaced"; exit 1; }
  echo "$OUT" | sed -n '/^WAVE 3/,/^$/p' | grep -q "bs-c8b" || { echo "bs-c8b unplaced"; exit 1; }
) && ok "dag-cycle" || fail "dag-cycle"

# ─── dag-nested-parent ───────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1)"
  # A child that is itself a parent contributes its children as full members.
  echo "$OUT" | grep -q "READY     bs-g1" || { echo "bs-g1 not a member"; exit 1; }
  echo "$OUT" | grep -q "DONE      bs-g2" || { echo "bs-g2 not a member"; exit 1; }
  echo "$OUT" | grep -q "READY     bs-c6.*(2 nested)" || { echo "nested label missing"; exit 1; }
) && ok "dag-nested-parent" || fail "dag-nested-parent"

# ─── dag-verdicts ────────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1)"
  echo "$OUT" | grep -q "READY     bs-c6.*qa:DONE review-pr:none" \
    || { echo "verdicts not on a movable row: $(echo "$OUT" | grep bs-c6)"; exit 1; }
  echo "$OUT" | grep -q "RUNNING   bs-c4.*qa:none" || { echo "missing verdict defaults"; exit 1; }
) && ok "dag-verdicts" || fail "dag-verdicts"

# ─── dag-mermaid ─────────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1 --mermaid)"
  echo "$OUT" | head -1 | grep -q '^```mermaid$' || { echo "no fence"; exit 1; }
  echo "$OUT" | sed -n '2p' | grep -q '^flowchart LR$' || { echo "not flowchart LR"; exit 1; }
  [ "$(echo "$OUT" | grep -c '^  subgraph w.*"WAVE ')" -eq 4 ] || { echo "expected 4 wave subgraphs"; exit 1; }
  echo "$OUT" | grep -q '^    direction TB$' || { echo "waves are not columns"; exit 1; }
  # The edge is the whole point; a ticket id carrying `-` must be sanitized.
  echo "$OUT" | grep -q '^  n_bs_c1 --> n_bs_c2$' || { echo "edge missing/unsanitized"; exit 1; }
  echo "$OUT" | grep -q 'classDef ready' || { echo "no classDefs"; exit 1; }
  echo "$OUT" | tail -1 | grep -q '^```$' || { echo "unterminated fence"; exit 1; }
) && ok "dag-mermaid" || fail "dag-mermaid"

# ─── dag-json ────────────────────────────────────────────────────────────────
if command -v jq >/dev/null 2>&1; then
  (
    T="$(mktemp -d)"; build_dag_fixture "$T/home"
    OUT="$(dag_run "$T" bs-p1 --json)"
    echo "$OUT" | jq -e 'length == 1' >/dev/null || { echo "not one graph"; exit 1; }
    # counts.nodes counts the subtree (11), and the wave ids are exactly those
    # nodes with a non-negative wave, in the order the model assigned.
    echo "$OUT" | jq -e '.[0].counts | .nodes == 11 and .waves == 4 and .ready == 3 and .waiting == 4 and .running == 1 and .done == 2 and .external == 1 and .dangling == 1' >/dev/null \
      || { echo "counts wrong: $(echo "$OUT" | jq -c '.[0].counts')"; exit 1; }
    echo "$OUT" | jq -e '[.[0].waves[] | length] | add == 11' >/dev/null || { echo "wave ids != node count"; exit 1; }
    echo "$OUT" | jq -e '.[0] | ([.nodes[] | select(.external | not) | .id] | sort) == ([.waves[][]] | sort)' >/dev/null \
      || { echo "waves do not partition the in-subtree nodes"; exit 1; }
    # Each node's own wave field indexes the lane it is actually listed in.
    echo "$OUT" | jq -e '.[0] | .waves as $w | all(.nodes[] | select(.external | not); . as $n | ($w[$n.wave] | index($n.id)) != null)' >/dev/null \
      || { echo "wave field disagrees with lanes"; exit 1; }
    # Cycles are reported, not silently flattened.
    echo "$OUT" | jq -e '.[0].cycles | length == 1 and (.[0] | sort) == ["bs-c8","bs-c8b"]' >/dev/null \
      || { echo "cycles missing: $(echo "$OUT" | jq -c '.[0].cycles')"; exit 1; }
    echo "$OUT" | jq -e '.[0].nodes[] | select(.id == "bs-c7") | .dangling and .state == "not_found"' >/dev/null \
      || { echo "dangling node wrong"; exit 1; }
  ) && ok "dag-json" || fail "dag-json"
else
  printf '  \033[0;33mskip\033[0m  dag-json (jq not found)\n'
fi

# ─── dag-readonly ────────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  BEFORE="$(cd "$T/home" && find . -type f | LC_ALL=C sort | xargs shasum)"
  dag_run "$T" bs-p1 >/dev/null
  dag_run "$T" bs-p1 --mermaid >/dev/null
  dag_run "$T" bs-p1 --json >/dev/null
  dag_run "$T" >/dev/null
  AFTER="$(cd "$T/home" && find . -type f | LC_ALL=C sort | xargs shasum)"
  [ "$BEFORE" = "$AFTER" ] || { echo "dag mutated the project home"; diff <(printf '%s\n' "$BEFORE") <(printf '%s\n' "$AFTER"); exit 1; }
) && ok "dag-readonly" || fail "dag-readonly"

# ─── dag-bare-and-leaves ─────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  # Bare: every ticket that declares children roots a graph (bs-p1 and the
  # nested parent bs-c6), so both are printed.
  OUT="$(dag_run "$T")"
  echo "$OUT" | grep -q "^DAG bs-p1 (demo)" || { echo "bs-p1 missing from bare listing"; exit 1; }
  echo "$OUT" | grep -q "^DAG bs-c6 (demo)" || { echo "bs-c6 missing from bare listing"; exit 1; }
  # An explicit leaf ticket says so rather than printing an empty graph.
  dag_run "$T" bs-c1 | grep -q "(no children declared)" || { echo "leaf not reported"; exit 1; }
  # An id with no record is a BLOCKED exit 2, like every other unknown ticket.
  OUT="$(dag_run "$T" bs-nope)"; RC=$?
  [ "$RC" -eq 2 ] || { echo "unknown ticket exited $RC"; exit 1; }
  echo "$OUT" | grep -q "^STATUS: BLOCKED$" || { echo "no BLOCKED line"; exit 1; }
  # A project with nothing decomposed succeeds and says why it printed nothing.
  mkdir -p "$T/empty/projects/demo/tickets/bs-solo"
  printf '{"id":"bs-solo","status":"triage","children":[]}\n' > "$T/empty/projects/demo/tickets/bs-solo/index.json"
  OUT="$(cd "$T/empty" && HOME="$T/empty/home" BABYSIT_PROJECT_HOME="$T/empty/projects/demo" "$BBS_TICKET_BIN" dag 2>&1)"; RC=$?
  [ "$RC" -eq 0 ] || { echo "empty project exited $RC"; exit 1; }
  echo "$OUT" | grep -q "no decomposed tickets" || { echo "no empty-project message: $OUT"; exit 1; }
) && ok "dag-bare-and-leaves" || fail "dag-bare-and-leaves"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
