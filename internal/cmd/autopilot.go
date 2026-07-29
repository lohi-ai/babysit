package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/slug"
	"github.com/spf13/cobra"
)

// newAutopilotCmd ports bin/bbs-autopilot as `bbs autopilot` — the checkpoint +
// timeline + probe/explain state helper behind the autopilot skill. Flag
// parsing is disabled so each subcommand walks its own args exactly like the
// bash script (unknown flags/args are ignored, not rejected by cobra).
func newAutopilotCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "autopilot {checkpoint|read|clear|current|set-current|timeline|recover|base-branch|lint-workflow|probe|explain|check-skill-deps} ...",
		Short:              "autopilot state helper (checkpoints, timeline, probe/explain)",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			runAutopilot(args)
			return nil
		},
	}
}

const autopilotUsage = "usage: bbs-autopilot {checkpoint|read|clear|current|set-current|timeline|recover|base-branch|lint-workflow|probe|explain|check-skill-deps} ..."

// apState is the identity + state-root resolved once per invocation, mirroring
// the top-of-script derivation in bin/bbs-autopilot.
type apState struct {
	slug      string
	branch    string
	ticket    string // DERIVED_TICKET
	stateRoot string // PROJECT_HOME
	scriptDir string
}

func runAutopilot(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, retarget(autopilotUsage))
		os.Exit(2)
	}
	sub := args[0]
	rest := args[1:]
	a := resolveAP()

	switch sub {
	case "checkpoint":
		a.checkpoint(rest)
	case "read":
		a.read(rest)
	case "clear":
		a.clear(rest)
	case "current":
		a.current()
	case "set-current":
		a.setCurrent(rest)
	case "timeline":
		a.timeline(rest)
	case "recover":
		a.recover()
	case "base-branch":
		fmt.Println(a.baseBranch())
	case "lint-workflow":
		a.lintWorkflow(rest)
	case "probe":
		a.probe(rest)
	case "explain":
		a.explain(rest)
	case "check-skill-deps":
		a.checkSkillDeps(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", sub)
		os.Exit(2)
	}
}

// resolveAP mirrors lines 58-82 of bin/bbs-autopilot: eval `bbs-slug env` (via
// slug.Resolve), then derive ticket + project home with the bash fallbacks.
func resolveAP() *apState {
	slugv, branch, ticket, projectHome := "unknown", "unknown", "", ""
	if info, err := slug.Resolve(); err == nil {
		slugv, branch, ticket, projectHome = info.Slug, info.Branch, info.Ticket, info.ProjectHome
	}
	// DERIVED_TICKET = ${BBS_TICKET:-${TICKET:-}}
	derived := os.Getenv("BBS_TICKET")
	if derived == "" {
		derived = ticket
	}
	// PROJECT_HOME: slug.Resolve already honored $BABYSIT_PROJECT_HOME on the
	// success path. On the failure path projectHome is empty, so fall back
	// through the env then the $BABYSIT_HOME default, matching the bash.
	ph := projectHome
	if ph == "" {
		if ph = os.Getenv("BABYSIT_PROJECT_HOME"); ph == "" {
			ph = filepath.Join(babysitHome(), "projects", slugv)
		}
	}
	return &apState{slug: slugv, branch: branch, ticket: derived, stateRoot: ph, scriptDir: apScriptDir()}
}

func babysitHome() string {
	if h := os.Getenv("BABYSIT_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".babysit")
}

func apScriptDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe)
}

