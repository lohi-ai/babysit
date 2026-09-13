package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This file is the worktree surface engine: the one lifecycle that owns the
// shared test surface — lease acquisition, reentrancy and staleness, the
// reset/compose mechanics, the scratch marker, and release. The command
// handlers in ticket_baseops.go (merge-base, reset-base, switch, serve, land,
// qa-lease) select policy and mode on top of it and own their own wording;
// everything that touches <gitdir>/bbs-qa-lease, <gitdir>/bbs-serving, or the
// primary checkout's position lives here so the mechanics cannot diverge.

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

// ownerlessGrace bounds how long a lease directory may sit without a readable
// owner before another run may take it over. leasePublish installs a lease
// atomically, so a live acquire never leaves one — only a lease written by an
// older bbs (mkdir, then write) or a corrupt one can. Two minutes is an
// enormous margin next to the microseconds a publish takes, and it keeps a
// broken lease from wedging the pool until someone runs release --force.
const ownerlessGrace = 2 * time.Minute

func leaseDirOf(gitdir string) string { return filepath.Join(gitdir, "bbs-qa-lease") }

func leaseBodyFor(owner, kind, ttl string) string {
	return fmt.Sprintf("owner=%s\nkind=%s\npid=%d\nsince=%s\nsince_epoch=%d\nttl_min=%s\n",
		owner, kind, os.Getpid(), isoNow(), time.Now().Unix(), ttl)
}

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

// ─── the scratch marker ──────────────────────────────────────────────────────
//
// <gitdir>/bbs-serving records what the primary checkout serves. switch and
// merge-base write it (scratch composition), reset-base clears it, land refuses
// to merge while it is nonempty, and serve/board read it.

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

// servingTickets returns the tickets the marker says the primary serves —
// empty when the marker is absent or blank.
func servingTickets(gitdir string) []string {
	b, err := os.ReadFile(filepath.Join(gitdir, "bbs-serving"))
	if err != nil {
		return nil
	}
	return splitCommaSpace(string(b))
}

func splitCommaSpace(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' })
}

// ─── the surface lifecycle ───────────────────────────────────────────────────

// surface is one held lease over one primary checkout. A handler builds it
// with acquireSurface, defers release, and drives the lifecycle — guards,
// reset, compose, marker — through its methods. The guards print the shared
// STATUS: BLOCKED shape; the handler supplies the tail of each message.
type surface struct {
	primary string
	gitdir  string
	release func()
}

// acquireSurface resolves the shared git dir from primary and takes the
// surface lease for one op. ok=false means the reason is already on stderr
// and the caller should exit 2.
func acquireSurface(cmd, primary, ticketID string) (*surface, bool) {
	gitdir := gitCOut(primary, "rev-parse", "--absolute-git-dir")
	release, ok := surfaceAcquire(gitdir, cmd, ticketID)
	if !ok {
		return nil, false
	}
	return &surface{primary: primary, gitdir: gitdir, release: release}, true
}

// guardOnBase blocks unless the primary checkout sits on base. reasonTail
// completes "primary checkout %s is on '%s', not base '%s'" (it carries the
// closing punctuation or the command-specific clause); recLine is the full
// RECOMMENDATION: line.
func (s *surface) guardOnBase(base, reasonTail, recLine string) bool {
	if pb := gitCOut(s.primary, "branch", "--show-current"); pb != base {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s is on '%s', not base '%s'%s\n", s.primary, pb, base, reasonTail)
		fmt.Fprintln(os.Stderr, recLine)
		return false
	}
	return true
}

// guardClean blocks while the primary checkout has uncommitted changes.
// reasonTail completes "primary checkout %s has uncommitted changes"; recLine
// is the full RECOMMENDATION: line.
func (s *surface) guardClean(reasonTail, recLine string) bool {
	if gitCOut(s.primary, "status", "--porcelain") != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary checkout %s has uncommitted changes%s\n", s.primary, reasonTail)
		fmt.Fprintln(os.Stderr, recLine)
		return false
	}
	return true
}

// mergeResult is what a surface merge came to. ok=false means the merge was
// aborted and the handler owns the BLOCK wording for it.
type mergeResult struct {
	ok         bool
	conflicted bool
	detail     string
}

