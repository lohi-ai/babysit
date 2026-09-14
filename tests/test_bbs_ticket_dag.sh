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
#   dag-control-axis       the human control axis is admitted: a cancelled ticket
#                          is settled and unblocks its dependents, a paused one
#                          is not dispatchable and holds its dependents back —
#                          including one whose rung still reads in_progress
#   dag-half-linked-edge   an edge written only on the blocker's `blocks` side is
#                          still traversed, both directions serialize the same
#                          edge set, and an outside node carries no edges of its
#                          own (the one-hop bound lives in the model)
#   dag-self-cycle         a ticket blocking itself is reported as a cycle and
#                          never reads READY
#   dag-ascii-output       the default form carries no byte above ASCII
#   dag-single-root        a second positional is a usage error, not a merge
#   dag-id-guard           an id that is a path is refused for the root and
#                          rendered dangling as a child — never read outside the
#                          project's tickets directory
#   dag-corrupt-record     a root record that exists but does not parse reports
#                          the parse failure, not "no ticket record"; a corrupt
#                          child degrades to NOT FOUND
#   dag-children-scalar    a scalar `children` reads as one child, through the
#                          same parser the dashboard's tab decision uses
#   dag-order-position     a ticket carrying a non-numeric position outranks one
#                          with no position (the id is the fallback key)
#   dag-mermaid-label-escape  free text in `origin.position` cannot close a
#                          mermaid label early
#   dag-usage-spelling     help echoes the invocation: `bbs-ticket` keeps the
#                          hyphen form, plain `bbs` gets the space form

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
    "$2" "$5" "$parent" "${6:-[]}" "${8:-[]}" "${7:-[]}" "$4" > "$dir/index.json"
}

# mkctl writes one record carrying a control override — the exact shape
# `bbs ticket pause` / `cancel` produce, where the lifecycle rung is left
# untouched and only the human axis moves. $4 overrides the rung (default
# planned), which is how a paused `in_progress` ticket is built.
mkctl() { # $1 home, $2 id, $3 control state, [$4 status]
  local dir="$1/projects/demo/tickets/$2"
  mkdir -p "$dir"
  printf '{"id":"%s","status":"%s","parent":null,"children":[],"relations":{"blocks":[],"blocked_by":[]},"control":{"state":"%s","prior_status":"planned","actor":"developer","at":"2026-09-14T00:00:00Z"},"origin":{"position":null}}\n' \
    "$2" "${4:-planned}" "$3" > "$dir/index.json"
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
  echo "$OUT" | head -1 | grep -q "^DAG bs-p1 (demo) - 11 tickets, 4 waves: 3 ready, 4 waiting, 1 running, 2 done, 1 outside, 1 not found$" \
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
  echo "$OUT" | grep -q "bs-c8 -> bs-c8b -> bs-c8 - these tickets block each other" \
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
  # The edge is the whole point; `-` in a ticket id is not mermaid syntax, so
  # the id is escaped rather than dropped.
  echo "$OUT" | grep -q '^  n_bs_x2dc1 --> n_bs_x2dc2$' || { echo "edge missing/unsanitized"; exit 1; }
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

# ─── dag-control-axis ────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets"
  mk "$H" bs-cp "" null decomposed '["bs-cancel","bs-pause","bs-dep","bs-run"]'
  mkctl "$H" bs-cancel cancelled
  mkctl "$H" bs-pause paused
  # Pause leaves the rung alone, so a paused ticket can still read
  # `in_progress` — and no new attempt may dispatch, so RUNNING would be a lie.
  mkctl "$H" bs-run paused in_progress
  mk "$H" bs-dep bs-cp 3 planned '[]' '["bs-cancel"]'
  OUT="$(dag_run "$T" bs-cp)"
  # The tally itself is the fix: a cancelled ticket counts as settled and admits
  # its dependent, so nothing downstream of it stays stuck.
  echo "$OUT" | head -1 | grep -q "^DAG bs-cp (demo) - 4 tickets, 2 waves: 1 ready, 2 waiting, 0 running, 1 done$" \
    || { echo "header: $(echo "$OUT" | head -1)"; exit 1; }
  # `status` is untouched by cancel, so only the control axis can say DONE here.
  echo "$OUT" | grep -q "^  DONE      bs-cancel\s*-\s*planned" \
    || { echo "cancelled ticket not settled: $(echo "$OUT" | grep bs-cancel)"; exit 1; }
  echo "$OUT" | grep -q "^  READY     bs-dep" || { echo "dependent of a cancelled ticket not ready"; exit 1; }
  # A pause is not a completion: it cannot be dispatched, and neither is
  # anything waiting on it.
  echo "$OUT" | grep -q "^  WAITING   bs-pause" || { echo "paused ticket not waiting"; exit 1; }
  echo "$OUT" | grep -q "^  WAITING   bs-run" \
    || { echo "paused in_progress ticket read as running: $(echo "$OUT" | grep bs-run)"; exit 1; }
  exit 0
) && ok "dag-control-axis" || fail "dag-control-axis"

