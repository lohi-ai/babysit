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
      dispatch)    echo '{"ok":true,"result":{"preamble":"PRE"}}' ;;
      reply)       echo '{"ok":true,"result":{}}' ;;
      check)
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
        echo '{"ok":true,"result":{"deliveryId":"delivery_1","messages":[{"id":"msg_1","type":"worker_done","subject":"bs-x1","from_handle":"term_9","payload":"{\"taskId\":\"task_1\",\"outcome\":\"succeeded\",\"filesModified\":[\"a.go\"]}"},{"id":"msg_2","type":"question","subject":"409 or merge?","from_handle":"dispatch:ctx_1"}],"count":2}}' ;;
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
	if !strings.Contains(out, "COUNT=2") {
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
func TestMailboxDispatchReturnsThePreambleAndNeverInjects(t *testing.T) {
	log := fakeOrcaMailbox(t, `"orchestration.contract.v1"`)
	seedForeman(t, "fm-a", "run_rec")

	out := captureStdout(t, func() {
		if err := foremanMailbox([]string{"dispatch", "fm-a", "--ticket", "bs-x1", "--terminal", "term_9"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "TASK=task_1") || !strings.Contains(out, "PRE") {
		t.Errorf("dispatch = %q", out)
	}
	got := readCalls(t, log)
	if !strings.Contains(got, "--return-preamble") {
		t.Errorf("dispatch did not ask for the preamble:\n%s", got)
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
