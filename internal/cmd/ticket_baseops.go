package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This file ports the git-mutating base-ops family of bin/bbs-ticket.bash:
// merge-base, refresh, reset-base, switch, serve, and qa-lease. These land
// ticket-worktree branches on the shared primary checkout, keep the surface
// stable across parallel QA sessions (the qa-lease), and drive the human-review
// compose (serve). Every git mutation the bash did is reproduced exactly,
// including the merge-base lock in the shared git dir and the loud BLOCK
// messages on any unsafe position.

// ─── dir-aware git + shared helpers ──────────────────────────────────────────

func gitCOut(dir string, args ...string) string {
	return gitOut(append([]string{"-C", dir}, args...)...)
}

func gitCOK(dir string, args ...string) bool {
	return gitOK(append([]string{"-C", dir}, args...)...)
}

func insideWorkTree() bool { return gitOK("rev-parse", "--is-inside-work-tree") }

func haveGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// gitPrimary mirrors `git worktree list --porcelain | sed -n 's/^worktree //p'
// | head -1` — the primary checkout of the current repo.
func gitPrimary() string {
	for _, ln := range strings.Split(gitOut("worktree", "list", "--porcelain"), "\n") {
		if s, ok := strings.CutPrefix(ln, "worktree "); ok {
			return s
		}
	}
	return ""
}

// autopilotBin resolves the sibling bbs-autopilot the bash base-ops shell out to
// for base-branch (SCRIPT_DIR/bbs-autopilot → PATH → ~/.claude/bbs-autopilot).
func autopilotBin() string {
	if exe, err := os.Executable(); err == nil {
		if real, e := filepath.EvalSymlinks(exe); e == nil {
			exe = real
		}
		cand := filepath.Join(filepath.Dir(exe), "bbs-autopilot")
		if isExecutable(cand) {
			return cand
		}
	}
	if p, err := exec.LookPath("bbs-autopilot"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "bbs-autopilot")
}

// baseBranchIn resolves the base branch by shelling out to bbs-autopilot from
// dir (empty = cwd), falling back to "main" — exactly as the bash does.
func baseBranchIn(dir string) string {
	cmd := exec.Command(autopilotBin(), "base-branch")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	if b := strings.TrimRight(string(out), "\n"); b != "" {
		return b
	}
	return "main"
}

// lockAcquire / lockRelease mirror bin/lib/lock.sh: a spin-mkdir mutex at 100ms
// intervals. maxTries=300 ≈ 30s, matching bbs_lock_acquire "$LOCK" 300.
func lockAcquire(lockdir string, maxTries int) bool {
	for i := 0; i < maxTries; i++ {
		if err := os.Mkdir(lockdir, 0o755); err == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func lockRelease(lockdir string) { _ = os.RemoveAll(lockdir) }

// leaseRead returns the first `key=…` value from <dir>/owner (sed -n
// 's/^key=//p' | head -1).
func leaseRead(dir, key string) string {
	b, err := os.ReadFile(filepath.Join(dir, "owner"))
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(ln, key+"="); ok {
			return v
		}
	}
	return ""
}

func parseIntOr(s string, def int64) int64 {
	if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
		return n
	}
	return def
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ticketSafe mirrors bash safe(): keep [a-zA-Z0-9._/:-], cap at 256 bytes.
func ticketSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (r == '.' || r == '_' || r == '/' || r == ':' || r == '-' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 256 {
		out = out[:256]
	}
	return out
}

// qaLeaseGuard ports qa_lease_guard(): BLOCK (return 2) when the shared surface
// is qa-leased by a ticket other than `ownerTicket`. Own lease / free / stale
// (cleared with a warning) pass through (return 0).
func qaLeaseGuard(gitdir, cmd, ownerTicket string) int {
	dir := filepath.Join(gitdir, "bbs-qa-lease")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return 0
	}
	owner := leaseRead(dir, "owner")
	if ownerTicket != "" && owner == ownerTicket {
		return 0
	}
	now := time.Now().Unix()
	since := parseIntOr(leaseRead(dir, "since_epoch"), now)
	ttl := parseIntOr(leaseRead(dir, "ttl_min"), 60)
	age := (now - since) / 60
	if owner == "" || age > ttl {
		fmt.Fprintf(os.Stderr, "%s: warning — cleared stale qa-lease from '%s' (%dmin > %dmin ttl).\n",
			cmd, orUnknown(owner), age, ttl)
		_ = os.RemoveAll(dir)
		return 0
	}
	fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
	fmt.Fprintf(os.Stderr, "REASON: shared test surface is qa-leased by '%s' (%dmin into a %dmin lease) — %s would change it mid-QA.\n", owner, age, ttl, cmd)
	fmt.Fprintf(os.Stderr, "RECOMMENDATION: wait for '%s' to run 'bbs-ticket qa-lease release', or 'bbs-ticket qa-lease release --force' if that run is dead.\n", owner)
	return 2
}

