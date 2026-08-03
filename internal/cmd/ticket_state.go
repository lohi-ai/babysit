package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This file ports the index.json state-accessor family of bin/bbs-ticket.bash:
// env, get, set-status, set-phase, set-parent, add-child, add-relation,
// set-sibling, add-label, set-pointer, get-pointer, ensure-size, append-history.
// Each mutates index.json under the shared .index.lock and appends to
// history.jsonl exactly where the bash original does. Manifest.yaml operations
// (init/get-manifest/set-branch) and the git-mutating base-ops stay delegated.

// actorRole mirrors bash ${AGENT_ROLE:-${GT_ROLE:-developer}}.
func actorRole() string {
	if r := os.Getenv("AGENT_ROLE"); r != "" {
		return r
	}
	if r := os.Getenv("GT_ROLE"); r != "" {
		return r
	}
	return "developer"
}

// acquireOrDie mirrors bash `acquire_lock || exit 1`, printing the stale-lock
// hint. Used where the caller manages the release + its own exit codes (init,
// set-branch); mutateLocked wraps it for the common return-error case.
func acquireOrDie(st *ticket.Store) {
	if err := st.AcquireLock(); err != nil {
		fmt.Fprintln(os.Stderr, retarget("bbs-ticket: failed to acquire lock after 5s — stale?"))
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket: remove %s if no writer is active\n"), st.LockPath())
		os.Exit(1)
	}
}

// withLock is mutateLocked for callers that must not exit the process — the
// dashboard server runs the same mutations as the CLI, in a goroutine serving
// one request, where os.Exit would take the whole server down.
//
// The release is deferred, unlike mutateLocked's: net/http recovers a panicking
// handler and keeps serving, so a plain release would leave .index.lock on disk
// for the life of the machine, blocking every later write to that ticket from
// the server and the CLI alike.
func withLock(st *ticket.Store, fn func() error) error {
	if err := st.AcquireLock(); err != nil {
		return fmt.Errorf("failed to acquire lock after 5s — remove %s if no writer is active", st.LockPath())
	}
	defer st.ReleaseLock()
	return fn()
}

// mutateLocked mirrors `acquire_lock || exit 1` + the EXIT-trap release: fn must
// not call os.Exit (the lock would leak), so it returns errors instead.
func mutateLocked(st *ticket.Store, fn func() error) {
	acquireOrDie(st)
	err := fn()
	st.ReleaseLock()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// loadForMutate reads index.json, seeding defaults only when the file is absent
// — matching bash `[ -f "$F" ] || json_mutate "$F" ensure_defaults "$TICKET"`.
func loadForMutate(st *ticket.Store) ticket.Doc {
	p := st.IndexPath()
	doc := ticket.ReadDoc(p)
	if !fileExists(p) {
		doc.EnsureDefaults(st.Env.Ticket)
	}
	return doc
}

// printJSONRead mirrors the `json_read … ; echo` pair used by get / get-pointer,
// including the double-newline bash emits when index.json itself is absent.
func printJSONRead(st *ticket.Store, path string) {
	p := st.IndexPath()
	if !fileExists(p) {
		fmt.Println("") // json_read's `echo ""` for a missing file
	} else {
		fmt.Print(ticket.ReadDoc(p).Get(path))
	}
	fmt.Println() // the subcommand's trailing `echo`
}

// runTicketEnv prints the whole identity as shell assignments, for
// `eval "$(bbs ticket env)"`. It absorbed the derivation half — SLUG, BRANCH,
// BABYSIT_PROJECT_HOME — from the retired top-level `slug` command, so a
// preamble gets project *and* ticket scope from one call.
//
// No ticket is not an error here: outside a ticket branch the derivation half
// is still valid, and the preamble runs everywhere. The two ticket-scoped paths
// are simply omitted, so an eval'ing caller can test for an empty $TICKET.
func runTicketEnv() {
	env := identity.Resolve()
	fmt.Printf("SLUG=%s\n", env.Slug)
	fmt.Printf("BRANCH=%s\n", env.Branch)
	fmt.Printf("TICKET=%s\n", env.Ticket)
	fmt.Printf("BABYSIT_PROJECT_HOME=%s\n", env.ProjectHome)
	if env.Ticket != "" {
		st := ticket.New(env)
		fmt.Printf("TICKET_HOME=%s\n", st.Home())
		fmt.Printf("INDEX=%s\n", st.IndexPath())
	}
	os.Exit(0)
}

func runGet(args []string) {
	env := identity.Resolve()
	needTicket(env)
	if len(args) == 0 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "get: field path required")
		os.Exit(2)
	}
	printJSONRead(ticket.New(env), args[0])
	os.Exit(0)
}

