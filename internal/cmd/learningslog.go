package cmd

import (
	"fmt"
	"os"

	"github.com/reallongnguyen/babysit/internal/learnings"
	"github.com/spf13/cobra"
)

const learningsLogUsage = "usage: bbs-learnings-log decision --skill S --type T --choice C [--rationale R] [--ticket T] [--workflow W] [--state JSON]"

// newLearningsLogCmd ports bin/bbs-learnings-log as `bbs learnings-log`.
// Flag parsing is disabled and done by hand (bbs-env pattern): the bash's
// error text and exit codes — 2 on usage errors, 0 always otherwise, logging
// never fails the caller — are the contract.
func newLearningsLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "learnings-log",
		Short:              "append a decision/learning event to the analytics audit trail",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runLearningsLog(args)
		},
	}
}

func runLearningsLog(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, learningsLogUsage)
		os.Exit(2)
	}
	sub, args := args[0], args[1:]
	if sub != "decision" {
		fmt.Fprintf(os.Stderr, "bbs-learnings-log: unknown subcommand '%s'\n", sub)
		os.Exit(2)
	}

	var skill, dtype, choice, rationale, ticket, workflow, state string
	for len(args) > 0 {
		var dst *string
		switch args[0] {
		case "--skill":
			dst = &skill
		case "--type":
			dst = &dtype
		case "--choice":
			dst = &choice
		case "--rationale":
			dst = &rationale
		case "--ticket":
			dst = &ticket
		case "--workflow":
			dst = &workflow
		case "--state":
			dst = &state
		default: // unknown args are silently skipped
			args = args[1:]
			continue
		}
		if len(args) < 2 {
			// bash: `X="$2"` under set -u aborts with an unbound-variable
			// message naming $0 and a line number; exit 1 is the contract,
			// the $0-dependent stderr spew is not (bbs-env precedent).
			os.Exit(1)
		}
		*dst, args = args[1], args[2:]
	}

	if skill == "" || dtype == "" || choice == "" {
		fmt.Fprintln(os.Stderr, "decision: --skill, --type, --choice required")
		os.Exit(2)
	}
	switch dtype {
	case "mechanical", "taste", "user_challenge":
	default:
		fmt.Fprintln(os.Stderr, "decision: --type must be mechanical|taste|user_challenge")
		os.Exit(2)
	}

	line := fmt.Sprintf(`{"v":1,"ts":"%s","skill":"%s","type":"%s","choice":"%s","rationale":"%s","ticket":"%s","workflow":"%s","state":"%s"}`+"\n",
		learnings.Timestamp(),
		learnings.JSONSafe(skill),
		learnings.JSONSafe(dtype),
		learnings.JSONSafe(choice),
		learnings.JSONSafe(rationale),
		learnings.JSONSafe(ticket),
		learnings.JSONSafe(workflow),
		learnings.JSONSafe(state),
	)
	learnings.Append(learnings.AnalyticsDir(), line)
	return nil
}
