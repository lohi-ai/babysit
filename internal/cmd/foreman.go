package cmd

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/cmux"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
	"github.com/spf13/cobra"
)

const foremanUsage = `Usage:
  bbs foreman list
  bbs foreman inbox <id>
  bbs foreman register <id> [--dir <path>] [--workspace-title <title>] [--session <path>]
  bbs foreman heartbeat <id> [--status <status>] [--session <path>]
  bbs foreman spawn [<id>] [--dir <path>] [--command <text>]
  bbs foreman retire <id> [--keep-workspace]
  bbs foreman grant <id> [--hours <n>] [--max <n>] [--tickets <a,b>] [--unbounded]
  bbs foreman grant show <id>
  bbs foreman grant revoke <id>
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
	case "inbox":
		return foremanInbox(rest)
	case "register":
		return foremanRegister(rest)
	case "heartbeat":
		return foremanHeartbeat(rest)
	case "spawn":
		_, err := foremanSpawn(rest)
		return err
	case "retire":
		return foremanRetire(rest)
	case "grant":
		return foremanGrant(rest)
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
		if key == "keep-workspace" || key == "unbounded" { // the boolean flags
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

// foremanInbox is a foreman's read of its own assignment set. It reconciles
// every ticket before reading it, because the failure this command exists to
// prevent is a foreman dispatching off a status the filesystem already moved
// past. Assignment is a field on the ticket, so this scan *is* the queue —
// there is no second list that can drift away from it.
func foremanInbox(args []string) error {
	id, _, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("foreman inbox: needs an id\n%s", foremanUsage)
	}

	env := identity.Resolve()
	tdir := filepath.Join(env.ProjectHome, "tickets")
	entries, err := os.ReadDir(tdir)
	if err != nil {
		return fmt.Errorf("foreman inbox: no tickets at %s", tdir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	// Loaded once, outside the loop: the grant is a property of the foreman
	// reading its inbox, not of the tickets in it. A missing record is not an
	// error here — inbox is a read, and a foreman with no record simply has no
	// grant.
	rec, _ := foreman.Load(id)
	now := time.Now()

	rows := 0
	for _, tid := range names {
		// Reconcile writes; a paused or cancelled ticket is skipped inside.
		_ = reconcileOne(io.Discard, env, tid, false, true)
		doc := ticket.ReadDoc(filepath.Join(tdir, tid, "index.json"))
		if doc.Get("assignee") != id {
			continue
		}
		if rows == 0 {
			fmt.Printf("%-16s %-14s %-12s %s\n", "TICKET", "STATUS", "CONTROL", "APPROVAL")
		}
		rows++
		approval := orDefault(doc.Get("approval.state"), "-")
		// A pending row normally means "wait for the human". Under a grant
		// covering this ticket it means the opposite — the foreman is the one
		// being waited on — and a foreman that read `pending` and waited would
		// stall the batch the grant exists to keep moving.
		if approval == "pending" {
			if ok, _ := rec.Allows(tid, now); ok {
				approval = "pending(grantable)"
			}
		}
		fmt.Printf("%-16s %-14s %-12s %s\n", tid,
			orDefault(doc.Get("status"), "triage"),
			orDefault(doc.Get("control.state"), "-"),
			approval)
	}
	if rows == 0 {
		fmt.Printf("%s: no tickets assigned\n", id)
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

func foremanSpawn(args []string) (string, error) {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return "", err
	}
	return spawnForeman(id, kv["dir"], kv["command"])
}

// spawnForeman creates the cmux workspace a foreman runs in and registers the
// record in the same call, so a spawned foreman is never half-present: either
// both exist or the workspace is left for the operator to see. It returns the
// id it settled on — the dashboard sends no id when it wants the default, and
// still has to name the foreman it just created back to the human.
//
// It takes its inputs typed rather than as an argv: the dashboard's fields
// arrive as JSON, and round-tripping them through the flag parser would let an
// id beginning with "-" be re-read as a flag.
func spawnForeman(id, dir, command string) (string, error) {
	client, err := cmux.Preflight()
	if err != nil {
		return "", err
	}

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

// foremanGrant delegates the design checkpoint to a foreman — the human
// saying "you may approve plans and prototypes yourself" in a form that
// survives the session that said it.
//
// Bounds are opt-out rather than opt-in, and an unbounded grant has to be
// typed: `grant fm-a` with no bounds is refused, because the difference
// between "approve the next three designs while I sleep" and "approve
// anything forever" is exactly the difference this command exists to make
// visible.
func foremanGrant(args []string) error {
	if len(args) > 0 && (args[0] == "revoke" || args[0] == "show") {
		verb := args[0]
		id, _, err := foremanFlags(args[1:])
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("foreman grant %s: needs an id\n%s", verb, foremanUsage)
		}
		if verb == "show" {
			return foremanGrantShow(id)
		}
		return foremanGrantRevoke(id)
	}

	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("foreman grant: needs an id\n%s", foremanUsage)
	}
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}

	g := &foreman.Grant{GrantedBy: currentUser(), At: foreman.Now()}
	if h := kv["hours"]; h != "" {
		n, err := strconv.Atoi(h)
		if err != nil || n <= 0 {
			return fmt.Errorf("foreman grant: --hours needs a positive number, got %q", h)
		}
		g.ExpiresAt = time.Now().UTC().Add(time.Duration(n) * time.Hour).Format(time.RFC3339)
	}
	if m := kv["max"]; m != "" {
		n, err := strconv.Atoi(m)
		if err != nil || n <= 0 {
			return fmt.Errorf("foreman grant: --max needs a positive number, got %q", m)
		}
		g.MaxApprovals = n
	}
	if t := kv["tickets"]; t != "" {
		for _, part := range strings.Split(t, ",") {
			if p := strings.TrimSpace(part); p != "" {
				g.Tickets = append(g.Tickets, p)
			}
		}
	}
	if g.Unbounded() && kv["unbounded"] == "" {
		return fmt.Errorf("foreman grant: an unbounded grant must be typed — pass --hours/--max/--tickets, or --unbounded to mean it")
	}

	r.Grant = g
	if err := foreman.Save(r); err != nil {
		return err
	}
	fmt.Printf("granted %s design-checkpoint approval — %s\n", id, describeGrant(g))
	fmt.Println("the non-delegable floor still holds: money, auth and irreversible-data changes escalate.")
	return nil
}

func foremanGrantRevoke(id string) error {
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	if r.Grant == nil {
		fmt.Printf("%s has no grant\n", id)
		return nil
	}
	r.Grant = nil
	if err := foreman.Save(r); err != nil {
		return err
	}
	// Said plainly because it is the question a human asks next: revoking is
	// not a rollback, and it does not reach into a worker already building.
	fmt.Printf("revoked %s — it escalates by default again\n", id)
	fmt.Println("work already approved under the grant stands; in-flight workers keep going.")
	return nil
}

func foremanGrantShow(id string) error {
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	if r.Grant == nil {
		fmt.Println("none")
		return nil
	}
	fmt.Printf("%s — %s\n", describeGrant(r.Grant), "granted by "+r.Grant.GrantedBy+" at "+r.Grant.At)
	if ok, reason := r.Allows("", time.Now()); !ok && reason != "grant is ticket-scoped and no ticket was named" {
		fmt.Printf("inactive: %s\n", reason)
	}
	return nil
}

func describeGrant(g *foreman.Grant) string {
	if g.Unbounded() {
		return "unbounded"
	}
	var parts []string
	if g.ExpiresAt != "" {
		parts = append(parts, "until "+g.ExpiresAt)
	}
	if g.MaxApprovals > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d approvals used", g.Used, g.MaxApprovals))
	}
	if len(g.Tickets) > 0 {
		parts = append(parts, "tickets "+strings.Join(g.Tickets, ","))
	}
	return strings.Join(parts, "; ")
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}
