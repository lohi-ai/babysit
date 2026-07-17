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
	"github.com/reallongnguyen/babysit/internal/ticket"
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
		if merged {
			fmt.Printf("  ↳ PR merged — next: bbs-ticket reset-base; BABYSIT_TICKET=%s bbs-ticket set-status done\n", tid)
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
	}
	os.Exit(0)
}

func printSiblingRow(sib ticket.Sibling, primary string) {
	path, ok := relatedRepoPath(sib.Role, primary)
	if !ok {
		fmt.Printf("  └─ %s:%s (%s) — path unresolved (RELATED repo env unset)\n", sib.Role, sib.Ticket, sib.Repo)
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

// relatedRepoPath resolves a sibling repo's local path from <toplevel>/.babysit/.env.
func relatedRepoPath(role, toplevel string) (string, bool) {
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

// slugIn runs bbs-slug in dir and returns its SLUG.
func slugIn(dir string) string {
	c := exec.Command(identity.SlugBin(), "env")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(ln, "SLUG="); ok {
			return v
		}
	}
	return ""
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