// sibling resolves a companion bin the same way bin/bbs-autopilot does:
// $SCRIPT_DIR/<name> → PATH → ~/.claude/<name>.
func (a *apState) sibling(name string) string {
	p := filepath.Join(a.scriptDir, name)
	if isExecutable(p) {
		return p
	}
	if lp, err := exec.LookPath(name); err == nil {
		return lp
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", name)
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// ─── shared helpers ──────────────────────────────────────────────────────────

func isoNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// safeTicket keeps [a-zA-Z0-9._-] and caps at 64 bytes (tr -cd | head -c 64).
func safeTicket(s string) string {
	var b strings.Builder
	for i := 0; i < len(s) && b.Len() < 64; i++ {
		c := s[i]
		if isAlnumByte(c) || c == '.' || c == '_' || c == '-' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// jsonSafe strips `"`, `\`, and control chars, then caps at 500 bytes,
// matching the bash `tr -d '"\\' | tr -d '[:cntrl:]' | head -c 500`.
func jsonSafe(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	out := b.String()
	if len(out) > 500 {
		out = out[:500]
	}
	return out
}

func isAlnumByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (a *apState) ensureRoot() { os.MkdirAll(filepath.Join(a.stateRoot, "tickets"), 0o755) }

func (a *apState) ticketDir(t string) (string, bool) {
	st := safeTicket(t)
	if st == "" {
		return "", false
	}
	return filepath.Join(a.stateRoot, "tickets", st), true
}

// gitOut runs git and returns trimmed stdout, or "" on error.
func gitOut(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// gitOK reports whether git exited 0 (for --verify probes).
func gitOK(args ...string) bool {
	return exec.Command("git", args...).Run() == nil
}

func (a *apState) postComment(ticket, body string) {
	if os.Getenv("BABYSIT_SKIP_COMMENT") == "1" || ticket == "" {
		return
	}
	if _, err := exec.LookPath("babysit"); err != nil {
		return
	}
	_ = exec.Command("babysit", "ticket", "comment", ticket, body).Run()
}

func (a *apState) appendTimeline(t, f, s, st, n string) {
	a.ensureRoot()
	line := fmt.Sprintf(`{"ts":"%s","ticket":"%s","workflow":"%s","step":"%s","status":"%s","branch":"%s","note":"%s","actor":"work"}`+"\n",
		isoNow(), jsonSafe(t), jsonSafe(f), jsonSafe(s), jsonSafe(st), jsonSafe(a.branch), jsonSafe(n))
	appendFile(filepath.Join(a.stateRoot, "timeline.jsonl"), line)
}

func appendFile(path, s string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(s)
}

// resolveWorkflowPath finds a workflow file across the standard search dirs.
// Paths are built by string concatenation (not filepath.Join) so an empty
// REPO_ROOT / CLAUDE_PLUGIN_ROOT yields a root-anchored path that simply won't
// exist — matching the bash, where the `[ -n "$DIR" ]` guard never trips
// because the suffix keeps every candidate non-empty.
func resolveWorkflowPath(name string) (string, bool) {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(name) {
		return "", false
	}
	repoRoot := gitOut("rev-parse", "--show-toplevel")
	home, _ := os.UserHomeDir()
	dirs := []string{
		repoRoot + "/.claude/workflows",
		os.Getenv("CLAUDE_PLUGIN_ROOT") + "/.claude/skills/autopilot/workflows",
		home + "/.claude/skills/bbs:autopilot/workflows",
		repoRoot + "/.claude/skills/autopilot/workflows",
	}
	for _, d := range dirs {
		p := d + "/" + name + ".md"
		if fileExists(p) {
			return p, true
		}
	}
	return "", false
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func fileNonEmpty(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

// ─── checkpoint ──────────────────────────────────────────────────────────────

func (a *apState) checkpoint(args []string) {
	var ticket, workflow, step, status, note, depth string
	var force, refresh bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ticket":
			ticket, i = next(args, i)
		case "--workflow":
			workflow, i = next(args, i)
		case "--step":
			step, i = next(args, i)
		case "--status":
			status, i = next(args, i)
		case "--note":
			note, i = next(args, i)
		case "--depth":
			depth, i = next(args, i)
		case "--force":
			force = true
		case "--refresh":
			refresh = true
		}
	}

	if refresh {
		a.checkpointRefresh(ticket)
		return
	}

	if ticket == "" || workflow == "" || step == "" || status == "" {
		fmt.Fprintln(os.Stderr, "checkpoint: --ticket, --workflow, --step, --status are required")
		os.Exit(2)
	}
	switch status {
	case "in_progress", "done_step", "done", "blocked":
	default:
		fmt.Fprintf(os.Stderr, "checkpoint: invalid status '%s'\n", status)
		os.Exit(2)
	}

	if a.ticket != "" && a.ticket != safeTicket(ticket) {
		fmt.Fprintf(os.Stderr, retarget("bbs-autopilot: WARNING branch ticket '%s' != --ticket '%s'\n"), a.ticket, ticket)
	}

	// --- Checkpoint validation guard ---
	if os.Getenv("BABYSIT_SKIP_CHECKPOINT_VALIDATION") != "1" {
		if depth != "" {
			d, err := strconv.Atoi(depth)
			if err != nil || d < 0 || !isAllDigits(depth) {
				fmt.Fprintln(os.Stderr, "checkpoint: --depth must be a non-negative integer")
				os.Exit(2)
			}
			if d > 3 {
				fmt.Fprintf(os.Stderr, "checkpoint: depth %s exceeds max (3)\n", depth)
				os.Exit(1)
			}
		}
		if !force {
			if wfPath, ok := resolveWorkflowPath(workflow); ok {
				steps := stepList(wfPath)
				if len(steps) > 0 {
					newPos := indexOf(steps, step) // 1-based, 0 if absent
					existingCP := filepath.Join(a.stateRoot, "tickets", safeTicket(ticket), "checkpoint.json")
					if existingStep := readJSONField(existingCP, "step"); existingStep != "" && newPos > 0 {
						oldPos := indexOf(steps, existingStep)
						if oldPos > 0 && newPos < oldPos {
							fmt.Fprintf(os.Stderr, "checkpoint: step regression: %s (%d) < %s (%d)\n", step, newPos, existingStep, oldPos)
							os.Exit(1)
						}
					}
				}
			}
		}
	}

	a.ensureRoot()
	dir, ok := a.ticketDir(ticket)
	if !ok {
		fmt.Fprintln(os.Stderr, "checkpoint: invalid ticket")
		os.Exit(2)
	}
	os.MkdirAll(dir, 0o755)

	ts := isoNow()
	headSha := jsonSafe(gitOut("rev-parse", "HEAD"))
	cp := filepath.Join(dir, "checkpoint.json")

	// Iteration tracking (loop-safety counters).
	iterCount, consecSame := 1, 1
	firstAt := ts
	if !force {
		if prev, ok := readCheckpoint(cp); ok {
			iterCount = prev.iterInt() + 1
			if prev.Step != "" && prev.Step == jsonSafe(step) {
				consecSame = prev.consecInt() + 1
			} else {
				consecSame = 1
			}
			if prev.FirstIterationAt != "" {
				firstAt = prev.FirstIterationAt
			}
		}
	}

	body := fmt.Sprintf(`{"ticket":"%s","workflow":"%s","step":"%s","status":"%s","note":"%s","branch":"%s","head_sha":"%s","slug":"%s","depth":"%s","updated_at":"%s","iteration_count":%d,"consecutive_same_step":%d,"first_iteration_at":"%s"}`+"\n",
		jsonSafe(ticket), jsonSafe(workflow), jsonSafe(step), status, jsonSafe(note), jsonSafe(a.branch), headSha, jsonSafe(a.slug), jsonSafe(depth), ts,
		iterCount, consecSame, jsonSafe(firstAt))
	if !atomicWrite(cp, body) {
		fmt.Fprintln(os.Stderr, "checkpoint: write failed")
		os.Exit(1)
	}

	// Per-ticket history.
	hist := fmt.Sprintf(`{"ts": "%s", "ticket": "%s", "workflow": "%s", "step": "%s", "status": "%s", "note": "%s", "branch": "%s", "actor": "work"}`+"\n",
		ts, jsonSafe(ticket), jsonSafe(workflow), jsonSafe(step), status, jsonSafe(note), jsonSafe(a.branch))
	appendFile(filepath.Join(dir, "history.jsonl"), hist)

	a.appendTimeline(ticket, workflow, step, status, note)
	a.bumpSession(ts)

	var comment string
	switch status {
	case "in_progress":
		comment = fmt.Sprintf("[WORK] %s:%s — started", jsonSafe(workflow), jsonSafe(step))
	case "done_step":
		comment = fmt.Sprintf("[WORK] %s:%s — done%s", jsonSafe(workflow), jsonSafe(step), suffixNote(note))
	case "done":
		comment = fmt.Sprintf("[WORK] %s complete%s", jsonSafe(workflow), suffixNote(note))
	case "blocked":
		comment = fmt.Sprintf("[WORK] %s:%s — BLOCKED%s", jsonSafe(workflow), jsonSafe(step), suffixNote(note))
	}
	a.postComment(ticket, comment)
}

// suffixNote reproduces `${N:+ — }$N`: a leading " — " only when the note is
// non-empty (N is the json_safe'd note).
func suffixNote(note string) string {
	n := jsonSafe(note)
	if n == "" {
		return ""
	}
	return " — " + n
}

func (a *apState) checkpointRefresh(ticket string) {
	if ticket == "" {
		ticket = a.ticket
	}
	if ticket == "" {
		fmt.Fprintln(os.Stderr, "checkpoint --refresh: ticket id required (and branch doesn't encode one)")
		os.Exit(2)
	}
	dir, ok := a.ticketDir(ticket)
	if !ok {
		fmt.Fprintln(os.Stderr, "checkpoint --refresh: invalid ticket")
		os.Exit(2)
	}
	cp := filepath.Join(dir, "checkpoint.json")
	prev, ok := readCheckpoint(cp)
	if !ok {
		fmt.Fprintf(os.Stderr, "checkpoint --refresh: no checkpoint for %s\n", ticket)
		os.Exit(1)
	}
	ts := isoNow()
	firstAt := prev.FirstIterationAt
	if firstAt == "" {
		firstAt = ts
	}
	headSha := jsonSafe(gitOut("rev-parse", "HEAD"))
	body := fmt.Sprintf(`{"ticket":"%s","workflow":"%s","step":"%s","status":"%s","note":"%s","branch":"%s","head_sha":"%s","slug":"%s","depth":"%s","updated_at":"%s","iteration_count":%s,"consecutive_same_step":%s,"first_iteration_at":"%s"}`+"\n",
		jsonSafe(ticket), jsonSafe(prev.Workflow), jsonSafe(prev.Step), prev.statusOrEmpty(), jsonSafe(prev.Note), jsonSafe(a.branch), headSha, jsonSafe(prev.Slug), jsonSafe(prev.Depth), ts,
		prev.iterStr(), prev.consecStr(), jsonSafe(firstAt))
	if !atomicWrite(cp, body) {
		fmt.Fprintln(os.Stderr, "checkpoint --refresh: post-write validation failed")
		os.Exit(1)
	}
}

// atomicWrite writes via a temp file + rename in the same dir, then validates
// the result parses as JSON (restoring nothing — the caller reports failure).
func atomicWrite(path, body string) bool {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".cp.*")
	if err != nil {
		return false
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(body)
	tmp.Close()
	if werr != nil {
		os.Remove(name)
		return false
	}
	if !json.Valid([]byte(body)) {
		os.Remove(name)
		return false
	}
	return os.Rename(name, path) == nil
}

// ─── read / clear / current / set-current / timeline ─────────────────────────

func (a *apState) read(args []string) {
	ticket := arg0(args, a.ticket)
	if ticket == "" {
		fmt.Fprintln(os.Stderr, "read: ticket id required (and branch doesn't encode one)")
		os.Exit(2)
	}
	dir, ok := a.ticketDir(ticket)
	if !ok {
		os.Exit(2)
	}
	cp := filepath.Join(dir, "checkpoint.json")
	b, err := os.ReadFile(cp)
	if err != nil {
		os.Exit(0)
	}
	if !json.Valid(b) {
		fmt.Fprintf(os.Stderr, "read: %s is malformed JSON\n", cp)
		os.Exit(1)
	}
	os.Stdout.Write(b)
}

func (a *apState) clear(args []string) {
	ticket := arg0(args, "")
	if ticket == "" {
		fmt.Fprintln(os.Stderr, "clear: ticket id required")
		os.Exit(2)
	}
	dir, ok := a.ticketDir(ticket)
	if !ok {
		os.Exit(2)
	}
	os.RemoveAll(dir)
	cur := filepath.Join(a.stateRoot, "current.txt")
	if b, err := os.ReadFile(cur); err == nil {
		// bash: grep -q " $TICKET$" (raw ticket, line-anchored suffix)
		for _, ln := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if strings.HasSuffix(ln, " "+ticket) {
				os.Remove(cur)
				break
			}
		}
	}
}

func (a *apState) current() {
	cur := filepath.Join(a.stateRoot, "current.txt")
	if b, err := os.ReadFile(cur); err == nil {
		os.Stdout.Write(b)
	}
}

func (a *apState) setCurrent(args []string) {
	w := arg0(args, "")
	t := ""
	if len(args) > 1 {
		t = args[1]
	}
	if w == "" || t == "" {
		fmt.Fprintln(os.Stderr, "set-current: workflow and ticket required")
		os.Exit(2)
	}
	a.ensureRoot()
	os.WriteFile(filepath.Join(a.stateRoot, "current.txt"),
		[]byte(fmt.Sprintf("%s %s\n", jsonSafe(w), jsonSafe(t))), 0o644)
}

func (a *apState) timeline(args []string) {
	var ticket, workflow, step, note string
	status := "event"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ticket":
			ticket, i = next(args, i)
		case "--workflow":
			workflow, i = next(args, i)
		case "--step":
			step, i = next(args, i)
		case "--status", "--event":
			status, i = next(args, i)
		case "--note":
			note, i = next(args, i)
		}
	}
	if ticket == "" {
		ticket = a.ticket
	}
	a.appendTimeline(ticket, workflow, step, status, note)
}

// ─── recover ─────────────────────────────────────────────────────────────────

func (a *apState) recover() {
	fmt.Println("--- BABYSIT CONTEXT RECOVERY ---")
	fmt.Printf("SLUG: %s\n", a.slug)
	fmt.Printf("BRANCH: %s\n", a.branch)
	if a.ticket != "" {
		fmt.Printf("CURRENT_TICKET: %s (derived from branch)\n", a.ticket)
		cp := filepath.Join(a.stateRoot, "tickets", a.ticket, "checkpoint.json")
		if b, err := os.ReadFile(cp); err == nil {
			fmt.Printf("LATEST_CHECKPOINT: %s\n", cp)
			os.Stdout.Write(b)
			cpSha := readJSONField(cp, "head_sha")
			curSha := gitOut("rev-parse", "HEAD")
			if cpSha != "" && curSha != "" {
				if cpSha == curSha {
					fmt.Println("HEAD_DRIFT: none (HEAD == checkpoint sha)")
				} else {
					fmt.Printf("HEAD_DRIFT: YES — HEAD=%s != checkpoint=%s; commits landed since this checkpoint, re-probe before resuming\n", curSha, cpSha)
				}
			}
		} else {
			fmt.Println("LATEST_CHECKPOINT: none (no prior state for this ticket)")
		}
		ticketBin := a.sibling("bbs-ticket")
		if isExecutable(ticketBin) {
			evStatus := a.ticketOut(a.ticket, "evidence-status", "--kind", "verification")
			if evStatus == "" {
				evStatus = "none"
			}
			if evStatus == "valid" {
				evPath := filepath.Join(a.stateRoot, "tickets", a.ticket, "evidence", "verification", "result.json")
				res := readJSONField(evPath, "result")
				if res == "" {
					res = "unknown"
				}
				fmt.Printf("LAST_VERIFICATION: %s (typed evidence — %s)\n", res, evPath)
			} else {
				fmt.Printf("LAST_VERIFICATION: %s (no PASS/FAIL on record — re-run to establish)\n", evStatus)
			}
		}
	} else {
		fmt.Println("CURRENT_TICKET: (branch does not encode a ticket)")
	}
	if b, err := os.ReadFile(filepath.Join(a.stateRoot, "current.txt")); err == nil {
		fmt.Printf("ACTIVE_PAIR: %s\n", strings.TrimRight(string(b), "\n"))
	}
	tl := filepath.Join(a.stateRoot, "timeline.jsonl")
	if b, err := os.ReadFile(tl); err == nil {
		fmt.Println("RECENT_EVENTS:")
		fmt.Println(tailLines(strings.TrimRight(string(b), "\n"), 5))
	}
	fmt.Println("--- END RECOVERY ---")
}

// ─── base-branch ─────────────────────────────────────────────────────────────

func (a *apState) baseBranch() string {
	if v := os.Getenv("BBS_BASE_BRANCH"); v != "" {
		return v
	}
	top := gitOut("rev-parse", "--show-toplevel")
	if top != "" {
		gf := filepath.Join(top, ".babysit", "git-flow.yaml")
		if b, err := os.ReadFile(gf); err == nil {
			if base := gitFlowBase(string(b)); base != "" {
				return base
			}
		}
	}
	cfg := a.sibling("bbs-config")
	if isExecutable(cfg) {
		out, err := exec.Command(cfg, "get", "base_branch").Output()
		if err == nil {
			if v := strings.TrimRight(string(out), "\n"); v != "" {
				return v
			}
		}
	}
	if gitOK("rev-parse", "--verify", "-q", "origin/HEAD") {
		h := gitOut("rev-parse", "--abbrev-ref", "origin/HEAD")
		h = strings.TrimPrefix(h, "origin/")
		if h != "" {
			return h
		}
	}
	return "main"
}

// gitFlowBase ports the two awk passes: top-level `base_branch:`, then the
// `develop:` key nested under `branches:`.
func gitFlowBase(content string) string {
	lines := strings.Split(content, "\n")
	// awk sub(/[[:space:]]+#.*$/) strips only a comment preceded by whitespace.
	commentTail := regexp.MustCompile(`\s+#.*$`)
	trimComment := func(s string) string {
		if j := commentTail.FindStringIndex(s); j != nil {
			return s[:j[0]]
		}
		return s
	}
	unquote := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimPrefix(s, `"`)
		s = strings.TrimPrefix(s, `'`)
		s = strings.TrimSuffix(s, `"`)
		s = strings.TrimSuffix(s, `'`)
		return s
	}
	commentLine := regexp.MustCompile(`^\s*#`)
	// pass 1: base_branch:
	for _, ln := range lines {
		if commentLine.MatchString(ln) {
			continue
		}
		if strings.HasPrefix(ln, "base_branch:") {
			v := strings.TrimPrefix(ln, "base_branch:")
			v = strings.TrimLeft(v, " \t")
			v = trimComment(v)
			return unquote(v)
		}
	}
	// pass 2: branches: → develop:
	topKey := regexp.MustCompile(`^[A-Za-z_]`)
	branchesHdr := regexp.MustCompile(`^branches:\s*$`)
	develop := regexp.MustCompile(`^\s+develop:\s*`)
	inBranches := false
	for _, ln := range lines {
		if commentLine.MatchString(ln) {
			continue
		}
		if branchesHdr.MatchString(ln) {
			inBranches = true
			continue
		}
		if topKey.MatchString(ln) && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			inBranches = false
		}
		if inBranches && develop.MatchString(ln) {
			v := develop.ReplaceAllString(ln, "")
			v = trimComment(v)
			return unquote(v)
		}
	}
	return ""
}

