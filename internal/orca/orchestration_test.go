package orca

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// orchestrationBody is a stub Orca that serves the mailbox contract. It records
// every call (fakeOrca's log) so a test can assert on the flags, which is the
// half that matters most here: --inject must never appear.
func orchestrationBody(caps string) string {
	return `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"state":"ready","capabilities":[` + caps + `]}}}' ;;
  open) echo '{"ok":true,"result":{}}' ;;
  orchestration)
    case "$2" in
      run-create)  echo '{"ok":true,"result":{"run":{"id":"run_abc123"}}}' ;;
      run-current) echo '{"ok":true,"result":{"run":{"id":"run_abc123"}}}' ;;
      run-use)     echo '{"ok":true,"result":{}}' ;;
      task-create) echo '{"ok":true,"result":{"task":{"id":"task_def456"}}}' ;;
      dispatch)    echo '{"ok":true,"result":{"preamble":"LIFECYCLE PREAMBLE"}}' ;;
      reply)       echo '{"ok":true,"result":{}}' ;;
      ask)         echo '{"ok":true,"result":{"messageId":"msg_q1","answer":"option B"}}' ;;
      check)
        echo '{"ok":true,"result":{"runId":"run_abc123","deliveryId":"delivery_1","messages":[{"id":"msg_1","type":"worker_done","subject":"bs-x1 done","body":"QA passed","from_handle":"term_9","sequence":88,"payload":"{\"taskId\":\"task_def456\",\"dispatchId\":\"ctx_9\",\"outcome\":\"succeeded\",\"filesModified\":[\"a.go\",\"b.go\"]}"}],"count":1}}' ;;
      *) echo '{"ok":true,"result":{}}' ;;
    esac ;;
esac`
}

func withOrchestration(t *testing.T) (*Client, string) {
	t.Helper()
	log := fakeOrca(t, orchestrationBody(`"orchestration.contract.v1","terminal.multiplex.v1"`))
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	return c, log
}

func calls(t *testing.T, log string) string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		return ""
	}
	return string(b)
}

func TestCapabilityGateReadsTheAdvertisedList(t *testing.T) {
	c, _ := withOrchestration(t)
	if !c.Orchestration() {
		t.Error("runtime advertised orchestration.contract.v1 but the gate says no")
	}
	if !c.Supports("terminal.multiplex.v1") {
		t.Error("Supports missed a capability that was advertised")
	}
	if c.Supports("nope.v1") {
		t.Error("Supports invented a capability")
	}
}

// foreman must stay usable on an older Orca. Absent capability is not a
// failure to report — it is the signal to take the pane-monitor path, so every
// entry point returns one distinguishable error rather than a parse failure.
func TestWithoutTheCapabilityEveryMailboxCallRefusesDistinguishably(t *testing.T) {
	fakeOrca(t, orchestrationBody(`"terminal.multiplex.v1"`))
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	if c.Orchestration() {
		t.Fatal("gate passed on a runtime that does not advertise the contract")
	}

	_, err = c.RunCreate("x")
	assertNoOrchestration(t, "RunCreate", err)
	assertNoOrchestration(t, "RunUse", c.RunUse("run_1"))
	_, err = c.RunCurrent()
	assertNoOrchestration(t, "RunCurrent", err)
	_, err = c.TaskCreate("run_1", "t", "spec")
	assertNoOrchestration(t, "TaskCreate", err)
	_, err = c.Dispatch("run_1", "task_1", "term_1")
	assertNoOrchestration(t, "Dispatch", err)
	_, err = c.Check(CheckOpts{}) //nolint:errcheck // the error IS the assertion
	assertNoOrchestration(t, "Check", err)
	assertNoOrchestration(t, "Reply", c.Reply("run_1", "msg_1", "yes"))
	_, err = c.Ask(AskOpts{Question: "q"})
	assertNoOrchestration(t, "Ask", err)
}

func assertNoOrchestration(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, ErrNoOrchestration) {
		t.Errorf("%s: want ErrNoOrchestration so the caller can fall back, got %v", name, err)
	}
}

// The single most load-bearing flag assertion in this file. An injecting
// dispatch hands Orca's lifecycle the job of delivering the prompt — the exact
// half babysit keeps — and makes babysit's tabs reachable by worker-stop /
// worker-release.
func TestDispatchReturnsThePreambleAndNeverInjects(t *testing.T) {
	c, log := withOrchestration(t)
	preamble, err := c.Dispatch("run_abc123", "task_def456", "term_9")
	if err != nil {
		t.Fatal(err)
	}
	if preamble != "LIFECYCLE PREAMBLE" {
		t.Errorf("preamble = %q", preamble)
	}
	got := calls(t, log)
	if !strings.Contains(got, "--return-preamble") {
		t.Errorf("dispatch did not ask for the preamble:\n%s", got)
	}
	if strings.Contains(got, "--inject") {
		t.Fatalf("dispatch injected — babysit delivers the prompt itself:\n%s", got)
	}
}

func TestRunLifecycleBindsFreshAndRebindsOnResume(t *testing.T) {
	c, log := withOrchestration(t)
	id, err := c.RunCreate("batch of 3 tickets")
	if err != nil {
		t.Fatal(err)
	}
	if id != "run_abc123" {
		t.Errorf("run id = %q", id)
	}
	// A foreman resuming in a NEW terminal has to rebind: coordinator binding
	// is per-terminal, so without this its mailbox reads as somebody else's.
	if err := c.RunUse(id); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	if !strings.Contains(got, "run-use --id run_abc123") {
		t.Errorf("resume did not rebind the run:\n%s", got)
	}
	// Never adopt a legacy run: the contract's legacy-authority surface has
	// migration rules babysit has no reason to inherit.
	if strings.Contains(got, "--takeover-legacy") {
		t.Errorf("run-use adopted a legacy run:\n%s", got)
	}
}

