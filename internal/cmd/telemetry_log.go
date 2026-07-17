package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/reallongnguyen/babysit/internal/telemetry"
	"github.com/spf13/cobra"
)

// newTelemetryLogCmd ports bin/bbs-telemetry-log as `bbs telemetry-log`.
//
// Flag parsing is disabled and hand-rolled for the same reason as `bbs env`:
// the bash's grammar is the contract, and cobra/pflag would replace it. In
// particular the bash silently ignores unknown arguments (`*) shift ;;`)
// rather than erroring, and lets a flag steal the following token whatever it
// looks like — so `--skill --duration` sets skill to the literal "--duration".
func newTelemetryLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "telemetry-log",
		Short:              "append a telemetry event to local JSONL",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runTelemetryLog(args)
		},
	}
}

// firstNonEmptyEnv returns the first set-and-non-empty env var, mirroring a
// chain of ${A:-${B:-…}} defaults.
func firstNonEmptyEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

func runTelemetryLog(args []string) error {
	// Resolved first, mirroring the bash's header (line 22), where an unset HOME
	// aborts under `set -u` before any flag is read. Only the abort is load-
	// bearing, not its position: it and BUG 2 both exit 1 with empty stdout,
	// non-empty stderr and no row, so their order is unobservable (mutation-
	// tested). Same documented stderr caveat as BUG 2.
	dirs, err := telemetry.ResolveDirs(os.Args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "bbs-telemetry-log: %v\n", err)
		return errSilent
	}

	e := telemetry.Event{
		// Defaults as assigned at the top of the bash.
		Outcome:    "unknown",
		UsedBrowse: "false",
		EventType:  "skill_run",
	}

	// The bash reads "$2" under `set -u`, so a value-taking flag in final
	// position aborts the whole script before any tier check or write.
	//
	// BUG 2 (replicated): that means exit 1 — directly contradicting the
	// script's own header ("telemetry must never exit non-zero") and the
	// `set -uo pipefail` (no -e) choice made to guarantee it.
	//
	// The bash's stderr here is a line-numbered interpreter diagnostic
	// ("…/bbs-telemetry-log: line 43: $2: unbound variable") that a compiled
	// binary cannot reproduce byte-for-byte. Documented divergence: the exit
	// code, stdout and the absence of any write are reproduced; the stderr
	// text is only guaranteed non-empty.
	take := func(rest []string) (string, []string, error) {
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "bbs-telemetry-log: $2: unbound variable")
			return "", nil, errSilent
		}
		return rest[1], rest[2:], nil
	}

	var v string
	for len(args) > 0 {
		switch args[0] {
		case "--skill":
			v, args, err = take(args)
			e.Skill = v
		case "--duration":
			v, args, err = take(args)
			e.Duration = v
		case "--outcome":
			v, args, err = take(args)
			e.Outcome = v
		case "--used-browse":
			v, args, err = take(args)
			e.UsedBrowse = v
		case "--session-id":
			v, args, err = take(args)
			e.SessionID = v
		case "--error-class":
			v, args, err = take(args)
			e.ErrorClass = v
		case "--error-message":
			v, args, err = take(args)
			e.ErrorMessage = v
		case "--failed-step":
			v, args, err = take(args)
			e.FailedStep = v
		case "--event-type":
			v, args, err = take(args)
			e.EventType = v
		case "--invoker":
			v, args, err = take(args)
			e.Invoker = v
		default:
			args = args[1:] // unknown arg: silently skipped
		}
		if err != nil {
			return err
		}
	}

	if e.Invoker == "" {
		e.Invoker = firstNonEmptyEnv("AGENT_ROLE", "GT_ROLE", "INVOKER_ENV", "BABYSIT_INVOKER")
	}
	if e.Invoker == "" {
		e.Invoker = "developer"
	}

	if telemetry.Tier(dirs) == "off" {
		telemetry.RemovePending(dirs, e.SessionID)
		return nil
	}

	telemetry.FinalizePending(dirs, e.SessionID)

	if stderr := telemetry.Append(dirs, e, time.Now()); stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	return nil
}
