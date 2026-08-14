package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/slug"
	"github.com/reallongnguyen/babysit/internal/ticket"
	"github.com/reallongnguyen/babysit/internal/workspace"
)

const boardRowFmt = "%-14s %-12s %-9s %-9s %-7s %-16s %-12s %s\n"

// runBoard ports bin/bbs-ticket.bash:3275-3399 — a read-only aggregated view of
// every ticket joined with its verdicts, branch, session, PR and siblings, plus
// a qa-lease + serving footer. Zero mutation.
func runBoard(args []string) {
	withPR, showAll := false, false
	for _, a := range args {
		switch a {
		case "--pr":
			withPR = true
		case "--all":
			showAll = true
		default:
			fmt.Fprintf(os.Stderr, "board: unknown arg '%s'\n", a)
			os.Exit(2)
		}
	}

	env := identity.Resolve()
	tdir := filepath.Join(env.ProjectHome, "tickets")
	if fi, err := os.Stat(tdir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "board: no tickets at %s\n", tdir)
		os.Exit(0)
	}

	primary, gitdir, repo := gitContext()
	now := time.Now().Unix()

	fmt.Printf(boardRowFmt, "TICKET", "STATUS", "QA", "REVIEW", "PUSHED", "SESSION", "PR", "BRANCH")

	entries, _ := os.ReadDir(tdir)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, tid := range names {
		d := filepath.Join(tdir, tid)
		idx := ticket.ReadIndex(filepath.Join(d, "index.json"))

		status := idx.Status
		if status == "" {
			status = "triage"
		}
		if !showAll {
			switch status {
			case "done", "cancelled", "duplicate":
				continue
			}
		}

		// Board reads manifests through bash manifest_read, which exits 2 on
		// version != 1 and leaves both columns at "-". (resolve deliberately
		// parses any version — different codepath, different contract.)
		branch, pushed := "-", "-"
		if m, err := ticket.ReadManifest(filepath.Join(d, "manifest.yaml")); err == nil && m.Version == "1" && len(m.Repos) > 0 {
			r := m.Repos[0]
			for _, cand := range m.Repos {
				if cand.Name == repo {
					r = cand
					break
				}
			}
			branch, pushed = r.Branch, pyBool(r.Pushed)
		}

		st := ticket.New(identity.Env{ProjectHome: env.ProjectHome, Ticket: tid})
		qa := verdictStatus(st, "qa")
		review := verdictStatus(st, "review-pr")

		session := sessionFor(tid, now)
		if session == "" {
			session = "-"
		}

		prDisp, merged := "-", false
		if idx.Pointers.PR != "" {
			prDisp = prShort(idx.Pointers.PR)
			if withPR {
				if state := ghPRState(idx.Pointers.PR); state != "" {
					prDisp += " " + state
					merged = state == "MERGED"
				}
			}
		}

		fmt.Printf(boardRowFmt, tid, status, qa, review, pushed, session, prDisp, branch)
		// Control is a sub-row, not a STATUS cell: the two are different axes,
		// and a paused ticket's real rung is what the human needs to see.
		if c := idx.Control; c != nil && c.State != "" {
			fmt.Printf(retarget("  ⏸ %s by %s — status stays %s (bbs-ticket %s)\n"),
				c.State, c.Actor, status, undoVerb(c.State))
		}
		if merged {
			fmt.Printf(retarget("  ↳ PR merged — next: bbs-ticket reset-base; BABYSIT_TICKET=%s bbs-ticket set-status done\n"), tid)
		}

		for _, sib := range idx.Siblings {
			if sib.Ticket == "" {
				continue
			}
			printSiblingRow(sib, primary)
		}
	}

	if gitdir != "" {
		printFooter(gitdir, now)
		printBaseLine(primary, gitdir, now)
	}
	os.Exit(0)
}

