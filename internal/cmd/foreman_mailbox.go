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
`

// mailboxTypes are the message kinds a foreman acts on. Everything else on the
// bus is somebody else's business, and filtering here rather than in the skill
// keeps the batch's blocking read from waking on chatter.
var mailboxTypes = []string{"worker_done", "escalation", "question", "ask"}

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
// worker's terminal, printing the lifecycle preamble for babysit to deliver
// itself. One task per ticket, never with --deps: a foreman batch is
// independent tickets by construction.
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
	preamble, err := c.Dispatch(rec.Run, taskID, handle)
	if err != nil {
		return err
	}
	fmt.Printf("MAILBOX=on\nRUN=%s\nTASK=%s\n", rec.Run, taskID)
	if preamble != "" {
		fmt.Println("PREAMBLE<<EOF")
		fmt.Println(preamble)
		fmt.Println("EOF")
	}
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
	_, ack := kv["ack"]
	msgs, err := c.Check(orca.CheckOpts{Run: rec.Run, Types: types, WaitMS: timeout, Ack: ack})
	if err != nil {
		return err
	}
	fmt.Printf("MAILBOX=on\nCOUNT=%d\n", len(msgs))
	for _, m := range msgs {
		// worker_done is the doorbell, not the verdict: the skill still reads
		// `bbs ticket verdict-status` before believing anything finished.
		line, _ := json.Marshal(map[string]any{
			"id": m.ID, "type": m.Type, "subject": m.Subject,
			"task": m.TaskID, "dispatch": m.DispatchID,
			"outcome": m.Outcome, "files": m.FilesModified,
			"needs_answer": m.NeedsAnswer(), "body": m.Body,
		})
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