# ─── dag-half-linked-edge ────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets"
  mk "$H" bs-hp "" null decomposed '["bs-ha","bs-hb","bs-hx"]'
  # Written only on the blocker's side: bs-hb's own record declares no blocker.
  mk "$H" bs-ha bs-hp 1 planned '[]' '[]' '["bs-hb"]'
  mk "$H" bs-hb bs-hp 2 planned
  # An outside blocker carrying its own blocker: one hop of context, not two.
  mk "$H" bs-hx bs-hp 3 planned '[]' '["bs-out2"]'
  mk "$H" bs-out2 "" null planned '[]' '["bs-out3"]'
  OUT="$(dag_run "$T" bs-hp)"
  echo "$OUT" | grep -q "WAITING   bs-hb.*<- blocked by bs-ha (planned)" \
    || { echo "one-sided edge dropped: $(echo "$OUT" | grep bs-hb)"; exit 1; }
  [ "$(echo "$OUT" | grep -c 'bs-out3')" -eq 0 ] || { echo "second hop of an outside node leaked into text"; exit 1; }
  if command -v jq >/dev/null 2>&1; then
    OUTJ="$(dag_run "$T" bs-hp --json)"
    echo "$OUTJ" | jq -e '.[0].nodes[] | select(.id == "bs-hb") | .blocked_by == ["bs-ha"]' >/dev/null \
      || { echo "blocked_by not normalized: $(echo "$OUTJ" | jq -c '.[0].nodes[] | select(.id == "bs-hb")')"; exit 1; }
    # Both directions are filled from the same edge set, so the blocker's own
    # `blocks` names the dependent that only wrote its half of the relation.
    echo "$OUTJ" | jq -e '.[0].nodes[] | select(.id == "bs-ha") | .blocks == ["bs-hb"]' >/dev/null \
      || { echo "blocks not normalized: $(echo "$OUTJ" | jq -c '.[0].nodes[] | select(.id == "bs-ha")')"; exit 1; }
    # An outside node is one hop of context: its own relations stay out of the
    # graph, or every renderer gets a second hop to leak.
    echo "$OUTJ" | jq -e '.[0].nodes[] | select(.id == "bs-out2") | (.blocked_by == [] and .blocks == [])' >/dev/null \
      || { echo "outside node kept its own edges: $(echo "$OUTJ" | jq -c '.[0].nodes[] | select(.id == "bs-out2")')"; exit 1; }
  else
    printf '  \033[0;33mskip\033[0m  dag-half-linked-edge json assertions (jq not found)\n'
  fi
  # The outside node is drawn, but its own blocker is not: it is not a graph
  # node, and an edge to it would name a box no wave counted.
  OUTM="$(dag_run "$T" bs-hp --mermaid)"
  echo "$OUTM" | grep -q '^  n_bs_x2dout2' || { echo "outside node not declared in mermaid"; exit 1; }
  [ "$(echo "$OUTM" | grep -c 'bs_out3')" -eq 0 ] || { echo "second-hop edge drawn in mermaid"; exit 1; }
  # A ticket id is not a mermaid id: `-` must not survive as edge syntax, and
  # the mapping must not merge two ids that differ only in the separator.
  echo "$OUTM" | grep -q 'n_bs_x2dhb' || { echo "mermaid id not injective: $(echo "$OUTM" | grep -- '-->' | head -2)"; exit 1; }
  exit 0
) && ok "dag-half-linked-edge" || fail "dag-half-linked-edge"

# ─── dag-self-cycle ──────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets"
  mk "$H" bs-sp "" null decomposed '["bs-self"]'
  mk "$H" bs-self bs-sp 1 planned '[]' '["bs-self"]'
  OUT="$(dag_run "$T" bs-sp)"; RC=$?
  # A ticket that blocks itself can never start. Reporting it as a cycle is the
  # honest reading; the alternative — an edge quietly dropped — prints READY.
  [ "$RC" -eq 0 ] || { echo "self-cycle exited $RC"; exit 1; }
  echo "$OUT" | grep -q "bs-self -> bs-self - these tickets block each other" \
    || { echo "self-cycle not reported: $(echo "$OUT" | tail -4)"; exit 1; }
  echo "$OUT" | grep -q "^  WAITING   bs-self" || { echo "self-blocked ticket read as dispatchable"; exit 1; }
) && ok "dag-self-cycle" || fail "dag-self-cycle"