// ─── lint-workflow ───────────────────────────────────────────────────────────

func (a *apState) lintWorkflow(args []string) {
	wf := arg0(args, "")
	if wf == "" {
		fmt.Fprintln(os.Stderr, "lint-workflow: path required")
		os.Exit(2)
	}
	if !fileExists(wf) {
		fmt.Fprintf(os.Stderr, "lint-workflow: %s: not a file\n", wf)
		os.Exit(2)
	}
	b, _ := os.ReadFile(wf)
	lines := strings.Split(string(b), "\n")
	errors := 0

	fm := frontmatterLines(lines)
	if len(fm) == 0 {
		fmt.Fprintf(os.Stderr, "%s: missing YAML frontmatter\n", wf)
		errors++
	}
	nsRe := regexp.MustCompile(`^needs-state:(\s|$|#)`)
	hasNS := false
	for _, ln := range fm {
		if nsRe.MatchString(ln) {
			hasNS = true
			break
		}
	}
	if !hasNS {
		fmt.Fprintf(os.Stderr, "%s: missing 'needs-state:' frontmatter — every dispatch target must declare its prerequisites\n", wf)
		errors++
	}

	// Warn (not fail) per `## step` heading without a `> produces:` directive.
	var order []string
	produces := map[string]bool{}
	curStep := ""
	for _, ln := range lines {
		if strings.HasPrefix(ln, "## ") {
			curStep = strings.TrimPrefix(ln, "## ")
			produces[curStep] = false
			order = append(order, curStep)
		} else if strings.HasPrefix(ln, "> produces:") && curStep != "" {
			produces[curStep] = true
		}
	}
	for _, s := range order {
		if !produces[s] {
			fmt.Fprintf(os.Stderr, "WARN %s: step \"%s\" has no `> produces:` directive — Verify-post will be skipped\n", wf, s)
		}
	}
	os.Exit(errors)
}

