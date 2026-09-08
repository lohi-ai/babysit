package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/foreman"
)

// orcaScript replaces the stub `orca` that fakeOrcaFor put on PATH. The
// mailbox tests need a richer stub than the terminal tests do, but the same
// isolated PATH, HOME and BABYSIT_HOME around it.
func orcaScript(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(os.Getenv("PATH"), "orca")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeOrcaMailbox is fakeOrcaFor plus the orchestration surface. caps is the
// JSON body of runtime.capabilities, which is what decides whether a batch is
// supervised through the bus or through the pane monitor.
func fakeOrcaMailbox(t *testing.T, caps string) string {
	t.Helper()
	log, _ := fakeOrcaFor(t)
	orcaScript(t, `#!/bin/sh
PATH=/bin:/usr/bin
printf '%s\n' "$*" >> `+log+`
case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":[`+caps+`]}}}' ;;
  open|repo) echo '{"ok":true,"result":{}}' ;;
  orchestration)
    case "$2" in
      run-create)  echo '{"ok":true,"result":{"run":{"id":"run_new"}}}' ;;
      run-current) echo '{"ok":true,"result":{"run":{"id":"run_rec"}}}' ;;
      run-use)     echo '{"ok":true,"result":{}}' ;;
      task-create) echo '{"ok":true,"result":{"task":{"id":"task_1"}}}' ;;
      dispatch)    echo '{"ok":true,"result":{}}' ;;
      reply)       echo '{"ok":true,"result":{}}' ;;
      # The runtime refuses a worker_done that carries no dispatch id
      # ("worker_done requires dispatchId") — a task id alone will not settle a
      # dispatch. Reject it here too, so the doorbell's dependency on
      # dispatch-show stays load-bearing in the suite instead of reading like an
      # optimization someone could drop.
      send)
        case "$*" in
          *"--type worker_done"*)
            case "$*" in
              *--dispatch-id*) ;;
              *) echo "orca orchestration: worker_done requires dispatchId." >&2; exit 1 ;;
            esac ;;
        esac
        echo '{"ok":true,"result":{}}' ;;
      # Real task rows, down to the field names: a task carries the title it was
      # created with and no dispatch id at all.
      task-list)   echo '{"ok":true,"result":{"tasks":[{"id":"task_1","task_title":"bs-x1","display_name":"bs-x1","status":"dispatched"},{"id":"task_other","task_title":"bs-other","status":"dispatched"}]}}' ;;
      dispatch-show)
        # $FAKE_NO_DISPATCH stands in for a task whose dispatch orca cannot
        # produce — released, fenced, or never dispatched at all.
        [ -n "${FAKE_NO_DISPATCH:-}" ] && { echo "orca orchestration: no dispatch for task" >&2; exit 1; }
        echo '{"ok":true,"result":{"dispatch":{"id":"ctx_1","task_id":"task_1","status":"dispatched"}}}' ;;
      check)
        # The worker's own read of the bus. --peek answers "which run am I on?"
        # without marking the coordinator's queued messages read.
        case "$*" in *--peek*) echo '{"ok":true,"result":{"runId":"run_rec","messages":[],"count":0}}'; exit 0 ;; esac
        # The runtime validates --types against a fixed enum and rejects the
        # whole call on an unknown one ("Invalid --types: ask") rather than
        # ignoring it. The fake used to accept anything, which is exactly how a
        # type name that does not exist shipped and took the monitor's only
        # blocking read down with it. Reject here too, so a made-up name fails
        # in the suite instead of against a live batch.
        for t in $(echo "$*" | tr ' ' '\n' | grep -A0 -E '^[a-z_]+,|^[a-z_]+$' | tail -1 | tr ',' ' '); do
          case "$t" in
            worker_done|escalation|question|handoff) ;;
            *) echo "orca orchestration: Invalid --types: $t" >&2; exit 1 ;;
          esac
        done
        # msg_3 is a refusal exactly as the runtime delivers one: still typed
        # worker_done, still claiming "succeeded", with the refusal only in
        # _orcaLifecycleRejection. Captured from a live run.
        echo '{"ok":true,"result":{"deliveryId":"delivery_1","messages":[{"id":"msg_1","type":"worker_done","subject":"bs-x1","from_handle":"term_9","payload":"{\"taskId\":\"task_1\",\"outcome\":\"succeeded\",\"filesModified\":[\"a.go\"]}"},{"id":"msg_2","type":"question","subject":"409 or merge?","from_handle":"dispatch:ctx_1"},{"id":"msg_3","type":"worker_done","subject":"Rejected worker_done: bs-x9 DONE","from_handle":"term_9","payload":"{\"taskId\":\"task_9\",\"outcome\":\"succeeded\",\"_orcaLifecycleRejection\":{\"code\":\"missing_dispatch_id\",\"reason\":\"worker_done requires dispatchId.\"}}"}],"count":3}}' ;;
      *) echo '{"ok":true,"result":{}}' ;;
    esac ;;
  *) echo '{"ok":true,"result":{}}' ;;
esac
`)
	return log
}

func seedForeman(t *testing.T, id, run string) {
	t.Helper()
	if err := foreman.Save(foreman.Record{
		ID: id, Owner: "t", ProjectDir: t.TempDir(),
		WorkspaceTitle: "bbs foreman " + id, Run: run,
		Status: "idle", Heartbeat: foreman.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// The acceptance criterion: a batch is supervised through check --wait when
// the runtime serves the contract, and through the existing pane monitor when
// it does not. Both paths, one decision, one place it is made.
func TestMailboxStatusPicksTheSupervisionPath(t *testing.T) {
	t.Run("contract served", func(t *testing.T) {
		fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
		seedForeman(t, "fm-a", "run_rec")
		out := captureStdout(t, func() {
			if err := foremanMailbox([]string{"status", "fm-a"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, "MAILBOX=on") {
			t.Errorf("want MAILBOX=on, got %q", out)
		}
		if !strings.Contains(out, "RECORDED_RUN=run_rec") {
			t.Errorf("status hid the recorded run, so a rebind mismatch is invisible: %q", out)
		}
	})

	// An older Orca simply does not advertise it. That is not a failure to
	// report — foreman has to stay usable there — so it is an explicit line
	// the skill branches on.
	t.Run("contract absent", func(t *testing.T) {
		fakeOrcaMailbox(t, `"terminal.multiplex.v1"`)
		seedForeman(t, "fm-a", "run_rec")
		out := captureStdout(t, func() {
			if err := foremanMailbox([]string{"status", "fm-a"}); err != nil {
				t.Fatalf("an old Orca must not be an error: %v", err)
			}
		})
		if strings.TrimSpace(out) != "MAILBOX=off" {
			t.Errorf("want a clean MAILBOX=off, got %q", out)
		}
	})
}

// Every verb degrades the same way, so the skill never has to tell "the
// mailbox is unavailable" apart from "the call failed".
func TestEveryMailboxVerbDegradesToOffWithoutTheContract(t *testing.T) {
	fakeOrcaMailbox(t, `"terminal.multiplex.v1"`)
	seedForeman(t, "fm-a", "run_rec")
	for _, args := range [][]string{
		{"bind", "fm-a", "--objective", "batch"},
		{"dispatch", "fm-a", "--ticket", "bs-x1", "--terminal", "term_9"},
		{"wait", "fm-a", "--timeout-ms", "1000"},
		{"reply", "fm-a", "--message", "msg_1", "--body", "use 409"},
	} {
		out := captureStdout(t, func() {
			if err := foremanMailbox(args); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
		})
		if strings.TrimSpace(out) != "MAILBOX=off" {
			t.Errorf("%v printed %q, want MAILBOX=off", args, out)
		}
	}
}

// Coordinator binding is per-terminal: a foreman resuming in a new tab has to
// rebind to its recorded run, and a first bind has to record the fresh one.
func TestMailboxBindCreatesOnceThenRebinds(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "")

	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"bind", "fm-a", "--objective", "3 tickets"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "RUN=run_new") || !strings.Contains(out, "BOUND=created") {
		t.Errorf("first bind = %q", out)
	}
	rec, err := foreman.Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Run != "run_new" {
		t.Fatalf("bind did not record the run: %+v", rec)
	}

	out = captureStdout(t, func() {
		if err := foremanMailbox([]string{"bind", "fm-a"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "BOUND=rebound") {
		t.Errorf("second bind = %q, want a rebind of the recorded run", out)
	}
	if !strings.Contains(readCalls(t, log), "run-use --id run_new") {
		t.Errorf("rebind did not run-use:\n%s", readCalls(t, log))
	}
}

// One blocking read for the whole batch, replacing N per-worker poll loops.
func TestMailboxWaitReturnsTypedMessagesForTheWholeBatch(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "run_rec")

	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"wait", "fm-a", "--timeout-ms", "45000", "--ack"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "COUNT=3") {
		t.Errorf("wait = %q", out)
	}
	// The payload the pane monitor could never have: typed, and not lost when
	// a line scrolls past a 60-line tail.
	if !strings.Contains(out, `"outcome":"succeeded"`) || !strings.Contains(out, `"task":"task_1"`) {
		t.Errorf("worker_done payload not surfaced: %q", out)
	}
	// An `ask` blocks its worker until a reply lands, so it has to be
	// distinguishable from a report at a glance.
	if !strings.Contains(out, `"needs_answer":true`) {
		t.Errorf("ask not flagged as needing an answer: %q", out)
	}

	got := readCalls(t, log)
	for _, want := range []string{"--wait", "--timeout-ms 45000", "--types worker_done,escalation,question,handoff"} {
		if !strings.Contains(got, want) {
			t.Errorf("wait missing %q:\n%s", want, got)
		}
	}
}

// --ack is not a default. Acknowledging before the foreman has acted on the
// batch would drop the FIFO replay that is the whole reason for the bus.
func TestMailboxWaitDoesNotAcknowledgeUnlessAsked(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "run_rec")
	captureStdout(t, func() {
		if err := foremanMailbox([]string{"wait", "fm-a"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(readCalls(t, log), "--ack") {
		t.Errorf("wait acknowledged a batch nobody has acted on yet:\n%s", readCalls(t, log))
	}
}

// The whole justification for adopting the bus is that a batch replays until
// acknowledged, so no event can be lost. `--ack` takes the id of the batch
// being acknowledged: passed bare it is accepted and acknowledges nothing
// (`acknowledged: null, replayed: true`), and a foreman that believed it was
// acking would be handed the same worker_done on every read, forever, acting
// on it every time. Nothing surfaces that — the call succeeds.
//
// The id therefore has to survive between two separate `bbs foreman mailbox
// wait` processes, which is why it lives on the record and not in the reader.
func TestMailboxWaitAcknowledgesThePreviousBatchByIdAcrossProcesses(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "run_rec")

	// First read: nothing to acknowledge yet, and the delivery id is recorded.
	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"wait", "fm-a"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "DELIVERY=delivery_1") {
		t.Errorf("wait did not report the delivery id: %q", out)
	}
	rec, err := foreman.Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Delivery != "delivery_1" {
		t.Fatalf("delivery id not persisted: %q — the next process cannot ack what it never saw", rec.Delivery)
	}

	// Second read, in what is a fresh process in real use: it acknowledges the
	// batch the first one handed out, by id.
	captureStdout(t, func() {
		if err := foremanMailbox([]string{"wait", "fm-a", "--ack"}); err != nil {
			t.Fatal(err)
		}
	})
	if got := readCalls(t, log); !strings.Contains(got, "--ack delivery_1") {
		t.Errorf("--ack carried no delivery id — it acknowledges nothing and the batch replays:\n%s", got)
	}
}

// babysit delivers the prompt itself. --inject would cede that, and with it
// make babysit's tabs reachable by Orca's worker lifecycle.
//
// It also asserts the preamble is gone. Asking for it produced a block the
// skill was told to prepend to a prompt that had already been sent — an
// instruction with no moment left to carry it out, which is why no worker ever
// reported done.
func TestMailboxDispatchNeverInjectsAndAsksForNoPreamble(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "run_rec")

	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"dispatch", "fm-a", "--ticket", "bs-x1", "--terminal", "term_9"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "TASK=task_1") {
		t.Errorf("dispatch = %q", out)
	}
	if strings.Contains(out, "PREAMBLE") {
		t.Errorf("dispatch still prints a preamble nobody can deliver:\n%s", out)
	}
	got := readCalls(t, log)
	if strings.Contains(got, "--return-preamble") {
		t.Errorf("dispatch still asks for the preamble:\n%s", got)
	}
	if strings.Contains(got, "--inject") {
		t.Fatalf("dispatch injected:\n%s", got)
	}
	// A foreman batch is independent tickets by construction — a DAG would
	// model a dependency that does not exist.
	if strings.Contains(got, "--deps") {
		t.Errorf("task-create passed --deps:\n%s", got)
	}
}

func TestMailboxReplyAddressesTheMessageById(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "run_rec")
	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"reply", "fm-a", "--message", "msg_2", "--body", "reject with 409"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "REPLIED=msg_2") {
		t.Errorf("reply = %q", out)
	}
	if !strings.Contains(readCalls(t, log), "reply --id msg_2 --body reject with 409") {
		t.Errorf("reply not addressed by id:\n%s", readCalls(t, log))
	}
}

// The receive half of the dispatch-id defect. Orca does not drop a worker_done
// it refuses — it delivers it, typed worker_done, with the sender's own
// "succeeded" still in the payload and the refusal buried in
// _orcaLifecycleRejection. A foreman reading `outcome` alone therefore counts a
// thrown-away report as a finished worker, and the task it names never settles.
// Rendering it as anything but succeeded is the whole fix.
func TestMailboxWaitNeverRendersARefusedReportAsSuccess(t *testing.T) {
	fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "run_rec")

	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"wait", "fm-a", "--timeout-ms", "45000"}); err != nil {
			t.Fatal(err)
		}
	})
	var refused string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, `"id":"msg_3"`) {
			refused = ln
		}
	}
	if refused == "" {
		t.Fatalf("refused message not surfaced at all:\n%s", out)
	}
	if strings.Contains(refused, `"outcome":"succeeded"`) {
		t.Errorf("a report orca threw away still reads as succeeded:\n%s", refused)
	}
	if !strings.Contains(refused, `"outcome":"rejected"`) {
		t.Errorf("refusal not in the outcome:\n%s", refused)
	}
	if !strings.Contains(refused, "requires dispatchId") {
		t.Errorf("refusal surfaced without orca's reason:\n%s", refused)
	}
	// The ordinary messages must be untouched — `rejected` appears only on the
	// one that carries a refusal, so nothing else has to learn a new field.
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, `"id":"msg_1"`) && strings.Contains(ln, "rejected") {
			t.Errorf("accepted report tagged as rejected:\n%s", ln)
		}
	}
}