var statusEnum = map[string]bool{
	"triage": true, "backlog": true, "planned": true, "decomposed": true,
	"in_progress": true, "in_review": true, "blocked": true, "done": true,
	"cancelled": true, "duplicate": true,
}

func runSetStatus(args []string) {
	env := identity.Resolve()
	needTicket(env)
	status := ""
	if len(args) > 0 {
		status = args[0]
	}
	if !statusEnum[status] {
		fmt.Fprintf(os.Stderr, "set-status: invalid status '%s'\n", status)
		fmt.Fprintln(os.Stderr, "valid: triage backlog planned decomposed in_progress in_review blocked done cancelled duplicate")
		os.Exit(2)
	}
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		old := doc.Get("status")
		doc.Set("status", status)
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("status_changed", actorRole(),
			fmt.Sprintf(`{"from":"%s","to":"%s"}`, old, status))
		return nil
	})
	os.Exit(0)
}

func runSetPhase(args []string) {
	env := identity.Resolve()
	needTicket(env)
	phase := ""
	if len(args) > 0 {
		phase = args[0]
	}
	if phase == "" {
		fmt.Fprintln(os.Stderr, "set-phase: skill name required")
		os.Exit(2)
	}
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		doc.Set("phase", phase)
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("phase_changed", actorRole(), fmt.Sprintf(`{"phase":"%s"}`, phase))
		return nil
	})
	os.Exit(0)
}

func runSetParent(args []string) {
	env := identity.Resolve()
	needTicket(env)
	parent := ""
	if len(args) > 0 {
		parent = args[0]
	}
	if parent == "" {
		fmt.Fprintln(os.Stderr, "set-parent: parent ticket id required")
		os.Exit(2)
	}
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		doc.Set("parent", parent)
		doc.Set("origin.parent", parent)
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("parent_set", actorRole(), fmt.Sprintf(`{"parent":"%s"}`, parent))
		return nil
	})
	os.Exit(0)
}

func runAddChild(args []string) {
	env := identity.Resolve()
	needTicket(env)
	child := ""
	if len(args) > 0 {
		child = args[0]
	}
	if child == "" {
		fmt.Fprintln(os.Stderr, "add-child: child ticket id required")
		os.Exit(2)
	}
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		if err := doc.Append("children", child); err != nil {
			return err
		}
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("child_added", actorRole(), fmt.Sprintf(`{"child":"%s"}`, child))
		return nil
	})
	os.Exit(0)
}

var relationTypes = map[string]bool{"blocks": true, "blocked_by": true, "duplicate_of": true, "related": true}

func runAddRelation(args []string) {
	env := identity.Resolve()
	needTicket(env)
	typ, target := "", ""
	if len(args) > 0 {
		typ = args[0]
	}
	if len(args) > 1 {
		target = args[1]
	}
	if !relationTypes[typ] {
		fmt.Fprintln(os.Stderr, "add-relation: type must be blocks|blocked_by|duplicate_of|related")
		os.Exit(2)
	}
	if target == "" {
		fmt.Fprintln(os.Stderr, "add-relation: target ticket id required")
		os.Exit(2)
	}
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		if typ == "duplicate_of" {
			doc.Set("relations.duplicate_of", target)
		} else if err := doc.Append("relations."+typ, target); err != nil {
			return err
		}
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("relation_added", actorRole(),
			fmt.Sprintf(`{"type":"%s","target":"%s"}`, typ, target))
		return nil
	})
	os.Exit(0)
}

func runSetSibling(args []string) {
	env := identity.Resolve()
	needTicket(env)
	var role, repo, sticket string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--role":
			role, i = valueOf(args, i, "--role"), i+1
		case "--repo":
			repo, i = valueOf(args, i, "--repo"), i+1
		case "--ticket":
			sticket, i = valueOf(args, i, "--ticket"), i+1
		}
	}
	if role == "" || repo == "" || sticket == "" {
		fmt.Fprintln(os.Stderr, "set-sibling: --role, --repo, --ticket required")
		os.Exit(2)
	}
	obj := fmt.Sprintf(`{"role":"%s","repo":"%s","ticket":"%s"}`, role, repo, sticket)
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		if err := doc.AppendObj("siblings", obj); err != nil {
			return err
		}
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("sibling_added", actorRole(), obj)
		return nil
	})
	os.Exit(0)
}

func runAddLabel(args []string) {
	env := identity.Resolve()
	needTicket(env)
	label := ""
	if len(args) > 0 {
		label = args[0]
	}
	if label == "" {
		fmt.Fprintln(os.Stderr, "add-label: label required")
		os.Exit(2)
	}
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		if err := doc.Append("labels", label); err != nil {
			return err
		}
		return ticket.WriteDoc(st.IndexPath(), doc)
	})
	os.Exit(0)
}