// frontmatterLines returns the lines between the first two `---` delimiters.
func frontmatterLines(lines []string) []string {
	delim := regexp.MustCompile(`^---\s*$`)
	n := 0
	var out []string
	for _, ln := range lines {
		if delim.MatchString(ln) {
			n++
			if n == 2 {
				break
			}
			continue
		}
		if n == 1 {
			out = append(out, ln)
		}
	}
	return out
}

// ─── probe ───────────────────────────────────────────────────────────────────

type probeResult struct {
	ticket         string
	requirementMD  int
	planMD         int
	planApproved   int
	manifestMD     int
	originType     string
	commitsAhead   string
	branchPushed   int
	repoConfigured int
	landingDoc     int
	branch         string
	slug           string
	base           string
}

func (a *apState) probeState(ticket string) probeResult {
	if ticket == "" {
		ticket = a.ticket
	}
	r := probeResult{ticket: ticket, commitsAhead: "0", branch: a.branch, slug: a.slug}
	ticketBin := a.sibling("bbs-ticket")
	if ticket != "" && isExecutable(ticketBin) {
		a.ticketRun(ticket, "init")
		if req := a.ticketOut(ticket, "path", "requirement", "--read"); req != "" && fileNonEmpty(req) {
			r.requirementMD = 1
		}
		if plan := a.ticketOut(ticket, "path", "plan", "--read"); plan != "" && fileNonEmpty(plan) {
			r.planMD = 1
		}
		if man := a.ticketOut(ticket, "path", "manifest", "--read"); man != "" && fileNonEmpty(man) {
			r.manifestMD = 1
		}
		r.originType = a.ticketOut(ticket, "get", "origin.type")
		verdict := a.ticketOut(ticket, "verdict-status", "--skill", "plan-draft")
		if verdict == "" {
			verdict = "none"
		}
		switch {
		case verdict == "DONE" || verdict == "DONE_WITH_CONCERNS" || strings.HasPrefix(verdict, "PLANNED"):
			r.planApproved = 1
		}
	}

	r.base = a.baseBranch()
	if c := gitOut("rev-list", "--count", "origin/"+r.base+"..HEAD"); c != "" {
		r.commitsAhead = c
	}
	if gitOK("rev-parse", "--verify", "origin/"+a.branch) {
		r.branchPushed = 1
	}

	top := gitOut("rev-parse", "--show-toplevel")
	if top != "" {
		if fileExists(filepath.Join(top, ".babysit", "git-flow.yaml")) {
			r.repoConfigured = 1
		}
		if fileExists(filepath.Join(top, "CLAUDE.md")) || fileExists(filepath.Join(top, "AGENTS.md")) {
			r.landingDoc = 1
		}
	}
	return r
}

