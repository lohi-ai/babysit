package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reallongnguyen/babysit/internal/slug"
	"github.com/spf13/cobra"
)

const slugUsage = "usage: bbs-slug [env|home|ticket-home|slug|branch|ticket]"

// newSlugCmd ports bin/bbs-slug as `bbs slug`, matching its subcommands,
// output, and exit codes exactly. Flag parsing is disabled so an unknown
// argument routes to the bash default case (exit 2) instead of cobra's help.
func newSlugCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "slug [env|home|ticket-home|slug|branch|ticket]",
		Short:              "derive slug / branch / ticket from git remote + branch",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// Bash reads only $1, defaulting to "env"; extra args are ignored.
			sub := "env"
			if len(args) > 0 {
				sub = args[0]
			}
			// Derivation (including the env-conflict abort) runs before the
			// dispatch for every subcommand, matching the bash ordering.
			info, err := slug.Resolve()
			// Outside a git repo bin/bbs-slug dies at its unguarded
			// `git worktree list` under `set -euo pipefail`: exit 128, no
			// output, for every subcommand and even when the ticket env vars
			// conflict. Reproduce that before anything else can print.
			if errors.Is(err, slug.ErrNoRepo) {
				os.Exit(128)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return errSilent
			}
			switch sub {
			case "env", "":
				fmt.Printf("SLUG=%s\n", info.Slug)
				fmt.Printf("BRANCH=%s\n", info.Branch)
				fmt.Printf("TICKET=%s\n", info.Ticket)
				fmt.Printf("BABYSIT_PROJECT_HOME=%s\n", info.ProjectHome)
			case "home":
				os.MkdirAll(info.ProjectHome, 0o755)
				fmt.Println(info.ProjectHome)
			case "ticket-home":
				if info.Ticket == "" {
					fmt.Fprintf(os.Stderr, "bbs-slug: no ticket in branch '%s'\n", info.Branch)
					return errSilent
				}
				th := filepath.Join(info.ProjectHome, "tickets", info.Ticket)
				os.MkdirAll(th, 0o755)
				fmt.Println(th)
			case "slug":
				fmt.Println(info.Slug)
			case "branch":
				fmt.Println(info.Branch)
			case "ticket":
				fmt.Println(info.Ticket)
			default:
				fmt.Fprintln(os.Stderr, slugUsage)
				os.Exit(2)
			}
			return nil
		},
	}
}