func printSiblingRow(sib ticket.Sibling, primary string) {
	path, ok, err := relatedRepoPathVia(workspace.NewResolver(primary, ""), sib.Role, primary)
	if err != nil {
		// The board is the screen a human reads to find out why something is
		// stuck, so a conflict has to read as a conflict here — "unresolved"
		// would send them to add the entry that already exists.
		fmt.Printf("  └─ %s:%s (%s) — path conflicts: workspace and .babysit/.env disagree (run `bbs ticket serve` for both paths)\n", sib.Role, sib.Ticket, sib.Repo)
		return
	}
	if !ok {
		fmt.Printf("  └─ %s:%s (%s) — path unresolved (no workspace entry for this role, RELATED repo env unset)\n", sib.Role, sib.Ticket, sib.Repo)
		return
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		fmt.Printf("  └─ %s:%s (%s) — path '%s' missing\n", sib.Role, sib.Ticket, sib.Repo, path)
		return
	}

	slug := slugIn(path)
	home := filepath.Join(identity.BabysitHome(), "projects", slug)
	sst := ticket.ReadIndex(filepath.Join(home, "tickets", sib.Ticket, "index.json")).Status
	if sst == "" {
		sst = "?"
	}
	sqa := verdictStatus(ticket.New(identity.Env{ProjectHome: home, Ticket: sib.Ticket}), "qa")
	fmt.Printf("  └─ %s:%s (%s) status=%s qa=%s\n", sib.Role, sib.Ticket, filepath.Base(path), sst, sqa)
}

func printFooter(gitdir string, now int64) {
	leaseDir := filepath.Join(gitdir, "bbs-qa-lease")
	if fi, err := os.Stat(leaseDir); err == nil && fi.IsDir() {
		owner := leaseField(leaseDir, "owner")
		since := leaseField(leaseDir, "since_epoch")
		ttl := leaseField(leaseDir, "ttl_min")
		if owner == "" {
			owner = "unknown"
		}
		sinceN, err := strconv.ParseInt(since, 10, 64)
		if err != nil {
			sinceN = now
		}
		if ttl == "" {
			ttl = "60"
		}
		fmt.Printf("QA-LEASE: %s (%dmin into %smin ttl)\n", owner, (now-sinceN)/60, ttl)
	} else {
		fmt.Println("QA-LEASE: FREE")
	}

	serving := strings.TrimRight(readFile(filepath.Join(gitdir, "bbs-serving")), "\n")
	if serving == "" {
		serving = "(base only)"
	}
	fmt.Printf("SERVING: %s\n", serving)
}