func (a *apState) probe(args []string) {
	ticket := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--ticket" {
			ticket, i = next(args, i)
		}
	}
	r := a.probeState(ticket)
	fmt.Printf("state_ticket=%s\n", r.ticket)
	fmt.Printf("state_requirement_md=%d\n", r.requirementMD)
	fmt.Printf("state_plan_md=%d\n", r.planMD)
	fmt.Printf("state_plan_approved=%d\n", r.planApproved)
	fmt.Printf("state_manifest_md=%d\n", r.manifestMD)
	fmt.Printf("state_origin_type=%s\n", r.originType)
	fmt.Printf("state_commits_ahead=%s\n", r.commitsAhead)
	fmt.Printf("state_branch_pushed=%d\n", r.branchPushed)
	fmt.Printf("state_repo_configured=%d\n", r.repoConfigured)
	fmt.Printf("state_landing_doc=%d\n", r.landingDoc)
	fmt.Printf("BRANCH=%s\n", r.branch)
	fmt.Printf("SLUG=%s\n", r.slug)
	fmt.Printf("BASE=%s\n", r.base)
}

// ─── explain ─────────────────────────────────────────────────────────────────

func (a *apState) explain(args []string) {
	ticket := ""
	details := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--details" {
			details = true
		} else {
			ticket = args[i]
		}
	}
	r := a.probeState(ticket)

	fmt.Println("=== autopilot state ===")
	ticketDisp := r.ticket
	if ticketDisp == "" {
		ticketDisp = "<none — branch does not encode one>"
	}
	fmt.Printf("ticket:          %s\n", ticketDisp)
	fmt.Printf("branch:          %s\n", r.branch)
	fmt.Printf("slug:            %s\n", r.slug)
	fmt.Printf("base_branch:     %s\n", r.base)
	fmt.Println()
	fmt.Printf("requirement_md:  %d\n", r.requirementMD)
	fmt.Printf("plan_md:         %d\n", r.planMD)
	fmt.Printf(retarget("plan_approved:   %d  (from bbs-ticket verdict-status --skill plan-draft)\n"), r.planApproved)
	fmt.Printf("manifest_md:     %d  (plan-draft DECOMPOSED writes manifest.md)\n", r.manifestMD)
	originDisp := r.originType
	if originDisp == "" {
		originDisp = "<unset>"
	}
	fmt.Printf("origin_type:     %s  (from index.json origin.type)\n", originDisp)
	fmt.Printf("commits_ahead:   %s  (vs origin/%s)\n", r.commitsAhead, r.base)
	fmt.Printf("branch_pushed:   %d\n", r.branchPushed)
	fmt.Printf("repo_configured: %d  (.babysit/git-flow.yaml present — Phase-1 readiness)\n", r.repoConfigured)
	fmt.Printf("landing_doc:     %d  (CLAUDE.md or AGENTS.md present — Phase-1 readiness)\n", r.landingDoc)
	fmt.Println()

	best, reason := routeWorkflow(r)
	fmt.Println("=== recommended workflow ===")
	if best != "" {
		fmt.Printf("%s — %s\n", best, reason)
	} else {
		fmt.Println("NEEDS_CONTEXT — no ticket, requirement, plan, manifest, or branch work to route.")
		fmt.Println("  For intent-driven work, name the archetype: prototyper | sweeper | grower | maintainer.")
	}
	fmt.Println()

	if !details {
		fmt.Println(retarget("details: run 'bbs-autopilot explain --details' for the workflow prereq matrix"))
		return
	}
	a.explainMatrix(r)
}

