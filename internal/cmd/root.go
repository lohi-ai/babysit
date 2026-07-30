// Package cmd wires the `bbs` cobra command tree. Adding a ported bin is a
// new file here plus a compat symlink; the multicall dispatch in cmd/bbs
// routes `bbs-<name>` invocations to the matching subcommand.
package cmd

import (
	"errors"
	"path/filepath"

	"github.com/spf13/cobra"
)

// errSilent signals an exit-code-1 failure whose message the RunE already
// printed. main maps any non-nil Execute error to exit 1; SilenceErrors keeps
// cobra from printing on top of it.
var errSilent = errors.New("")

// version is the CLI version, injected by release builds via
// -ldflags "-X github.com/reallongnguyen/babysit/internal/cmd.version=X.Y.Z".
// It is deliberately empty in a plain `go build`: a git-clone install (the
// setup-skills path) resolves the VERSION file in the checkout instead, which
// stays correct after a `git pull` without needing a rebuild.
var version string

// resolveVersion prefers the injected value, then the checkout's VERSION file.
// A brew install has no checkout, so it relies on the injected value; "unknown"
// means neither source was available rather than a fabricated number.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if v := readVersion(filepath.Join(babysitDir(), "VERSION")); v != "" {
		return v
	}
	return "unknown"
}

// guardHelp makes `<cmd> --help` print usage instead of running the command.
//
// Every ported bin sets DisableFlagParsing to keep cobra away from arguments
// the bash parsed itself, which also means cobra's own --help handling never
// runs: a leading -h/--help falls through to the action as junk. For the
// commands wrapped here that is not merely unhelpful but destructive — upgrade
// pulls and relinks, analytics-cron dispatches a review, telemetry-log and
// update-check write state. Intercepting the one flag whose entire purpose is
// "tell me what this does without doing it" is a deliberate divergence from the
// bash originals; the rest of the argument contract is untouched.
//
// Commands that already print their own usage for an unrecognized flag (config,
// env, slug, ticket, design, …) are left alone.
func guardHelp(c *cobra.Command) *cobra.Command {
	inner := c.RunE
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
			return cmd.Help()
		}
		return inner(cmd, args)
	}
	return c
}

// NewRootCmd builds the root `bbs` command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "bbs",
		Short:         "babysit CLI",
		Version:       resolveVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("bbs {{.Version}}\n")
	root.AddCommand(
		newConfigCmd(), newEnvCmd(), newSlugCmd(), newTicketCmd(), newQAConfigCmd(),
		newCodexCompetitiveCmd(), newSecretsCmd(), newDesignCmd(), newDashboardCmd(),
		guardHelp(newUpdateCheckCmd()), guardHelp(newUpgradeCmd()),
		guardHelp(newLearningsLogCmd()), guardHelp(newLearningsSearchCmd()),
		guardHelp(newTelemetryLogCmd()), guardHelp(newAnalyticsCronCmd()),
		guardHelp(newAutopilotCmd()),
	)
	return root
}