// printBaseLine reports how the primary checkout's local base compares to
// origin/<base>. Under the branch-cutting modes every ticket branch is cut from
// origin/<base>, never local, so commits sitting only on local base are
// invisible to every ticket cut afterwards and nothing else on the board says
// so. Under `mode: trunk` no branch is cut at all, which inverts what the same
// count means — the ahead line is worded per mode rather than asserting the
// branch-cutting consequence at a pet repo it does not apply to.
//
// Read-only and offline — board never fetches, so the comparison is against the
// last-known remote ref. The fetch age is printed alongside it so a stale
// answer reads as stale instead of as fact. Warn-only by construction: a
// diverged base is normal mid-flight, so this never blocks and never advises
// anything destructive.
func printBaseLine(primary, gitdir string, now int64) {
	base := baseBranchIn(primary)
	if base == "" {
		return
	}
	haveOrigin := gitCOK(primary, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+base)
	if !gitCOK(primary, "show-ref", "--verify", "--quiet", "refs/heads/"+base) {
		// No local base means no local drift, but staying silent here would be
		// the one case with no line at all — and it is usually a misconfigured
		// base_branch, which breaks composing too. Say so only when origin has
		// the branch; when neither side does, the repo is simply new.
		if haveOrigin {
			fmt.Printf(retarget("BASE: %s — no local '%s' branch (origin/%s exists) — bbs-ticket serve, switch and merge-base need one\n"), base, base, base)
		}
		return
	}
	if !haveOrigin {
		fmt.Printf("BASE: %s — no origin/%s ref (no remote, or never fetched)\n", base, base)
		return
	}

	counts := gitCOut(primary, "rev-list", "--left-right", "--count", "origin/"+base+"..."+base)
	fields := strings.Fields(counts)
	if len(fields) != 2 {
		return
	}
	behind, ahead := fields[0], fields[1]

	age := ""
	if fi, err := os.Stat(filepath.Join(gitdir, "FETCH_HEAD")); err == nil {
		// FETCH_HEAD's mtime is the last fetch of anything, not of this branch
		// specifically — hence "last fetch", which is the claim it supports.
		age = fmt.Sprintf(" (last fetch %s ago)", roughAge(now-fi.ModTime().Unix()))
	}

	if behind == "0" && ahead == "0" {
		fmt.Printf("BASE: %s — matches origin/%s%s\n", base, base, age)
		return
	}
	fmt.Printf("BASE: %s — %s ahead / %s behind origin/%s%s\n", base, ahead, behind, base, age)
	if ahead != "0" {
		// A resolve error (invalid profile/mode) leaves the policy unknown, so
		// fall back to what every profile now resolves to: trunk. The
		// branch-cutting wording only applies to a repo that asked for it.
		mode := "trunk"
		if p, err := resolveGitFlow(primary); err == nil {
			mode = p.Mode
		}
		if mode == "trunk" {
			fmt.Printf("  ↑ %s commit(s) on local '%s' are not on origin/%s — mode: trunk cuts no branch, so tickets build on them; they are only unpushed\n", ahead, base, base)
		} else {
			fmt.Printf("  ↑ %s commit(s) exist only on local '%s' — tickets are cut from origin/%s, so new ones will not have them\n", ahead, base, base)
		}
	}
	if behind != "0" {
		fmt.Printf(retarget("  ↓ origin/%s moved on — bbs-ticket refresh (in a ticket) or bbs-ticket reset-base (on the primary)\n"), base)
	}
}

// roughAge renders a duration the way a human skims it — one unit, no decimals.
func roughAge(sec int64) string {
	switch {
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh", sec/3600)
	default:
		return fmt.Sprintf("%dd", sec/86400)
	}
}

// sessionFor returns "id(Nm)" for the first live session claiming this ticket,
// or "" — mirroring bash bd_session_for (glob order, 120m window, id cut to 12).
func sessionFor(tid string, now int64) string {
	dir := ticket.SessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, n := range names {
		f := filepath.Join(dir, n)
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		mt := fi.ModTime().Unix()
		if now-mt > ticket.SessionWindow {
			continue
		}
		if ticket.SessionField(f, "ticket") != tid {
			continue
		}
		id := ticket.SessionField(f, "session_id")
		if len(id) > 12 {
			id = id[:12]
		}
		return fmt.Sprintf("%s(%dm)", id, (now-mt)/60)
	}
	return ""
}

// pyBool reproduces a quirk of the PUSHED column, not a choice: bash renders it
// via manifest_read, whose coerce() turns bare true/false into a Python bool and
// then f-strings it — so the column has always read "True"/"False". The writer
// (manifest_write yval) only ever emits bare true/false; any other value was
// never a bool in Python either, so it passes through as-is.
func pyBool(v string) string {
	switch v {
	case "true":
		return "True"
	case "false":
		return "False"
	}
	return v
}

// prShort mimics `sed -n 's|.*/pull/|#|p'`: no match → the raw URL.
func prShort(url string) string {
	if i := strings.LastIndex(url, "/pull/"); i >= 0 {
		return "#" + url[i+len("/pull/"):]
	}
	return url
}

