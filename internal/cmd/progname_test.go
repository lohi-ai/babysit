package cmd

import (
	"os"
	"testing"
)

// retarget's contract has two halves, and both are load-bearing:
//
//   - Invoked as plain `bbs`, help text must name commands the way every skill
//     and doc now calls them (`bbs ticket`), because a Homebrew install ships
//     only the bbs-config and bbs-env aliases — telling that user to run
//     `bbs-ticket` hands them a command that is not on their PATH.
//   - Invoked through a `bbs-<sub>` compat symlink it must return the text
//     byte-for-byte unchanged, because the differential harnesses in tests/
//     drive those symlinks and diff against frozen bash oracles.
func TestRetarget(t *testing.T) {
	orig := os.Args[0]
	t.Cleanup(func() { os.Args[0] = orig })

	t.Run("space form rewrites commands", func(t *testing.T) {
		os.Args[0] = "/usr/local/bin/bbs"
		cases := []struct{ in, want string }{
			{"usage: bbs-ticket path <kind>", "usage: bbs ticket path <kind>"},
			{"bbs-env resolve: requires a varname", "bbs env resolve: requires a varname"},
			// Adjacent matches share a boundary char — the fixed-point loop
			// exists so the second one is not skipped.
			{"run bbs-ticket reset-base; bbs-ticket set-status done", "run bbs ticket reset-base; bbs ticket set-status done"},
			// Not commands: lock/state identifiers that merely look like aliases.
			{"held by bbs-qa-lease", "held by bbs-qa-lease"},
			{"session bbs-serving is live", "session bbs-serving is live"},
			{"lock bbs-merge-base", "lock bbs-merge-base"},
			// A filesystem path is not an invocation.
			{"run bin/bbs-codex-competitive", "run bin/bbs-codex-competitive"},
			// Longest-alternative-first: qa-config must not degrade to config.
			{"bbs-qa-config probe", "bbs qa-config probe"},
			{"bbs-learnings-search --limit 5", "bbs learnings-search --limit 5"},
			// Format verbs must survive untouched.
			{"bbs-ticket path: %s: unknown kind\n", "bbs ticket path: %s: unknown kind\n"},
		}
		for _, c := range cases {
			if got := retarget(c.in); got != c.want {
				t.Errorf("retarget(%q)\n got: %q\nwant: %q", c.in, got, c.want)
			}
		}
	})

	t.Run("alias form is byte-identical", func(t *testing.T) {
		for _, argv0 := range []string{"/home/u/.claude/bbs-ticket", "bbs-env"} {
			os.Args[0] = argv0
			for _, in := range []string{
				"usage: bbs-ticket path <kind>",
				"bbs-env resolve: requires a varname",
				"run bbs-ticket reset-base; bbs-ticket set-status done",
			} {
				if got := retarget(in); got != in {
					t.Errorf("argv0=%s: retarget(%q) = %q, want unchanged", argv0, in, got)
				}
			}
		}
	})
}
