package cmd

import (
	"fmt"
	"os"

	"github.com/reallongnguyen/babysit/internal/qaconfig"
	"github.com/reallongnguyen/babysit/internal/workspace"
	"github.com/spf13/cobra"
)

// The registry workspace, not the cmux one and not the worktree pool. Output
// here always names the workspace ("workspace acme") so a reader can tell
// which of the three is meant without guessing; `bbs foreman` says "cmux
// workspace" for its own, and the per-ticket pool is only ever called a
// worktree.
const workspaceUsage = `Usage:
  bbs workspace list
  bbs workspace show [<name>]
  bbs workspace create <name>
  bbs workspace add-repo <name> --git-url <url> [--path <dir>] [--role <fe|be|shared>]
  bbs workspace config show
  bbs workspace config get <key>
  bbs workspace config set <key> <value>

'config' reads and writes <repo>/.babysit/config.yaml for the current repo.
The global ~/.babysit/config.yaml is a different file with a different
command: bbs config.
`

func newWorkspaceCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "workspace",
		Short:              "multi-repo registry: which repos belong to one product",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWorkspace(args)
		},
	}
}

func runWorkspace(args []string) error {
	if err := dispatchWorkspace(args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
	return nil
}

func dispatchWorkspace(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, workspaceUsage)
		os.Exit(2)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return workspaceList()
	case "show":
		return workspaceShow(rest)
	case "create":
		return workspaceCreate(rest)
	case "add-repo":
		return workspaceAddRepo(rest)
	case "config":
		return workspaceConfig(rest)
	}
	fmt.Fprint(os.Stderr, workspaceUsage)
	os.Exit(2)
	return nil
}

func workspaceList() error {
	all := workspace.List()
	if len(all) == 0 {
		fmt.Println("no workspaces registered")
		return nil
	}
	for _, w := range all {
		fmt.Printf("workspace %s (%d repos)\n", w.Name, len(w.Repos))
	}
	return nil
}

// workspaceShow prints one workspace's repos. With no name it reports the
// current repo's membership, which is also where a stale harness_version
// surfaces.
func workspaceShow(args []string) error {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		top := qaconfig.RepoToplevel()
		if top == "" {
			return fmt.Errorf("not in a git repo — pass a workspace name")
		}
		cfg, present, err := workspace.LoadRepoConfig(top)
		if err != nil {
			return err
		}
		if !present {
			fmt.Printf("%s: no .babysit/config.yaml — not a workspace member\n", top)
			return nil
		}
		fmt.Printf("repo:      %s\n", top)
		fmt.Printf("workspace: %s\n", cfg.Workspace)
		fmt.Printf("harness:   %s\n", harnessDisplay(cfg))
		if cfg.RepoType != "" {
			fmt.Printf("repo_type: %s\n", cfg.RepoType)
		}
		if r := workspace.NewResolver(top, originURL(top)); r.Err() != nil {
			return r.Err()
		}
		name = cfg.Workspace
	}
	w, err := workspace.Load(name)
	if err != nil {
		return err
	}
	fmt.Printf("workspace %s\n", w.Name)
	for _, r := range w.Repos {
		path := r.Path
		if path == "" {
			path = "(not on this machine)"
		}
		role := ""
		if r.Role != "" {
			role = "  role=" + r.Role
		}
		fmt.Printf("  %s  %s%s\n", r.GitURL, path, role)
	}
	return nil
}

// harnessDisplay renders harness_version for humans. Null is the ordinary
// state — every repo configured before setup-project learned to write it — so
// it reads as plain "not set", never as a warning.
func harnessDisplay(cfg workspace.RepoConfig) string {
	if cfg.HarnessVersion == nil || *cfg.HarnessVersion == "" {
		return "not set (run bbs:setup-project to record it)"
	}
	if cur := resolveVersion(); cfg.Stale(cur) {
		return *cfg.HarnessVersion + " (stale — current is " + cur + ")"
	}
	return *cfg.HarnessVersion
}

