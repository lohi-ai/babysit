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
	"github.com/reallongnguyen/babysit/internal/workspace"
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

// gitCRun is gitCOK plus git's own last line of output, so a caller can say
// what went wrong instead of guessing.
func gitCRun(dir string, args ...string) (bool, string) {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.CombinedOutput()
	last := ""
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			last = ln
		}
	}
	return err == nil, last
}

// mergeFailure classifies a failed `git merge` in dir. A content conflict
// leaves unmerged paths in the index; everything else (no committer identity, a
// rejected hook, a stale index.lock) leaves none and is a different problem
// with a different fix. Calling all of them "merge conflict" sends the operator
// into a worktree to resolve a file that was never in conflict — which is
// exactly how a missing git identity read as a conflict on every Linux run.
func mergeFailure(dir, gitSaid string) (conflicted bool, detail string) {
	if u := strings.TrimSpace(gitCOut(dir, "diff", "--name-only", "--diff-filter=U")); u != "" {
		return true, strings.Join(strings.Fields(u), ", ")
	}
	return false, gitSaid
}

func insideWorkTree() bool { return gitOK("rev-parse", "--is-inside-work-tree") }

// baseOpsPrimary resolves the primary checkout that reset-base/switch/serve/land
// all mutate, behind the two guards they all need first. It prints the reason
// and returns "" when there is nothing to work on, so every caller is one
// `if primary == "" { return 2 }`.
func baseOpsPrimary(cmd string) string {
	if !haveGit() {
		fmt.Fprintf(os.Stderr, "%s: git not found\n", cmd)
		return ""
	}
	if !insideWorkTree() {
		fmt.Fprintf(os.Stderr, "%s: not in a git work tree\n", cmd)
		return ""
	}
	if p := gitPrimary(); p != "" {
		return p
	}
	return gitOut("rev-parse", "--show-toplevel")
}

// parseTicketsAndBase is the argument contract switch and land share: bare
// words are ticket ids, --base overrides the resolved base, anything else is a
// typo worth refusing. ok=false means the reason is already on stderr.
func parseTicketsAndBase(cmd string, args []string) (tickets []string, base string, ok bool) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--base":
			base, i = valueAt(args, i), i+1
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "%s: unknown flag '%s'\n", cmd, args[i])
			return nil, "", false
		default:
			tickets = append(tickets, args[i])
		}
	}
	return tickets, base, true
}

// ticketBranch finds the local branch a ticket was cut on. Under the default
// trunk mode there is none — babysit cut nothing — so the miss is the common
// case, and it prints the BLOCK that says so before returning "".
func ticketBranch(t string) string {
	for _, ln := range strings.Split(gitOut("for-each-ref", "--format=%(refname:short)", "refs/heads/*/"+t+"_*"), "\n") {
		if ln != "" {
			return ln
		}
	}
	fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
	fmt.Fprintf(os.Stderr, "REASON: no local branch matches '*/%s_*' — unknown ticket or branch never cut.\n", t)
	fmt.Fprintln(os.Stderr, retarget("RECOMMENDATION: check the id (bbs-ticket session list), or run ensure for it first."))
	return ""
}

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

// The surface lease, the scratch marker, and the lifecycle that owns them
// (acquire → guard → reset/compose → mark → release) live in
// ticket_surface.go — the one implementation every handler below drives.

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

// ─── shared helpers ──────────────────────────────────────────────────────────

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

func runMergeBase(args []string) { os.Exit(mergeBase(args)) }

