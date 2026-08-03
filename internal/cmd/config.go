package cmd

import (
	"fmt"
	"os"
	"regexp"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/spf13/cobra"
)

var keyRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

const (
	// Two files share the basename config.yaml: the global one this command
	// owns, and <repo>/.babysit/config.yaml. They are reached by different
	// commands rather than one scope flag, so the usage line has to say which
	// is which — otherwise the basename is the only clue.
	configUsage = "Usage: bbs-config {get|set|list} [key] [value]\n" +
		"  Reads ~/.babysit/config.yaml (global). For a repo's own\n" +
		"  .babysit/config.yaml, use: bbs config repo"
	badKeyMsg = "Error: key must contain only alphanumeric characters and underscores"
)

// newConfigCmd ports bin/bbs-config as `bbs config`, matching its get/set/list
// behavior and exit codes exactly.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "read/write babysit config: global, workspace registry, per-repo",
		// Bare `config` or an unknown subcommand mirrors bin/bbs-config's
		// default case: print usage to stdout, exit 1.
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println(retarget(configUsage))
			return errSilent
		},
	}
	cmd.AddCommand(newConfigGetCmd(), newConfigSetCmd(), newConfigListCmd(),
		newConfigWorkspaceCmd(), newConfigRepoCmd())
	return cmd
}

// Three files answer to the name "config", so all three hang off `bbs config`:
// the global ~/.babysit/config.yaml (get/set/list), the machine-local registry
// under ~/.babysit/workspaces/ (workspace), and the committed
// <repo>/.babysit/config.yaml (repo). DisableFlagParsing on both because
// add-repo's --git-url/--path/--role are parsed by hand downstream.
func newConfigWorkspaceCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "workspace",
		Short:              "multi-repo registry: which repos belong to one product",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWorkspace(args)
		},
	}
}

func newConfigRepoCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "repo",
		Short:              "read/write this repo's .babysit/config.yaml",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWorkspace(append([]string{"config"}, args...))
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "read a config value",
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				fmt.Fprintln(os.Stderr, retarget("Usage: bbs-config get <key>"))
				return errSilent
			}
			key := args[0]
			if !keyRe.MatchString(key) {
				fmt.Fprintln(os.Stderr, badKeyMsg)
				return errSilent
			}
			if val, found := config.Get(key); found {
				fmt.Println(val)
			}
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "write a config value",
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, retarget("Usage: bbs-config set <key> <value>"))
				return errSilent
			}
			key, value := args[0], args[1]
			if !keyRe.MatchString(key) {
				fmt.Fprintln(os.Stderr, badKeyMsg)
				return errSilent
			}
			if err := config.Set(key, value); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return errSilent
			}
			return nil
		},
	}
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "show all config",
		RunE: func(_ *cobra.Command, _ []string) error {
			os.Stdout.Write(config.List())
			return nil
		},
	}
}