# ─── dag-ascii-output ────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1)"
  # The default form is read by terminals, log scrapers and byte-oriented
  # scripts with no encoding promise, so it carries no byte above ASCII.
  BAD="$(printf '%s\n' "$OUT" | LC_ALL=C grep -n '[^ -~]' | head -3)"
  [ -z "$BAD" ] || { echo "non-ASCII in default output:"; printf '%s\n' "$BAD"; exit 1; }
) && ok "dag-ascii-output" || fail "dag-ascii-output"

# ─── dag-single-root ─────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  OUT="$(dag_run "$T" bs-p1 bs-c6)"; RC=$?
  [ "$RC" -eq 2 ] || { echo "two roots exited $RC"; exit 1; }
  echo "$OUT" | grep -q "only one <ticket> may be named" || { echo "no single-root message: $OUT"; exit 1; }
) && ok "dag-single-root" || fail "dag-single-root"

# ─── dag-id-guard ────────────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets"
  # A record outside this project that a traversal id would reach: joining the
  # id would render another project's graph under this project's name.
  mkdir -p "$H/projects/other/tickets/bs-far"
  printf '{"id":"bs-far","status":"decomposed","children":["bs-nobody"],"relations":{"blocks":[],"blocked_by":[]}}\n' \
    > "$H/projects/other/tickets/bs-far/index.json"
  OUT="$(dag_run "$T" "../../other/tickets/bs-far")"; RC=$?
  [ "$RC" -eq 2 ] || { echo "traversal root exited $RC"; exit 1; }
  echo "$OUT" | grep -q 'is not a ticket id' || { echo "no id-guard message: $OUT"; exit 1; }
  ! echo "$OUT" | grep -q "bs-nobody" || { echo "traversal root read a foreign record"; exit 1; }
  # The same id as a child is a dangling node, not a read outside the project.
  mk "$H" bs-strict "" null decomposed '["../../other/tickets/bs-far"]'
  OUT="$(dag_run "$T" bs-strict)"
  echo "$OUT" | grep -q "NOT FOUND ../../other/tickets/bs-far" \
    || { echo "path-shaped child not reported dangling: $(echo "$OUT" | tail -3)"; exit 1; }
  ! echo "$OUT" | grep -q "bs-nobody" || { echo "path-shaped child read a foreign record"; exit 1; }
) && ok "dag-id-guard" || fail "dag-id-guard"

# ─── dag-corrupt-record ──────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets/bs-bad"
  printf '{"id":"bs-bad","status":"planned"\n' > "$H/projects/demo/tickets/bs-bad/index.json"
  OUT="$(dag_run "$T" bs-bad)"; RC=$?
  [ "$RC" -eq 2 ] || { echo "corrupt root exited $RC"; exit 1; }
  echo "$OUT" | grep -q "^STATUS: BLOCKED$" || { echo "no BLOCKED line"; exit 1; }
  # The record is on disk, so "has no ticket record" would send the reader
  # hunting for a ticket that exists; the parse error is the honest reason.
  ! echo "$OUT" | grep -q "has no ticket record" || { echo "corrupt root misreported as missing: $OUT"; exit 1; }
  echo "$OUT" | grep -qi "unexpected EOF\|unexpected end of JSON\|invalid\|unmarshal" || { echo "no parse reason: $OUT"; exit 1; }
  # A corrupt *child* degrades to a dangling node rather than failing the graph.
  mk "$H" bs-cp2 "" null decomposed '["bs-bad"]'
  OUT="$(dag_run "$T" bs-cp2)"; RC=$?
  [ "$RC" -eq 0 ] || { echo "corrupt child failed the graph: $RC"; exit 1; }
  echo "$OUT" | grep -q "NOT FOUND bs-bad" || { echo "corrupt child not shown as not found"; exit 1; }
) && ok "dag-corrupt-record" || fail "dag-corrupt-record"

# ─── dag-children-scalar ─────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets/bs-sc" "$H/projects/demo/tickets/bs-one"
  # One field, one parser: a scalar `children` reads as a single child, the same
  # way the dashboard decides whether the ticket gets a DAG tab at all.
  printf '{"id":"bs-sc","status":"decomposed","children":"bs-one","relations":{"blocks":[],"blocked_by":[]}}\n' \
    > "$H/projects/demo/tickets/bs-sc/index.json"
  printf '{"id":"bs-one","status":"planned","parent":"bs-sc","children":[],"relations":{"blocks":[],"blocked_by":[]}}\n' \
    > "$H/projects/demo/tickets/bs-one/index.json"
  OUT="$(dag_run "$T" bs-sc)"
  echo "$OUT" | grep -q "^DAG bs-sc (demo) - 1 ticket, 1 wave" || { echo "header: $(echo "$OUT" | head -1)"; exit 1; }
  echo "$OUT" | grep -q "^  READY     bs-one" || { echo "scalar children not read as one child"; exit 1; }
  # The bare listing asks the same question through the same parser.
  OUT="$(dag_run "$T")"
  echo "$OUT" | grep -q "^DAG bs-sc (demo)" || { echo "scalar-children root missing from bare listing"; exit 1; }
) && ok "dag-children-scalar" || fail "dag-children-scalar"