// mergeBase returns the exit code rather than calling os.Exit, so the surface
// lease can be released by a defer instead of by hand at each of the eight ways
// out. Releasing by hand is how a lock gets leaked.
func mergeBase(args []string) int {
	base := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--base" {
			base, i = valueAt(args, i), i+1
		}
	}
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "merge-base: git not found")
		return 2
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "merge-base: not in a git work tree")
		return 2
	}
	top := gitOut("rev-parse", "--show-toplevel")
	primary := gitPrimary()
	if primary == "" || primary == top {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintln(os.Stderr, "REASON: merge-base must run from a linked ticket worktree — cwd is the primary checkout.")
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: cd into the ticket worktree (.babysit/worktrees/<ticket>_<slug>/) and re-run.")
		return 2
	}
	branch := gitOut("branch", "--show-current")
	if branch == "" {
		fmt.Fprintln(os.Stderr, "merge-base: detached HEAD in worktree — checkout the ticket branch first")
		return 2
	}
	if gitOut("status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintln(os.Stderr, "REASON: worktree has uncommitted changes — the merge lands commits, not the working tree.")
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit (or stash) in the worktree, then re-run merge-base.")
		return 2
	}
	if base == "" {
		base = baseBranchIn(primary)
	}
	if branch == base {
		fmt.Fprintf(os.Stderr, "merge-base: worktree is on the base branch '%s' — nothing to land\n", base)
		return 2
	}
	env := resolveEnv()
	s, ok := acquireSurface("merge-base", primary, env.Ticket)
	if !ok {
		return 2
	}
	defer s.release()
	// Everything that reads the primary belongs under the lease. A peer's merge
	// leaves that checkout mid-write for as long as it runs, and reading it from
	// outside the lease sees the peer's half-written tree as the operator's own
	// uncommitted work — the ticket then fails to land, told to stash changes
	// that are not theirs.
	if !s.guardOnBase(base, ".",
		fmt.Sprintf("RECOMMENDATION: checkout '%s' in the primary checkout (or pass --base), then re-run.", base)) {
		return 2
	}
	if !s.guardClean(" — merging would tangle them with the ticket.",
		"RECOMMENDATION: commit or stash the primary checkout's changes, then re-run merge-base.") {
		return 2
	}
	pre := gitCOut(primary, "rev-parse", "HEAD")
	if m := s.merge(branch, false); !m.ok {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		if !m.conflicted {
			fmt.Fprintf(os.Stderr, "REASON: could not merge '%s' onto '%s' (%s) — no files conflicted; git said: %s\n", branch, base, primary, m.detail)
			fmt.Fprintln(os.Stderr, "RECOMMENDATION: fix what git reported (commonly an unset user.email/user.name, or a leftover index.lock), then re-run.")
			return 2
		}
		fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' (%s) in %s; merge aborted, primary untouched.\n", branch, base, primary, m.detail)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: in the worktree, merge 'origin/%s' into '%s' (never local '%s' — it carries other tickets), resolve, commit, re-run merge-base.\n", base, branch, base)
		fmt.Fprintf(os.Stderr, retarget("  If origin/%s merges clean, the conflict is with another in-flight ticket — QA solo via 'bbs-ticket switch %s' and land the PRs in sequence.\n"), base, env.Ticket)
		return 2
	}
	post := gitCOut(primary, "rev-parse", "HEAD")
	if env.Ticket != "" {
		servingWrite(s.gitdir, "append", []string{env.Ticket})
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
	return 0
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
	if ok, said := gitCRun(".", "merge", "--no-edit", "origin/"+base); !ok {
		conflicted, detail := mergeFailure(".", said)
		gitOK("merge", "--abort")
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		if !conflicted {
			fmt.Fprintf(os.Stderr, "REASON: could not bring origin/%s into '%s' — no files conflicted; git said: %s\n", base, branch, detail)
			fmt.Fprintln(os.Stderr, "RECOMMENDATION: fix what git reported (commonly an unset user.email/user.name, or a leftover index.lock), then re-run.")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "REASON: merge conflict bringing origin/%s into '%s' in %s; merge aborted, branch untouched.\n", base, branch, detail)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: run 'git merge origin/%s' here, resolve, commit; then re-run merge-base/switch if this ticket is on the test surface.\n", base)
		os.Exit(2)
	}
	env := resolveEnv()
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

func runResetBase(args []string) { os.Exit(resetBase(args)) }

// resetBase performs the reset and returns the exit code so switch can invoke it
// in-process the way bash calls `"$0" reset-base --quiet`.
//
// Its guards therefore fire for `switch` and `serve` too (serve shells out to
// switch, whose stderr passes straight through). That is why the RECOMMENDATION
// lines below say "then re-run" rather than naming reset-base: the operator ran
// serve, and telling them to re-run a command they never invoked sends them
// somewhere else entirely.
//
// When switch calls it, the surface lease is already held by this process and
// surfaceAcquire is reentrant — reset and merge have to be one step, or a second
// switch resets the base between them and its merge lands on a tree still
// carrying the first switch's ticket, while serving names only its own.
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
	primary := baseOpsPrimary("reset-base")
	if primary == "" {
		return 2
	}
	if base == "" {
		base = baseBranchIn(primary)
	}
	env := resolveEnv()
	s, ok := acquireSurface("reset-base", primary, env.Ticket)
	if !ok {
		return 2
	}
	defer s.release()
	// The fetch and every check inside read the primary, so they belong under
	// the lease: the stray-commit scan in particular compares refs a peer's
	// merge is in the middle of moving.
	if !s.resetToOrigin(base, quiet, env) {
		return 2
	}
	return 0
}

