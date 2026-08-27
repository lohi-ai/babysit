package cmd

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/agent"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/orca"
	"github.com/reallongnguyen/babysit/internal/qaconfig"
	"github.com/reallongnguyen/babysit/internal/ticket"
	"github.com/spf13/cobra"
)

const foremanUsage = `Usage:
  bbs foreman list
  bbs foreman inbox <id>
  bbs foreman register <id> [--dir <path>] [--workspace-title <title>] [--session <uuid>]
  bbs foreman heartbeat <id> [--status <status>] [--session <uuid>]
  bbs foreman spawn [<id>] [--dir <path>] [--command <text>] [--agent <name>]
  bbs foreman worker-command --prompt <text> [--skill <name>] [--agent <name>] [--dir <path>]
  bbs foreman mailbox <status|bind|dispatch|wait|reply|done> ...
  bbs foreman watch [<id>] [--interval <sec>] [--idle <sec>] [--lines <n>]
                    [--nudge <text>] [--max-nudges <n>] [--once]
  bbs foreman retire <id> [--keep-workspace]
  bbs foreman hold <id>
  bbs foreman hold show <id>
  bbs foreman hold release <id>
  bbs foreman grant <id> [--hours <n>] [--max <n>] [--tickets <a,b>] [--unbounded]
  bbs foreman grant show <id>
  bbs foreman grant revoke <id>
`

// foremanSkillName names the skill in the opening prompt every spawned foreman gets, and the
// prefix on every poke sent into a running one. A workspace that comes up on a
// bare `claude` is just a Claude session sitting in a repo — it does not know
// it is a foreman until something tells it, and a long-lived one loses the
// skill to context compaction. Re-invoking is cheap and idempotent by the
// skill's own design (bare `/bbs:foreman` = reconcile and resume), so the
// prompt doubles as the refresh.
//
// Unquoted: the agent profile shell-quotes it when rendering the command line.
// The `bbs:` prefix comes from the profile rather than being spelled here —
// an agent that discovers skills through a flat directory list exposes them
// bare, and a prompt naming the wrong prefix resolves to nothing at all.
const foremanSkillName = "foreman"

func newForemanCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "foreman",
		Short:              "manage foreman sessions and their Orca terminals",
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
	case "worker-command":
		return foremanWorkerCommand(rest)
	case "mailbox":
		return foremanMailbox(rest)
	case "watch":
		return foremanWatch(rest)
	case "retire":
		return foremanRetire(rest)
	case "hold":
		return foremanHold(rest)
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
		if key == "keep-workspace" || key == "unbounded" || key == "once" || key == "ack" { // the boolean flags
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
	fmt.Printf("%-16s %-12s %-8s %-14s %-28s %-38s %s\n", "ID", "OWNER", "LIVE", "STATUS", "WORKSPACE", "SESSION", "DIR")
	for _, r := range records {
		live := "stale"
		if r.Live() {
			live = "live"
		}
		fmt.Printf("%-16s %-12s %-8s %-14s %-28s %-38s %s\n",
			r.ID, r.Owner, live, orDefault(r.Status, "-"), orDefault(r.WorkspaceTitle, "-"),
			orDefault(r.Session, "-"), r.WorkspaceDir)
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

	// Loaded once, outside the loop: posture (hold / grant bounds) is a
	// property of the foreman reading its inbox, not of the tickets in it.
	// A missing record is not an error here — inbox is a read, and a
	// foreman with no record is treated as default autonomy.
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
		// A pending row under human hold means "wait for the human". Under
		// default autonomy (or a covering grant bound) it means the opposite
		// — the foreman is the one being waited on — and a foreman that read
		// plain `pending` and waited would stall the batch.
		if approval == "pending" {
			if ok, _ := rec.Allows(tid, now); ok {
				approval = "pending(auto)"
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
	return spawnForeman(id, kv["dir"], kv["command"], kv["agent"])
}

// foremanWorkerCommand prints the command line that runs one worker on a
// prompt, so the foreman skill does not have to know which CLI it is dispatching
// or how that CLI spells "stop asking for approval". Resolution lives here for
// the same reason ticket identity has exactly one codepath: a bash conditional
// in SKILL.md would be a second place agent selection could drift.
//
// It preflights, so a missing binary — or a directory the agent would stop and
// ask about — is reported before Orca opens a terminal on a command that cannot
// run. --dir is the worker's cwd, defaulting to the repo the foreman is in,
// matching the `--worktree path:$REPO` the skill passes to `orca terminal create`.
func foremanWorkerCommand(args []string) error {
	_, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	prompt := kv["prompt"]
	if prompt == "" {
		return fmt.Errorf("foreman worker-command: needs --prompt <text>\n%s", foremanUsage)
	}
	prof, err := agent.Resolve(agent.WorkerKey, kv["agent"])
	if err != nil {
		return err
	}
	// --skill is how a caller names the skill without knowing how this agent
	// namespaces it. It exists for the same reason agent selection lives here
	// rather than in SKILL.md: a hard-coded `/bbs:autopilot` in the dispatch
	// line is a second place the agent's shape can drift, and this particular
	// drift is silent — an agent that discovers skills through a flat directory
	// list exposes them bare, so the worker comes up cleanly and resolves the
	// prompt to nothing.
	if skill := kv["skill"]; skill != "" {
		prompt = prof.SkillRef(skill) + " " + prompt
	}
	if err := prof.Preflight(); err != nil {
		return err
	}
	dir := kv["dir"]
	if dir == "" {
		dir = qaconfig.RepoToplevel()
	}
	if err := prof.PreflightDir(dir); err != nil {
		return err
	}
	fmt.Println(prof.WorkerCommand(prompt))
	return nil
}

// spawnForeman creates the Orca terminal a foreman runs in and registers the
// record in the same call, so a spawned foreman is never half-present: either
// both exist or the terminal is left for the operator to see. It returns the
// id it settled on — the dashboard sends no id when it wants the default, and
// still has to name the foreman it just created back to the human.
//
// It takes its inputs typed rather than as an argv: the dashboard's fields
// arrive as JSON, and round-tripping them through the flag parser would let an
// id beginning with "-" be re-read as a flag.
//
// Spawn is also the resume path. A registered id whose terminal has since been
// closed used to be a hard error ("retire it first"), which is the one thing a
// human must not do here: retiring drops the record, and with it the only
// pointer back to the conversation the foreman was having. So a registered id
// with a dead terminal re-opens one on `claude --resume <session>` instead. A
// registered id whose terminal is still OPEN stays an error — that is a real
// collision, not a restart.
func spawnForeman(id, dir, command, agentFlag string) (string, error) {
	client, err := orca.Preflight()
	if err != nil {
		return "", err
	}

	dirGiven := dir != ""
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

	r, loadErr := foreman.Load(id)
	resuming := loadErr == nil
	if resuming {
		if ref, err := client.Ref(r.WorkspaceTitle); err == nil {
			return "", fmt.Errorf("foreman %s already registered and running in %s — retire it first", id, ref)
		} else if !errors.Is(err, orca.ErrNoTerminal) {
			// Orca could not answer. Spawning anyway would create a second
			// terminal next to a live one we simply failed to see.
			return "", fmt.Errorf("cannot tell whether %s is still running: %w", id, err)
		}
		// A foreman is bound to ONE project. Resuming from wherever the human
		// happened to be standing must not re-point it: only an explicit --dir
		// moves a foreman.
		if !dirGiven && r.WorkspaceDir != "" {
			dir = r.WorkspaceDir
		}
	} else {
		r = foreman.Record{ID: id}
	}

	// The session id is minted here rather than read back afterwards. A foreman
	// is one long conversation, and the only durable handle on it is the uuid
	// Claude Code was told to use: correlating after the fact through
	// ~/.babysit/sessions by cwd cannot tell two sessions in the same repo
	// apart, which is exactly the case a foreman lives in.
	//
	// A resume of a foreman that was registered rather than spawned starts
	// fresh — there is nothing to resume, and guessing a handle would fail at
	// launch.
	//
	// "Has a conversation to resume" is keyed on the recorded AGENT, not on the
	// recorded session: an agent with no mint flag records no session id and
	// would otherwise look, forever, like a foreman that had never run.
	verb, session := "spawned", r.Session
	resumable := resuming && r.Agent != ""
	if resumable {
		verb = "resumed"
	}

	// Which CLI runs this foreman. On a resume the recorded agent wins over
	// config: the handle recorded above is only meaningful to the CLI that
	// minted it, so re-resolving from a config that has changed since would
	// hand a different agent something it has never heard of. That is true even
	// for an agent whose handle is empty — resuming "the most recent
	// conversation" under a different CLI resumes a different conversation. An
	// explicit --agent that contradicts the recording is a mistake worth naming
	// rather than silently honoring either way.
	var prof agent.Profile
	if resumable {
		pinned := r.Agent
		if agentFlag != "" && agentFlag != pinned {
			return "", fmt.Errorf("foreman %s has a %s session — cannot resume it as %s; "+
				"retire it first to start a fresh conversation", id, pinned, agentFlag)
		}
		prof, err = agent.ByName(pinned)
	} else if resuming && r.Session != "" {
		// A record written before agents were selectable: it has a session but
		// no agent, and that session can only have been claude's.
		if agentFlag != "" && agentFlag != agent.Default {
			return "", fmt.Errorf("foreman %s has a %s session — cannot resume it as %s; "+
				"retire it first to start a fresh conversation", id, agent.Default, agentFlag)
		}
		verb = "resumed"
		prof, err = agent.ByName(agent.Default)
	} else {
		prof, err = agent.Resolve(agent.ForemanKey, agentFlag)
	}
	if err != nil {
		return "", err
	}

	// Mint the durable handle only once the profile is known, because what a
	// handle even IS depends on the agent. Only some agents take a uuid;
	// omp has no mint flag and gets a private session directory instead, so
	// that "the most recent conversation in there" is unambiguously this
	// foreman's rather than whatever else ran in the same checkout; codex has
	// neither and gets "", resuming by "most recent" alone.
	//
	// Recording a uuid against an agent that cannot be told to use it is the
	// specific bug this ordering prevents: the record would look resumable and
	// the resume would hand the CLI an id it has never heard of.
	if session == "" {
		uuid := ""
		if prof.MintsSessionID() {
			if uuid, err = newSessionID(); err != nil {
				return "", err
			}
		}
		session = prof.SessionToken(uuid, filepath.Join(foreman.Dir(), id+".sessions"))
	}

	title := "bbs foreman " + id
	if command == "" {
		// Only when we build the command ourselves: an explicit --command is the
		// caller's business, and preflighting an agent it may not even invoke
		// would refuse a legitimate `--command "bash -l"`.
		if err := prof.Preflight(); err != nil {
			return "", err
		}
		if err := prof.PreflightDir(dir); err != nil {
			return "", err
		}
		if verb == "resumed" {
			command = prof.ResumeCommand(session, prof.SkillRef(foremanSkillName))
		} else {
			command = prof.NewSessionCommand(session, prof.SkillRef(foremanSkillName))
		}
	}
	ref, err := client.Create(orca.CreateOpts{Title: title, Cwd: dir, Command: command})
	if err != nil {
		return "", err
	}

	r.Owner = currentUser()
	r.ProjectDir, r.WorkspaceDir = dir, dir
	r.WorkspaceRef, r.WorkspaceTitle = ref, title
	r.Session = session
	r.Agent = prof.Name
	r.Status = "idle"
	r.Heartbeat = foreman.Now()
	r.Unreachable = ""
	if err := foreman.Save(r); err != nil {
		return "", fmt.Errorf("workspace %s created but registering %s failed: %w", ref, id, err)
	}
	fmt.Printf("%s %s in %s (%s, session %s)\n", verb, id, ref, dir, session)
	return id, nil
}

// newSessionID mints the uuid v4 `claude --session-id` requires. crypto/rand
// matches newTicketID's source; unlike a ticket id this one has no epoch
// fallback, because a non-uuid would be rejected by the CLI at launch and the
// workspace would come up dead.
func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cannot mint a session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
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
	msg, err := retireForeman(id, kv["keep-workspace"] != "")
	if err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}

// retireForeman drops a foreman record and, unless asked to keep it, closes the
// Orca terminal it was running in. Shared with the dashboard's retire endpoint:
// the two entry points must not drift, because a record dropped without its
// terminal closed leaves a pane that looks like a working foreman and can never
// be reached again from the list it was just removed from.
//
// Returns the line to report, which is not always "retired": a workspace left
// open is the one outcome the caller must be told about.
func retireForeman(id string, keepWorkspace bool) (string, error) {
	r, err := foreman.Load(id)
	if err != nil {
		return "", err
	}

	if !keepWorkspace && r.WorkspaceTitle != "" {
		client, err := orca.Preflight()
		if err != nil {
			// The record is still worth dropping — but say what was left
			// behind, so nobody hunts for a terminal that is still open.
			if rmErr := foreman.Remove(id); rmErr != nil {
				return "", rmErr
			}
			return fmt.Sprintf("retired %s; workspace %q left open (%v)", id, r.WorkspaceTitle, err), nil
		}
		if err := client.Close(r.WorkspaceTitle); err != nil {
			return "", err
		}
	}
	if err := foreman.Remove(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("retired %s", id), nil
}

// foremanHold opts a foreman into human-held design checkpoints — the only
// path away from the autonomous default. Survives the session that said it.
func foremanHold(args []string) error {
	if len(args) > 0 && (args[0] == "release" || args[0] == "show") {
		verb := args[0]
		id, _, err := foremanFlags(args[1:])
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("foreman hold %s: needs an id\n%s", verb, foremanUsage)
		}
		if verb == "show" {
			return foremanHoldShow(id)
		}
		return foremanHoldRelease(id)
	}

	id, _, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("foreman hold: needs an id\n%s", foremanUsage)
	}
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	r.Hold = &foreman.Hold{HeldBy: currentUser(), At: foreman.Now()}
	if err := foreman.Save(r); err != nil {
		return err
	}
	fmt.Printf("hold set on %s — design checkpoints escalate to a human\n", id)
	fmt.Println("release with: bbs foreman hold release " + id)
	return nil
}

func foremanHoldRelease(id string) error {
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	if r.Hold == nil {
		fmt.Printf("%s has no hold — already on default autonomy\n", id)
		return nil
	}
	r.Hold = nil
	if err := foreman.Save(r); err != nil {
		return err
	}
	fmt.Printf("released hold on %s — design checkpoints self-resolve by default again\n", id)
	fmt.Println("work already escalated under the hold stands; in-flight workers keep going.")
	return nil
}

func foremanHoldShow(id string) error {
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	if r.Hold == nil {
		fmt.Println("none — default autonomy")
		return nil
	}
	fmt.Printf("human hold — held by %s at %s\n", r.Hold.HeldBy, r.Hold.At)
	return nil
}

// foremanGrant optionally bounds a foreman's design-checkpoint autonomy —
// hours, max approvals, or ticket scope — in a form that survives the
// session that said it. Autonomy itself is the default; grant is not
// required for self-resolve. Use bbs foreman hold to put checkpoints back
// with a human.
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
	// A grant is a bound on autonomy, not a way around a hold. Setting a
	// grant while held would look active and still escalate — clear the
	// hold so the operator's intent (self-resolve under these bounds) wins.
	if r.Hold != nil {
		r.Hold = nil
	}
	if err := foreman.Save(r); err != nil {
		return err
	}
	fmt.Printf("bounded %s design-checkpoint autonomy — %s\n", id, describeGrant(g))
	fmt.Println("autonomy is already the default; this only narrows it. the non-delegable floor still holds: money, auth and irreversible-data changes escalate.")
	return nil
}

