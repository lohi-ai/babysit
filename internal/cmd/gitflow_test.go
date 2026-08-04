package cmd

import "testing"

// The derivation table in the ticket is the contract: one `profile:` key sets
// branch mechanics *and* rigor, explicit keys always win, and the four legacy
// profile names keep resolving.
func TestGitFlowFrom(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want gitFlowPolicy
	}{
		{
			"pet", "profile: pet\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "true", "smoke", "low"},
		},
		{
			"startup", "profile: startup\n",
			gitFlowPolicy{"startup", "main", "branch", "pr", "true", "standard", "medium"},
		},
		{
			"enterprise", "profile: enterprise\n",
			gitFlowPolicy{"enterprise", "main", "branch", "pr", "true", "strict", "high"},
		},
		{
			// No profile key: repos predating this keep today's shape.
			"no-profile-defaults-to-startup", "base_branch: main\n",
			gitFlowPolicy{"startup", "main", "branch", "pr", "true", "standard", "medium"},
		},
		{
			"legacy-trunk", "profile: trunk\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "true", "smoke", "low"},
		},
		{
			"legacy-branch-pr", "profile: branch-pr\n",
			gitFlowPolicy{"startup", "main", "branch", "pr", "true", "standard", "medium"},
		},
		{
			// worktree-pr keeps its PRs — the worktree land: local default
			// must not overwrite what the alias asked for.
			"legacy-worktree-pr", "profile: worktree-pr\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "true", "strict", "high"},
		},
		{
			"legacy-worktree-review", "profile: worktree-review\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "local", "true", "strict", "high"},
		},
		{
			"explicit-keys-win", "profile: pet\nmode: branch\nland: pr\npush: false\n",
			gitFlowPolicy{"pet", "main", "branch", "pr", "false", "smoke", "low"},
		},
		{
			// A bare `mode: worktree` still means a locally composed batch.
			"worktree-implies-land-local", "profile: enterprise\nmode: worktree\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "local", "true", "strict", "high"},
		},
		{
			"worktree-explicit-land-pr", "profile: enterprise\nmode: worktree\nland: pr\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "true", "strict", "high"},
		},
		{
			"legacy-ticket-branch-optional", "ticket_branch: optional\n",
			gitFlowPolicy{"startup", "main", "trunk", "pr", "true", "standard", "medium"},
		},
		{
			"legacy-ticket-branch-required", "profile: pet\nticket_branch: required\n",
			gitFlowPolicy{"pet", "main", "branch", "none", "true", "smoke", "low"},
		},
		{
			"comments-and-quotes", "# profile: enterprise\nprofile: 'pet'   # hobby\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "true", "smoke", "low"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := gitFlowFrom(c.yaml, "main")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestGitFlowFromInvalid(t *testing.T) {
	for _, yaml := range []string{
		"profile: hobby\n",
		"profile: pet\nmode: bogus\n",
		"profile: pet\nland: sometimes\n",
		"profile: pet\npush: maybe\n",
	} {
		if _, err := gitFlowFrom(yaml, "main"); err == nil {
			t.Errorf("expected an error for %q", yaml)
		}
	}
}

func TestGitFlowBase(t *testing.T) {
	cases := []struct{ yaml, want string }{
		{"base_branch: develop\n", "develop"},
		{"base_branch: 'release'  # comment\n", "release"},
		{"branches:\n  develop: dev\n", "dev"},
		{"# base_branch: nope\nbranches:\n  develop: dev\nother: x\n", "dev"},
		{"profile: pet\n", ""},
		// An empty `base_branch:` does not shadow the develop fallback. The
		// pre-Go bash returned "" here and stopped; a key with no value is a
		// key the human never answered, so the ladder keeps walking.
		{"base_branch:\nbranches:\n  develop: dev\n", "dev"},
	}
	for _, c := range cases {
		if got := gitFlowBase(c.yaml); got != c.want {
			t.Errorf("gitFlowBase(%q) = %q, want %q", c.yaml, got, c.want)
		}
	}
}