// routeWorkflow reproduces the best_workflow decision cascade.
func routeWorkflow(r probeResult) (string, string) {
	switch {
	case r.originType == "sub_ticket":
		return "builder", "ticket origin is sub_ticket → builder (child mode)"
	case r.manifestMD == 1:
		return "builder", "manifest.md exists → builder (orchestrate mode)"
	case r.ticket != "" && r.planMD == 1:
		return "builder", "ticket has plan.md → builder (implement mode)"
	case r.requirementMD == 1 && r.planMD == 0:
		return "builder", "requirement.md exists, plan.md absent → builder (build mode)"
	case atoiSafe(r.commitsAhead) >= 1:
		return "builder", "branch has commits ahead of origin/" + r.base + " → builder (verify mode)"
	case r.branchPushed == 1 && r.branch != r.base:
		return "builder", "current non-base branch exists on origin → builder (verify mode)"
	}
	return "", ""
}

func (a *apState) explainMatrix(r probeResult) {
	repoRoot := gitOut("rev-parse", "--show-toplevel")
	home, _ := os.UserHomeDir()
	var dirs []string
	if repoRoot != "" && isDir(filepath.Join(repoRoot, ".claude/workflows")) {
		dirs = append(dirs, filepath.Join(repoRoot, ".claude/workflows"))
	}
	if pr := os.Getenv("CLAUDE_PLUGIN_ROOT"); pr != "" && isDir(filepath.Join(pr, ".claude/skills/autopilot/workflows")) {
		dirs = append(dirs, filepath.Join(pr, ".claude/skills/autopilot/workflows"))
	}
	if isDir(filepath.Join(home, ".claude/skills/bbs:autopilot/workflows")) {
		dirs = append(dirs, filepath.Join(home, ".claude/skills/bbs:autopilot/workflows"))
	}
	if repoRoot != "" && isDir(filepath.Join(repoRoot, ".claude/skills/autopilot/workflows")) {
		dirs = append(dirs, filepath.Join(repoRoot, ".claude/skills/autopilot/workflows"))
	}

	fmt.Println("=== workflow prereq evaluation ===")
	hdr := "%-14s %-8s %-8s %-8s %-11s %-10s %-12s %-14s  %s\n"
	fmt.Printf(hdr, "workflow", "ticket", "req_md", "plan_md", "plan_approv", "manifest", "origin_type", "commits/pushed", "verdict")
	fmt.Printf(hdr, "--------", "------", "------", "-------", "-----------", "--------", "-----------", "--------------", "-------")

	seen := map[string]bool{}
	for _, d := range dirs {
		entries, _ := filepath.Glob(filepath.Join(d, "*.md"))
		sortStrings(entries)
		for _, wf := range entries {
			name := strings.TrimSuffix(filepath.Base(wf), ".md")
			if seen[name] {
				continue
			}
			seen[name] = true

			block := needsStateBlock(wf)
			if len(block) == 0 {
				fmt.Printf("%-12s %s\n", name, "(no needs-state: declared — will not match)")
				continue
			}
			cells := map[string]string{
				"ticket": "—", "req": "—", "plan": "—", "approv": "—",
				"manifest": "—", "origin": "—", "commits": "—", "pushed": "",
			}
			overall := "MATCH"
			for _, kv := range block {
				key := strings.TrimSpace(kv[:strings.Index(kv, ":")])
				val := strings.TrimSpace(kv[strings.Index(kv, ":")+1:])
				mark := ""
				switch key {
				case "ticket":
					probed := boolTo01(r.ticket != "")
					mark = evalMark(probed, val)
					cells["ticket"] = mark
				case "requirement_md":
					mark = evalMark(strconv.Itoa(r.requirementMD), val)
					cells["req"] = mark
				case "plan_md":
					mark = evalMark(strconv.Itoa(r.planMD), val)
					cells["plan"] = mark
				case "plan_approved":
					mark = evalMark(strconv.Itoa(r.planApproved), val)
					cells["approv"] = mark
				case "manifest_md":
					mark = evalMark(strconv.Itoa(r.manifestMD), val)
					cells["manifest"] = mark
				case "origin_type":
					mark = evalLiteral(r.originType, val)
					cells["origin"] = mark
				case "commits_ahead":
					mark = evalMark(r.commitsAhead, val)
					cells["commits"] = mark
				case "branch_pushed":
					mark = evalMark(strconv.Itoa(r.branchPushed), val)
					cells["pushed"] = mark
				}
				if mark == "FAIL" {
					overall = "NO MATCH"
				}
			}
			cp := cells["commits"]
			if cells["pushed"] != "" {
				cp = cells["commits"] + "/" + cells["pushed"]
			}
			fmt.Printf(hdr, name, cells["ticket"], cells["req"], cells["plan"],
				cells["approv"], cells["manifest"], cells["origin"], cp, overall)
		}
	}
}

