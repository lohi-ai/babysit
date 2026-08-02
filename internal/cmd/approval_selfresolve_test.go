package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// The floor is only as good as the text it reads. A design.md can be bland
// while the prototype it describes is a checkout screen, and design-ui may
// have relocated the spec via pointers.design — both were blind spots that let
// a Stripe prototype auto-approve under an unbounded grant.
func TestDesignTextReadsThePrototypeAndTheDesignPointer(t *testing.T) {
	home := t.TempDir()
	tickets := filepath.Join(home, "tickets", "bs-test")
	if err := os.MkdirAll(tickets, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(tickets, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("design.md", "Adds an upgrade panel. Tokens from DESIGN.md.")
	write("prototype.html", `<button data-stripe-checkout>Pay</button>`)
	write("index.json", `{"pointers":{"design":"spec/real-design.md"}}`)
	if err := os.MkdirAll(filepath.Join(tickets, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("spec/real-design.md", "the invoice total is recomputed on renewal")

	st := ticket.New(identity.Env{ProjectHome: home, Ticket: "bs-test"})
	got := designText(st, "Coverage: AC1 maps to plan step 2")

	for _, want := range []string{"stripe-checkout", "invoice total"} {
		if !strings.Contains(got, want) {
			t.Errorf("design corpus missed %q — floor cannot see it", want)
		}
	}
	if hits := floorHits(got); len(hits) == 0 {
		t.Error("floor cleared a prototype with a Stripe checkout and an invoice")
	}
}

func TestFloorCatchesNonDelegablePaths(t *testing.T) {
	hits := []string{
		"adds a Stripe checkout step to the pricing page",
		"the invoice total is recomputed",
		"gate the route behind the login session",
		"adds a permission check for the admin role",
		"the migration drops the legacy column",
		"users can delete their workspace",
		"stores the API key in the vault",
	}
	for _, s := range hits {
		if got := floorHits(s); len(got) == 0 {
			t.Errorf("floor missed a non-delegable path: %q", s)
		}
	}

	misses := []string{
		"adds a sort control to the ticket table",
		"the empty state gets an illustration",
		"renames the column header and widens it",
		"the author byline moves under the title",
	}
	for _, s := range misses {
		if got := floorHits(s); len(got) > 0 {
			t.Errorf("floor tripped on ordinary UI work: %q hit %v", s, got)
		}
	}
}

// The floor is biased to escalate: a sentence that merely mentions a
// non-delegable noun still escalates, and that is the intended tradeoff.
func TestFloorIsBiasedToEscalate(t *testing.T) {
	if got := floorHits("no payment code is touched by this change"); len(got) == 0 {
		t.Error("floor should trip on a denial that names a money path")
	}
}

// Three words are auth vocabulary in the abstract and presentation vocabulary
// in practice — design tokens, ARIA roles, Claude Code sessions. Matching them
// bare fired the floor on every user-facing ticket, i.e. on exactly the work
// the grant exists to automate. These are the real strings from the
// bs-bfq34gq0 design artifacts.
func TestFloorDoesNotTripOnPresentationVocabulary(t *testing.T) {
	misses := []string{
		"tokens come from web/src/styles; the badge is token-skinned",
		"reuses the existing status and text tokens from DESIGN.md",
		`<div role="tab" aria-selected="true">Plan</div>`,
		`the dialog is role="dialog" with role="img" on the thumbnail`,
		"one worker per Claude Code session, listed under ~/.babysit/sessions/",
		"the sessions column shows which foreman owns the workspace",
	}
	for _, s := range misses {
		if got := floorHits(s); len(got) > 0 {
			t.Errorf("floor tripped on presentation vocabulary: %q hit %v", s, got)
		}
	}
}

// Precision, not a weaker floor: the genuine auth paths behind those same
// words still escalate, spelled the way an auth path is actually written.
func TestFloorStillCatchesTheGenuineAuthPaths(t *testing.T) {
	hits := []string{
		"the endpoint authenticates on a bearer token",
		"stores the access token in localStorage",
		"rotates the refresh token on every request",
		"the session cookie is now SameSite=None",
		"lets an operator change a user role from the members table",
		"gates the export behind an admin role check",
		"adds role-based access to the settings page",
		"the pricing page gains an annual toggle",
		"an operator can update price on the plan row",
	}
	for _, s := range hits {
		if got := floorHits(s); len(got) == 0 {
			t.Errorf("floor missed a genuine non-delegable path: %q", s)
		}
	}
}

func TestParseRubricAcceptsMarkdown(t *testing.T) {
	rubric := `
- **Coverage** — AC1 maps to plan step 2, AC2 to the empty-state section
- **Host-page consistency**: borrows the toolbar from tickets/board
* Reuse — uses <DataTable> and <Badge> from the shared kit
Prototype inspected: read prototype.html, tokens match DESIGN.md scale
  Scope — nothing beyond the ticket wording
`
	filled, missing := parseRubric(rubric)
	if len(missing) != 0 {
		t.Fatalf("unfilled lines in a complete rubric: %v", missing)
	}
	if !strings.Contains(filled["coverage"], "AC1") {
		t.Errorf("coverage evidence lost: %q", filled["coverage"])
	}
	if !strings.Contains(filled["host-page consistency"], "toolbar") {
		t.Errorf("host-page evidence lost: %q", filled["host-page consistency"])
	}
}

func TestParseRubricRejectsPlaceholders(t *testing.T) {
	rubric := `
Coverage: AC1 maps to plan step 2 and AC2 to the list section
Host-page consistency: n/a
Reuse: -
Prototype inspected: ok
Scope: nothing beyond the ticket wording
`
	_, missing := parseRubric(rubric)
	for _, want := range []string{"host-page consistency", "reuse", "prototype inspected"} {
		found := false
		for _, m := range missing {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Errorf("placeholder passed as evidence for %q (missing=%v)", want, missing)
		}
	}
}

func TestParseRubricReportsEveryMissingLine(t *testing.T) {
	_, missing := parseRubric("Coverage: AC1 maps to plan step 2 of the plan")
	if len(missing) != 4 {
		t.Fatalf("want 4 unfilled lines, got %v", missing)
	}
}