func runSetPointer(args []string) {
	env := identity.Resolve()
	needTicket(env)
	key, value := "", ""
	if len(args) > 0 {
		key = args[0]
	}
	if len(args) > 1 {
		value = args[1]
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "set-pointer: key required")
		os.Exit(2)
	}
	st := ticket.New(env)
	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		doc.Set("pointers."+key, value)
		return ticket.WriteDoc(st.IndexPath(), doc)
	})
	os.Exit(0)
}

func runGetPointer(args []string) {
	env := identity.Resolve()
	needTicket(env)
	key := ""
	if len(args) > 0 {
		key = args[0]
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "get-pointer: key required")
		os.Exit(2)
	}
	printJSONRead(ticket.New(env), "pointers."+key)
	os.Exit(0)
}

func runAppendHistory(args []string) {
	env := identity.Resolve()
	needTicket(env)
	var event, actor, extra string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--event":
			event, i = valueOf(args, i, "--event"), i+1
		case "--actor":
			actor, i = valueOf(args, i, "--actor"), i+1
		case "--extra-json":
			extra, i = valueOf(args, i, "--extra-json"), i+1
		}
	}
	if event == "" {
		fmt.Fprintln(os.Stderr, "append-history: --event required")
		os.Exit(2)
	}
	st := ticket.New(env)
	st.EnsureDirs()
	st.HistoryAppendExtra(event, actor, extra)
	os.Exit(0)
}

var (
	migrationRe = regexp.MustCompile(`(^|/)(migrations?|alembic)/|(^|/)schema\.|\.sql$`)
	depsRe      = regexp.MustCompile(`(^|/)(package\.json|package-lock|requirements\.txt|go\.(mod|sum)|Gemfile|pyproject\.toml|Cargo\.(toml|lock))$`)
)

// runEnsureSize ports the ensure-size subcommand: return the ticket_size pointer
// if set, else estimate from the PR diff (rubric in
// .claude/skills/references/ticket-size-rubric.md) and persist it.
func runEnsureSize() {
	env := identity.Resolve()
	needTicket(env)
	st := ticket.New(env)

	existing := ticket.ReadDoc(st.IndexPath()).Get("pointers.ticket_size")
	if existing != "" {
		fmt.Println(existing)
		os.Exit(0)
	}

	base := gitOut("merge-base", "HEAD", "origin/main")
	if base == "" {
		base = gitOut("merge-base", "HEAD", "main")
	}
	if base == "" {
		base = "HEAD~1"
	}
	names := gitOut("diff", "--name-only", base+"...HEAD")
	changed := splitNonEmptyLines(names)
	files := len(changed)
	loc := parseShortstat(gitOut("diff", "--shortstat", base+"...HEAD"))
	mods := map[string]bool{}
	migrations, deps := 0, 0
	for _, f := range changed {
		if i := strings.IndexByte(f, '/'); i > 0 {
			mods[f[:i]] = true
		}
		if migrationRe.MatchString(f) {
			migrations++
		}
		if depsRe.MatchString(f) {
			deps++
		}
	}
	modules := len(mods)

	var size string
	switch {
	case files <= 1 && loc <= 20 && modules <= 1 && migrations == 0 && deps == 0:
		size = "XS"
	case files <= 3 && loc <= 50 && modules <= 1 && migrations == 0 && deps == 0:
		size = "S"
	case files <= 10 && loc <= 300 && modules <= 3 && deps <= 1:
		size = "M"
	default:
		size = "L"
	}
	fmt.Fprintf(os.Stderr, "estimated ticket_size=%s (files=%d loc=%d modules=%d migrations=%d deps=%d)\n",
		size, files, loc, modules, migrations, deps)

	mutateLocked(st, func() error {
		doc := loadForMutate(st)
		doc.Set("pointers.ticket_size", size)
		return ticket.WriteDoc(st.IndexPath(), doc)
	})
	fmt.Println(size)
	os.Exit(0)
}

// splitNonEmptyLines counts changed files the way `wc -l` would on the untrimmed
// diff output: one per non-empty line.
func splitNonEmptyLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if ln != "" {
			out = append(out, ln)
		}
	}
	return out
}

// parseShortstat sums each number that precedes an "insertion"/"deletion" field
// in `git diff --shortstat` output — the awk in the bash original.
func parseShortstat(s string) int {
	fields := strings.Fields(s)
	sum := 0
	for i, f := range fields {
		if i > 0 && (strings.Contains(f, "insertion") || strings.Contains(f, "deletion")) {
			if n, err := strconv.Atoi(fields[i-1]); err == nil {
				sum += n
			}
		}
	}
	return sum
}
