package cmd

import "testing"

// AGENT_ROLE is read by every skill in the pack, so adding a value to it is the
// one change here that can break work it never touched. `orca` is additive by
// construction: `developer` is the only role any code branches on, so a fourth
// name lands in the same bucket `mayor` and `scanner` already sit in. This test
// exists to make that an assertion rather than an observation — the day
// someone adds a second role comparison, it fails here instead of in a batch.
func TestAgentRoleOrcaIsAdditive(t *testing.T) {
	for _, tc := range []struct {
		name, agentRole, gtRole, want string
	}{
		{"unset is developer", "", "", "developer"},
		{"legacy GT_ROLE still honored", "", "mayor", "mayor"},
		{"AGENT_ROLE wins over GT_ROLE", "dashboard", "mayor", "dashboard"},
		{"orca is carried through like any other role", "orca", "", "orca"},
		{"an unknown role is not rewritten", "somebody-elses-orchestrator", "", "somebody-elses-orchestrator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT_ROLE", tc.agentRole)
			t.Setenv("GT_ROLE", tc.gtRole)
			if got := actorRole(); got != tc.want {
				t.Errorf("actorRole() = %q, want %q", got, tc.want)
			}
		})
	}
}