// The typed replacement for grepping an ANSI tail. Payload arrives as a JSON
// string inside the envelope, so it is decoded twice — a caller must never
// have to do that itself.
func TestCheckReturnsTypedMessagesWithTheirPayloadFlattened(t *testing.T) {
	c, log := withOrchestration(t)
	batch, err := c.Check(CheckOpts{
		Run:    "run_abc123",
		Types:  []string{"worker_done", "escalation", "question"},
		WaitMS: 60000,
		Ack:    "delivery_prev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(batch.Messages))
	}
	// The id that acknowledges this batch has to come back with it: acking is
	// a separate later call, and an id dropped in between is a batch that
	// replays for ever.
	if batch.DeliveryID != "delivery_1" {
		t.Errorf("deliveryId = %q, want delivery_1", batch.DeliveryID)
	}
	m := batch.Messages[0]
	if !m.Done() {
		t.Errorf("worker_done did not read as done: %+v", m)
	}
	if m.NeedsAnswer() {
		t.Error("worker_done must not read as a question")
	}
	if m.TaskID != "task_def456" || m.DispatchID != "ctx_9" || m.Outcome != "succeeded" {
		t.Errorf("payload not flattened: %+v", m)
	}
	if strings.Join(m.FilesModified, ",") != "a.go,b.go" {
		t.Errorf("filesModified = %v", m.FilesModified)
	}
	if m.Sequence != 88 {
		t.Errorf("sequence = %d", m.Sequence)
	}

	got := calls(t, log)
	for _, want := range []string{
		"--types worker_done,escalation,question",
		"--wait",
		"--timeout-ms 60000",
		// FIFO replays until acknowledged — that is the property the 60-line
		// pane tail did not have. `--ack` takes the id of the batch being
		// acknowledged; a bare `--ack` is accepted and acknowledges nothing,
		// so the same messages come back on every read.
		"--ack delivery_prev",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("check missing %q:\n%s", want, got)
		}
	}
}

// A wait that expires with nothing is the normal quiet tick, not an error.
func TestCheckTreatsAnEmptyWaitAsQuietRatherThanFailure(t *testing.T) {
	fakeOrca(t, `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
  orchestration) echo '{"ok":true,"result":{"runId":"run_abc123","messages":[],"count":0}}' ;;
esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := c.Check(CheckOpts{WaitMS: 1000})
	if err != nil {
		t.Fatalf("an expired wait must not be an error: %v", err)
	}
	if len(batch.Messages) != 0 {
		t.Errorf("got %d messages from an empty mailbox", len(batch.Messages))
	}
}

// Reply is addressed by message id. The pane path answered questions by typing
// words into a composer and hoping the TUI submitted them; this removes that
// failure class rather than mitigating it.
func TestReplyAddressesTheMessageById(t *testing.T) {
	c, log := withOrchestration(t)
	if err := c.Reply("run_abc123", "msg_1", "use option B"); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	if !strings.Contains(got, "reply --id msg_1 --body use option B") {
		t.Errorf("reply not addressed by id:\n%s", got)
	}
}

func TestAskCarriesOptionsAndKeepsTheIdToResumeBy(t *testing.T) {
	c, log := withOrchestration(t)
	res, err := c.Ask(AskOpts{
		Run:       "run_abc123",
		Question:  "reject with 409, merge and sum, or keep newest?",
		Options:   []string{"409", "merge", "newest"},
		TimeoutMS: 300000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "option B" || res.Pending {
		t.Errorf("ask result = %+v", res)
	}
	// The id is what makes a timeout resumable. Re-asking would open a second
	// thread on the same question, which is the failure the guide names.
	if res.MessageID != "msg_q1" {
		t.Errorf("ask lost the message id: %+v", res)
	}
	got := calls(t, log)
	if !strings.Contains(got, "--options 409,merge,newest") || !strings.Contains(got, "--timeout-ms 300000") {
		t.Errorf("ask flags:\n%s", got)
	}
}

// A timeout leaves the question pending, and the caller resumes by id rather
// than asking again.
func TestAskResumesByIdInsteadOfAskingTwice(t *testing.T) {
	log := fakeOrca(t, `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
  orchestration) echo '{"ok":true,"result":{"messageId":"msg_q1","status":"pending"}}' ;;
esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Ask(AskOpts{Question: "q", TimeoutMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pending || res.MessageID != "msg_q1" {
		t.Fatalf("a timed-out ask must stay pending with a resumable id: %+v", res)
	}
	if _, err := c.Ask(AskOpts{Resume: res.MessageID}); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	if !strings.Contains(got, "--resume msg_q1") {
		t.Errorf("resume did not use the id:\n%s", got)
	}
	if strings.Count(got, "--question") != 1 {
		t.Errorf("the question was asked twice — a duplicate thread the human cannot reconcile:\n%s", got)
	}
}

func TestAskNeedsAQuestionOrAnIdToResume(t *testing.T) {
	c, _ := withOrchestration(t)
	if _, err := c.Ask(AskOpts{}); err == nil {
		t.Error("want an error for an ask with neither a question nor a resume id")
	}
}