func foremanGrantRevoke(id string) error {
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	if r.Grant == nil {
		fmt.Printf("%s has no grant bound — already on default autonomy\n", id)
		return nil
	}
	r.Grant = nil
	if err := foreman.Save(r); err != nil {
		return err
	}
	// Revoke removes bounds only. It does not force human-held; that is
	// bbs foreman hold. Already-approved work is not rolled back.
	fmt.Printf("revoked grant bound on %s — back to unbounded default autonomy\n", id)
	fmt.Println("work already approved under the grant stands; in-flight workers keep going.")
	return nil
}

func foremanGrantShow(id string) error {
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	if r.Grant == nil {
		if r.Hold != nil {
			fmt.Println("none — human hold (bbs foreman hold show " + id + ")")
			return nil
		}
		fmt.Println("none — default autonomy")
		return nil
	}
	fmt.Printf("%s — granted by %s at %s\n", describeGrant(r.Grant), r.Grant.GrantedBy, r.Grant.At)
	// Probe with a ticket the grant is meant to cover, so "inactive" reports a
	// real bound — expiry or budget — rather than the scope check tripping on
	// the empty string we passed it. Hold still wins if set.
	probe := ""
	if len(r.Grant.Tickets) > 0 {
		probe = r.Grant.Tickets[0]
	}
	if ok, reason := r.Allows(probe, time.Now()); !ok {
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