// servingWrite ports _serving_write: persist what the primary serves as a comma
// list in <gitdir>/bbs-serving. set = exactly these tickets (none → 0 bytes);
// append = union with the existing list, order preserved, deduped.
func servingWrite(gitdir, mode string, tickets []string) {
	f := filepath.Join(gitdir, "bbs-serving")
	var items []string
	if mode == "append" {
		if b, err := os.ReadFile(f); err == nil {
			items = append(items, splitCommaSpace(string(b))...)
		}
	}
	items = append(items, tickets...)
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if it == "" || seen[it] {
			continue
		}
		seen[it] = true
		out = append(out, it)
	}
	if len(out) == 0 {
		_ = os.WriteFile(f, []byte{}, 0o644)
		return
	}
	_ = os.WriteFile(f, []byte(strings.Join(out, ",")+"\n"), 0o644)
}

func splitCommaSpace(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' })
}

// storeForTicket returns a Store bound to a specific ticket id under the same
// project home — the Go form of bash `TICKET=<id> history_append …`.
func storeForTicket(base identity.Env, ticketID string) *ticket.Store {
	e := base
	e.Ticket = ticketID
	return ticket.New(e)
}

// ticketExec re-invokes this binary as `bbs ticket <args>` in an isolated
// subprocess — the Go form of the bash base-ops calling `"$0" …`. dir sets cwd;
// extraEnv overlays os.Environ(); captureOut returns stdout; quietErr silences
// stderr (else it passes through so BLOCK messages surface).
func ticketExec(dir string, extraEnv map[string]string, captureOut, quietErr bool, args ...string) (string, int) {
	exe, err := os.Executable()
	if err == nil {
		if real, e := filepath.EvalSymlinks(exe); e == nil {
			exe = real
		}
	}
	cmd := exec.Command(exe, append([]string{"ticket"}, args...)...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	if captureOut {
		cmd.Stdout = &out
	}
	if !quietErr {
		cmd.Stderr = os.Stderr
	}
	rc := 0
	if e := cmd.Run(); e != nil {
		if ee, ok := e.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			rc = 1
		}
	}
	return strings.TrimRight(out.String(), "\n"), rc
}

// ─── merge-base ──────────────────────────────────────────────────────────────