// The worker half of the doorbell. Everything it needs is on the bus already —
// no id is delivered to it, which is the whole point: there is no delivery step
// left to forget. The join is the ticket, because that is the task's title.
func TestMailboxDoneFindsItsOwnTaskByTicket(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	t.Setenv("BABYSIT_TICKET", "bs-x1")
	t.Setenv("ORCA_TERMINAL_HANDLE", "term_9")

	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"done", "--status", "DONE", "--body", "Did. Found. Left.", "--files", "a.go,b.go"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "DONE=succeeded") || !strings.Contains(out, "TASK=task_1") {
		t.Errorf("done = %q", out)
	}
	got := readCalls(t, log)
	if !strings.Contains(got, "check --peek") {
		t.Errorf("done marked the coordinator's queued messages read:\n%s", got)
	}
	// The one field a real task row does not have. Matching on it is how this
	// went out as a silent no-op the first time.
	if strings.Contains(got, "dispatch_id") {
		t.Errorf("done joined on a field orca task rows do not carry:\n%s", got)
	}
	for _, want := range []string{"--type worker_done", "--task-id task_1", "--dispatch-id ctx_1", "--outcome succeeded", "--from term_9", "--files-modified a.go,b.go", "--subject bs-x1 DONE"} {
		if !strings.Contains(got, want) {
			t.Errorf("worker_done missing %q:\n%s", want, got)
		}
	}
}

