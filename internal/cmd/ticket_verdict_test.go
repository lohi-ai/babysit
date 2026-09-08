package cmd

import (
	"os"
	"testing"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// liveHollowVerdict is verbatim what a spawned verifier persisted for
// bs-ho0yw43e on 2026-08-29: a real review, a confident PASS, and no STATUS:
// line — so every gate read the skill as unrun and the run stalled at the push
// gate with nobody at the pane. The write guard exists for exactly this body.
const liveHollowVerdict = `# review-pr — bs-ho0yw43e

VERDICT: PASS (with 1 finding, fixed)
Effort: medium. Scope: ` + "`git diff HEAD~1`" + ` — slugify.py, test_slugify.py.
`

// TestTheWriteGuardAgreesWithTheGateItProtects pins the one invariant that makes
// the guard worth having: a body set-verdict accepts is a body verdict-status
// can read a status out of. If the two ever disagree, set-verdict is back to
// minting verdicts that look done on disk and read as none at the gate.
func TestTheWriteGuardAgreesWithTheGateItProtects(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // what verdictStatus should report
	}{
		{"plain done", "STATUS: DONE\n", "DONE"},
		{"status after a heading", "# qa — t\n\nSTATUS: DONE_WITH_CONCERNS\nnotes\n", "DONE_WITH_CONCERNS"},
		{"blocked", "STATUS: BLOCKED\nreason\n", "BLOCKED"},
		{"needs context", "STATUS: NEEDS_CONTEXT\n", "NEEDS_CONTEXT"},
		{"live hollow verdict", liveHollowVerdict, "none"},
		{"verdict prose is not a status", "VERDICT: PASS\n", "none"},
		{"indented status does not count", "  STATUS: DONE\n", "none"},
		{"unknown token", "STATUS: PASS\n", "none"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := ticket.New(identity.Env{ProjectHome: t.TempDir(), Ticket: "t"})
			st.EnsureDirs()
			if err := os.WriteFile(st.VerdictPath("qa"), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got := verdictStatus(st, "qa")
			if got != tc.want {
				t.Fatalf("verdictStatus = %q, want %q", got, tc.want)
			}
			// The guard admits a body iff the gate can read a status from it.
			if accepted, readable := bodyHasStatus([]byte(tc.body)), got != "none"; accepted != readable {
				t.Fatalf("guard accepts=%v but gate readable=%v (status %q) — the guard would let a hollow verdict through, or block a good one", accepted, readable, got)
			}
		})
	}
}
