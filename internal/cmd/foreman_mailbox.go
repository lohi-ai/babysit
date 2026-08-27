package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/orca"
)

// `bbs foreman mailbox` is the seam between the foreman skill and Orca's
// durable message bus. It exists so the skill does not branch on a capability
// in bash: which supervision path a batch takes is one decision, and it is the
// decision most worth having a test on.
//
// The two paths it picks between:
//
//	mailbox on  — one blocking `check --wait` for the whole batch, FIFO
//	              replay until acked, typed payloads.
//	mailbox off — today's per-worker pane monitor, unchanged.
//
// "off" is a first-class outcome, not a failure. foreman must stay usable on an
// older Orca, so every verb here degrades to an explicit MAILBOX=off line the
// skill can branch on rather than an error it has to interpret.

const foremanMailboxUsage = `Usage:
  bbs foreman mailbox status [<id>]
  bbs foreman mailbox bind <id> --objective <text>
  bbs foreman mailbox dispatch <id> --ticket <t> --terminal <handle> [--spec <text>]
  bbs foreman mailbox wait <id> [--timeout-ms <n>] [--types <a,b>] [--ack]
  bbs foreman mailbox reply <id> --message <msg_id> --body <text>
  bbs foreman mailbox done --status <STATUS> --body <text> [--ticket <t>] [--files <a,b>]
`

// mailboxTypes are the message kinds a foreman acts on. Everything else on the
// bus is somebody else's business, and filtering here rather than in the skill
// keeps the batch's blocking read from waking on chatter.
//
// These are the names the runtime accepts, not the names of the commands that
// produce them: `orca orchestration ask` — the worker half of the
// AGENT_ROLE=orca escalation channel — delivers a message of type `question`.
// There is no `ask` type, and asking for one is not ignored: the whole call is
// rejected with `Invalid --types: ask`, which takes the monitor's one blocking
// read down with it.
var mailboxTypes = []string{"worker_done", "escalation", "question", "handoff"}

func foremanMailbox(args []string) error {
	if len(args) == 0 {
		fmt.Print(foremanMailboxUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "status":
		return foremanMailboxStatus(rest)
	case "bind":
		return foremanMailboxBind(rest)
	case "dispatch":
		return foremanMailboxDispatch(rest)
	case "wait":
		return foremanMailboxWait(rest)
	case "reply":
		return foremanMailboxReply(rest)
	case "done":
		return foremanMailboxDone(rest)
	case "help", "--help", "-h":
		fmt.Print(foremanMailboxUsage)
		return nil
	}
	return fmt.Errorf("foreman mailbox: unknown subcommand '%s'\n%s", sub, foremanMailboxUsage)
}

// mailboxClient returns a client only when the mailbox is actually usable.
// A missing Orca and an Orca too old to serve the contract are the same answer
// to the caller — take the fallback — so both come back as (nil, nil).
func mailboxClient() *orca.Client {
	c, err := orca.Preflight()
	if err != nil || !c.Orchestration() {
		return nil
	}
	return c
}

// foremanMailboxStatus prints the one line the skill branches on.
func foremanMailboxStatus(args []string) error {
	id, _, err := foremanFlags(args)
	if err != nil {
		return err
	}
	c := mailboxClient()
	if c == nil {
		fmt.Println("MAILBOX=off")
		return nil
	}
	fmt.Println("MAILBOX=on")
	// The bound run is per-terminal state; the recorded one is what a resumed
	// foreman has to rebind to. Print both so a mismatch is visible rather than
	// silently resolved in the skill's favour.
	if cur, err := c.RunCurrent(); err == nil && cur != "" {
		fmt.Println("BOUND_RUN=" + cur)
	}
	if id != "" {
		if r, err := foreman.Load(id); err == nil && r.Run != "" {
			fmt.Println("RECORDED_RUN=" + r.Run)
		}
	}
	return nil
}

// foremanMailboxBind binds this terminal to the foreman's Run, creating one on
// first use and rebinding on resume.
//
// Fresh per foreman session rather than adopting an existing run: the contract
// carries a legacy-authority surface whose migration rules babysit has no
// reason to inherit. Rebinding matters because coordinator binding is
// per-terminal — a foreman that comes back in a new tab has a mailbox that
// reads as somebody else's until it says run-use.
func foremanMailboxBind(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return errors.New("foreman mailbox bind: needs a foreman id")
	}
	rec, err := foreman.Load(id)
	if err != nil {
		return err
	}
	c := mailboxClient()
	if c == nil {
		fmt.Println("MAILBOX=off")
		return nil
	}
	if rec.Run != "" {
		if err := c.RunUse(rec.Run); err == nil {
			fmt.Printf("MAILBOX=on\nRUN=%s\nBOUND=rebound\n", rec.Run)
			return nil
		}
		// The recorded run is gone or fenced. Falling back to a fresh one is
		// right: a foreman with an unusable mailbox and no way to make a new
		// one would drop to the pane monitor for the rest of its life.
		fmt.Fprintf(os.Stderr, "foreman mailbox: recorded run %s is unusable — binding a fresh one\n", rec.Run)
	}
	objective := kv["objective"]
	if objective == "" {
		objective = "babysit foreman " + id
	}
	runID, err := c.RunCreate(objective)
	if err != nil {
		return err
	}
	rec.Run = runID
	if err := foreman.Save(rec); err != nil {
		return fmt.Errorf("run %s bound but recording it on %s failed: %w", runID, id, err)
	}
	fmt.Printf("MAILBOX=on\nRUN=%s\nBOUND=created\n", runID)
	return nil
}

