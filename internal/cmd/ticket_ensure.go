package cmd

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This file ports the `ensure` subcommand of bin/bbs-ticket.bash: the
// idempotent ticket-creation entry point. On a branch that already encodes a
// ticket it is a no-op that optionally seeds requirement.md; otherwise it
// derives a new ticket id + slug and cuts the branch per git-flow mode (trunk /
// branch / worktree), guarded by the safe-cut gate. It is the only bbs-ticket
// subcommand that mutates git, so the differential harness exercises it inside a
// throwaway repo.

var ensureTypeOK = map[string]bool{"feat": true, "fix": true, "chore": true, "bug": true, "refactor": true}

func runEnsure(args []string) {
	fromInput, slugHint, typ, promptReason := "", "", "feat", ""
	forceCut, forceNoBranch := false, false
	modeFlag := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from-input":
			v := valueAt(args, i)
			// Foot-gun guard: a short single-line value that is an existing file
			// almost always means "use this file's contents" — refuse with a hint.
			if fi, err := os.Stat(v); err == nil && !fi.IsDir() && !strings.Contains(v, "\n") && len(v) < 256 {
				fmt.Fprintf(os.Stderr, "ensure: --from-input got '%s' which is an existing file path.\n", v)
				fmt.Fprintf(os.Stderr, "  Did you mean: --from-input-file '%s'\n", v)
				fmt.Fprintln(os.Stderr, "  (--from-input takes literal text; use --from-input-file for paths)")
				os.Exit(2)
			}
			fromInput, i = v, i+1
		case "--from-input-file":
			if b, err := os.ReadFile(valueAt(args, i)); err == nil {
				fromInput = strings.TrimRight(string(b), "\n")
			}
			i++
		case "--slug-hint":
			slugHint, i = valueAt(args, i), i+1
		case "--type":
			typ, i = valueAt(args, i), i+1
		case "--reason":
			promptReason, i = valueAt(args, i), i+1
		case "--cut-branch":
			forceCut = true
		case "--no-branch":
			forceNoBranch = true
		case "--mode":
			modeFlag, i = valueAt(args, i), i+1
		}
	}

	if !ensureTypeOK[typ] {
		fmt.Fprintf(os.Stderr, "ensure: invalid --type '%s' (feat|fix|chore|bug|refactor)\n", typ)
		os.Exit(2)
	}
	switch modeFlag {
	case "", "trunk", "branch", "worktree":
	default:
		fmt.Fprintf(os.Stderr, "ensure: invalid --mode '%s' (trunk|branch|worktree)\n", modeFlag)
		os.Exit(2)
	}

	env := identity.Resolve()

	// Fast-path: branch already encodes a ticket.
	if env.Ticket != "" {
		st := ticket.New(env)
		reqPath := filepath.Join(st.Home(), "requirement.md")
		if fromInput != "" && !fileExists(reqPath) {
			st.EnsureDirs()
			_ = os.WriteFile(reqPath, []byte(fromInput+"\n"), 0o644)
			acquireOrDie(st)
			doc := loadForMutate(st)
			doc.Set("pointers.requirement", "requirement.md")
			werr := ticket.WriteDoc(st.IndexPath(), doc)
			st.ReleaseLock()
			if werr != nil {
				fmt.Fprintln(os.Stderr, werr)
				os.Exit(1)
			}
			st.HistoryAppendExtra("requirement_seeded", actorRole(), "")
			fmt.Printf("TICKET=%s\n", env.Ticket)
			fmt.Printf("BRANCH=%s\n", env.Branch)
			fmt.Printf("TICKET_HOME=%s\n", st.Home())
			fmt.Println("CREATED=0")
			fmt.Printf("REQUIREMENT=%s\n", reqPath)
		} else {
			fmt.Printf("TICKET=%s\n", env.Ticket)
			fmt.Printf("BRANCH=%s\n", env.Branch)
			fmt.Printf("TICKET_HOME=%s\n", st.Home())
			fmt.Println("CREATED=0")
			if fileExists(reqPath) {
				fmt.Printf("REQUIREMENT=%s\n", reqPath)
			}
		}
		os.Exit(0)
	}

	// Slow-path: create a ticket branch.
	if !haveGit() {
		fmt.Fprintln(os.Stderr, "ensure: git not found — cannot create ticket branch")
		os.Exit(2)
	}
	if !insideWorkTree() {
		fmt.Fprintln(os.Stderr, "ensure: not in a git work tree — cannot create ticket branch")
		os.Exit(2)
	}

	// Git-flow mode: --mode > the repo's derived policy (gitflow.go owns the
	// profile → mode derivation and the legacy ticket_branch alias).
	policy, gfErr := resolveGitFlow("")
	if gfErr != nil {
		fmt.Fprintf(os.Stderr, "ensure: %v\n", gfErr)
		os.Exit(2)
	}
	gfMode := modeFlag
	if gfMode == "" {
		gfMode = policy.Mode
	}
	switch gfMode {
	case "trunk", "branch", "worktree":
	default:
		fmt.Fprintf(os.Stderr, "ensure: invalid mode '%s' in .babysit/git-flow.yaml (trunk|branch|worktree)\n", gfMode)
		os.Exit(2)
	}
	if forceNoBranch {
		gfMode = "trunk"
	}
	if forceCut && !forceNoBranch && gfMode == "trunk" {
		gfMode = "branch"
	}

	newTicket := newTicketID()

	// These are re-derived after a git op moves us onto the new branch. In the
	// worktree/trunk paths the cwd doesn't move, so we set them explicitly.
	ticketID := ""
	branch := ""
	baseBranch := ""
	ensureWorktree := ""

	if gfMode == "trunk" {
		ticketID = newTicket
		branch = env.Branch
		if branch == "unknown" || branch == "" {
			if b := gitOut("branch", "--show-current"); b != "" {
				branch = b
			} else {
				branch = "unknown"
			}
		}
		fmt.Fprintf(os.Stderr, "ensure: mode=trunk — %s stays on branch '%s' (no branch cut)\n", ticketID, branch)
		fmt.Fprintf(os.Stderr, "ensure: identity is env-carried; run: export BABYSIT_TICKET=%s\n", ticketID)
	} else {
		newSlug := ""
		if slugHint != "" {
			newSlug = deriveSlug(slugHint)
		}
		if newSlug == "" && fromInput != "" {
			newSlug = deriveSlug(fromInput)
		}
		if newSlug == "" {
			fmt.Fprintln(os.Stderr, "ensure: refusing to cut a branch with no slug — pass --from-input or --slug-hint")
			fmt.Fprintf(os.Stderr, "ensure: (an empty call would have produced branch '%s/%s_untitled')\n", typ, newTicket)
			os.Exit(2)
		}
		newBranch := fmt.Sprintf("%s/%s_%s", typ, newTicket, newSlug)

		curBranch := gitOut("branch", "--show-current")
		baseBranch = policy.BaseBranch
		if gitOK("remote", "get-url", "origin") {
			if !gitOK("fetch", "origin", baseBranch) {
				fmt.Fprintf(os.Stderr, "ensure: warning — fetch failed, using the last-known origin/%s\n", baseBranch)
			}
		}
		srcRef := ""
		if gitOK("show-ref", "--verify", "--quiet", "refs/remotes/origin/"+baseBranch) {
			srcRef = "origin/" + baseBranch
		}
		if srcRef == "" && gitOK("show-ref", "--verify", "--quiet", "refs/heads/"+baseBranch) {
			srcRef = baseBranch
		}
		safeCut := gfMode != "worktree" && curBranch == baseBranch && gitOut("status", "--porcelain") == ""

		if safeCut {
			if actorRole() == "developer" && !forceCut {
				fmt.Fprintln(os.Stderr, "STATUS: NEEDS_CONFIRM")
				fmt.Fprintf(os.Stderr, "REASON: creating this ticket cuts a new branch '%s' off '%s' and checks it out.\n", newBranch, srcRef)
				fmt.Fprintln(os.Stderr, "OPTIONS: re-run with --cut-branch to cut the branch,")
				fmt.Fprintf(os.Stderr, "         or --no-branch to stay on '%s' (identity carried by BABYSIT_TICKET).\n", curBranch)
				os.Exit(3)
			}
			if !gitOK("checkout", "--no-track", "-b", newBranch, srcRef) {
				fmt.Fprintf(os.Stderr, "ensure: git checkout -b '%s' '%s' failed\n", newBranch, srcRef)
				os.Exit(2)
			}
			// Re-derive identity now that we're on the new branch.
			re := identity.Resolve()
			ticketID = re.Ticket
			if ticketID == "" {
				ticketID = newTicket
			}
			branch = re.Branch
			if branch == "" || branch == "unknown" {
				branch = newBranch
			}
			env = re
		} else {
			if gfMode == "worktree" {
				fmt.Fprintf(os.Stderr, "ensure: mode=worktree — cutting into a worktree (primary checkout stays on '%s')\n", curBranch)
			} else {
				if curBranch != baseBranch {
					fmt.Fprintf(os.Stderr, "ensure: on '%s' (base is '%s') — diverting the cut to a worktree\n", curBranch, baseBranch)
				} else {
					fmt.Fprintf(os.Stderr, "ensure: '%s' has uncommitted changes — diverting the cut to a worktree\n", curBranch)
				}
				divertWarning(curBranch, baseBranch)
			}
			if srcRef == "" {
				fmt.Fprintf(os.Stderr, "ensure: base branch '%s' not found (local or origin) — cannot cut safely from '%s'.\n", baseBranch, curBranch)
				fmt.Fprintf(os.Stderr, "  Fix: fetch/create '%s', or re-run with --no-branch to ride the current branch.\n", baseBranch)
				os.Exit(2)
			}
			ensTop := gitOut("rev-parse", "--show-toplevel")
			if ensTop == "" {
				ensTop, _ = os.Getwd()
			}
			ensureWorktree = filepath.Join(ensTop, ".babysit", "worktrees", newTicket+"_"+newSlug)
			if _, err := os.Lstat(ensureWorktree); err == nil {
				fmt.Fprintf(os.Stderr, "ensure: worktree path %s already exists — remove it or resume the ticket there.\n", ensureWorktree)
				os.Exit(2)
			}
			// Exclude the worktree locally so it never shows as untracked.
			if excl := gitOut("rev-parse", "--git-path", "info/exclude"); excl != "" {
				needAdd := true
				if b, err := os.ReadFile(excl); err == nil {
					for _, ln := range strings.Split(string(b), "\n") {
						if ln == ".babysit/worktrees/" {
							needAdd = false
							break
						}
					}
				}
				if needAdd {
					_ = os.MkdirAll(filepath.Dir(excl), 0o755)
					f, err := os.OpenFile(excl, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					if err == nil {
						_, _ = f.WriteString(".babysit/worktrees/\n")
						f.Close()
					}
				}
			}
			_ = os.MkdirAll(filepath.Dir(ensureWorktree), 0o755)
			if !gitOK("worktree", "add", "--no-track", "-b", newBranch, "--", ensureWorktree, srcRef) {
				fmt.Fprintf(os.Stderr, "ensure: git worktree add failed (%s → %s)\n", srcRef, ensureWorktree)
				os.Exit(2)
			}
			ticketID = newTicket
			branch = newBranch
			fmt.Fprintf(os.Stderr, "ensure: worktree ready at %s (branch %s off %s)\n", ensureWorktree, newBranch, srcRef)
			fmt.Fprintln(os.Stderr, "ensure: current checkout untouched — cd into the worktree before doing any work")
		}
	}

	// Init the ticket under one lock.
	stEnv := env
	stEnv.Ticket = ticketID
	stEnv.Branch = branch
	st := ticket.New(stEnv)
	st.EnsureDirs()
	acquireOrDie(st)
	doc := loadForMutate(st)
	doc.EnsureDefaults(ticketID)
	doc.Set("id", ticketID)
	doc.Set("pointers.branch", branch)
	reqPath := ""
	if fromInput != "" {
		reqPath = filepath.Join(st.Home(), "requirement.md")
		_ = os.WriteFile(reqPath, []byte(fromInput+"\n"), 0o644)
		doc.Set("pointers.requirement", "requirement.md")
	}
	werr := ticket.WriteDoc(st.IndexPath(), doc)

	// Seed manifest.yaml if absent (non-fatal on failure).
	mPath := st.ManifestPath()
	var mErr error
	if werr == nil && !fileExists(mPath) {
		repos := []ticket.Repo{{
			Name:      orDefault(stEnv.Slug, "repo"),
			Branch:    branch,
			Canonical: ".",
			Worktree:  orDefault(ensureWorktree, "."),
			Base:      orDefault(baseBranch, "main"),
			Pushed:    "false",
		}}
		mErr = ticket.WriteManifest(mPath, ticketID, "", repos)
	}
	st.ReleaseLock()
	if werr != nil {
		fmt.Fprintln(os.Stderr, werr)
		os.Exit(1)
	}

	// History after the index write (bash appends inside the locked section, but
	// history.jsonl is a separate append-only file — order relative to the lock
	// doesn't affect its contents).
	initExtra := ""
	if promptReason != "" {
		b, _ := json.Marshal(map[string]string{"reason": promptReason})
		initExtra = string(b)
	}
	st.HistoryAppendExtra("ticket_initialized", actorRole(), initExtra)
	if fromInput != "" {
		st.HistoryAppendExtra("requirement_seeded", actorRole(), "")
	}
	if mErr != nil {
		fmt.Fprintln(os.Stderr, "ensure: warning — manifest.yaml seed failed (continuing)")
	}

	fmt.Printf("TICKET=%s\n", ticketID)
	fmt.Printf("BRANCH=%s\n", branch)
	fmt.Printf("TICKET_HOME=%s\n", st.Home())
	fmt.Println("CREATED=1")
	if ensureWorktree != "" {
		fmt.Printf("WORKTREE=%s\n", ensureWorktree)
	}
	if reqPath != "" {
		fmt.Printf("REQUIREMENT=%s\n", reqPath)
	}
	if gfMode == "trunk" {
		fmt.Printf("export BABYSIT_TICKET=%s\n", ticketID)
	}
	os.Exit(0)
}

