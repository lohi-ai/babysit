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
			gitFlowPolicy{"pet", "main", "trunk", "none", "review", "true", "smoke", "low"},
		},
		{
			// No profile derives a mode: the work rides the branch the user is
			// standing on, and isolation is asked for per run (--mode).
			"startup", "profile: startup\n",
			gitFlowPolicy{"startup", "main", "trunk", "pr", "review", "true", "standard", "medium"},
		},
		{
			"enterprise", "profile: enterprise\n",
			gitFlowPolicy{"enterprise", "main", "trunk", "pr", "review", "true", "strict", "high"},
		},
		{
			// No profile key means nobody configured this repo, so it behaves
			// like plain git: work rides the current branch, nothing is cut.
			"no-profile-defaults-to-pet", "base_branch: main\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "review", "true", "smoke", "low"},
		},
		{
			"legacy-trunk", "profile: trunk\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "review", "true", "smoke", "low"},
		},
		{
			"legacy-branch-pr", "profile: branch-pr\n",
			gitFlowPolicy{"startup", "main", "branch", "pr", "review", "true", "standard", "medium"},
		},
		{
			// worktree-pr keeps its PRs — an alias pins the shape its name
			// promises, it does not inherit what the profile derives today.
			"legacy-worktree-pr", "profile: worktree-pr\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "review", "true", "strict", "high"},
		},
		{
			"legacy-worktree-review", "profile: worktree-review\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "local", "review", "true", "strict", "high"},
		},
		{
			"explicit-keys-win", "profile: pet\nmode: branch\nland: pr\npush: false\n",
			gitFlowPolicy{"pet", "main", "branch", "pr", "review", "false", "smoke", "low"},
		},
		{
			// The parallel shape, written out by hand: the mode key is the only
			// thing that turns worktrees on, and it does not drag land with it.
			"hand-written-worktree-keeps-profile-land", "profile: enterprise\nmode: worktree\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "review", "true", "strict", "high"},
		},
		{
			"hand-written-worktree-and-land-local", "profile: enterprise\nmode: worktree\nland: local\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "local", "review", "true", "strict", "high"},
		},
		{
			"worktree-explicit-land-pr", "profile: enterprise\nmode: worktree\nland: pr\n",
			gitFlowPolicy{"enterprise", "main", "worktree", "pr", "review", "true", "strict", "high"},
		},
		{
			// A pet repo that wants separable parallel tickets keeps its
			// land: none — worktrees compose onto local base and the push is
			// still the release. Nothing may promote it to a PR flow.
			"pet-worktree-keeps-land-none", "profile: pet\nmode: worktree\n",
			gitFlowPolicy{"pet", "main", "worktree", "none", "review", "true", "smoke", "low"},
		},
		{
			// The documented opt-out, written in full.
			"solo-loop-opt-out", "profile: startup\nmode: branch\nland: pr\n",
			gitFlowPolicy{"startup", "main", "branch", "pr", "review", "true", "standard", "medium"},
		},
		{
			// …and written the short way: the profile's own landing applies.
			"lone-mode-branch-keeps-profile-land", "profile: enterprise\nmode: branch\n",
			gitFlowPolicy{"enterprise", "main", "branch", "pr", "review", "true", "strict", "high"},
		},
		{
			// An unconfigured repo that still writes `mode: branch` keeps the
			// key it wrote and `pet`'s landing: the branch is cut, and the push
			// is still the release. Nothing promotes it to a PR flow.
			"lone-mode-branch-no-profile", "base_branch: main\nmode: branch\n",
			gitFlowPolicy{"pet", "main", "branch", "none", "review", "true", "smoke", "low"},
		},
		{
			"legacy-ticket-branch-optional", "ticket_branch: optional\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "review", "true", "smoke", "low"},
		},
		{
			"legacy-ticket-branch-required", "profile: pet\nticket_branch: required\n",
			gitFlowPolicy{"pet", "main", "branch", "none", "review", "true", "smoke", "low"},
		},
		{
			// finish is opt-in per repo and unset in every preset, so a bare
			// profile never closes a ticket out by itself.
			"finish-land-opt-in", "profile: startup\nfinish: land\n",
			gitFlowPolicy{"startup", "main", "trunk", "pr", "land", "true", "standard", "medium"},
		},
		{
			// land: none is the pet shape where the base branch IS the venue —
			// exactly the repo that wants its finished tickets landed for it.
			"finish-land-under-land-none", "profile: pet\nmode: worktree\nfinish: land\n",
			gitFlowPolicy{"pet", "main", "worktree", "none", "land", "true", "smoke", "low"},
		},
		{
			"comments-and-quotes", "# profile: enterprise\nprofile: 'pet'   # hobby\n",
			gitFlowPolicy{"pet", "main", "trunk", "none", "review", "true", "smoke", "low"},
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
		"profile: pet\nfinish: someday\n",
		// auto_land was folded into finish. Erroring with the rewrite beats an
		// alias (two keys for one decision) and beats silence (a repo that used
		// to land quietly stops landing).
		"profile: pet\nauto_land: true\n",
		"profile: pet\nauto_land: false\n",
		// finish: pr with no PR venue — create-pr BLOCKs under land: none, so
		// the contradiction is named here instead of after a batch has run.
		"profile: pet\nfinish: pr\n",
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

// The mechanical half of "the verifier does not close out". A prompt asking a
// spawned verifier not to push is prose; the policy it reads when it consults
// git-flow says the same thing, and that is what the run acts on.
func TestVerifierClampsFinishAndPush(t *testing.T) {
	p := gitFlowPolicy{Profile: "startup", BaseBranch: "main", Mode: "trunk",
		Land: "pr", Finish: "land", Push: "true", Rigor: "standard", ReviewEffort: "medium"}

	t.Setenv("BABYSIT_VERIFIER", "")
	if got := verifierClamp(p); got != p {
		t.Errorf("clamped outside a verifier: %+v", got)
	}

	t.Setenv("BABYSIT_VERIFIER", "true")
	got := verifierClamp(p)
	if got.Finish != "review" || got.Push != "false" {
		t.Errorf("finish/push = %s/%s, want review/false", got.Finish, got.Push)
	}
	// Everything else is the repo's answer, not the verifier's business.
	if got.BaseBranch != "main" || got.Rigor != "standard" || got.Land != "pr" {
		t.Errorf("clamp touched more than finish/push: %+v", got)
	}
}
