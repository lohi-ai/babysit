package cmd

import "testing"

// The derivation table is the contract: one `profile:` key sets the landing
// venue and QA rigor — never a mode, which is a per-run choice — explicit keys
// always win, and the four legacy profile names keep resolving.
func TestGitFlowFrom(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want gitFlowPolicy
	}{
		{
			"pet", "profile: pet\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "false", "true", "smoke", "low"},
		},
		{
			// No profile derives a mode: the work rides the branch the user is
			// standing on, and isolation is asked for per run (--mode).
			"startup", "profile: startup\n",
			gitFlowPolicy{"startup", "main", "trunk", "pr", "false", "true", "standard", "medium"},
		},
		{
			"enterprise", "profile: enterprise\n",
			gitFlowPolicy{"enterprise", "main", "trunk", "pr", "false", "true", "strict", "high"},
		},
		{
			// No profile key means nobody configured this repo, so it behaves
			// like plain git: work rides the current branch, nothing is cut.
			"no-profile-defaults-to-pet", "base_branch: main\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "false", "true", "smoke", "low"},
		},
		{
			"legacy-trunk", "profile: trunk\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "false", "true", "smoke", "low"},
		},
		{
			"legacy-branch-pr", "profile: branch-pr\n",
			gitFlowPolicy{"startup", "main", "branch", "pr", "false", "true", "standard", "medium"},
		},
		{
			// worktree-pr keeps its PRs — an alias pins the shape its name
			// promises, it does not inherit what the profile derives today.
			"legacy-worktree-pr", "profile: worktree-pr\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "false", "true", "strict", "high"},
		},
		{
			"legacy-worktree-review", "profile: worktree-review\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "local", "false", "true", "strict", "high"},
		},
		{
			"explicit-keys-win", "profile: pet\nmode: branch\nland: pr\npush: false\n",
			gitFlowPolicy{"pet", "main", "branch", "pr", "false", "false", "smoke", "low"},
		},
		{
			// The parallel shape, written out by hand: the mode key is the only
			// thing that turns worktrees on, and it does not drag land with it.
			"hand-written-worktree-keeps-profile-land", "profile: enterprise\nmode: worktree\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "false", "true", "strict", "high"},
		},
		{
			"hand-written-worktree-and-land-local", "profile: enterprise\nmode: worktree\nland: local\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "local", "false", "true", "strict", "high"},
		},
		{
			"worktree-explicit-land-pr", "profile: enterprise\nmode: worktree\nland: pr\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "false", "true", "strict", "high"},
		},
		{
			// A pet repo that wants separable parallel tickets keeps its
			// land: none — worktrees compose onto local base and the push is
			// still the release. Nothing may promote it to a PR flow.
			"pet-worktree-keeps-land-none", "profile: pet\nmode: worktree\n",
			gitFlowPolicy{"pet", "main", "worktree", "none", "false", "true", "smoke", "low"},
		},
		{
			// The documented opt-out, written in full.
			"solo-loop-opt-out", "profile: startup\nmode: branch\nland: pr\n",
			gitFlowPolicy{"startup", "main", "branch", "pr", "false", "true", "standard", "medium"},
		},
		{
			// …and written the short way: the profile's own landing applies.
			"lone-mode-branch-keeps-profile-land", "profile: enterprise\nmode: branch\n",
			gitFlowPolicy{"enterprise", "main", "branch", "pr", "false", "true", "strict", "high"},
		},
		{
			// An unconfigured repo that still writes `mode: branch` keeps the
			// key it wrote and `pet`'s landing: the branch is cut, and the push
			// is still the release. Nothing promotes it to a PR flow.
			"lone-mode-branch-no-profile", "base_branch: main\nmode: branch\n",
			gitFlowPolicy{"pet", "main", "branch", "none", "false", "true", "smoke", "low"},
		},
		{
			"legacy-ticket-branch-optional", "ticket_branch: optional\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "false", "true", "smoke", "low"},
		},
		{
			"legacy-ticket-branch-required", "profile: pet\nticket_branch: required\n",
			gitFlowPolicy{"pet", "main", "branch", "none", "false", "true", "smoke", "low"},
		},
		{
			// auto_land is opt-in per repo and off in every preset, so a bare
			// profile never lands anything by itself.
			"auto-land-opt-in", "profile: startup\nauto_land: true\n",
			gitFlowPolicy{"startup", "main", "trunk", "pr", "true", "true", "standard", "medium"},
		},
		{
			// land: none is the pet shape where the base branch IS the venue —
			// exactly the repo that wants its finished tickets landed for it.
			"auto-land-under-land-none", "profile: pet\nmode: worktree\nauto_land: true\n",
			gitFlowPolicy{"pet", "main", "worktree", "none", "true", "true", "smoke", "low"},
		},
		{
			"comments-and-quotes", "# profile: enterprise\nprofile: 'pet'   # hobby\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "false", "true", "smoke", "low"},
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
		"profile: pet\nauto_land: sometimes\n",
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