func runMergeBase(args []string) {
	base := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--base" {
			base, i = valueAt(args, i), i+1
		}
	}
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "merge-base: git not found")
		os.Exit(2)
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "merge-base: not in a git work tree")
		os.Exit(2)
	}
	top := gitOut("rev-parse", "--show-toplevel")
	primary := gitPrimary()
	if primary == "" || primary == top {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintln(os.Stderr, "REASON: merge-base must run from a linked ticket worktree — cwd is the primary checkout.")
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: cd into the ticket worktree (.babysit/worktrees/<ticket>_<slug>/) and re-run.")
		os.Exit(2)
	}
	branch := gitOut("branch", "--show-current")
	if branch == "" {
		fmt.Fprintln(os.Stderr, "merge-base: detached HEAD in worktree — checkout the ticket branch first")
		os.Exit(2)
	}
	if gitOut("status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintln(os.Stderr, "REASON: worktree has uncommitted changes — the merge lands commits, not the working tree.")
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit (or stash) in the worktree, then re-run merge-base.")
		os.Exit(2)
	}
	if base == "" {
		base = baseBranchIn(primary)
	}
	if branch == base {
		fmt.Fprintf(os.Stderr, "merge-base: worktree is on the base branch '%s' — nothing to land\n", base)
		os.Exit(2)
	}
	primaryBranch := gitCOut(primary, "branch", "--show-current")
	if primaryBranch != base {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s is on '%s', not base '%s'.\n", primary, primaryBranch, base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: checkout '%s' in the primary checkout (or pass --base), then re-run.\n", base)
		os.Exit(2)
	}
	if gitCOut(primary, "status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s has uncommitted changes — merging would tangle them with the ticket.\n", primary)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit or stash the primary checkout's changes, then re-run merge-base.")
		os.Exit(2)
	}
	env := identity.Resolve()
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	if qaLeaseGuard(gitdir, "merge-base", env.Ticket) != 0 {
		os.Exit(2)
	}
	lock := filepath.Join(gitdir, "bbs-merge-base.lock")
	if !lockAcquire(lock, 300) {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: could not acquire %s after 30s — another merge-base may be stuck.\n", lock)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: if no merge is in flight, remove the lock dir and re-run.")
		os.Exit(2)
	}
	pre := gitCOut(primary, "rev-parse", "HEAD")
	if !gitCOK(primary, "merge", "--no-edit", branch) {
		gitCOK(primary, "merge", "--abort")
		lockRelease(lock)
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' (%s); merge aborted, primary untouched.\n", branch, base, primary)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: in the worktree, merge 'origin/%s' into '%s' (never local '%s' — it carries other tickets), resolve, commit, re-run merge-base.\n", base, branch, base)
		fmt.Fprintf(os.Stderr, "  If origin/%s merges clean, the conflict is with another in-flight ticket — QA solo via 'bbs-ticket switch %s' and land the PRs in sequence.\n", base, env.Ticket)
		os.Exit(2)
	}
	post := gitCOut(primary, "rev-parse", "HEAD")
	if env.Ticket != "" {
		servingWrite(gitdir, "append", []string{env.Ticket})
	}
	lockRelease(lock)
	if env.Ticket != "" {
		ticket.New(env).HistoryAppendExtra("merge_base", actorRole(),
			fmt.Sprintf(`{"base":"%s","head":"%s"}`, base, post))
	}
	if pre == post {
		fmt.Println("MERGED=0")
	} else {
		fmt.Println("MERGED=1")
	}
	fmt.Printf("BASE=%s\n", base)
	fmt.Printf("BRANCH=%s\n", branch)
	fmt.Printf("PRIMARY=%s\n", primary)
	fmt.Printf("HEAD=%s\n", post)
	fmt.Fprintf(os.Stderr, "merge-base: primary checkout now includes '%s' — test against the server there;\n", branch)
	fmt.Fprintln(os.Stderr, "  fix in this worktree, commit, and re-run merge-base after every QA fix.")
	os.Exit(0)
}

// ─── refresh ─────────────────────────────────────────────────────────────────

func runRefresh(args []string) {
	base := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--base" {
			base, i = valueAt(args, i), i+1
		}
	}
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "refresh: git not found")
		os.Exit(2)
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "refresh: not in a git work tree")
		os.Exit(2)
	}
	branch := gitOut("branch", "--show-current")
	if branch == "" {
		fmt.Fprintln(os.Stderr, "refresh: detached HEAD — checkout the ticket branch first")
		os.Exit(2)
	}
	if base == "" {
		base = baseBranchIn("")
	}
	if branch == base {
		fmt.Fprintf(os.Stderr, "refresh: on the base branch '%s' — nothing to refresh (reset-base maintains the primary)\n", base)
		os.Exit(2)
	}
	if gitOut("status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: '%s' has uncommitted changes — the merge needs a clean tree.\n", branch)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit (or stash) them, then re-run refresh.")
		os.Exit(2)
	}
	if !gitOK("fetch", "origin", base) {
		fmt.Fprintf(os.Stderr, "refresh: warning — fetch failed, using the last-known origin/%s\n", base)
	}
	if !gitOK("rev-parse", "--verify", "-q", "origin/"+base) {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: origin/%s not found — nothing safe to refresh from (local '%s' may carry other tickets' merges).\n", base, base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: add an 'origin' remote tracking '%s' (or fetch it), then re-run.\n", base)
		os.Exit(2)
	}
	if gitOK("merge-base", "--is-ancestor", "origin/"+base, "HEAD") {
		fmt.Println("UPDATED=0")
		fmt.Printf("BASE=%s\n", base)
		fmt.Fprintf(os.Stderr, "refresh: '%s' already contains origin/%s\n", branch, base)
		os.Exit(0)
	}
	if !gitOK("merge", "--no-edit", "origin/"+base) {
		gitOK("merge", "--abort")
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: merge conflict bringing origin/%s into '%s'; merge aborted, branch untouched.\n", base, branch)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: run 'git merge origin/%s' here, resolve, commit; then re-run merge-base/switch if this ticket is on the test surface.\n", base)
		os.Exit(2)
	}
	env := identity.Resolve()
	head := gitOut("rev-parse", "HEAD")
	if env.Ticket != "" {
		ticket.New(env).HistoryAppendExtra("refresh", actorRole(),
			fmt.Sprintf(`{"base":"%s","head":"%s"}`, base, head))
	}
	fmt.Println("UPDATED=1")
	fmt.Printf("BASE=%s\n", base)
	fmt.Printf("HEAD=%s\n", head)
	fmt.Fprintf(os.Stderr, "refresh: merged origin/%s into '%s' — if this ticket is on the test surface, re-run merge-base/switch.\n", base, branch)
	os.Exit(0)
}