func ghPRState(url string) string {
	out, err := exec.Command("gh", "pr", "view", url, "--json", "state", "-q", ".state").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// relatedRepoEnv maps a sibling role to its .babysit/.env var. Unmapped roles
// return false — callers report, never guess (bash _related_repo_env).
func relatedRepoEnv(role string) (string, bool) {
	switch role {
	case "fe", "frontend":
		return "RELATED_FRONTEND_REPO", true
	case "be", "backend":
		return "RELATED_BACKEND_REPO", true
	case "shared":
		return "RELATED_SHARED_REPO", true
	}
	return "", false
}

// relatedRepoPathEnv resolves a sibling repo's local path from
// <toplevel>/.babysit/.env — the original and, for an unregistered repo, still
// the only answer.
func relatedRepoPathEnv(role, toplevel string) (string, bool) {
	v, ok := relatedRepoEnv(role)
	if !ok || toplevel == "" {
		return "", false
	}
	for _, ln := range strings.Split(readFile(filepath.Join(toplevel, ".babysit", ".env")), "\n") {
		if !strings.HasPrefix(ln, v+"=") {
			continue
		}
		val := strings.TrimPrefix(ln, v+"=")
		val = strings.Trim(val, `"'`)
		if val == "" {
			return "", false
		}
		return val, true
	}
	return "", false
}

// relatedRepoPathVia resolves a sibling role through a prepared resolver: the
// workspace registry is authoritative, .babysit/.env answers when the repo is
// not a workspace member, and a disagreement between the two BLOCKs.
//
// Two sources that can each name a different directory for the same role is
// exactly the failure this ticket exists to avoid, so when both answer and
// disagree the caller stops rather than picking — the same call the ticket
// identity ladder makes on a conflicting BABYSIT_TICKET. Agreement is not a
// conflict, so a repo that registered its siblings and also kept the old .env
// entries keeps working untouched.
func relatedRepoPathVia(r *workspace.Resolver, role, toplevel string) (string, bool, error) {
	envPath, envOK := relatedRepoPathEnv(role, toplevel)
	wsPath, wsOK := r.RolePath(role)
	switch {
	case wsOK && envOK && !workspace.SamePath(wsPath, envPath):
		v, _ := relatedRepoEnv(role)
		return "", false, fmt.Errorf(
			"sibling role '%s' resolves two ways: workspace %s says '%s', %s in %s/.babysit/.env says '%s'.\n"+
				"  Fix: make them agree, or drop %s from .env — the workspace is authoritative",
			role, r.Name(), wsPath, v, toplevel, envPath, v)
	case wsOK:
		return wsPath, true, nil
	case envOK:
		return envPath, true, nil
	}
	return "", false, nil
}

// slugIn derives the project slug of the repo at dir.
func slugIn(dir string) string {
	info, err := slug.ResolveIn(dir)
	if err != nil {
		return ""
	}
	return info.Slug
}

func leaseField(dir, key string) string {
	for _, ln := range strings.Split(readFile(filepath.Join(dir, "owner")), "\n") {
		if v, ok := strings.CutPrefix(ln, key+"="); ok {
			return v
		}
	}
	return ""
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// gitContext resolves the primary worktree, its git dir, and the repo name —
// the equivalents of `git worktree list --porcelain | head -1`,
// `git rev-parse --absolute-git-dir`, and basename. go-git confirms we are
// inside a work tree at all (it understands the .git-file indirection); the
// paths themselves come from that pointer, which go-git does not expose.
func gitContext() (primary, gitdir, repo string) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", ""
	}
	if _, err := gogit.PlainOpenWithOptions(cwd, &gogit.PlainOpenOptions{DetectDotGit: true}); err != nil {
		return "", "", ""
	}

	for dir := cwd; ; {
		gp := filepath.Join(dir, ".git")
		fi, err := os.Stat(gp)
		if err == nil {
			if fi.IsDir() {
				return dir, gp, filepath.Base(dir)
			}
			// Linked worktree: .git is a file holding
			// "gitdir: <primary>/.git/worktrees/<name>".
			v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(readFile(gp)), "gitdir:"))
			marker := string(os.PathSeparator) + "worktrees" + string(os.PathSeparator)
			if i := strings.Index(v, marker); i >= 0 {
				gd := v[:i]
				return filepath.Dir(gd), gd, filepath.Base(filepath.Dir(gd))
			}
			return "", "", ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", ""
		}
		dir = parent
	}
}