// merge lands target on the primary's current branch — `--no-edit`, plus
// `--no-ff` when the merge must stay one identifiable unit (land). A failure
// is classified and aborted here; the wording is the caller's.
func (s *surface) merge(target string, noFF bool) mergeResult {
	args := []string{"merge"}
	if noFF {
		args = append(args, "--no-ff")
	}
	args = append(args, "--no-edit", target)
	if ok, said := gitCRun(s.primary, args...); !ok {
		conflicted, detail := mergeFailure(s.primary, said)
		gitCOK(s.primary, "merge", "--abort")
		return mergeResult{conflicted: conflicted, detail: detail}
	}
	return mergeResult{ok: true}
}

// resetToOrigin is the reset half of the lifecycle: fetch, prove the reset is
// safe (origin ref exists, no stray commits, on base, clean tree), hard-reset,
// then clear the scratch marker. quiet suppresses the post-reset operator
// note. Its guards fire for switch and serve too (serve shells out to switch,
// whose stderr passes straight through), which is why the messages say
// "then re-run" rather than naming reset-base.
func (s *surface) resetToOrigin(base string, quiet bool, env identity.Env) bool {
	if !gitCOK(s.primary, "fetch", "origin", base) {
		fmt.Fprintf(os.Stderr, "reset-base: warning — fetch failed, using the last-known origin/%s\n", base)
	}
	if !gitCOK(s.primary, "rev-parse", "--verify", "-q", "origin/"+base) {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: origin/%s not found — nothing safe to reset to.\n", base)
		fmt.Fprintf(os.Stderr, "RECOMMENDATION: add an 'origin' remote tracking '%s' (or fetch it), then re-run.\n", base)
		return false
	}
	stray := gitCOut(s.primary, "rev-list", "--no-merges", "origin/"+base+".."+base,
		"--not", "--exclude="+base, "--branches")
	if stray != "" {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: '%s' has commits no other branch (or origin) holds — reset would lose real work:\n", base)
		lines := strings.Split(stray, "\n")
		for i, c := range lines {
			if i >= 5 {
				break
			}
			if ol := gitCOut(s.primary, "log", "-1", "--oneline", c); ol != "" {
				fmt.Fprintln(os.Stderr, ol)
			}
		}
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: push them, or move them to a ticket branch, then re-run.")
		return false
	}
	if !s.guardOnBase(base, ".",
		fmt.Sprintf("RECOMMENDATION: checkout '%s' there (or pass --base), then re-run.", base)) {
		return false
	}
	if !s.guardClean(" — reset --hard would destroy them.",
		"RECOMMENDATION: commit or stash them, then re-run.") {
		return false
	}
	pre := gitCOut(s.primary, "rev-parse", "HEAD")
	if !gitCOK(s.primary, "reset", "--hard", "origin/"+base) {
		fmt.Fprintf(os.Stderr, "reset-base: git reset --hard origin/%s failed\n", base)
		return false
	}
	post := gitCOut(s.primary, "rev-parse", "HEAD")
	servingWrite(s.gitdir, "set", nil)
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
	fmt.Printf("PRIMARY=%s\n", s.primary)
	fmt.Printf("HEAD=%s\n", post)
	if !quiet {
		fmt.Fprintf(os.Stderr, "reset-base: '%s' now matches origin — re-run merge-base from any in-flight worktree\n", base)
		fmt.Fprintln(os.Stderr, "  so the shared server serves those tickets again.")
	}
	return true
}

// noScratch enforces the scratch-versus-retained invariant: a nonempty serving
// marker means the base carries a scratch composition (switch/serve/merge-base)
// that reset-base is expected to discard. Landing on top of it either no-ops
// ("already on base") or mixes scratch commits into real history — refuse and
// name the recovery.
func (s *surface) noScratch() bool {
	if serving := servingTickets(s.gitdir); len(serving) > 0 {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: primary is serving a scratch composition (%s) — land never merges on top of it.\n", strings.Join(serving, ","))
		fmt.Fprintln(os.Stderr, retarget("RECOMMENDATION: run 'bbs-ticket reset-base' to discard the composition, then re-run land."))
		return false
	}
	return true
}