// ─── reset-base ──────────────────────────────────────────────────────────────

func runResetBase(args []string) {
	rc := resetBase(args)
	os.Exit(rc)
}

// resetBase performs the reset and returns the exit code so switch can invoke it
// in-process the way bash calls `"$0" reset-base --quiet`.
func resetBase(args []string) int {
	base, quiet := "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--base":
			base, i = valueAt(args, i), i+1
		case "--quiet":
			quiet = true
		}
	}
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "reset-base: git not found")
		return 2
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "reset-base: not in a git work tree")
		return 2
	}
	primary := gitPrimary()
	if primary == "" {
		primary = gitOut("rev-parse", "--show-toplevel")
	}
	if base == "" {
		base = baseBranchIn(primary)
	}
	primaryBranch := gitCOut(primary, "branch", "--show-current")
	if primaryBranch != base {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s is on '%s', not base '%s'.\n", primary, primaryBranch, base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: checkout '%s' there (or pass --base), then re-run reset-base.\n", base)
		return 2
	}
	if gitCOut(primary, "status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s has uncommitted changes — reset --hard would destroy them.\n", primary)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit or stash them, then re-run reset-base.")
		return 2
	}
	if !gitCOK(primary, "fetch", "origin", base) {
		fmt.Fprintf(os.Stderr, "reset-base: warning — fetch failed, using the last-known origin/%s\n", base)
	}
	if !gitCOK(primary, "rev-parse", "--verify", "-q", "origin/"+base) {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: origin/%s not found — nothing safe to reset to.\n", base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: add an 'origin' remote tracking '%s' (or fetch it), then re-run.\n", base)
		return 2
	}
	stray := gitCOut(primary, "rev-list", "--no-merges", "origin/"+base+".."+base,
		"--not", "--exclude="+base, "--branches")
	if stray != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: '%s' has commits no other branch (or origin) holds — reset would lose real work:\n", base)
		lines := strings.Split(stray, "\n")
		for i, c := range lines {
			if i >= 5 {
				break
			}
			if ol := gitCOut(primary, "log", "-1", "--oneline", c); ol != "" {
				fmt.Fprintln(os.Stderr, ol)
			}
		}
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: push them, or move them to a ticket branch, then re-run reset-base.")
		return 2
	}
	env := identity.Resolve()
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	if qaLeaseGuard(gitdir, "reset-base", env.Ticket) != 0 {
		return 2
	}
	lock := filepath.Join(gitdir, "bbs-merge-base.lock")
	if !lockAcquire(lock, 300) {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: could not acquire %s after 30s — a merge-base may be in flight.\n", lock)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: if nothing is running, remove the lock dir and re-run.")
		return 2
	}
	pre := gitCOut(primary, "rev-parse", "HEAD")
	if !gitCOK(primary, "reset", "--hard", "origin/"+base) {
		lockRelease(lock)
		fmt.Fprintf(os.Stderr, "reset-base: git reset --hard origin/%s failed\n", base)
		return 2
	}
	post := gitCOut(primary, "rev-parse", "HEAD")
	servingWrite(gitdir, "set", nil)
	lockRelease(lock)
	if env.Ticket != "" {
		ticket.New(env).HistoryAppendExtra("reset_base", actorRole(),
			fmt.Sprintf(`{"base":"%s","head":"%s"}`, base, post))
	}
	if pre == post {
		fmt.Println("RESET=0")
	} else {
		fmt.Println("RESET=1")
	}
	fmt.Printf("BASE=%s\n", base)
	fmt.Printf("PRIMARY=%s\n", primary)
	fmt.Printf("HEAD=%s\n", post)
	if !quiet {
		fmt.Fprintf(os.Stderr, "reset-base: '%s' now matches origin — re-run merge-base from any in-flight worktree\n", base)
		fmt.Fprintln(os.Stderr, "  so the shared server serves those tickets again.")
	}
	return 0
}

