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

// ownerlessGrace bounds how long a lease directory may sit without a readable
// owner before another run may take it over. leasePublish installs a lease
// atomically, so a live acquire never leaves one — only a lease written by an
// older bbs (mkdir, then write) or a corrupt one can. Two minutes is an
// enormous margin next to the microseconds a publish takes, and it keeps a
// broken lease from wedging the pool until someone runs release --force.
const ownerlessGrace = 2 * time.Minute

// leaseStale reports whether the lease in dir may be taken over, and the
// reason to print. An unreadable owner is NOT staleness on its own: reading
// it as stale is what let two runs each claim the one test surface — a racer
// that caught the lease before its owner file landed deleted it and published
// its own, and both printed ACQUIRED=1.
func leaseStale(dir, owner string, age, ttl int64) (bool, string) {
	if owner == "" {
		if fi, err := os.Stat(dir); err != nil || time.Since(fi.ModTime()) > ownerlessGrace {
			return true, "lease has no readable owner"
		}
		return false, ""
	}
	if age > ttl {
		return true, fmt.Sprintf("%dmin > %dmin ttl", age, ttl)
	}
	return false, ""
}

// leasePublish installs body as the lease at dir, atomically: the owner file
// is written inside a temp directory that is then renamed into place, so the
// lease never exists on disk without its owner. rename(2) fails with ENOTEMPTY
// when dir already holds a published lease, which is what makes this the
// mutual-exclusion primitive — a bare os.Mkdir published an empty directory
// that every other racer read as abandoned.
func leasePublish(dir, body string) bool {
	tmp, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+".tmp-")
	if err != nil {
		return false
	}
	if err := os.WriteFile(filepath.Join(tmp, "owner"), []byte(body), 0o644); err != nil {
		_ = os.RemoveAll(tmp)
		return false
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return false
	}
	return true
}

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

// ─── the surface lease ───────────────────────────────────────────────────────
//
// One lock guards the shared test surface: the lease at <gitdir>/bbs-qa-lease.
// Every op that changes what the primary checkout serves — merge-base, switch,
// reset-base — holds it for the length of its work, and a QA session holds the
// same lease across many such ops.
//
// There used to be a second mutex (bbs-merge-base.lock) for the merge itself,
// with the lease layered over it as an advisory guard. Guard and action then
// lived in different critical sections, so every check had to be repeated once
// the lock was finally held, and three separate races came out of that seam.
// One lock has no seam to race in.
//
// The single contention rule: **wait out a short holder, block on a long one.**
// A merge lands in seconds, so waiting for it costs nothing and leaves the
// surface genuinely still. A QA session runs for as long as someone is testing,
// so the operator is told whose lease it is instead of hanging.
const (
	leaseShort = "short" // one surface op
	leaseLong  = "long"  // a whole QA session
	// shortTTLMin bounds how long a surface op may hold the lease. The lock it
	// replaces had no timeout at all, so a single killed merge wedged every
	// ticket in the repo until someone deleted the directory by hand. A merge
	// takes seconds; five minutes is slack for a slow fetch on a large repo,
	// and a holder past it is dead.
	shortTTLMin = 5
	// leaseWait is how long to wait out a short holder before blocking.
	leaseWait = 30 * time.Second
)

// surfaceHeld names the lease this process already owns. It makes an op invoked
// in-process by another (switch → reset-base) reentrant without threading a
// "the caller already holds it" flag through every signature — and without it,
// a ticketless switch would sit waiting on its own lease.
var surfaceHeld string

func leaseDirOf(gitdir string) string { return filepath.Join(gitdir, "bbs-qa-lease") }

func leaseBodyFor(owner, kind, ttl string) string {
	return fmt.Sprintf("owner=%s\nkind=%s\npid=%d\nsince=%s\nsince_epoch=%d\nttl_min=%s\n",
		owner, kind, os.Getpid(), isoNow(), time.Now().Unix(), ttl)
}

// leaseKind reads a lease's kind. One written by an older bbs has no kind= at
// all — read it as long, which is what every lease meant back then.
func leaseKind(dir string) string { return orDefault(leaseRead(dir, "kind"), leaseLong) }

// leaseRewrite replaces the owner of a lease we already hold. Write-then-rename
// so a concurrent reader never sees the truncated file and mistakes this lease
// for an ownerless one.
func leaseRewrite(dir, body string) {
	tmp := filepath.Join(dir, ".owner.tmp")
	if os.WriteFile(tmp, []byte(body), 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(dir, "owner"))
	}
}

