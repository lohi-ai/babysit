package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/reallongnguyen/babysit/internal/env"
	"github.com/spf13/cobra"
)

// envUsage must stay byte-identical to the usage() heredoc in bin/bbs-env.
const envUsage = `bbs-env — env resolution for babysit skills

Usage:
  bbs-env [--env-file <path>] [--app <app>] resolve [--prefix] <varname> [<varname>...]
                                              Resolve first set var (priority order)
  bbs-env [--env-file <path>] [--app <app>] is-set [--prefix] <varname>
                                              Check if var is set (yes/no)
  bbs-env list-prefix <prefix>                List all vars with given prefix (LOCAL_, STG_, etc.)
  bbs-env prompt <varname> [<varname>...]     Prompt user for missing vars (developer mode)

Auto-loading:
  Auto-loads .env.base files from config/<app>/ before resolving.
  Priority: --env-file flag > --app flag > BABYSIT_APP env > GT_RIG env > CWD detection.
  If no app is detected, all config/*/.env.base files are loaded.
  Shell env vars always take priority over .env file values.

Examples:
  bbs-env resolve --prefix QA_EMAIL
  bbs-env --app my-app resolve --prefix QA_EMAIL
  bbs-env --env-file ./my.env resolve DB_URL
  bbs-env is-set --prefix DB_URL
`

// newEnvCmd ports bin/bbs-env as `bbs env`, matching its subcommands, output
// bytes, and exit codes exactly.
//
// Flag parsing is disabled and done by hand: bin/bbs-env accepts --env-file /
// --app only *before* the subcommand and treats --prefix as a positional
// marker, and its error text and exit codes are part of the contract. Letting
// cobra/pflag near the arguments would replace all of that with cobra's own.
func newEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "env",
		Short:              "env resolution for babysit skills",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runEnv(args)
		},
	}
}

func runEnv(args []string) error {
	// Global env flags precede the subcommand; each overrides its env var for
	// the rest of the run, as assigning BABYSIT_* does in the bash.
	envFile := os.Getenv("BABYSIT_ENV_FILE")
	app := os.Getenv("BABYSIT_APP")
flags:
	for len(args) > 0 {
		switch args[0] {
		case "--env-file":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "bbs-env: --env-file requires a path")
				return errSilent
			}
			envFile, args = args[1], args[2:]
		case "--app":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "bbs-env: --app requires an app name")
				return errSilent
			}
			app, args = args[1], args[2:]
		default:
			break flags
		}
	}

	var sub string
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}

	// The bash auto-loads before dispatching, so every subcommand — including
	// help and the unknown-subcommand path — sees the loaded env.
	env.LoadProject(app, envFile)

	switch sub {
	case "resolve":
		return runEnvResolve(args)
	case "is-set":
		return runEnvIsSet(args)
	case "list-prefix":
		return runEnvListPrefix(args)
	case "prompt":
		return runEnvPrompt(args)
	case "help", "--help", "-h", "":
		fmt.Print(envUsage)
		// A bare invocation prints usage but still fails.
		if sub == "" {
			return errSilent
		}
		return nil
	default:
		fmt.Fprintf(os.Stderr, "bbs-env: unknown subcommand '%s'\n", sub)
		fmt.Fprint(os.Stderr, envUsage)
		return errSilent
	}
}

// takePrefixFlag strips a leading --prefix marker, reporting whether it was
// present.
func takePrefixFlag(args []string) ([]string, bool) {
	if len(args) > 0 && args[0] == "--prefix" {
		return args[1:], true
	}
	return args, false
}

func runEnvResolve(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "bbs-env resolve: requires at least one varname")
		return errSilent
	}
	args, prefix := takePrefixFlag(args)
	val, ok := env.Resolve(args, prefix)
	if !ok {
		// Nothing resolved: exit 1 silently.
		return errSilent
	}
	fmt.Println(val)
	return nil
}

func runEnvIsSet(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "bbs-env is-set: requires a varname")
		return errSilent
	}
	args, prefix := takePrefixFlag(args)
	// The bash only ever inspects "$1"; a bare `is-set --prefix` clears the
	// guard and then tests the empty name (always "no").
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	if env.IsSet(name, prefix) {
		fmt.Println("yes")
	} else {
		fmt.Println("no")
	}
	return nil
}

func runEnvListPrefix(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "bbs-env list-prefix: requires a prefix")
		return errSilent
	}
	for _, kv := range env.ListPrefix(args[0]) {
		fmt.Println(kv)
	}
	return nil
}

func runEnvPrompt(args []string) error {
	if missing := env.Missing(args); len(missing) > 0 {
		fmt.Println("MISSING: " + strings.Join(missing, " "))
	}
	return nil
}