// ─── switch ──────────────────────────────────────────────────────────────────

func runSwitch(args []string) {
	base := ""
	var tickets []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--base":
			base, i = valueAt(args, i), i+1
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "switch: unknown flag '%s'\n", args[i])
			os.Exit(2)
		default:
			tickets = append(tickets, args[i])
		}
	}
	if len(tickets) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bbs-ticket switch <ticket> [<ticket>...] [--base BRANCH]")
		os.Exit(2)
	}
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "switch: git not found")
		os.Exit(2)
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "switch: not in a git work tree")
		os.Exit(2)
	}
	primary := gitPrimary()
	if primary == "" {
		primary = gitOut("rev-parse", "--show-toplevel")
	}
	env := identity.Resolve()
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	if qaLeaseGuard(gitdir, "switch", env.Ticket) != 0 {
		os.Exit(2)
	}
	// Resolve every ticket to a branch before touching anything.
	var branches []string
	for _, t := range tickets {
		b := ""
		for _, ln := range strings.Split(gitOut("for-each-ref", "--format=%(refname:short)", "refs/heads/*/"+t+"_*"), "\n") {
			if ln != "" {
				b = ln
				break
			}
		}
		if b == "" {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			fmt.Fprintf(os.Stderr, "REASON: no local branch matches '*/%s_*' — unknown ticket or branch never cut.\n", t)
			fmt.Fprintln(os.Stderr, "RECOMMENDATION: check the id (bbs-ticket session list), or run ensure for it first.")
			os.Exit(2)
		}
		branches = append(branches, b)
	}
	// Clean slate via reset-base (its safety checks BLOCK loudly, stderr passes).
	rbArgs := []string{"--quiet"}
	if base != "" {
		rbArgs = append(rbArgs, "--base", base)
	}
	if rc := resetBase(rbArgs); rc != 0 {
		os.Exit(rc)
	}
	if base == "" {
		base = baseBranchIn(primary)
	}
	lock := filepath.Join(gitdir, "bbs-merge-base.lock")
	if !lockAcquire(lock, 300) {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: could not acquire %s after 30s — a merge-base may be in flight.\n", lock)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: if nothing is running, remove the lock dir and re-run.")
		os.Exit(2)
	}
	for _, b := range branches {
		if !gitCOK(primary, "merge", "--no-edit", b) {
			gitCOK(primary, "merge", "--abort")
			lockRelease(lock)
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' — that merge aborted; earlier tickets in this switch are already on the surface.\n", b, base)
			fmt.Fprintf(os.Stderr, "RECOMMENDATION: in that ticket's worktree, merge 'origin/%s' in (never local '%s'), resolve, commit, re-run switch.\n", base, base)
			fmt.Fprintf(os.Stderr, "  If origin/%s merges clean, it conflicts with an earlier ticket in this switch — switch them separately or resolve the pair together.\n", base)
			os.Exit(2)
		}
	}
	head := gitCOut(primary, "rev-parse", "HEAD")
	servingWrite(gitdir, "set", tickets)
	lockRelease(lock)
	fmt.Printf("BASE=%s\n", base)
	fmt.Printf("PRIMARY=%s\n", primary)
	fmt.Printf("HEAD=%s\n", head)
	fmt.Printf("SERVING=%s\n", strings.Join(tickets, ","))
	fmt.Fprintf(os.Stderr, "switch: test surface now serves '%s' + %s — fixes go in the ticket worktree(s), then re-run switch.\n", base, strings.Join(tickets, " "))
	os.Exit(0)
}

// ─── serve ───────────────────────────────────────────────────────────────────