# ─── dag-order-position ──────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets"
  mk "$H" bs-op "" null decomposed '["bs-pos","bs-nopos"]'
  mkdir -p "$H/projects/demo/tickets/bs-pos" "$H/projects/demo/tickets/bs-nopos"
  # A non-numeric position is storable (`ticket init --position a`), and a
  # ticket that carries one outranks a sibling that carries none — the id is the
  # fallback key, not the empty string.
  printf '{"id":"bs-pos","status":"planned","parent":"bs-op","children":[],"relations":{"blocks":[],"blocked_by":[]},"origin":{"position":"a"}}\n' \
    > "$H/projects/demo/tickets/bs-pos/index.json"
  printf '{"id":"bs-nopos","status":"planned","parent":"bs-op","children":[],"relations":{"blocks":[],"blocked_by":[]},"origin":{"position":null}}\n' \
    > "$H/projects/demo/tickets/bs-nopos/index.json"
  OUT="$(dag_run "$T" bs-op)"
  ORDER="$(echo "$OUT" | sed -n '/^WAVE 0/,/^$/p' | grep -oE 'bs-(pos|nopos)' | paste -sd, -)"
  [ "$ORDER" = "bs-pos,bs-nopos" ] || { echo "position order: $ORDER"; exit 1; }
) && ok "dag-order-position" || fail "dag-order-position"

# ─── dag-mermaid-label-escape ────────────────────────────────────────────────
(
  T="$(mktemp -d)"; H="$T/home"
  mkdir -p "$H/projects/demo/tickets"
  mk "$H" bs-me "" null decomposed '["bs-quote"]'
  mkdir -p "$H/projects/demo/tickets/bs-quote"
  # `origin.position` is free text: an unescaped quote would close the label and
  # turn the rest of the line into diagram syntax.
  printf '{"id":"bs-quote","status":"planned","parent":"bs-me","children":[],"relations":{"blocks":[],"blocked_by":[]},"origin":{"position":"1\\"]"}}\n' \
    > "$H/projects/demo/tickets/bs-quote/index.json"
  OUT="$(dag_run "$T" bs-me --mermaid)"
  echo "$OUT" | grep -q '#quot;' || { echo "label quote not escaped: $(echo "$OUT" | grep x2dquote)"; exit 1; }
  # Each declaration carries exactly two quote characters — the label's own
  # delimiters. An unescaped quote inside the label makes three, which is what
  # ends the label early and lets the rest of the field parse as syntax.
  DECLS="$(echo "$OUT" | grep -c '^    n_')"
  QUOTES="$(echo "$OUT" | grep '^    n_' | grep -o '"' | wc -l | tr -d ' ')"
  [ "$DECLS" -ge 1 ] && [ "$QUOTES" -eq "$((DECLS * 2))" ] \
    || { echo "label escaping broke a declaration: decls=$DECLS quotes=$QUOTES"; exit 1; }
  echo "$OUT" | tail -1 | grep -q '^```$' || { echo "unterminated fence"; exit 1; }
) && ok "dag-mermaid-label-escape" || fail "dag-mermaid-label-escape"

# ─── dag-usage-spelling ──────────────────────────────────────────────────────
(
  T="$(mktemp -d)"; build_dag_fixture "$T/home"
  # help echoes the spelling the caller used: the compat symlink keeps the
  # hyphen form, plain `bbs` gets the space form.
  dag_run "$T" --help | grep -q "^usage: bbs-ticket dag" \
    || { echo "symlink help did not keep the hyphen form: $(dag_run "$T" --help | head -1)"; exit 1; }
  ( cd "$T" && HOME="$T/home" BABYSIT_PROJECT_HOME="$T/home/projects/demo" \
      "$SCRIPT_DIR/bin/bbs" ticket dag --help 2>&1 ) | grep -q "^usage: bbs ticket dag" \
    || { echo "space-form help not retargeted"; exit 1; }
) && ok "dag-usage-spelling" || fail "dag-usage-spelling"

echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[0;32mPASS\033[0m %d scenario(s)\n' "$PASS"
  exit 0
else
  printf '\033[0;31mFAIL\033[0m %d / %d failed:\n' "$FAIL" "$((PASS + FAIL))"
  for n in "${FAIL_NAMES[@]}"; do printf '  - %s\n' "$n"; done
  exit 1
fi