func workspaceCreate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bbs workspace create <name>")
	}
	if err := workspace.Create(args[0]); err != nil {
		return err
	}
	fmt.Printf("workspace %s ready (%s)\n", args[0], workspace.Path(args[0]))
	return nil
}

func workspaceAddRepo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bbs workspace add-repo <name> --git-url <url> [--path <dir>] [--role <role>]")
	}
	name, rest := args[0], args[1:]
	var r workspace.Repo
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--git-url":
			r.GitURL, i = valueAt(rest, i), i+1
		case "--path":
			r.Path, i = valueAt(rest, i), i+1
		case "--role":
			r.Role, i = valueAt(rest, i), i+1
		}
	}
	if r.GitURL == "" {
		return fmt.Errorf("add-repo needs --git-url (the machine-independent half of a repo's identity)")
	}
	if err := workspace.AddRepo(name, r); err != nil {
		return err
	}
	fmt.Printf("workspace %s: added %s\n", name, r.GitURL)
	return nil
}

// workspaceConfig is the repo-scoped counterpart of `bbs config`. They are
// separate commands rather than one scope-aware command because `bbs config`
// is a byte-pinned port of bin/bbs-config and its contract is not ours to
// widen.
func workspaceConfig(args []string) error {
	top := qaconfig.RepoToplevel()
	if top == "" {
		return fmt.Errorf("not in a git repo")
	}
	if len(args) == 0 {
		args = []string{"show"}
	}
	cfg, present, err := workspace.LoadRepoConfig(top)
	if err != nil {
		return err
	}
	switch args[0] {
	case "show":
		if !present {
			fmt.Printf("%s: no config.yaml\n", workspace.RepoConfigPath(top))
			return nil
		}
		b, _ := os.ReadFile(workspace.RepoConfigPath(top))
		os.Stdout.Write(b)
		return nil
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: bbs workspace config get <key>")
		}
		if v, ok := repoConfigField(cfg, args[1]); ok {
			fmt.Println(v)
			return nil
		}
		return fmt.Errorf("unknown key %q (workspace|harness_version|name|description|repo_type)", args[1])
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: bbs workspace config set <key> <value>")
		}
		if err := setRepoConfigField(&cfg, args[1], args[2]); err != nil {
			return err
		}
		if err := workspace.SaveRepoConfig(top, cfg); err != nil {
			return err
		}
		fmt.Printf("%s: %s=%s\n", workspace.RepoConfigPath(top), args[1], args[2])
		return nil
	}
	fmt.Fprint(os.Stderr, workspaceUsage)
	os.Exit(2)
	return nil
}

func repoConfigField(c workspace.RepoConfig, key string) (string, bool) {
	switch key {
	case "workspace":
		return c.Workspace, true
	case "harness_version":
		if c.HarnessVersion == nil {
			return "", true // null prints empty, exit 0 — it is a legal value
		}
		return *c.HarnessVersion, true
	case "name":
		return c.Name, true
	case "description":
		return c.Description, true
	case "repo_type":
		return c.RepoType, true
	}
	return "", false
}

func setRepoConfigField(c *workspace.RepoConfig, key, value string) error {
	switch key {
	case "workspace":
		c.Workspace = value
	case "harness_version":
		if value == "" || value == "null" {
			c.HarnessVersion = nil
		} else {
			c.HarnessVersion = &value
		}
	case "name":
		c.Name = value
	case "description":
		c.Description = value
	case "repo_type":
		c.RepoType = value
	default:
		return fmt.Errorf("unknown key %q (workspace|harness_version|name|description|repo_type)", key)
	}
	return nil
}

// originURL is the repo's origin url, one of the two ways a registry entry is
// matched to a checkout. Empty when there is no origin — matching falls back
// to the local path.
func originURL(toplevel string) string {
	return gitOut("-C", toplevel, "remote", "get-url", "origin")
}