func runServe(args []string) {
	var tickets []string
	release := false
	ttl := "240"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--release" || args[i] == "--done":
			release = true
		case args[i] == "--ttl-min":
			ttl, i = valueAt(args, i), i+1
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "serve: unknown flag '%s'\n", args[i])
			os.Exit(2)
		default:
			tickets = append(tickets, args[i])
		}
	}
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "serve: git not found")
		os.Exit(2)
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "serve: not in a git work tree")
		os.Exit(2)
	}
	primary := gitPrimary()
	if primary == "" {
		primary = gitOut("rev-parse", "--show-toplevel")
	}
	repo := filepath.Base(primary)
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	env := identity.Resolve()

	leaseOwner := func(gd string) string {
		return leaseRead(filepath.Join(gd, "bbs-qa-lease"), "owner")
	}
	// sibs returns index.json siblings for a ticket as (role,repo,ticket) rows.
	sibs := func(t string) []ticket.Sibling {
		return ticket.ReadIndex(filepath.Join(env.ProjectHome, "tickets", ticketSafe(t), "index.json")).Siblings
	}

	if release {
		if len(tickets) == 0 {
			if b, err := os.ReadFile(filepath.Join(gitdir, "bbs-serving")); err == nil {
				tickets = splitCommaSpace(string(b))
			}
		}
		owner := leaseOwner(gitdir)
		if owner != "" {
			if _, rc := ticketExec(primary, map[string]string{"BABYSIT_TICKET": owner}, false, false,
				"qa-lease", "release", "--ticket", owner); rc != 0 {
				os.Exit(rc)
			}
			fmt.Printf("RELEASED: %s %s\n", repo, owner)
		} else {
			fmt.Printf("RELEASED: %s (already free)\n", repo)
		}
		for _, t := range tickets {
			for _, s := range sibs(t) {
				if s.Ticket == "" {
					continue
				}
				sp, ok := relatedRepoPath(s.Role, primary)
				if !ok {
					fmt.Fprintf(os.Stderr, "serve: sibling %s/%s not released — RELATED repo path for role '%s' unresolved\n", s.Repo, s.Ticket, s.Role)
					continue
				}
				sgd := gitCOut(sp, "rev-parse", "--absolute-git-dir")
				sowner := leaseOwner(sgd)
				if sowner == "" {
					continue
				}
				if _, rc := ticketExec(sp, map[string]string{"BABYSIT_TICKET": sowner}, false, true,
					"qa-lease", "release", "--ticket", sowner); rc == 0 {
					fmt.Printf("RELEASED: %s %s\n", filepath.Base(sp), sowner)
				} else {
					fmt.Fprintf(os.Stderr, "serve: sibling release failed in %s for %s\n", sp, sowner)
				}
			}
		}
		os.Exit(0)
	}

	// Bare serve: a ticket in scope serves that ticket; otherwise the finished
	// batch — every open ticket with qa + review-pr DONE.
	if len(tickets) == 0 {
		if env.Ticket != "" {
			tickets = []string{env.Ticket}
		} else {
			dirs, _ := os.ReadDir(filepath.Join(env.ProjectHome, "tickets"))
			for _, d := range dirs {
				if !d.IsDir() {
					continue
				}
				t := d.Name()
				switch ticket.ReadDoc(filepath.Join(env.ProjectHome, "tickets", t, "index.json")).Get("status") {
				case "done", "cancelled", "duplicate":
					continue
				}
				if !serveVerdictOK(primary, t, "qa") || !serveVerdictOK(primary, t, "review-pr") {
					continue
				}
				tickets = append(tickets, t)
			}
			if len(tickets) == 0 {
				fmt.Fprintln(os.Stderr, "serve: nothing finished to serve — no open ticket has qa + review-pr DONE (see bbs-ticket board)")
				os.Exit(0)
			}
		}
	}

	svRC := 0
	// pickOwner reuses the live lease owner when it is one of the served tickets.
	pickOwner := func(gd string, ts []string) string {
		cur := leaseOwner(gd)
		if cur != "" {
			for _, t := range ts {
				if t == cur {
					return cur
				}
			}
		}
		return ts[0]
	}
	// serveSet: long lease + composed switch; a lease this call created (not one
	// refreshed) is released if the switch BLOCKs.
	serveSet := func(dir, owner string, ts []string) int {
		out, rc := ticketExec(dir, map[string]string{"BABYSIT_TICKET": owner}, true, false,
			"qa-lease", "acquire", "--ticket", owner, "--ttl-min", ttl)
		if rc != 0 {
			return 2
		}
		if _, rc := ticketExec(dir, map[string]string{"BABYSIT_TICKET": owner}, false, false,
			append([]string{"switch"}, ts...)...); rc != 0 {
			if !strings.Contains(out, "REFRESHED=1") {
				ticketExec(dir, map[string]string{"BABYSIT_TICKET": owner}, false, true,
					"qa-lease", "release", "--ticket", owner)
			}
			return 2
		}
		return 0
	}

	owner := pickOwner(gitdir, tickets)
	if serveSet(primary, owner, tickets) != 0 {
		os.Exit(2)
	}
	list := strings.Join(tickets, ",")
	fmt.Printf("SERVED: %s %s\n", repo, list)
	storeForTicket(env, owner).HistoryAppendExtra("serve", actorRole(),
		fmt.Sprintf(`{"tickets":"%s","ttl_min":%s}`, list, ttl))

	// Sibling fan-out: group served siblings by repo path (two tickets sharing a
	// sibling repo land there together), then one lease + one switch per repo.
	type sibRow struct{ path, ticket string }
	var rows []sibRow
	for _, t := range tickets {
		for _, s := range sibs(t) {
			if s.Ticket == "" {
				continue
			}
			sp, ok := relatedRepoPath(s.Role, primary)
			if !ok {
				fmt.Fprintln(os.Stderr, "STATUS: NEEDS_CONTEXT")
				fmt.Fprintf(os.Stderr, "REASON: sibling %s/%s (role '%s') has no resolvable local path — RELATED_*_REPO unset in %s/.babysit/.env.\n", s.Repo, s.Ticket, s.Role, primary)
				fmt.Fprintf(os.Stderr, "RECOMMENDATION: set it (see setup-project § Related Repos), then re-run serve; this repo is already serving %s.\n", list)
				svRC = 2
				continue
			}
			if fi, err := os.Stat(sp); err != nil || !fi.IsDir() {
				fmt.Fprintln(os.Stderr, "STATUS: NEEDS_CONTEXT")
				fmt.Fprintf(os.Stderr, "REASON: sibling repo path '%s' (role '%s') does not exist on this machine.\n", sp, s.Role)
				fmt.Fprintf(os.Stderr, "RECOMMENDATION: fix RELATED_*_REPO in .babysit/.env, then re-run serve; this repo is already serving %s.\n", list)
				svRC = 2
				continue
			}
			rows = append(rows, sibRow{sp, s.Ticket})
		}
	}
	// Unique repo paths, first-seen order.
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.path] {
			continue
		}
		seen[r.path] = true
		var ts []string
		for _, r2 := range rows {
			if r2.path == r.path {
				ts = append(ts, r2.ticket)
			}
		}
		sgd := gitCOut(r.path, "rev-parse", "--absolute-git-dir")
		sowner := pickOwner(sgd, ts)
		if serveSet(r.path, sowner, ts) == 0 {
			fmt.Printf("SERVED: %s %s\n", filepath.Base(r.path), strings.Join(ts, ","))
		} else {
			svRC = 2
		}
	}
	if svRC == 0 {
		fmt.Fprintf(os.Stderr, "serve: review on the dev server(s); fixes commit in the ticket worktree, then re-run 'bbs-ticket serve %s'.\n", strings.Join(tickets, " "))
		fmt.Fprintln(os.Stderr, "  Done reviewing → bbs-ticket serve --release")
	}
	os.Exit(svRC)
}

