package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/reallongnguyen/babysit/internal/cmux"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/spf13/cobra"
)

const foremanUsage = `Usage:
  bbs foreman list
  bbs foreman register <id> [--dir <path>] [--workspace-title <title>] [--session <path>]
  bbs foreman heartbeat <id> [--status <status>] [--session <path>]
  bbs foreman spawn [<id>] [--dir <path>] [--command <text>]
  bbs foreman retire <id> [--keep-workspace]
`

func newForemanCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "foreman",
		Short:              "manage foreman sessions and their cmux workspaces",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runForeman(args)
		},
	}
}

// runForeman reports its own errors. The root command sets SilenceErrors and
// main only maps a returned error to exit 1, so anything returned from here
// would fail silently — the preflight messages exist precisely to be read.
func runForeman(args []string) error {
	if err := dispatchForeman(args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
	return nil
}

func dispatchForeman(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, foremanUsage)
		os.Exit(2)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return foremanList()
	case "register":
		return foremanRegister(rest)
	case "heartbeat":
		return foremanHeartbeat(rest)
	case "spawn":
		_, err := foremanSpawn(rest)
		return err
	case "retire":
		return foremanRetire(rest)
	case "help", "--help", "-h":
		fmt.Print(foremanUsage)
		return nil
	}
	fmt.Fprintf(os.Stderr, "foreman: unknown subcommand '%s'\n%s", sub, foremanUsage)
	os.Exit(2)
	return nil
}

// flags is the shared arg parser for the foreman verbs: a leading positional
// id followed by --key value pairs. Flag parsing is disabled on the cobra
// command so `--command "…"` reaches us with its text intact.
func foremanFlags(args []string) (id string, kv map[string]string, err error) {
	kv = map[string]string{}
	i := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, i = args[0], 1
	}
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			return "", nil, fmt.Errorf("foreman: unexpected argument '%s'", a)
		}
		key := strings.TrimPrefix(a, "--")
		if key == "keep-workspace" { // the one boolean flag
			kv[key] = "1"
			continue
		}
		if i+1 >= len(args) {
			return "", nil, fmt.Errorf("foreman: %s needs a value", a)
		}
		i++
		kv[key] = args[i]
	}
	return id, kv, nil
}

func foremanList() error {
	records := foreman.List()
	if len(records) == 0 {
		fmt.Println("no foremen registered")
		return nil
	}
	fmt.Printf("%-16s %-12s %-8s %-14s %-28s %s\n", "ID", "OWNER", "LIVE", "STATUS", "WORKSPACE", "DIR")
	for _, r := range records {
		live := "stale"
		if r.Live() {
			live = "live"
		}
		fmt.Printf("%-16s %-12s %-8s %-14s %-28s %s\n",
			r.ID, r.Owner, live, orDefault(r.Status, "-"), orDefault(r.WorkspaceTitle, "-"), r.WorkspaceDir)
	}
	return nil
}

func foremanRegister(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("foreman register: needs an id\n%s", foremanUsage)
	}

	dir := kv["dir"]
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return err
		}
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return err
	}

	// Re-registering keeps whatever the record already knows: a foreman that
	// restarts in the same workspace should not lose its workspace binding
	// just because this call did not repeat it.
	r, err := foreman.Load(id)
	if err != nil {
		r = foreman.Record{ID: id, Status: "idle"}
	}
	r.Owner = currentUser()
	r.ProjectDir = dir
	r.WorkspaceDir = dir
	if t := kv["workspace-title"]; t != "" {
		r.WorkspaceTitle = t
	}
	if s := kv["session"]; s != "" {
		r.Session = s
	}
	r.Heartbeat = foreman.Now()
	if err := foreman.Save(r); err != nil {
		return err
	}
	fmt.Printf("registered %s at %s\n", id, dir)
	return nil
}

func foremanHeartbeat(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("foreman heartbeat: needs an id\n%s", foremanUsage)
	}
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	if s := kv["status"]; s != "" {
		r.Status = s
	}
	if s := kv["session"]; s != "" {
		r.Session = s
	}
	r.Heartbeat = foreman.Now()
	return foreman.Save(r)
}

// foremanSpawn creates the cmux workspace a foreman runs in and registers the
// record in the same call, so a spawned foreman is never half-present: either
// both exist or the workspace is left for the operator to see. It returns the
// id it settled on — the dashboard sends no id when it wants the default, and
// still has to name the foreman it just created back to the human.
func foremanSpawn(args []string) (string, error) {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return "", err
	}

	client, err := cmux.Preflight()
	if err != nil {
		return "", err
	}

	dir := kv["dir"]
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return "", err
	}
	if id == "" {
		id = "fm-" + filepath.Base(dir)
	}
	// Check before Create, not just in Save: a rejected id after the workspace
	// exists would leave an orphan workspace with no record naming it.
	if err := foreman.ValidID(id); err != nil {
		return "", err
	}
	if _, err := foreman.Load(id); err == nil {
		return "", fmt.Errorf("foreman %s already registered — retire it first", id)
	}

	title := "bbs foreman " + id
	command := kv["command"]
	if command == "" {
		command = "claude"
	}
	ref, err := client.Create(cmux.CreateOpts{Title: title, Cwd: dir, Command: command})
	if err != nil {
		return "", err
	}

	r := foreman.Record{
		ID: id, Owner: currentUser(),
		ProjectDir: dir, WorkspaceDir: dir,
		WorkspaceRef: ref, WorkspaceTitle: title,
		Status: "idle", Heartbeat: foreman.Now(),
	}
	if err := foreman.Save(r); err != nil {
		return "", fmt.Errorf("workspace %s created but registering %s failed: %w", ref, id, err)
	}
	fmt.Printf("spawned %s in %s (%s)\n", id, ref, dir)
	return id, nil
}

// foremanRetire closes the workspace and drops the record. Retiring is not
// destructive to work: the tickets assigned to this foreman keep their
// assignment, so a replacement foreman registered under the same id picks
// them up.
func foremanRetire(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("foreman retire: needs an id\n%s", foremanUsage)
	}
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}

	if kv["keep-workspace"] == "" && r.WorkspaceTitle != "" {
		client, err := cmux.Preflight()
		if err != nil {
			// The record is still worth dropping — but say what was left
			// behind, so nobody hunts for a workspace that is still open.
			if rmErr := foreman.Remove(id); rmErr != nil {
				return rmErr
			}
			fmt.Printf("retired %s; workspace %q left open (%v)\n", id, r.WorkspaceTitle, err)
			return nil
		}
		if err := client.Close(r.WorkspaceTitle); err != nil {
			return err
		}
	}
	if err := foreman.Remove(id); err != nil {
		return err
	}
	fmt.Printf("retired %s\n", id)
	return nil
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}
