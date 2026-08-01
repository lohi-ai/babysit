package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// Control state is the human override axis — pause and cancel. It is stored in
// index.json under `control` and never touches `status`:
//
//	status  = where the ticket is on the lifecycle ladder (derived, reconciled)
//	control = whether a human has stopped it there (explicit, reversible)
//
// Overwriting status with "cancelled" would satisfy "a foreman must not dispatch
// it" while destroying "where was it?" — resume/restore could then only guess a
// rung. Keeping the two axes apart makes both operations lossless: clearing
// control returns the ticket to the ladder exactly where it already was.

// applyControl sets control to the given state, recording where the ticket was
// so the UI can name the undo ("Restore to planned") without re-deriving it.
func applyControl(state string, args []string) {
	env := identity.Resolve()
	needTicket(env)
	note := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--note" {
			note, i = valueOf(args, i, "--note"), i+1
		}
	}
	st := ticket.New(env)

	// The conflict check reads under the same lock as the write: these verbs are
	// driven from dashboard buttons, so a pause and a cancel can genuinely land
	// together, and a check outside the lock would let both pass and the loser
	// silently overwrite the winner's record.
	var conflict, status string
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		if cur := doc.Get("control.state"); cur != "" {
			conflict = cur
			return nil
		}
		status = doc.Get("status")
		if status == "" {
			status = "triage"
		}
		obj, err := json.Marshal(map[string]interface{}{
			"state": state, "prior_status": status, "note": note,
			"actor": actorRole(), "at": time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		if err := doc.SetObj("control", string(obj)); err != nil {
			return err
		}
		// status is deliberately NOT written here.
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("control_set", actorRole(), string(obj))
		return nil
	})

	if conflict != "" {
		if conflict == state {
			fmt.Printf("%s: already %s\n", env.Ticket, state)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "%s: already %s — clear it first (%s)\n",
			env.Ticket, conflict, undoVerb(conflict))
		os.Exit(2)
	}
	fmt.Printf("%s: %s (status stays %s)\n", env.Ticket, state, status)
	os.Exit(0)
}

// clearControl reverses pause/cancel. want names which state this verb undoes,
// so `resume` cannot silently un-cancel a ticket a human deliberately dropped.
func clearControl(want string) {
	env := identity.Resolve()
	needTicket(env)
	st := ticket.New(env)

	// Read the state under the lock, like applyControl: clearing on a stale read
	// is how a resume ends up wiping a cancel that landed in between.
	var cur, status string
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		cur = doc.Get("control.state")
		status = doc.Get("status")
		if cur == "" || cur != want {
			return nil
		}
		prior := doc.Get("control.prior_status")
		doc.Set("control", "null")
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("control_cleared", actorRole(),
			fmt.Sprintf(`{"was":"%s","prior_status":"%s"}`, cur, prior))
		return nil
	})

	if cur == "" {
		fmt.Printf("%s: not paused or cancelled — nothing to clear\n", env.Ticket)
		os.Exit(0)
	}
	if cur != want {
		fmt.Fprintf(os.Stderr, "%s: is %s, not %s — use `bbs ticket %s`\n",
			env.Ticket, cur, want, undoVerb(cur))
		os.Exit(2)
	}
	fmt.Printf("%s: %s cleared (status %s)\n", env.Ticket, cur, status)
	os.Exit(0)
}

// undoVerb names the subcommand that clears a given control state.
func undoVerb(state string) string {
	if state == "cancelled" {
		return "restore"
	}
	return "resume"
}

func runPause(args []string)  { applyControl("paused", args) }
func runCancel(args []string) { applyControl("cancelled", args) }
func runResume()              { clearControl("paused") }
func runRestore()             { clearControl("cancelled") }

// runAssign sets the owning foreman. Assignment lives on the ticket itself, so a
// foreman's inbox is derived by scanning tickets — there is no second queue that
// can disagree with this field.
func runAssign(args []string) {
	env := identity.Resolve()
	needTicket(env)
	foreman := ""
	if len(args) > 0 {
		foreman = args[0]
	}
	if foreman == "" {
		fmt.Fprintln(os.Stderr, "assign: foreman id required (or --none to unassign)")
		os.Exit(2)
	}
	clearing := foreman == "--none"

	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		old := doc.Get("assignee")
		if clearing {
			doc.Set("assignee", "null")
		} else {
			doc.Set("assignee", foreman)
		}
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("assigned", actorRole(),
			fmt.Sprintf(`{"from":"%s","to":"%s"}`, old, doc.Get("assignee")))
		return nil
	})
	if clearing {
		fmt.Printf("%s: unassigned\n", env.Ticket)
	} else {
		fmt.Printf("%s: assigned to %s\n", env.Ticket, foreman)
	}
	os.Exit(0)
}