// ─── switch ──────────────────────────────────────────────────────────────────

func runSwitch(args []string) { os.Exit(switchSurface(args)) }

func switchSurface(args []string) int {
	tickets, base, ok := parseTicketsAndBase("switch", args)
	if !ok {
		return 2
	}
	if len(tickets) == 0 {
		fmt.Fprintln(os.Stderr, retarget("usage: bbs-ticket switch <ticket> [<ticket>...] [--base BRANCH]"))
		return 2
	}
	primary := baseOpsPrimary("switch")
	if primary == "" {
		return 2
	}
	env := resolveProject() // tickets are explicit args; env.Ticket is only the lease actor
	// Resolve every ticket to a branch before touching anything.
	var branches []string
	for _, t := range tickets {
		b := ticketBranch(t)
		if b == "" {
			return 2
		}
		branches = append(branches, b)
	}
	// One lease covers the reset and every merge. reset-base below finds it
	// already held by this process and is reentrant.
	s, ok := acquireSurface("switch", primary, env.Ticket)
	if !ok {
		return 2
	}
	defer s.release()
	// Clean slate via reset-base (its safety checks BLOCK loudly, stderr passes).
	rbArgs := []string{"--quiet"}
	if base != "" {
		rbArgs = append(rbArgs, "--base", base)
	}
	if rc := resetBase(rbArgs); rc != 0 {
		return rc
	}
	if base == "" {
		base = baseBranchIn(primary)
	}
	for _, b := range branches {
		if m := s.merge(b, false); !m.ok {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			if !m.conflicted {
				fmt.Fprintf(os.Stderr, "REASON: could not merge '%s' onto '%s' — no files conflicted; git said: %s\n", b, base, m.detail)
				fmt.Fprintln(os.Stderr, "RECOMMENDATION: fix what git reported (commonly an unset user.email/user.name, or a leftover index.lock), then re-run.")
				return 2
			}
			fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' in %s — that merge aborted; earlier tickets in this switch are already on the surface.\n", b, base, m.detail)
			fmt.Fprintf(os.Stderr, "RECOMMENDATION: in that ticket's worktree, merge 'origin/%s' in (never local '%s'), resolve, commit, re-run switch.\n", base, base)
			fmt.Fprintf(os.Stderr, "  If origin/%s merges clean, it conflicts with an earlier ticket in this switch — switch them separately or resolve the pair together.\n", base)
			return 2
		}
	}
	head := gitCOut(primary, "rev-parse", "HEAD")
	servingWrite(s.gitdir, "set", tickets)
	fmt.Printf("BASE=%s\n", base)
	fmt.Printf("PRIMARY=%s\n", primary)
	fmt.Printf("HEAD=%s\n", head)
	fmt.Printf("SERVING=%s\n", strings.Join(tickets, ","))
	fmt.Fprintf(os.Stderr, "switch: test surface now serves '%s' + %s — fixes go in the ticket worktree(s), then re-run switch.\n", base, strings.Join(tickets, " "))
	return 0
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
	primary := baseOpsPrimary("serve")
	if primary == "" {
		os.Exit(2)
	}
	repo := filepath.Base(primary)
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	var env identity.Env
	if len(tickets) > 0 || release {
		// Explicit tickets, or --release reading targets from the serving
		// marker — neither needs inferred identity; cwd ambiguity must not
		// block releasing the shared surface.
		env = resolveProject()
	} else {
		env = resolveEnv()
	}

	leaseOwner := func(gd string) string {
		return leaseRead(leaseDirOf(gd), "owner")
	}
	// sibs returns index.json siblings for a ticket as (role,repo,ticket) rows.
	sibs := func(t string) []ticket.Sibling {
		return ticket.ReadIndex(filepath.Join(env.ProjectHome, "tickets", ticketSafe(t), "index.json")).Siblings
	}

	if release {
		if len(tickets) == 0 {
			tickets = servingTickets(gitdir)
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
		rwsr := workspace.NewResolver(primary, gitCOut(primary, "remote", "get-url", "origin"))
		for _, t := range tickets {
			for _, s := range sibs(t) {
				if s.Ticket == "" || !rwsr.FanOut() {
					continue
				}
				sp, ok, cErr := relatedRepoPathVia(rwsr, s.Role, primary)
				if cErr != nil {
					// Release is a cleanup path: report and keep releasing the
					// rest rather than stopping with leases still held.
					fmt.Fprintf(os.Stderr, "serve: sibling %s/%s not released — %s\n", s.Repo, s.Ticket, cErr)
					continue
				}
				if !ok {
					fmt.Fprintf(os.Stderr, "serve: sibling %s/%s not released — no local path for role '%s' in the workspace or .babysit/.env\n", s.Repo, s.Ticket, s.Role)
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
				fmt.Fprintln(os.Stderr, retarget("serve: nothing finished to serve — no open ticket has qa + review-pr DONE (see bbs-ticket board)"))
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
	// One resolver for the whole fan-out: primary is constant across the loop,
	// and resolving per sibling would re-read config.yaml and the workspace file
	// once per sibling.
	wsr := workspace.NewResolver(primary, gitCOut(primary, "remote", "get-url", "origin"))
	if err := wsr.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: %s\n", err)
		os.Exit(2)
	}
	for _, t := range tickets {
		for _, s := range sibs(t) {
			if s.Ticket == "" {
				continue
			}
			// In a monorepo the siblings are in this same checkout, already on
			// the served surface — there is nothing to fan out to, and reporting
			// an unresolvable path would name a problem that does not exist.
			if !wsr.FanOut() {
				continue
			}
			sp, ok, cErr := relatedRepoPathVia(wsr, s.Role, primary)
			if cErr != nil {
				fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
				fmt.Fprintf(os.Stderr, "REASON: %s\n", cErr)
				fmt.Fprintf(os.Stderr, "RECOMMENDATION: resolve the disagreement, then re-run serve; this repo is already serving %s.\n", list)
				svRC = 2
				continue
			}
			if !ok {
				fmt.Fprintln(os.Stderr, "STATUS: NEEDS_CONTEXT")
				fmt.Fprintf(os.Stderr, "REASON: sibling %s/%s (role '%s') has no resolvable local path — no entry with that role in %s, and RELATED_*_REPO unset in %s/.babysit/.env.\n", s.Repo, s.Ticket, s.Role, siblingSourceName(wsr), primary)
				fmt.Fprintf(os.Stderr, "RECOMMENDATION: bbs config workspace add-repo --role %s (preferred), or set RELATED_*_REPO (see setup-project § Related Repos), then re-run serve; this repo is already serving %s.\n", s.Role, list)
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
		fmt.Fprintf(os.Stderr, retarget("serve: review on the dev server(s); fixes commit in the ticket worktree, then re-run 'bbs-ticket serve %s'.\n"), strings.Join(tickets, " "))
		fmt.Fprintln(os.Stderr, retarget("  Done reviewing → bbs-ticket serve --release"))
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

// siblingSourceName names the authority a failed role lookup consulted, so the
// message stays accurate for both an unregistered repo (where .env is still
// the only source) and a registered one.
func siblingSourceName(r *workspace.Resolver) string {
	if n := r.Name(); n != "" {
		return "workspace " + n
	}
	return "any workspace (this repo has no .babysit/config.yaml)"
}

// ─── land ────────────────────────────────────────────────────────────────────

func runLand(args []string) { os.Exit(landTickets(args)) }

// landTickets merges finished ticket branches into the LOCAL base branch and
// keeps the merge. It is the deliberate opposite of `switch`, which resets the
// base first and treats the composition as scratch: land is what a repo running
// `land: local` (or `land: none`, where the base branch is the only venue there
// is) does when review is over and the work should stay.
//
// Three things make it safe enough to run unattended behind `finish: land`:
//
//   - It gates on the same verdicts `serve` gates on — qa AND review-pr DONE,
//     per ticket, with no override flag. Landing unverified work is the whole
//     hazard, and a `--force` here would be the thing every stuck run reaches
//     for. A human who genuinely wants it has `git merge` one line away.
//   - It takes the surface lease, because moving local base races every
//     merge-base/switch/reset-base in flight.
//   - It never resets, never force-updates and never pushes. The worst outcome
//     of a wrong land is a merge commit on a local branch, recoverable from the
//     ticket branch that still holds every commit.
//
// The interaction worth knowing: a later `reset-base` (or the `serve` that
// calls it) hard-resets base to origin and DISCARDS these merges. That is not a
// bug in either — the ticket branches still hold the work, and the stray-commit
// guard stays quiet precisely because they do — but it means a landed base is
// only durable once pushed. Hence the closing note on stdout.
func landTickets(args []string) int {
	tickets, base, ok := parseTicketsAndBase("land", args)
	if !ok {
		return 2
	}
	primary := baseOpsPrimary("land")
	if primary == "" {
		return 2
	}
	var env identity.Env
	if len(tickets) > 0 {
		env = resolveProject() // explicit tickets — cwd ambiguity must not block
	} else {
		env = resolveEnv()
	}
	if len(tickets) == 0 {
		if env.Ticket == "" {
			fmt.Fprintln(os.Stderr, retarget("usage: bbs-ticket land <ticket> [<ticket>...] [--base BRANCH]"))
			return 2
		}
		tickets = []string{env.Ticket}
	}
	if base == "" {
		base = baseBranchIn(primary)
	}

	// Resolve branches and verdicts for EVERY ticket before merging any of them:
	// a batch that lands two tickets and then blocks on the third's missing QA
	// leaves a base nobody asked for.
	type row struct{ ticket, branch, verifiedHead string }
	var rows []row
	for _, t := range tickets {
		b := ticketBranch(t)
		if b == "" {
			return 2
		}
		if enforced, ready, reasons, verifiedHead, err := ticketV2Readiness(primary, env, t, "land"); err != nil {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			fmt.Fprintf(os.Stderr, "REASON: %s readiness could not be evaluated: %v\n", t, err)
			return 2
		} else if enforced {
			if !ready {
				fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
				fmt.Fprintf(os.Stderr, "REASON: %s has stale or incomplete v2 readiness: %s\n", t, strings.Join(reasons, ","))
				return 2
			}
			if !fullSHARe.MatchString(verifiedHead) {
				fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
				fmt.Fprintf(os.Stderr, "REASON: %s readiness did not return a verified full head SHA.\n", t)
				return 2
			}
			rows = append(rows, row{ticket: t, branch: b, verifiedHead: verifiedHead})
			continue
		}
		var missing []string
		for _, skill := range []string{"qa", "review-pr"} {
			if !serveVerdictOK(primary, t, skill) {
				missing = append(missing, skill)
			}
		}
		if len(missing) > 0 {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			fmt.Fprintf(os.Stderr, "REASON: %s is not finished — %s verdict is not DONE, and land never merges unverified work.\n", t, strings.Join(missing, " + "))
			fmt.Fprintf(os.Stderr, "RECOMMENDATION: finish the ticket (see bbs-ticket board), then re-run; to compose it for review without landing, use 'bbs-ticket serve %s'.\n", t)
			return 2
		}
		rows = append(rows, row{ticket: t, branch: b})
	}

	s, ok := acquireSurface("land", primary, env.Ticket)
	if !ok {
		return 2
	}
	defer s.release()

	// The same two guards reset-base applies, for the same reasons: a primary
	// that wandered off base would land onto whatever it is standing on, and a
	// dirty tree turns a conflicted merge into an unrecoverable mess.
	if !s.guardOnBase(base, " — landing there would merge into the wrong branch.",
		fmt.Sprintf("RECOMMENDATION: checkout '%s' there (or pass --base), then re-run.", base)) {
		return 2
	}
	if !s.guardClean(" — a merge would mix them into the landing.",
		"RECOMMENDATION: commit or stash them, then re-run.") {
		return 2
	}
	if !s.noScratch() {
		return 2
	}
	landed, already := 0, 0
	for _, r := range rows {
		mergeTarget := r.branch
		if r.verifiedHead != "" {
			mergeTarget = r.verifiedHead
		}
		// An already-landed ticket is the normal state on a re-run (a foreman
		// re-reconciling its batch, a resumed session): report it and move on
		// rather than minting an empty merge commit per pass.
		if gitCOK(primary, "merge-base", "--is-ancestor", mergeTarget, "HEAD") {
			fmt.Printf("LANDED=0 %s %s (already on %s)\n", r.ticket, r.branch, base)
			already++
			continue
		}
		// --no-ff so the ticket stays one identifiable unit on base even when it
		// could fast-forward, which is what makes a landing reviewable after the
		// fact and revertable as a whole.
		if m := s.merge(mergeTarget, true); !m.ok {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			if !m.conflicted {
				fmt.Fprintf(os.Stderr, "REASON: could not merge '%s' onto '%s' — no files conflicted; git said: %s\n", r.branch, base, m.detail)
				fmt.Fprintln(os.Stderr, "RECOMMENDATION: fix what git reported (commonly an unset user.email/user.name, or a leftover index.lock), then re-run.")
			} else {
				fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' in %s — that merge aborted; tickets landed earlier in this run stay on '%s'.\n", r.branch, base, m.detail, base)
				fmt.Fprintf(os.Stderr, "RECOMMENDATION: in that ticket's worktree, merge 'origin/%s' in (never local '%s'), resolve, commit, re-run land.\n", base, base)
			}
			return 2
		}
		landed++
		fmt.Printf("LANDED=1 %s %s\n", r.ticket, r.branch)
		storeForTicket(env, r.ticket).HistoryAppendExtra("land", actorRole(),
			fmt.Sprintf(`{"base":"%s","branch":"%s"}`, base, r.branch))
	}

	head := gitCOut(primary, "rev-parse", "HEAD")
	fmt.Printf("BASE=%s\n", base)
	fmt.Printf("PRIMARY=%s\n", primary)
	fmt.Printf("HEAD=%s\n", head)
	fmt.Printf("LANDED=%d ALREADY=%d\n", landed, already)
	if landed > 0 {
		fmt.Fprintf(os.Stderr, "land: '%s' is now ahead of origin — push it. A later reset-base (or serve, which calls it)\n", base)
		fmt.Fprintf(os.Stderr, "  resets '%s' to origin and would discard these merges; the ticket branches keep the work either way.\n", base)
	}
	return 0
}

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
	var env identity.Env
	// status never needs a ticket; release --force overrides ownership;
	// explicit --ticket supplies its own. Only acquire/release paths that
	// use the current ticket as owner need the ladder.
	if qlTicket != "" || verb == "status" || (verb == "release" && force) {
		env = resolveProject()
	} else {
		env = resolveEnv()
	}
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
	qlDir := leaseDirOf(gitdir)
	read := func(k string) string { return leaseRead(qlDir, k) }

	switch verb {
	case "acquire":
		if qlTicket == "" {
			fmt.Fprintln(os.Stderr, "qa-lease: no owner — no ticket in scope and no --ticket given")
			os.Exit(2)
		}
		// A QA session is a long hold of the same lease every surface op takes,
		// so leaseAcquire waits out a merge already landing before granting it:
		// a lease published in that gap would cover a surface about to change.
		// Waiting is the point — the merge finishes in seconds, and the surface
		// is then genuinely still.
		res := leaseAcquire(qlDir, qlTicket, leaseLong, ttl)
		if b := res.blocked; b != nil {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			if b.owner == "" {
				fmt.Fprintln(os.Stderr, "REASON: another run is taking the qa-lease right now — one QA session at a time on the shared surface.")
				fmt.Fprintln(os.Stderr, "RECOMMENDATION: re-run acquire in a moment; a lease still ownerless after two minutes is treated as stale and stolen.")
				os.Exit(2)
			}
			if b.kind == leaseShort {
				fmt.Fprintf(os.Stderr, "REASON: '%s' has held the surface for 30s and is still landing — the lease would cover a surface still changing.\n", b.owner)
				fmt.Fprintf(os.Stderr, "RECOMMENDATION: re-run acquire; if that run died, its lease lapses %d minutes after it started.\n", shortTTLMin)
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "REASON: qa-lease held by '%s' (%dmin into a %dmin lease) — one QA session at a time on the shared surface.\n", b.owner, b.age, b.ttl)
			fmt.Fprintln(os.Stderr, retarget("RECOMMENDATION: wait and re-run acquire, or 'bbs-ticket qa-lease release --force' if that run is dead."))
			os.Exit(2)
		}
		if res.stoleOwner != "" {
			fmt.Fprintf(os.Stderr, "qa-lease: stole stale lease from '%s' (%s)\n", res.stoleOwner, res.stoleWhy)
			if env.Ticket != "" {
				ticket.New(env).HistoryAppendExtra("qa_lease_steal", actorRole(),
					fmt.Sprintf(`{"owner":"%s","stolen_from":"%s","ttl_min":%s}`, qlTicket, res.stoleOwner, ttl))
			}
			fmt.Printf("OWNER=%s\nTTL_MIN=%s\nACQUIRED=1\nSTOLE_FROM=%s\n", qlTicket, ttl, res.stoleOwner)
			os.Exit(0)
		}
		if res.refreshed {
			fmt.Printf("OWNER=%s\nTTL_MIN=%s\nACQUIRED=1\nREFRESHED=1\n", qlTicket, ttl)
			os.Exit(0)
		}
		if env.Ticket != "" {
			ticket.New(env).HistoryAppendExtra("qa_lease_acquire", actorRole(),
				fmt.Sprintf(`{"owner":"%s","ttl_min":%s}`, qlTicket, ttl))
		}
		fmt.Printf("OWNER=%s\nTTL_MIN=%s\nACQUIRED=1\n", qlTicket, ttl)
		os.Exit(0)
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
		age, _ := leaseAge(qlDir)
		fmt.Printf("OWNER=%s\n", read("owner"))
		fmt.Printf("AGE_MIN=%d\n", age)
		fmt.Printf("TTL_MIN=%s\n", read("ttl_min"))
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, retarget("usage: bbs-ticket qa-lease <acquire|release|status> [--ticket ID] [--ttl-min N] [--force]"))
		os.Exit(2)
	}
}