// newTicketID mirrors bash: bs-<8 lowercase-alnum> from crypto rand, epoch
// fallback.
func newTicketID() string {
	const cs = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		for i := range b {
			b[i] = cs[int(b[i])%len(cs)]
		}
		return "bs-" + string(b)
	}
	s := fmt.Sprintf("%d", time.Now().Unix())
	if len(s) > 8 {
		s = s[len(s)-8:]
	}
	return "bs-" + s
}

// deriveSlug mirrors the bash derive_slug pipeline: lowercase, non-[a-z0-9] →
// space, first ≤5 words joined by '-', truncate to 48 bytes, strip trailing '-'.
func deriveSlug(raw string) string {
	lower := strings.ToLower(raw)
	var sb strings.Builder
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			sb.WriteByte(c)
		} else {
			sb.WriteByte(' ')
		}
	}
	words := strings.Fields(sb.String())
	if len(words) > 5 {
		words = words[:5]
	}
	joined := strings.Join(words, "-")
	if len(joined) > 48 {
		joined = joined[:48]
	}
	return strings.TrimRight(joined, "-")
}


// divertWarning is printed when a `branch`-mode cut is forced into a worktree.
// The divert is not free: the worktree inner loop costs a commit +
// `merge-base` per test iteration, where cutting in place costs nothing. A dev
// who never chose that tax should be told how to get back to the fast loop
// rather than silently dropped into the slow one.
func divertWarning(curBranch, baseBranch string) {
	fmt.Fprintln(os.Stderr, "ensure: WARNING — the worktree loop costs a commit + 'bbs ticket merge-base' per test iteration.")
	if curBranch != baseBranch {
		fmt.Fprintf(os.Stderr, "ensure: to keep the 0-step loop: check out '%s' with a clean tree and re-run.\n", baseBranch)
	} else {
		fmt.Fprintf(os.Stderr, "ensure: to keep the 0-step loop: commit or stash on '%s' and re-run.\n", curBranch)
	}
}
