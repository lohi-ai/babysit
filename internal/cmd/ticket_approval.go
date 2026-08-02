package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/cmux"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// The approval record is the design checkpoint made durable. A worker that
// reaches "plan + prototype are ready" publishes one and waits on the verdict
// file instead of asking a question into a terminal nobody is watching, which
// is the whole point of AGENT_ROLE=dashboard.
//
// It lives in index.json under `approval`, next to `control`, for the same
// reason: one lock, one writer, and it rides the snapshot the dashboard already
// composes. The artifacts are not copied into it — requirement.md, plan.md,
// design.md and prototype.html are already on the ticket, and a record that
// duplicated them could disagree with the files it is supposed to be about.

// approvalOutcome maps a decision verb to the state it parks the record at.
var approvalOutcome = map[string]string{
	"approve":  "approved",
	"redirect": "redirected",
	"drop":     "dropped",
}

// approvalPublish opens a pending record. Re-publishing over a pending record
// is a no-op returning conflict="pending" rather than an error: a resumed
// worker re-runs its checkpoint step, and re-opening would reset the clock on a
// decision the human may already be reading.
func approvalPublish(st *ticket.Store, kind, note, actor string) (conflict string, err error) {
	if kind == "" {
		kind = "plan"
	}
	err = withLock(st, func() error {
		doc := loadForMutate(st)
		if cur := doc.Get("approval.state"); cur != "" && cur != "dropped" {
			if cur == "pending" {
				conflict = "pending"
				return nil
			}
			// An approved or redirected record is history; publishing again is a
			// second checkpoint on the same ticket (redirect → rework → re-ask),
			// which is exactly the loop this is for.
		}
		obj, err := json.Marshal(map[string]interface{}{
			"state": "pending", "kind": kind, "note": note,
			"requested_by": actor, "at": time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		if err := doc.SetObj("approval", string(obj)); err != nil {
			return err
		}
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("approval_published", actor, string(obj))
		return nil
	})
	return conflict, err
}

// approvalNote is one anchored comment: the human pointing at a paragraph of
// the plan or an element of the prototype and saying what is wrong with *that*.
// Design review is mostly made of those, and a single free-text redirect note
// flattens them into prose the worker then has to re-locate in the document.
//
// `anchor` is how the comment finds its spot again (`block:<n>` for a markdown
// paragraph, a CSS-ish path for a prototype element) and `excerpt` is what the
// human actually pointed at. The excerpt is the load-bearing one: anchors go
// stale the moment the artifact is rewritten, the quoted text does not.
type approvalNote struct {
	ID      string `json:"id"`
	Target  string `json:"target"`
	Anchor  string `json:"anchor,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
	Body    string `json:"body"`
	Actor   string `json:"actor"`
	At      string `json:"at"`
}

var approvalTargets = map[string]bool{
	"requirement": true, "plan": true, "design": true, "prototype": true,
}

// approvalAddComment appends a comment to the pending record. Comments are only
// accepted while the record is pending — after a verdict the record is history,
// and a comment landing on it would be feedback nobody is waiting for.
func approvalAddComment(st *ticket.Store, n approvalNote, actor string) (id string, missing bool, err error) {
	if n.Body == "" {
		return "", false, fmt.Errorf("a comment needs a body")
	}
	if !approvalTargets[n.Target] {
		return "", false, fmt.Errorf("unknown target %q (requirement|plan|design|prototype)", n.Target)
	}
	n.Actor, n.At = actor, time.Now().UTC().Format(time.RFC3339)
	// Nanoseconds, not millis: AppendObj dedupes by deep equality, so two
	// identical comments saved in the same tick would silently become one.
	n.ID = fmt.Sprintf("c%d", time.Now().UnixNano())
	err = withLock(st, func() error {
		doc := loadForMutate(st)
		if doc.Get("approval.state") != "pending" {
			missing = true
			return nil
		}
		obj, err := json.Marshal(n)
		if err != nil {
			return err
		}
		if err := doc.AppendObj("approval.comments", string(obj)); err != nil {
			return err
		}
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("approval_commented", actor, string(obj))
		id = n.ID
		return nil
	})
	return id, missing, err
}

// approvalComments reads the record's comments back, for the waiter and for
// `approval comments`.
func approvalComments(st *ticket.Store) []approvalNote {
	raw := ticket.ReadDoc(st.IndexPath()).Get("approval.comments")
	if raw == "" {
		return nil
	}
	var out []approvalNote
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// formatComment is the one-line rendering a worker reads: what was pointed at,
// then what to do about it.
func formatComment(n approvalNote) string {
	where := n.Target
	if n.Excerpt != "" {
		where += " · " + strings.Join(strings.Fields(n.Excerpt), " ")
	} else if n.Anchor != "" {
		where += " · " + n.Anchor
	}
	return fmt.Sprintf("[%s] %s", where, n.Body)
}

// approvalResolve records the human's decision. It is shared by the CLI verb and
// the dashboard's POST handler so both write the same shape under one lock.
// `missing` is set when there is nothing pending — the dashboard turns that into
// a 409 rather than inventing a record to resolve.
func approvalResolve(st *ticket.Store, action, note, actor string) (state string, missing bool, err error) {
	outcome, ok := approvalOutcome[action]
	if !ok {
		return "", false, fmt.Errorf("unknown decision %q (approve|redirect|drop)", action)
	}
	err = withLock(st, func() error {
		doc := loadForMutate(st)
		if doc.Get("approval.state") != "pending" {
			missing = true
			return nil
		}
		// A redirect the foreman cannot act on is worse than no answer: it ends
		// the wait and says nothing. Inline comments are that same information in
		// anchored form, so a redirect carrying comments needs no summary note.
		if c := doc.Get("approval.comments"); action == "redirect" && note == "" && (c == "" || c == "[]") {
			return fmt.Errorf("redirect needs a note or at least one inline comment saying what to change")
		}
		at := time.Now().UTC().Format(time.RFC3339)
		doc.Set("approval.state", outcome)
		resolved, err := json.Marshal(map[string]interface{}{
			"outcome": outcome, "actor": actor, "at": at, "note": note,
		})
		if err != nil {
			return err
		}
		if err := doc.SetObj("approval.resolved", string(resolved)); err != nil {
			return err
		}
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("approval_resolved", actor, string(resolved))
		state = outcome
		return nil
	})
	return state, missing, err
}

// approvalRead returns the record's state and the resolution note, for the
// waiter and for `approval status`.
func approvalRead(st *ticket.Store) (state, outcome, note string) {
	doc := ticket.ReadDoc(st.IndexPath())
	return doc.Get("approval.state"), doc.Get("approval.resolved.outcome"), doc.Get("approval.resolved.note")
}

// ─── CLI ─────────────────────────────────────────────────────────────────────

func runApproval(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "approval: publish | status | resolve | await | comment | comments | self-resolve")
		os.Exit(2)
	}
	env := identity.Resolve()
	needTicket(env)
	st := ticket.New(env)

	switch args[0] {
	case "publish":
		kind, note := "plan", ""
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--kind":
				kind, i = valueOf(rest, i, "--kind"), i+1
			case "--note":
				note, i = valueOf(rest, i, "--note"), i+1
			}
		}
		conflict, err := approvalPublish(st, kind, note, actorRole())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if conflict != "" {
			fmt.Printf("%s: approval already pending\n", env.Ticket)
			os.Exit(0)
		}
		fmt.Printf("%s: approval published (%s) — waiting on the dashboard\n", env.Ticket, kind)
		os.Exit(0)

	case "status":
		state, outcome, note := approvalRead(st)
		if state == "" {
			fmt.Println("none")
			os.Exit(0)
		}
		fmt.Println(state)
		if outcome != "" && note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		os.Exit(0)

	case "resolve":
		action, note := "", ""
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--action":
				action, i = valueOf(rest, i, "--action"), i+1
			case "--note":
				note, i = valueOf(rest, i, "--note"), i+1
			}
		}
		state, missing, err := approvalResolve(st, action, note, actorRole())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if missing {
			fmt.Fprintf(os.Stderr, "%s: no approval pending\n", env.Ticket)
			os.Exit(2)
		}
		fmt.Printf("%s: %s\n", env.Ticket, state)
		os.Exit(0)

	case "comment":
		n := approvalNote{Target: "plan"}
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--target":
				n.Target, i = valueOf(rest, i, "--target"), i+1
			case "--anchor":
				n.Anchor, i = valueOf(rest, i, "--anchor"), i+1
			case "--excerpt":
				n.Excerpt, i = valueOf(rest, i, "--excerpt"), i+1
			case "--body":
				n.Body, i = valueOf(rest, i, "--body"), i+1
			}
		}
		id, missing, err := approvalAddComment(st, n, actorRole())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if missing {
			fmt.Fprintf(os.Stderr, "%s: no approval pending\n", env.Ticket)
			os.Exit(2)
		}
		fmt.Printf("%s: comment %s on %s\n", env.Ticket, id, n.Target)
		os.Exit(0)

	case "comments":
		for _, n := range approvalComments(st) {
			fmt.Println(formatComment(n))
		}
		os.Exit(0)

	case "await":
		runApprovalAwait(st, env, args[1:])

	case "self-resolve":
		runApprovalSelfResolve(st, env, args[1:])

	default:
		fmt.Fprintln(os.Stderr, "approval: publish | status | resolve | await | comment | comments | self-resolve")
		os.Exit(2)
	}
}

// runApprovalAwait is the AGENT_ROLE=dashboard side of the checkpoint: block on
// the verdict file, not on a prompt.
//
// There is deliberately no hard timeout. The session is already held by /goal's
// Stop hook, and a timeout would resume the run with a *guess* at the decision —
// the one outcome this whole feature exists to prevent. What it does instead is
// make the wait visible: one reminder into the workspace after --reminder-min,
// and nothing after that.
func runApprovalAwait(st *ticket.Store, env identity.Env, args []string) {
	interval, reminder := 10, 30
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--interval":
			interval, i = atoiOr(valueOf(args, i, "--interval"), interval), i+1
		case "--reminder-min":
			reminder, i = atoiOr(valueOf(args, i, "--reminder-min"), reminder), i+1
		}
	}

	state, _, _ := approvalRead(st)
	if state == "" {
		fmt.Fprintf(os.Stderr, "%s: nothing to await — publish an approval first\n", env.Ticket)
		os.Exit(2)
	}

	start := time.Now()
	reminded := false
	for {
		state, outcome, note := approvalRead(st)
		if state != "pending" {
			if outcome == "" {
				outcome = state
			}
			// stdout carries the verdict word alone — callers do
			// DECISION=$(… await). Everything the human wrote goes to stderr, so
			// it stays visible in the transcript without polluting that capture.
			fmt.Println(outcome)
			if note != "" {
				fmt.Fprintln(os.Stderr, note)
			}
			for _, n := range approvalComments(st) {
				fmt.Fprintln(os.Stderr, formatComment(n))
			}
			// drop ends the wait like any other verdict; the caller decides what
			// to do with it, and "stop work" is a decision, not a failure.
			os.Exit(0)
		}
		if !reminded && reminder > 0 && time.Since(start) >= time.Duration(reminder)*time.Minute {
			remindApproval(st, env)
			reminded = true
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

// remindApproval nudges the assigned foreman's workspace once — that is the
// window a human is actually watching, and the ticket's assignee is the only
// workspace this process can name (a waiting worker does not reliably know its
// own cmux title).
//
// cmux being unreachable is not worth failing the wait over: the record is
// already on the dashboard, which is where the decision happens. The reminder
// is a convenience on top of it, so every failure here is a warning.
func remindApproval(st *ticket.Store, env identity.Env) {
	assignee := ticket.ReadDoc(st.IndexPath()).Get("assignee")
	if assignee == "" {
		fmt.Fprintf(os.Stderr, "approval await: %s is unassigned — no workspace to remind\n", env.Ticket)
		return
	}
	rec, err := foreman.Load(assignee)
	if err != nil || rec.WorkspaceTitle == "" {
		fmt.Fprintf(os.Stderr, "approval await: no workspace for %s — reminder skipped\n", assignee)
		return
	}
	client, err := cmux.Preflight()
	if err != nil {
		fmt.Fprintf(os.Stderr, "approval await: reminder not sent (%v)\n", err)
		return
	}
	if err := client.Notify(rec.WorkspaceTitle, "bbs approval: "+env.Ticket,
		env.Ticket+" has been waiting on a plan/prototype decision. Open the dashboard to approve, redirect, or drop it."); err != nil {
		fmt.Fprintf(os.Stderr, "approval await: reminder not sent (%v)\n", err)
	}
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}