// needsStateBlock extracts the indented `key: value` lines under `needs-state:`
// in a workflow's frontmatter.
func needsStateBlock(path string) []string {
	b, _ := os.ReadFile(path)
	lines := strings.Split(string(b), "\n")
	delim := regexp.MustCompile(`^---\s*$`)
	indentedKey := regexp.MustCompile(`^\s+[A-Za-z_]`)
	topKey := regexp.MustCompile(`^[A-Za-z]`)
	fm := 0
	inNS := false
	var out []string
	for _, ln := range lines {
		if delim.MatchString(ln) {
			fm++
			if fm == 2 {
				break
			}
			continue
		}
		if fm != 1 {
			continue
		}
		if strings.HasPrefix(ln, "needs-state:") {
			inNS = true
			continue
		}
		if inNS {
			if indentedKey.MatchString(ln) {
				line := ln
				if i := strings.Index(line, "#"); i >= 0 {
					line = line[:i]
				}
				line = strings.TrimSpace(line)
				if line != "" {
					out = append(out, line)
				}
			} else if topKey.MatchString(ln) {
				inNS = false
			}
		}
	}
	return out
}

func evalMark(probed, rule string) string {
	switch rule {
	case "required":
		return passIf(probed == "1")
	case "absent":
		return passIf(probed == "0")
	case "optional":
		return "PASS(opt)"
	case "present":
		return "PASS(any)"
	case "1+":
		return passIf(atoiSafe(probed) >= 1)
	default:
		fmt.Fprintf(os.Stderr, "warn: unknown needs-state rule '%s'\n", rule)
		return "FAIL"
	}
}

func evalLiteral(probed, rule string) string { return passIf(probed == rule) }