// foremanMailboxDispatch creates this ticket's task and binds it to the
// worker's terminal. One task per ticket, never with --deps: a foreman batch is
// independent tickets by construction.
//
// Nothing has to be delivered to the worker for this to work: the task is
// titled with the ticket, and the ticket is what the worker already knows. It
// rings the doorbell with `mailbox done`, which finds this task by that title —
// so dispatch stays a coordinator-side call with no second half the skill could
// forget.
func foremanMailboxDispatch(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	ticket, handle := kv["ticket"], kv["terminal"]
	if id == "" || ticket == "" || handle == "" {
		return errors.New("foreman mailbox dispatch: needs a foreman id, --ticket and --terminal")
	}
	rec, err := foreman.Load(id)
	if err != nil {
		return err
	}
	c := mailboxClient()
	if c == nil || rec.Run == "" {
		fmt.Println("MAILBOX=off")
		return nil
	}
	spec := kv["spec"]
	if spec == "" {
		spec = "babysit ticket " + ticket
	}
	taskID, err := c.TaskCreate(rec.Run, ticket, spec)
	if err != nil {
		return err
	}
	if err := c.Dispatch(rec.Run, taskID, handle); err != nil {
		return err
	}
	fmt.Printf("MAILBOX=on\nRUN=%s\nTASK=%s\n", rec.Run, taskID)
	return nil
}

// doneOutcome maps babysit's terminal status onto Orca's two-valued outcome.
//
// The worker reports in the vocabulary it already prints — the STATUS line at
// the end of every skill — and this is the only place that knows Orca has a
// different one. A status outside that set is a caller bug rather than a
// default to guess at: ringing the doorbell early is worse than not ringing it.
var doneOutcome = map[string]string{
	"DONE":               "succeeded",
	"DONE_WITH_CONCERNS": "succeeded",
	"BLOCKED":            "failed",
	"NEEDS_CONTEXT":      "failed",
}

// foremanMailboxDone is the worker half of the doorbell, and the only verb here
// a worker runs. It takes no foreman id and no task id: a worker knows its
// ticket, which is the title foreman gave the task, and that is the whole join.
//
// Everything about it is best-effort by design. A worker that was never
// dispatched over the bus, or is running under an Orca too old to serve the
// contract, prints MAILBOX=off and exits 0 — reporting completion must never be
// the thing that fails a finished ticket.
func foremanMailboxDone(args []string) error {
	_, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	status := strings.ToUpper(kv["status"])
	outcome, ok := doneOutcome[status]
	if !ok {
		return fmt.Errorf("foreman mailbox done: --status must be one of DONE, DONE_WITH_CONCERNS, BLOCKED, NEEDS_CONTEXT")
	}
	body := kv["body"]
	if body == "" {
		return errors.New("foreman mailbox done: needs a --body (what you did, what you found, what is left)")
	}
	ticket := kv["ticket"]
	if ticket == "" {
		ticket = os.Getenv("BABYSIT_TICKET")
	}
	// Every way this can fail lands here: say so on stderr, stay silent about it
	// on stdout beyond MAILBOX=off, and exit 0.
	off := func(err error) error {
		fmt.Println("MAILBOX=off")
		if err != nil {
			fmt.Fprintf(os.Stderr, "foreman mailbox done: %v\n", err)
		}
		return nil
	}
	c := mailboxClient()
	if c == nil || ticket == "" {
		return off(nil)
	}
	run, err := c.Self()
	if err != nil {
		return off(nil)
	}
	task, err := c.TaskFor(run, ticket)
	if err != nil {
		// No task titled for this ticket is the ordinary shape for a worker
		// nobody dispatched over the bus, not a fault to report.
		return off(nil)
	}
	// Not optional: orca refuses a worker_done with no dispatch id, so failing
	// here means no doorbell — off() reports that rather than swallowing it.
	dispatch, err := c.DispatchFor(task)
	if err != nil {
		return off(err)
	}
	subject := strings.TrimSpace(ticket + " " + status)
	var files []string
	for _, f := range strings.Split(kv["files"], ",") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	if err := c.WorkerDone(orca.DoneOpts{
		Run: run, Task: task, Dispatch: dispatch,
		From:    os.Getenv("ORCA_TERMINAL_HANDLE"),
		Subject: subject, Body: body, Outcome: outcome, Files: files,
	}); err != nil {
		return off(err)
	}
	fmt.Printf("MAILBOX=on\nDONE=%s\nTASK=%s\n", outcome, task)
	return nil
}

