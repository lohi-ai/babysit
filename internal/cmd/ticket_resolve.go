package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// runResolve ports bin/bbs-ticket.bash:922-1051 — the single identity entry
// point documented in docs/identity.md.
//
// Ladder: env → manifest.yaml cwd match → branch regex.
// Exit codes: 0 resolved (ticket id on stdout), 1 no resolution, 2 conflict.
func runResolve(args []string) {
	explainOn := false
	for _, a := range args {
		if a == "--explain" {
			explainOn = true
		}
	}
	explain := func(format string, v ...any) {
		if explainOn {
			fmt.Fprintf(os.Stderr, "resolve: "+format+"\n", v...)
		}
	}

	// ─── Step 1: env ─────────────────────────────────────────────────
	babysitTicket, bbsTicket := os.Getenv("BABYSIT_TICKET"), os.Getenv("BBS_TICKET")
	if babysitTicket != "" && bbsTicket != "" && babysitTicket != bbsTicket {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: env conflict — BABYSIT_TICKET=%s, BBS_TICKET=%s\n", babysitTicket, bbsTicket)
		fmt.Fprintln(os.Stderr, "ATTEMPTED: env (both set, disagree)")
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: unset one of them, e.g. `unset BBS_TICKET` to use the BABYSIT_TICKET value.")
		os.Exit(2)
	}
	if babysitTicket != "" {
		explain("matched env BABYSIT_TICKET=%s", babysitTicket)
		fmt.Println(babysitTicket)
		os.Exit(0)
	}
	if bbsTicket != "" {
		explain("matched env BBS_TICKET=%s (legacy alias)", bbsTicket)
		fmt.Println(bbsTicket)
		os.Exit(0)
	}

	env := identity.Resolve()
	explain("bootstrap bbs-slug SLUG=%s → %s", env.Slug, env.ProjectHome)

	// ─── Step 2: manifest.yaml cwd match ─────────────────────────────
	if matches := manifestCwdMatches(env.ProjectHome); len(matches) == 1 {
		explain("matched manifest.yaml cwd → %s", matches[0])
		fmt.Println(matches[0])
		os.Exit(0)
	} else if len(matches) > 1 {
		pwd, _ := os.Getwd()
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: multiple manifest.yaml worktrees claim cwd %s\n", pwd)
		fmt.Fprintln(os.Stderr, "ATTEMPTED: step 2 — manifest.yaml cwd walk")
		fmt.Fprintf(os.Stderr, "CANDIDATES: %s\n", strings.Join(matches, " "))
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: cd into a more specific worktree, or set BABYSIT_TICKET explicitly.")
		os.Exit(2)
	}

	// ─── Step 3: branch fallback ─────────────────────────────────────
	if env.DerivedTicket != "" {
		explain("matched branch regex → %s", env.DerivedTicket)
		fmt.Println(env.DerivedTicket)
		os.Exit(0)
	}
	explain("no resolution")
	os.Exit(1)
}

// manifestCwdMatches returns the ticket ids whose manifest.yaml declares a
// worktree containing $PWD, deduped and in directory order.
func manifestCwdMatches(projectHome string) []string {
	tdir := filepath.Join(projectHome, "tickets")
	entries, err := os.ReadDir(tdir)
	if err != nil {
		return nil
	}
	pwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	pwdReal, err := filepath.EvalSymlinks(pwd)
	if err != nil {
		pwdReal = pwd
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var matches []string
	seen := map[string]bool{}
	for _, tid := range names {
		m, err := ticket.ReadManifest(filepath.Join(tdir, tid, "manifest.yaml"))
		if err != nil {
			continue
		}
		mid := m.Ticket
		if mid == "" {
			mid = tid
		}
		for _, repo := range m.Repos {
			w := repo.Worktree
			// Single-repo manifests record "." — the repo root is unknown from
			// here, so cwd matching is skipped and step 3 handles it. (An empty
			// value is skipped too; bash normpath("")="." would match every cwd
			// and resolve this ticket everywhere — a bug not worth reproducing.)
			if w == "" || w == "." || w == "./" {
				continue
			}
			// bash realpath()s a relative worktree against CWD. Go's
			// EvalSymlinks leaves a relative path relative, so it could never
			// match the absolute pwdReal — absolutize first.
			wabs := w
			if !filepath.IsAbs(w) {
				wabs = filepath.Join(pwd, w)
			}
			wreal, err := filepath.EvalSymlinks(wabs)
			if err != nil {
				wreal = filepath.Clean(wabs)
			}
			if wreal == pwdReal || strings.HasPrefix(pwdReal, wreal+string(os.PathSeparator)) {
				if !seen[mid] {
					seen[mid] = true
					matches = append(matches, mid)
				}
				break
			}
		}
	}
	return matches
}