// leaseAge returns how many minutes the lease has stood, and its declared ttl.
func leaseAge(dir string) (age, ttl int64) {
	now := time.Now().Unix()
	return (now - parseIntOr(leaseRead(dir, "since_epoch"), now)) / 60,
		parseIntOr(leaseRead(dir, "ttl_min"), 60)
}

// leaseResult is what an acquire attempt came to. blocked non-nil means the
// lease is someone else's and the caller should report and give up; the caller
// owns the wording, since "would change the surface mid-QA" and "one QA session
// at a time" are the same fact told to two different audiences.
type leaseResult struct {
	blocked    *leaseHolder
	refreshed  bool   // the lease was already ours
	stoleOwner string // non-empty when a stale lease was taken over
	stoleWhy   string
}

type leaseHolder struct {
	owner    string // "" = a peer is publishing right now
	kind     string
	age, ttl int64
}

// leaseAcquire takes the surface lease at dir for owner, as kind.
func leaseAcquire(dir, owner, kind, ttl string) leaseResult {
	deadline := time.Now().Add(leaseWait)
	for {
		if leasePublish(dir, leaseBodyFor(owner, kind, ttl)) {
			return leaseResult{}
		}
		held := leaseRead(dir, "owner")
		// Ours already — but only a long lease is reentrant. Our own *short*
		// lease is a peer op mid-merge (same ticket, another process), and
		// stepping on it would run two merges in one checkout.
		if held != "" && held == owner && leaseKind(dir) == leaseLong {
			if kind == leaseLong {
				// A QA session refreshing itself. A surface op that finds the
				// session's lease leaves the body alone — rewriting it would
				// cut the QA ttl down to a merge's.
				leaseRewrite(dir, leaseBodyFor(owner, kind, ttl))
			}
			return leaseResult{refreshed: true}
		}
		age, heldTTL := leaseAge(dir)
		if stale, _ := leaseStale(dir, held, age, heldTTL); stale {
			// One attempt, then report whatever is there. Retrying a steal in a
			// loop is how a run spins for as long as a peer keeps re-taking it.
			if stoleFrom, why, ok := leaseSteal(dir, owner, kind, ttl); ok {
				return leaseResult{stoleOwner: stoleFrom, stoleWhy: why}
			}
			age, heldTTL = leaseAge(dir)
			return leaseResult{blocked: &leaseHolder{leaseRead(dir, "owner"), leaseKind(dir), age, heldTTL}}
		}
		if heldKind := leaseKind(dir); heldKind == leaseLong || time.Now().After(deadline) {
			return leaseResult{blocked: &leaseHolder{held, heldKind, age, heldTTL}}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// leaseSteal takes over a lease already judged stale. Read-then-replace needs
// its own mutex: without one, every run that read the same stale lease deletes
// it and publishes, and each reports a successful steal. The re-check under the
// lock catches a peer that got there first.
func leaseSteal(dir, owner, kind, ttl string) (stoleFrom, why string, ok bool) {
	stealLock := dir + ".steal"
	if !lockAcquire(stealLock, 50) {
		return "", "", false
	}
	defer lockRelease(stealLock)
	held := leaseRead(dir, "owner")
	age, heldTTL := leaseAge(dir)
	stale, reason := leaseStale(dir, held, age, heldTTL)
	if !stale {
		return "", "", false
	}
	_ = os.RemoveAll(dir)
	if !leasePublish(dir, leaseBodyFor(owner, kind, ttl)) {
		return "", "", false
	}
	return orUnknown(held), reason, true
}

// surfaceAcquire takes the surface lease for one op and returns the release to
// run when it is done. Reentrant two ways — for a QA session already holding the
// lease under this ticket, and for an op this process invoked in-process — and
// both give back a no-op release, because whoever took the lease releases it.
//
// ok=false means the reason is already on stderr and the caller should exit 2.
func surfaceAcquire(gitdir, cmd, ticketID string) (release func(), ok bool) {
	if surfaceHeld != "" {
		return func() {}, true
	}
	dir := leaseDirOf(gitdir)
	owner := ticketID
	if owner == "" {
		// switch and reset-base can run from the primary with no ticket in
		// scope. They still need the lease, so give them an owner unique to the
		// run — two of them are peers, not one reentrant session.
		owner = fmt.Sprintf("%s-%d", cmd, os.Getpid())
	}
	res := leaseAcquire(dir, owner, leaseShort, strconv.Itoa(shortTTLMin))
	if res.stoleOwner != "" {
		fmt.Fprintf(os.Stderr, "%s: warning — cleared stale qa-lease from '%s' (%s).\n",
			cmd, res.stoleOwner, res.stoleWhy)
	}
	if b := res.blocked; b != nil {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		if b.owner == "" {
			// Within ownerlessGrace: a peer is publishing its lease right now.
			fmt.Fprintf(os.Stderr, "REASON: another run is taking the qa-lease right now — %s would change the surface mid-QA.\n", cmd)
			fmt.Fprintln(os.Stderr, "RECOMMENDATION: re-run in a moment; a lease still ownerless after two minutes is treated as stale and cleared.")
			return nil, false
		}
		if b.kind == leaseShort {
			// Waited out the full leaseWait and the peer op is still going. Not a
			// QA session, so say so — the operator is waiting on a merge, not on
			// someone testing.
			fmt.Fprintf(os.Stderr, "REASON: '%s' has held the surface for 30s and is still landing — %s waited and gave up.\n", b.owner, cmd)
			fmt.Fprintf(os.Stderr, "RECOMMENDATION: re-run; if that run died, its lease lapses %d minutes after it started and the next op clears it.\n", shortTTLMin)
			return nil, false
		}
		fmt.Fprintf(os.Stderr, "REASON: shared test surface is qa-leased by '%s' (%dmin into a %dmin lease) — %s would change it mid-QA.\n", b.owner, b.age, b.ttl, cmd)
		fmt.Fprintf(os.Stderr, retarget("RECOMMENDATION: wait for '%s' to run 'bbs-ticket qa-lease release', or 'bbs-ticket qa-lease release --force' if that run is dead.\n"), b.owner)
		return nil, false
	}
	surfaceHeld = owner
	if res.refreshed {
		// The QA session's lease, not ours to end.
		return func() { surfaceHeld = "" }, true
	}
	return func() {
		surfaceHeld = ""
		// Only drop a lease still ours. Past shortTTLMin a peer may have judged
		// this op dead and taken over, and removing theirs would hand the
		// surface to a third run while they are using it.
		if leaseRead(dir, "owner") == owner {
			_ = os.RemoveAll(dir)
		}
	}, true
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
	env := identity.Resolve()
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	release, ok := surfaceAcquire(gitdir, "merge-base", env.Ticket)
	if !ok {
		return 2
	}
	defer release()
	// Everything that reads the primary belongs under the lease. A peer's merge
	// leaves that checkout mid-write for as long as it runs, and reading it from
	// outside the lease sees the peer's half-written tree as the operator's own
	// uncommitted work — the ticket then fails to land, told to stash changes
	// that are not theirs.
	primaryBranch := gitCOut(primary, "branch", "--show-current")
	if primaryBranch != base {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s is on '%s', not base '%s'.\n", primary, primaryBranch, base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: checkout '%s' in the primary checkout (or pass --base), then re-run.\n", base)
		return 2
	}
	if gitCOut(primary, "status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s has uncommitted changes — merging would tangle them with the ticket.\n", primary)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit or stash the primary checkout's changes, then re-run merge-base.")
		return 2
	}
	pre := gitCOut(primary, "rev-parse", "HEAD")
	if ok, said := gitCRun(primary, "merge", "--no-edit", branch); !ok {
		conflicted, detail := mergeFailure(primary, said)
		gitCOK(primary, "merge", "--abort")
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		if !conflicted {
			fmt.Fprintf(os.Stderr, "REASON: could not merge '%s' onto '%s' (%s) — no files conflicted; git said: %s\n", branch, base, primary, detail)
			fmt.Fprintln(os.Stderr, "RECOMMENDATION: fix what git reported (commonly an unset user.email/user.name, or a leftover index.lock), then re-run.")
			return 2
		}
		fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' (%s) in %s; merge aborted, primary untouched.\n", branch, base, primary, detail)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: in the worktree, merge 'origin/%s' into '%s' (never local '%s' — it carries other tickets), resolve, commit, re-run merge-base.\n", base, branch, base)
		fmt.Fprintf(os.Stderr, retarget("  If origin/%s merges clean, the conflict is with another in-flight ticket — QA solo via 'bbs-ticket switch %s' and land the PRs in sequence.\n"), base, env.Ticket)
		return 2
	}
	post := gitCOut(primary, "rev-parse", "HEAD")
	if env.Ticket != "" {
		servingWrite(gitdir, "append", []string{env.Ticket})
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
	env := identity.Resolve()
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	release, ok := surfaceAcquire(gitdir, "reset-base", env.Ticket)
	if !ok {
		return 2
	}
	defer release()
	// The fetch and every check below read the primary, so they belong under the
	// lease: the stray-commit scan in particular compares refs a peer's merge is
	// in the middle of moving.
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
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: push them, or move them to a ticket branch, then re-run.")
		return 2
	}
	primaryBranch := gitCOut(primary, "branch", "--show-current")
	if primaryBranch != base {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s is on '%s', not base '%s'.\n", primary, primaryBranch, base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: checkout '%s' there (or pass --base), then re-run.\n", base)
		return 2
	}
	if gitCOut(primary, "status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s has uncommitted changes — reset --hard would destroy them.\n", primary)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit or stash them, then re-run.")
		return 2
	}
	pre := gitCOut(primary, "rev-parse", "HEAD")
	if !gitCOK(primary, "reset", "--hard", "origin/"+base) {
		fmt.Fprintf(os.Stderr, "reset-base: git reset --hard origin/%s failed\n", base)
		return 2
	}
	post := gitCOut(primary, "rev-parse", "HEAD")
	servingWrite(gitdir, "set", nil)
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
	env := identity.Resolve()
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
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
	release, ok := surfaceAcquire(gitdir, "switch", env.Ticket)
	if !ok {
		return 2
	}
	defer release()
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
		if ok, said := gitCRun(primary, "merge", "--no-edit", b); !ok {
			conflicted, detail := mergeFailure(primary, said)
			gitCOK(primary, "merge", "--abort")
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			if !conflicted {
				fmt.Fprintf(os.Stderr, "REASON: could not merge '%s' onto '%s' — no files conflicted; git said: %s\n", b, base, detail)
				fmt.Fprintln(os.Stderr, "RECOMMENDATION: fix what git reported (commonly an unset user.email/user.name, or a leftover index.lock), then re-run.")
				return 2
			}
			fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' in %s — that merge aborted; earlier tickets in this switch are already on the surface.\n", b, base, detail)
			fmt.Fprintf(os.Stderr, "RECOMMENDATION: in that ticket's worktree, merge 'origin/%s' in (never local '%s'), resolve, commit, re-run switch.\n", base, base)
			fmt.Fprintf(os.Stderr, "  If origin/%s merges clean, it conflicts with an earlier ticket in this switch — switch them separately or resolve the pair together.\n", base)
			return 2
		}
	}
	head := gitCOut(primary, "rev-parse", "HEAD")
	servingWrite(gitdir, "set", tickets)
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
// Three things make it safe enough to run unattended behind `auto_land: true`:
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
	env := identity.Resolve()
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
	type row struct{ ticket, branch string }
	var rows []row
	for _, t := range tickets {
		b := ticketBranch(t)
		if b == "" {
			return 2
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
		rows = append(rows, row{t, b})
	}

	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	release, ok := surfaceAcquire(gitdir, "land", env.Ticket)
	if !ok {
		return 2
	}
	defer release()

	// The same two guards reset-base applies, for the same reasons: a primary
	// that wandered off base would land onto whatever it is standing on, and a
	// dirty tree turns a conflicted merge into an unrecoverable mess.
	if pb := gitCOut(primary, "branch", "--show-current"); pb != base {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s is on '%s', not base '%s' — landing there would merge into the wrong branch.\n", primary, pb, base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: checkout '%s' there (or pass --base), then re-run.\n", base)
		return 2
	}
	if gitCOut(primary, "status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s has uncommitted changes — a merge would mix them into the landing.\n", primary)
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: commit or stash them, then re-run.")
		return 2
	}

	landed, already := 0, 0
	for _, r := range rows {
		// An already-landed ticket is the normal state on a re-run (a foreman
		// re-reconciling its batch, a resumed session): report it and move on
		// rather than minting an empty merge commit per pass.
		if gitCOK(primary, "merge-base", "--is-ancestor", r.branch, "HEAD") {
			fmt.Printf("LANDED=0 %s %s (already on %s)\n", r.ticket, r.branch, base)
			already++
			continue
		}
		// --no-ff so the ticket stays one identifiable unit on base even when it
		// could fast-forward, which is what makes a landing reviewable after the
		// fact and revertable as a whole.
		if ok, said := gitCRun(primary, "merge", "--no-ff", "--no-edit", r.branch); !ok {
			conflicted, detail := mergeFailure(primary, said)
			gitCOK(primary, "merge", "--abort")
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			if !conflicted {
				fmt.Fprintf(os.Stderr, "REASON: could not merge '%s' onto '%s' — no files conflicted; git said: %s\n", r.branch, base, detail)
				fmt.Fprintln(os.Stderr, "RECOMMENDATION: fix what git reported (commonly an unset user.email/user.name, or a leftover index.lock), then re-run.")
			} else {
				fmt.Fprintf(os.Stderr, "REASON: merge conflict landing '%s' on '%s' in %s — that merge aborted; tickets landed earlier in this run stay on '%s'.\n", r.branch, base, detail, base)
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