// Reading the dispatch is load-bearing, not a nicety: orca refuses a
// worker_done that carries no dispatch id, so a task whose dispatch cannot be
// read rings no doorbell at all. The run still ends DONE — the ticket is
// finished either way, and the foreman falls back to the pane monitor — but
// the reason is reported rather than swallowed. Pins the dispatch-show call
// against a later "best-effort, drop it" simplification, which would put the
// doorbell back to the silent no-op it shipped as the first time.
func TestMailboxDoneNeedsItsDispatchToRing(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	t.Setenv("FAKE_NO_DISPATCH", "1")
	t.Setenv("BABYSIT_TICKET", "bs-x1")
	t.Setenv("ORCA_TERMINAL_HANDLE", "term_9")

	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"done", "--status", "DONE", "--body", "Did. Found. Left."}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "MAILBOX=off") {
		t.Errorf("a dispatch-less worker_done should degrade, got %q", out)
	}
	// The reason itself goes to stderr; stdout stays the machine-readable line.
	if strings.Contains(out, "DONE=succeeded") {
		t.Errorf("reported a doorbell orca rejected:\n%s", out)
	}
	if !strings.Contains(readCalls(t, log), "dispatch-show --task task_1") {
		t.Errorf("never tried to read the dispatch:\n%s", readCalls(t, log))
	}
}

