package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/learnings"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// `bbs autopilot review-gate` is the agent reviewer's design checkpoint, and it
// exists because the old one was not a gate at all.
//
// Before this, `--reviewer` spawned an agent, told it in prose to "APPROVE
// (every line filled, no money/auth/irreversible-data path): run spawn-goal",
// and nothing checked either clause. A foreman making the same decision runs a
// machine-checked gate — floor, then rubric, then posture — and logs it. So the
// same plan touching Stripe was auto-approved under `--reviewer` and escalated
// under a foreman. That is precisely the "two places for the floor to be wrong"
// argument the recon used to reject Orca's gate-*, and it was already true
// inside babysit.
//
// The fix is not a sterner prompt. It is that spawn-goal now happens HERE, on
// the far side of the gate, so approval is something the reviewer earns rather
// than something it asserts. A reviewer that skips this command does not
// approve anything; it just fails to start the builder.
//
// Rounds, not silence: a redirect used to end the chain in a detached process
// writing to review.log that nobody reads. Now the gate says what to fix and
// how many rounds are left, and spends them — two failed rounds is BLOCKED with
// the gaps named, which is the same budget a foreman gets.

// reviewRounds is the feedback budget. Two, matching the foreman's design
// checkpoint: enough for one real correction, few enough that a reviewer and a
// planner cannot loop on each other unattended.
const reviewRounds = 2

type reviewGateOpts struct {
	ticket, workflow, builder, foremanID, rubric, rubricPath string
	printOnly                                                bool
}

func parseReviewGateArgs(args []string) reviewGateOpts {
	var o reviewGateOpts
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ticket":
			o.ticket, i = next(args, i)
		case "--workflow":
			o.workflow, i = next(args, i)
		case "--builder":
			o.builder, i = next(args, i)
		case "--foreman":
			o.foremanID, i = next(args, i)
		case "--rubric":
			o.rubric, i = next(args, i)
		case "--rubric-file":
			o.rubricPath, i = next(args, i)
		case "--print":
			o.printOnly = true
		}
	}
	return o
}

