// Package cmd wires the `bbs` cobra command tree. Adding a ported bin is a
// new file here plus a compat symlink; the multicall dispatch in cmd/bbs
// routes `bbs-<name>` invocations to the matching subcommand.
package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

// errSilent signals an exit-code-1 failure whose message the RunE already
// printed. main maps any non-nil Execute error to exit 1; SilenceErrors keeps
// cobra from printing on top of it.
var errSilent = errors.New("")

// NewRootCmd builds the root `bbs` command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "bbs",
		Short:         "babysit CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newConfigCmd())
	return root
}