// A concerned pass is still a pass; a block is a failure. Orca has two values
// and babysit has four, and this is the only place that knows it.
func TestMailboxDoneMapsBabysitStatusOntoOrcaOutcome(t *testing.T) {
	for status, want := range map[string]string{
		"DONE": "succeeded", "DONE_WITH_CONCERNS": "succeeded",
		"BLOCKED": "failed", "NEEDS_CONTEXT": "failed",
	} {
		fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
		t.Setenv("BABYSIT_TICKET", "bs-x1")
		out := captureStdout(t, func() {
			if err := foremanMailbox([]string{"done", "--status", status, "--body", "b"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, "DONE="+want) {
			t.Errorf("%s → %q, want DONE=%s", status, out, want)
		}
	}
}

// Reporting completion must never be the thing that fails a finished ticket.
// A worker nobody dispatched, or an Orca too old to serve the contract, gets
// MAILBOX=off and exit 0 — the verdicts are on disk either way.
func TestMailboxDoneIsANoOpWhenNobodyIsSupervising(t *testing.T) {
	fakeOrcaMailbox(t, `"terminal.multiplex.v1"`) // no orchestration contract
	t.Setenv("BABYSIT_TICKET", "bs-x1")
	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"done", "--status", "DONE", "--body", "b"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "MAILBOX=off") {
		t.Errorf("done on an older Orca = %q", out)
	}
}

// A ticket nobody made a task for is a worker outside the batch, not a fault:
// same MAILBOX=off, same exit 0. Same for a worker with no ticket at all.
func TestMailboxDoneIsANoOpForATicketNobodyDispatched(t *testing.T) {
	fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	for _, ticket := range []string{"bs-nobody", ""} {
		t.Setenv("BABYSIT_TICKET", ticket)
		out := captureStdout(t, func() {
			if err := foremanMailbox([]string{"done", "--status", "DONE", "--body", "b"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, "MAILBOX=off") {
			t.Errorf("done with ticket %q = %q", ticket, out)
		}
	}
}

// A status outside the terminal set is a caller bug. Guessing an outcome would
// ring the doorbell for a run that has not finished.
func TestMailboxDoneRefusesANonTerminalStatus(t *testing.T) {
	fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	err := foremanMailbox([]string{"done", "--status", "IN_PROGRESS", "--body", "b"})
	if err == nil || !strings.Contains(err.Error(), "--status") {
		t.Errorf("err = %v", err)
	}
}