// serveVerdictOK reports whether the ticket's verdict for skill is DONE or
// DONE_WITH_CONCERNS, via a self-invocation matching the bash `verdict-status`.
func serveVerdictOK(primary, t, skill string) bool {
	out, _ := ticketExec(primary, map[string]string{"BABYSIT_TICKET": t}, true, true,
		"verdict-status", "--skill", skill)
	return out == "DONE" || out == "DONE_WITH_CONCERNS"
}

// (relatedRepoEnv / relatedRepoPath live in ticket_board.go.)

// ─── qa-lease ────────────────────────────────────────────────────────────────

func runQALease(args []string) {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
		args = args[1:]
	}
	qlTicket, ttl, force := "", "60", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ticket":
			qlTicket, i = valueAt(args, i), i+1
		case "--ttl-min":
			ttl, i = valueAt(args, i), i+1
		case "--force":
			force = true
		default:
			fmt.Fprintf(os.Stderr, "qa-lease: unknown arg '%s'\n", args[i])
			os.Exit(2)
		}
	}
	env := identity.Resolve()
	if qlTicket == "" {
		qlTicket = env.Ticket
	}
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "qa-lease: git not found")
		os.Exit(2)
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "qa-lease: not in a git work tree")
		os.Exit(2)
	}
	primary := gitPrimary()
	if primary == "" {
		primary = gitOut("rev-parse", "--show-toplevel")
	}
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	qlDir := filepath.Join(gitdir, "bbs-qa-lease")
	read := func(k string) string { return leaseRead(qlDir, k) }
	writeOwner := func() {
		body := fmt.Sprintf("owner=%s\npid=%d\nsince=%s\nsince_epoch=%d\nttl_min=%s\n",
			qlTicket, os.Getpid(), isoNow(), time.Now().Unix(), ttl)
		_ = os.WriteFile(filepath.Join(qlDir, "owner"), []byte(body), 0o644)
	}
	ageMin := func() int64 {
		now := time.Now().Unix()
		return (now - parseIntOr(read("since_epoch"), now)) / 60
	}

	switch verb {
	case "acquire":
		if qlTicket == "" {
			fmt.Fprintln(os.Stderr, "qa-lease: no owner — no ticket in scope and no --ticket given")
			os.Exit(2)
		}
		if os.Mkdir(qlDir, 0o755) == nil {
			writeOwner()
			if env.Ticket != "" {
				ticket.New(env).HistoryAppendExtra("qa_lease_acquire", actorRole(),
					fmt.Sprintf(`{"owner":"%s","ttl_min":%s}`, qlTicket, ttl))
			}
			fmt.Printf("OWNER=%s\nTTL_MIN=%s\nACQUIRED=1\n", qlTicket, ttl)
			os.Exit(0)
		}
		owner := read("owner")
		if owner != "" && owner == qlTicket {
			writeOwner()
			fmt.Printf("OWNER=%s\nTTL_MIN=%s\nACQUIRED=1\nREFRESHED=1\n", qlTicket, ttl)
			os.Exit(0)
		}
		age := ageMin()
		heldTTL := parseIntOr(read("ttl_min"), 60)
		if owner == "" || age > heldTTL {
			_ = os.RemoveAll(qlDir)
			if os.Mkdir(qlDir, 0o755) == nil {
				writeOwner()
				fmt.Fprintf(os.Stderr, "qa-lease: stole stale lease from '%s' (%dmin > %dmin ttl)\n", orUnknown(owner), age, heldTTL)
				if env.Ticket != "" {
					ticket.New(env).HistoryAppendExtra("qa_lease_steal", actorRole(),
						fmt.Sprintf(`{"owner":"%s","stolen_from":"%s","ttl_min":%s}`, qlTicket, orUnknown(owner), ttl))
				}
				fmt.Printf("OWNER=%s\nTTL_MIN=%s\nACQUIRED=1\nSTOLE_FROM=%s\n", qlTicket, ttl, orUnknown(owner))
				os.Exit(0)
			}
			owner = read("owner")
			age = ageMin()
		}
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: qa-lease held by '%s' (%dmin into a %dmin lease) — one QA session at a time on the shared surface.\n", orUnknown(owner), age, heldTTL)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: wait and re-run acquire, or 'bbs-ticket qa-lease release --force' if that run is dead.")
		os.Exit(2)
	case "release":
		if fi, err := os.Stat(qlDir); err != nil || !fi.IsDir() {
			fmt.Println("FREE=1")
			os.Exit(0)
		}
		owner := read("owner")
		if !force && owner != "" && owner != qlTicket {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			ticketDisp := qlTicket
			if ticketDisp == "" {
				ticketDisp = "<none>"
			}
			fmt.Fprintf(os.Stderr, "REASON: qa-lease belongs to '%s', not '%s' — releasing someone else's lease mid-QA corrupts their verdict.\n", owner, ticketDisp)
			fmt.Fprintln(os.Stderr, "RECOMMENDATION: let the owner release it, or pass --force if that run is dead.")
			os.Exit(2)
		}
		_ = os.RemoveAll(qlDir)
		if env.Ticket != "" {
			ticket.New(env).HistoryAppendExtra("qa_lease_release", actorRole(),
				fmt.Sprintf(`{"owner":"%s"}`, orUnknown(owner)))
		}
		fmt.Printf("RELEASED=1\nOWNER=%s\n", orUnknown(owner))
		os.Exit(0)
	case "status":
		if fi, err := os.Stat(qlDir); err != nil || !fi.IsDir() {
			fmt.Println("FREE")
			os.Exit(0)
		}
		fmt.Printf("OWNER=%s\n", read("owner"))
		fmt.Printf("AGE_MIN=%d\n", ageMin())
		fmt.Printf("TTL_MIN=%s\n", read("ttl_min"))
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "usage: bbs-ticket qa-lease <acquire|release|status> [--ticket ID] [--ttl-min N] [--force]")
		os.Exit(2)
	}
}
