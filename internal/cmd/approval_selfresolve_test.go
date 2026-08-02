package cmd

import (
	"strings"
	"testing"
)

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
