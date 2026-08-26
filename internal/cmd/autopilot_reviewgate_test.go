package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// A rubric with every line carrying real evidence, for a change with a UI.
const goodRubric = `Coverage: each acceptance criterion maps to a named step in plan.md
Host-page consistency: matches the sibling Settings > Profile panel
Reuse: uses the shared <Button> and <Panel> already in the kit
Prototype inspected: read prototype.html; spacing matches the token scale
Scope: nothing beyond the request wording
`

// The same review for a change with no UI surface at all — which is what a CLI,
// Go or markdown ticket actually looks like.
const nonUIRubric = `Coverage: each acceptance criterion maps to a named step in plan.md
Host-page consistency: N/A — no UI surface; this ticket is Go and markdown only
Reuse: extends the existing internal/agent registry rather than a new package
Prototype inspected: N/A — no prototype exists; there is nothing to render
Scope: nothing beyond the request wording
`

func gateState(t *testing.T) *apState {
	t.Helper()
	t.Setenv("BABYSIT_SPAWNED", "")
	t.Setenv("BABYSIT_REVIEWER", "")
	t.Setenv("BABYSIT_BUILDER", "")
	t.Setenv("BABYSIT_AGENT", "")
	t.Setenv("BABYSIT_FOREMAN", "")
	t.Setenv("GROK_AGENT", "")
	t.Setenv("GROK_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	home := t.TempDir()
	t.Setenv("BABYSIT_STATE_DIR", t.TempDir())
	t.Setenv("BABYSIT_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return &apState{stateRoot: home}
}

// seedDesign writes the ticket-root artifacts the floor is checked against.
// Reading them, not just the rubric, is what keeps the floor from being
// something a reviewer can talk its way past.
func seedDesign(t *testing.T, a *apState, ticketID string, docs map[string]string) {
	t.Helper()
	td := filepath.Join(a.stateRoot, "tickets", ticketID)
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range docs {
		if err := os.WriteFile(filepath.Join(td, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func gate(t *testing.T, a *apState, ticketID, rubric string, rec foreman.Record) (int, map[string]string, string) {
	t.Helper()
	env := identity.Env{ProjectHome: a.stateRoot, Ticket: ticketID}
	return selfResolveGate(ticket.New(env), env, rec, rubric)
}

// The defect this whole phase exists for. `approval self-resolve` runs a
// machine-checked gate; `--reviewer` was told the same rule in prose and
// nothing checked it — so the same Stripe plan was auto-approved under one and
// escalated under the other. One gate, one answer.
func TestReviewGateEscalatesAMoneyPathUnderDefaultAutonomy(t *testing.T) {
	a := gateState(t)
	seedDesign(t, a, "bs-pay", map[string]string{
		"plan.md": "Add a Stripe checkout button to the pricing page.",
	})
	// Most permissive posture there is: no hold, no grant.
	rec := foreman.Record{ID: "autopilot-reviewer"}
	code, _, reason := gate(t, a, "bs-pay", goodRubric, rec)
	if code != exitFloor {
		t.Fatalf("code = %d (%s), want exitFloor=%d — a filled rubric must not clear a money path",
			code, reason, exitFloor)
	}
	if !strings.Contains(reason, "stripe") && !strings.Contains(reason, "checkout") {
		t.Errorf("floor did not name the path: %q", reason)
	}
}

// The rubric was authored for user-facing work. On a CLI/Go/markdown ticket
// two of five lines have no referent, so insisting a missing line is never a
// pass left exactly those tickets unable to pass their own review — including
// the ticket that added this gate.
func TestReviewGateLetsANonUITicketPassWithReasonedNA(t *testing.T) {
	a := gateState(t)
	seedDesign(t, a, "bs-cli", map[string]string{
		"plan.md": "Widen the agent registry and wrap Orca's message bus.",
	})
	code, filled, reason := gate(t, a, "bs-cli", nonUIRubric, foreman.Record{ID: "r"})
	if code != 0 {
		t.Fatalf("code = %d (%s) — a non-UI ticket cannot fill a UI rubric and must not redirect forever", code, reason)
	}
	// The reason is recorded as the evidence, so the decisions log shows what
	// was declined and why rather than a word that means nothing.
	if !strings.Contains(filled["host-page consistency"], "no UI surface") {
		t.Errorf("the stated reason was not kept as evidence: %q", filled["host-page consistency"])
	}
}

// The escape hatch must not become a way to approve anything.
func TestReviewGateRefusesNAWithoutAReasonAndOnNonNullableLines(t *testing.T) {
	a := gateState(t)
	seedDesign(t, a, "bs-cli", map[string]string{"plan.md": "a small refactor"})

	bare := strings.Replace(nonUIRubric,
		"Host-page consistency: N/A — no UI surface; this ticket is Go and markdown only",
		"Host-page consistency: N/A", 1)
	if code, _, _ := gate(t, a, "bs-cli", bare, foreman.Record{ID: "r"}); code != exitRubric {
		t.Errorf("a bare N/A with no reason passed (code %d) — the reason IS the evidence", code)
	}

	// Coverage, reuse and scope mean as much for a Go package as for a screen.
	for _, line := range []string{"Coverage", "Reuse", "Scope"} {
		body := nonUIRubric
		for _, orig := range strings.Split(nonUIRubric, "\n") {
			if strings.HasPrefix(orig, line+":") {
				body = strings.Replace(body, orig, line+": N/A — this is a backend change", 1)
			}
		}
		if code, _, _ := gate(t, a, "bs-cli", body, foreman.Record{ID: "r"}); code != exitRubric {
			t.Errorf("%s was declined as N/A (code %d) — it is never nullable", line, code)
		}
	}
}

// A word merely starting with "na" is not an N/A.
func TestNAReasonNeedsAWordBoundary(t *testing.T) {
	if _, ok := naReason("nabbed the sibling Settings panel for this"); ok {
		t.Error("a line starting with \"na\" was read as N/A")
	}
	if _, ok := naReason("N/A — no UI surface on this ticket"); !ok {
		t.Error("a reasoned N/A was rejected")
	}
	if _, ok := naReason("n/a"); ok {
		t.Error("a bare n/a was accepted")
	}
}

// A redirect used to end the chain in a detached process writing to a log
// nobody reads. It is a round now, and the rounds are spent on disk — the
// reviewer may be a different process each time, so a budget only it remembers
// resets on every crash.
func TestReviewRoundsAreSpentOnDiskAndClearedOnApproval(t *testing.T) {
	td := t.TempDir()
	if n := bumpReviewRound(td); n != 1 {
		t.Fatalf("first round = %d", n)
	}
	if n := bumpReviewRound(td); n != reviewRounds {
		t.Fatalf("second round = %d, want %d", n, reviewRounds)
	}
	if _, err := os.Stat(filepath.Join(td, "review.rounds")); err != nil {
		t.Fatalf("rounds not persisted: %v", err)
	}
	// A later checkpoint on the same ticket starts from a full budget rather
	// than inheriting a spent one.
	clearReviewRounds(td)
	if n := bumpReviewRound(td); n != 1 {
		t.Errorf("rounds not cleared on approval: %d", n)
	}
}

// An autopilot run under --reviewer usually has no foreman at all. That must be
// default autonomy with the floor intact, not a refusal — but also not a way to
// skip the floor by simply not naming a foreman.
func TestReviewGateWithoutAForemanIsDefaultAutonomyWithTheFloorIntact(t *testing.T) {
	a := gateState(t)
	seedDesign(t, a, "bs-cli", map[string]string{"plan.md": "widen a registry"})
	if code, _, reason := gate(t, a, "bs-cli", nonUIRubric, foreman.Record{ID: "autopilot-reviewer"}); code != 0 {
		t.Fatalf("no foreman must not be a refusal: code %d (%s)", code, reason)
	}

	seedDesign(t, a, "bs-del", map[string]string{"plan.md": "hard delete the orphaned rows"})
	if code, _, _ := gate(t, a, "bs-del", nonUIRubric, foreman.Record{ID: "autopilot-reviewer"}); code != exitFloor {
		t.Errorf("code = %d — no foreman must not mean no floor", code)
	}
}

// A human hold still wins. Autonomy is the default, not the only posture.
func TestReviewGateHonorsAHumanHold(t *testing.T) {
	a := gateState(t)
	seedDesign(t, a, "bs-cli", map[string]string{"plan.md": "widen a registry"})
	held := foreman.Record{ID: "fm-held", Hold: &foreman.Hold{HeldBy: "long", At: foreman.Now()}}
	code, _, reason := gate(t, a, "bs-cli", nonUIRubric, held)
	if code != exitGrant {
		t.Fatalf("code = %d, want exitGrant=%d", code, exitGrant)
	}
	if !strings.Contains(reason, "hold") {
		t.Errorf("reason does not name the hold: %q", reason)
	}
}