func passIf(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// ─── check-skill-deps ────────────────────────────────────────────────────────

func (a *apState) checkSkillDeps(args []string) {
	skillMD := arg0(args, "")
	if skillMD == "" {
		fmt.Fprintln(os.Stderr, "check-skill-deps: SKILL.md path required")
		os.Exit(2)
	}
	if !fileExists(skillMD) {
		fmt.Fprintf(os.Stderr, "check-skill-deps: %s not found\n", skillMD)
		os.Exit(2)
	}
	b, _ := os.ReadFile(skillMD)
	needsLine := frontmatterNeeds(string(b))
	if needsLine == "" {
		os.Exit(0)
	}
	depsStr := strings.NewReplacer("[", "", "]", "").Replace(
		strings.TrimSpace(strings.TrimPrefix(needsLine, "needs:")))
	if strings.TrimSpace(depsStr) == "" {
		os.Exit(0)
	}
	failures := 0
	for _, dep := range strings.Split(depsStr, ",") {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		typ := dep
		value := ""
		if i := strings.Index(dep, ":"); i >= 0 {
			typ = dep[:i]
			value = dep[i+1:]
		}
		switch typ {
		case "binary":
			if _, err := exec.LookPath(value); err != nil {
				failures++
				fmt.Fprintf(os.Stderr, "missing dep: %s (binary '%s' not on PATH)\n", dep, value)
			}
		case "env":
			if os.Getenv(value) == "" {
				failures++
				fmt.Fprintf(os.Stderr, "missing dep: %s (env '%s' not set)\n", dep, value)
			}
		case "connected":
			fmt.Fprintf(os.Stderr, "warn: dep type 'connected' not yet implemented, skipping %s\n", dep)
		default:
			fmt.Fprintf(os.Stderr, "warn: unknown dep type '%s' for '%s', skipping\n", typ, dep)
		}
	}
	if failures > 0 {
		os.Exit(1)
	}
	os.Exit(0)
}

// frontmatterNeeds returns the `needs:` line inside the first `---`…`---` block.
func frontmatterNeeds(content string) string {
	lines := strings.Split(content, "\n")
	inFM := false
	for _, ln := range lines {
		if ln == "---" {
			if !inFM {
				inFM = true
				continue
			}
			break
		}
		if inFM && strings.HasPrefix(ln, "needs:") {
			return ln
		}
	}
	return ""
}

// ─── session bump ────────────────────────────────────────────────────────────

func (a *apState) bumpSession(ts string) {
	sess := os.Getenv("BABYSIT_SESSION")
	if sess == "" {
		return
	}
	sf := filepath.Join(babysitHome(), "sessions", sess+".yaml")
	b, err := os.ReadFile(sf)
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, "last_seen_at:") {
			lines[i] = "last_seen_at: " + ts
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(sf), ".session.*")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(strings.Join(lines, "\n"))
	tmp.Close()
	if werr != nil || os.Rename(name, sf) != nil {
		os.Remove(name)
	}
}

// ─── small utilities ─────────────────────────────────────────────────────────

// next returns args[i+1] and the advanced index (consuming the value), or the
// empty string when the flag has no value.
func next(args []string, i int) (string, int) {
	if i+1 < len(args) {
		return args[i+1], i + 1
	}
	return "", i
}

func arg0(args []string, def string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return def
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func boolTo01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i + 1
		}
	}
	return 0
}

func stepList(path string) []string {
	b, _ := os.ReadFile(path)
	var out []string
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "## ") {
			out = append(out, strings.TrimPrefix(ln, "## "))
		}
	}
	return out
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// ─── bbs-ticket exec + checkpoint JSON ───────────────────────────────────────

func (a *apState) ticketOut(ticket string, args ...string) string {
	c := exec.Command(a.sibling("bbs-ticket"), args...)
	c.Env = append(os.Environ(), "BBS_TICKET="+ticket)
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

func (a *apState) ticketRun(ticket string, args ...string) {
	c := exec.Command(a.sibling("bbs-ticket"), args...)
	c.Env = append(os.Environ(), "BBS_TICKET="+ticket)
	_ = c.Run()
}

// checkpointFile mirrors the fields refresh/iteration tracking read back.
type checkpointFile struct {
	Workflow            string          `json:"workflow"`
	Step                string          `json:"step"`
	Status              string          `json:"status"`
	Note                string          `json:"note"`
	Slug                string          `json:"slug"`
	Depth               string          `json:"depth"`
	IterationCount      json.RawMessage `json:"iteration_count"`
	ConsecutiveSameStep json.RawMessage `json:"consecutive_same_step"`
	FirstIterationAt    string          `json:"first_iteration_at"`
}

func readCheckpoint(path string) (checkpointFile, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return checkpointFile{}, false
	}
	var cf checkpointFile
	if json.Unmarshal(b, &cf) != nil {
		return checkpointFile{}, false
	}
	return cf, true
}

func (c checkpointFile) iterInt() int   { return rawInt(c.IterationCount, 0) }
func (c checkpointFile) consecInt() int { return rawInt(c.ConsecutiveSameStep, 0) }

// iterStr / consecStr preserve the existing numeric value verbatim for refresh
// (bash re-emits `.iteration_count // 1` unquoted). Default to 1 when absent.
func (c checkpointFile) iterStr() string   { return rawNum(c.IterationCount, "1") }
func (c checkpointFile) consecStr() string { return rawNum(c.ConsecutiveSameStep, "1") }

func (c checkpointFile) statusOrEmpty() string { return c.Status }

func rawInt(raw json.RawMessage, def int) int {
	if len(raw) == 0 {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return def
	}
	return n
}

func rawNum(raw json.RawMessage, def string) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return def
	}
	return s
}

// readJSONField reads a top-level string field from a JSON file, or "".
func readJSONField(path, field string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}
