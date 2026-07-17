package cmd

import (
	"fmt"
	"os"

	"github.com/reallongnguyen/babysit/internal/learnings"
	"github.com/spf13/cobra"
)

// newLearningsSearchCmd ports bin/bbs-learnings-search as
// `bbs learnings-search`. Hand parsing for the same reason as learnings-log:
// exit codes (0 on every quiet path, 2 on unknown flags) and error bytes are
// the contract, and search must never fail the caller.
func newLearningsSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "learnings-search",
		Short:              "query the decisions.jsonl audit trail",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runLearningsSearch(args)
		},
	}
}

func runLearningsSearch(args []string) error {
	limit := "10"
	crossProject := false
	query := ""
	for len(args) > 0 {
		switch {
		case args[0] == "--limit":
			if len(args) < 2 {
				os.Exit(1) // bash set -u unbound $2; exit code is the contract, stderr spew is not
			}
			limit, args = args[1], args[2:]
		case args[0] == "--cross-project":
			crossProject, args = true, args[1:]
		case len(args[0]) > 0 && args[0][0] == '-':
			fmt.Fprintf(os.Stderr, "bbs-learnings-search: unknown flag '%s'\n", args[0])
			os.Exit(2)
		default: // positional QUERY — last one wins
			query, args = args[0], args[1:]
		}
	}

	content, ok := learnings.ReadStore(learnings.AnalyticsDir())
	if !ok {
		return nil
	}

	slug := ""
	if !crossProject {
		slug = learnings.ProjectSlug()
	}

	if query != "" {
		if content, ok = learnings.Grep(content, query); !ok {
			return nil
		}
	}
	if slug != "" {
		if content, ok = learnings.Grep(content, slug); !ok {
			return nil
		}
	}

	if content != "" {
		out, errMsg := learnings.TailN(content, limit)
		if errMsg != "" {
			fmt.Fprintln(os.Stderr, errMsg)
		}
		fmt.Print(out) // no trailing newline — printf '%s' | tail
	}
	return nil
}