// foremanMailboxWait is the call that replaces the per-worker poll loop: one
// blocking read for the whole batch. It prints one line per message in a shape
// the skill can read without parsing JSON, because the point of the typed bus
// is that nobody greps a tail any more.
func foremanMailboxWait(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return errors.New("foreman mailbox wait: needs a foreman id")
	}
	rec, err := foreman.Load(id)
	if err != nil {
		return err
	}
	c := mailboxClient()
	if c == nil || rec.Run == "" {
		fmt.Println("MAILBOX=off")
		return nil
	}
	timeout := 60000
	if v := kv["timeout-ms"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}
	types := mailboxTypes
	if v := kv["types"]; v != "" {
		types = strings.Split(v, ",")
	}
	// --ack acknowledges the batch handed out by the PREVIOUS wait, which is
	// the one the caller has now finished acting on. The id comes off the
	// record, not from the caller: `--ack` is a decision ("I am done with the
	// last batch"), and making the skill carry a delivery id between two shell
	// invocations would be one more thing to lose on a crash.
	var ack string
	if _, want := kv["ack"]; want {
		ack = rec.Delivery
	}
	batch, err := c.Check(orca.CheckOpts{Run: rec.Run, Types: types, WaitMS: timeout, Ack: ack})
	if err != nil {
		return err
	}
	// Record before printing. If the process dies between the two, the worst
	// case is a batch acked without being acted on — recoverable from disk,
	// where the verdicts are. The other order loses the id and replays the
	// batch for ever.
	if batch.DeliveryID != rec.Delivery {
		rec.Delivery = batch.DeliveryID
		if err := foreman.Save(rec); err != nil {
			return err
		}
	}
	msgs := batch.Messages
	fmt.Printf("MAILBOX=on\nCOUNT=%d\nDELIVERY=%s\n", len(msgs), batch.DeliveryID)
	for _, m := range msgs {
		// worker_done is the doorbell, not the verdict: the skill still reads
		// `bbs ticket verdict-status` before believing anything finished.
		fields := map[string]any{
			"id": m.ID, "type": m.Type, "subject": m.Subject,
			"task": m.TaskID, "dispatch": m.DispatchID,
			"outcome": m.Outcome, "files": m.FilesModified,
			"needs_answer": m.NeedsAnswer(), "body": m.Body,
		}
		// Only present when orca threw the report away. Absent on every ordinary
		// message, so a reader that has never seen one is not asked to care.
		if m.Rejected != "" {
			fields["rejected"] = m.Rejected
		}
		line, _ := json.Marshal(fields)
		fmt.Println(string(line))
	}
	return nil
}

// foremanMailboxReply answers one message by id, unblocking a worker parked on
// `ask`. The pane path answered by typing words into a composer and hoping the
// TUI submitted them; this removes that failure class rather than mitigating it.
func foremanMailboxReply(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	msgID, body := kv["message"], kv["body"]
	if id == "" || msgID == "" || body == "" {
		return errors.New("foreman mailbox reply: needs a foreman id, --message and --body")
	}
	rec, err := foreman.Load(id)
	if err != nil {
		return err
	}
	c := mailboxClient()
	if c == nil || rec.Run == "" {
		fmt.Println("MAILBOX=off")
		return nil
	}
	if err := c.Reply(rec.Run, msgID, body); err != nil {
		return err
	}
	fmt.Printf("MAILBOX=on\nREPLIED=%s\n", msgID)
	return nil
}