func (a *apState) reviewGate(args []string) {
	o := parseReviewGateArgs(args)
	if o.foremanID == "" {
		o.foremanID = os.Getenv("BABYSIT_FOREMAN")
	}
	ticketID := safeTicket(orDefault(o.ticket, a.ticket))
	if ticketID == "" {
		fmt.Fprintln(os.Stderr, "review-gate: --ticket is required (or run on a ticket branch)")
		os.Exit(2)
	}
	if o.rubricPath != "" {
		b, err := os.ReadFile(o.rubricPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "review-gate: %v\n", err)
			os.Exit(2)
		}
		o.rubric = string(b)
	}
	if strings.TrimSpace(o.rubric) == "" {
		fmt.Fprintln(os.Stderr, "review-gate: needs --rubric or --rubric-file — an approval with no evidence is not an approval")
		os.Exit(2)
	}

	// The posture record. An autopilot run under `--reviewer` usually has no
	// foreman at all, and that is not a refusal: the zero Record is default
	// autonomy, which is the same posture a foreman has before anyone narrows
	// it. The floor still applies — it is checked before Allows precisely so
	// that no posture, including this one, can reach a non-delegable path.
	rec := foreman.Record{ID: "autopilot-reviewer"}
	if o.foremanID != "" {
		if loaded, err := foreman.Load(o.foremanID); err == nil {
			rec = loaded
		} else {
			fmt.Fprintf(os.Stderr, "review-gate: foreman %s not readable (%v) — falling back to default autonomy with the floor intact\n", o.foremanID, err)
		}
	}

	env := identity.Env{ProjectHome: a.stateRoot, Ticket: ticketID}
	st := ticket.New(env)
	code, filled, reason := selfResolveGate(st, env, rec, o.rubric)

	td, _ := a.ticketDir(ticketID)
	switch code {
	case exitFloor:
		a.logReviewGate(ticketID, "escalate", "floor: "+reason, filled)
		fmt.Printf("VERDICT=ESCALATE\nREASON=floor\nPATHS=%s\n", reason)
		fmt.Fprintf(os.Stderr,
			"FLOOR: %s touches a non-delegable path (%s).\n"+
				"Money, auth and irreversible-data changes escalate to a human under every posture.\n"+
				"Do not spawn-goal. Report NEEDS_CONTEXT naming the path.\n", ticketID, reason)
		os.Exit(exitFloor)

	case exitRubric:
		spent := bumpReviewRound(td)
		a.logReviewGate(ticketID, "redirect", "rubric unfilled: "+reason, filled)
		if spent >= reviewRounds {
			fmt.Printf("VERDICT=BLOCKED\nREASON=rubric\nUNFILLED=%s\nROUNDS=%d/%d\n",
				reason, spent, reviewRounds)
			fmt.Fprintf(os.Stderr,
				"BLOCKED: %s still cannot fill its rubric after %d rounds — unfilled: %s\n",
				ticketID, spent, reason)
			os.Exit(exitRubric)
		}
		// A redirect is a round, not a dead end: say what to fix and who fixes
		// it, so the chain re-enters planning instead of stopping in a log.
		fmt.Printf("VERDICT=REDIRECT\nREASON=rubric\nUNFILLED=%s\nROUNDS=%d/%d\n",
			reason, spent, reviewRounds)
		fmt.Fprintf(os.Stderr,
			"REDIRECT (round %d of %d): %s is not filled with named evidence — unfilled: %s\n"+
				"Re-enter planning with those gaps (run plan-draft against them), then run review-gate again.\n"+
				"A line that is structurally inapplicable is written `N/A — <reason>`, not left blank;\n"+
				"only host-page consistency and prototype inspected may be declined that way.\n",
			spent, reviewRounds, ticketID, reason)
		os.Exit(exitRubric)

	case exitGrant:
		a.logReviewGate(ticketID, "escalate", "posture: "+reason, filled)
		fmt.Printf("VERDICT=ESCALATE\nREASON=posture\nDETAIL=%s\n", reason)
		fmt.Fprintf(os.Stderr, "POSTURE: %s may not be approved here — %s\n", ticketID, reason)
		os.Exit(exitGrant)
	}

	// Approved. Clear the round counter so a later checkpoint on the same
	// ticket starts from a full budget rather than inheriting a spent one.
	clearReviewRounds(td)
	a.logReviewGate(ticketID, "approve", "rubric filled; no non-delegable path", filled)
	fmt.Printf("VERDICT=APPROVE\nTICKET=%s\n", ticketID)

	if o.builder == "" {
		// No builder pinned: this was a review-only run. Approving is the whole
		// outcome, and the human starts the build.
		fmt.Println("NEXT=human")
		return
	}
	res, err := a.runSpawnGoal(spawnOpts{
		ticket:    ticketID,
		workflow:  o.workflow,
		agentFlag: o.builder,
		printOnly: o.printOnly,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "review-gate: approved, but starting the builder failed: "+err.Error())
		os.Exit(exitStatus(err))
	}
	printSpawn(res)
}

// reviewRoundsPath is where the feedback budget lives. On disk in the ticket
// dir, not in the reviewer's context: the reviewer is a detached process that
// may be a different one each round, and a budget only that process remembers
// is a budget that resets on every crash.
func reviewRoundsPath(td string) string {
	if td == "" {
		return ""
	}
	return filepath.Join(td, "review.rounds")
}

func bumpReviewRound(td string) int {
	p := reviewRoundsPath(td)
	if p == "" {
		return 1
	}
	n := 0
	if b, err := os.ReadFile(p); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	n++
	_ = os.MkdirAll(td, 0o755)
	_ = os.WriteFile(p, []byte(strconv.Itoa(n)+"\n"), 0o644)
	return n
}

func clearReviewRounds(td string) {
	if p := reviewRoundsPath(td); p != "" {
		_ = os.Remove(p)
	}
}

// logReviewGate mirrors logSelfResolvedApproval: nobody watched this decision
// happen, so the evidence it rested on has to be recoverable afterwards. Same
// log, same shape — the point of routing both resolvers through one gate is
// that the audit trail does not depend on which one ran.
func (a *apState) logReviewGate(ticketID, choice, rationale string, filled map[string]string) {
	state, _ := json.Marshal(map[string]any{
		"reviewer": "autopilot",
		"rubric":   filled,
	})
	line, _ := json.Marshal(map[string]string{
		"ts":        learnings.Timestamp(),
		"skill":     "autopilot",
		"tier":      "taste",
		"type":      "design-checkpoint",
		"ticket":    ticketID,
		"choice":    choice,
		"rationale": rationale,
		"state":     string(state),
	})
	learnings.Append(learnings.AnalyticsDir(), string(line)+"\n")
}
