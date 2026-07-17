// Package slug ports bin/bbs-slug's derivation: project slug, sanitized
// branch, and derived ticket id, computed from the git remote + branch.
//
// It deliberately hardcodes $HOME/.babysit (NOT BABYSIT_STATE_DIR): the bash
// script never honored that override for the slug cache or project home, and
// this port must match byte-for-byte.
package slug

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/reallongnguyen/babysit/internal/git"
)

// ErrNoRepo signals that git could not resolve a worktree — i.e. we are not in
// a git repository. bin/bbs-slug aborts here under `set -euo pipefail` (its
// unguarded `git worktree list` on line 29), exiting 128 with no output before
// any subcommand logic runs; the caller reproduces that exit exactly.
var ErrNoRepo = errors.New("not a git repository")

// Info is the resolved identity, mirroring the KEY=VALUE lines bin/bbs-slug
// emits for `eval`.
type Info struct {
	Slug        string
	Branch      string
	Ticket      string
	ProjectHome string
}

// The two sed substitutions from bin/bbs-slug, applied in sequence to the
// remote URL. First strips a trailing `.git`, capturing owner/repo; second
// captures owner/repo when there is no `.git`. A non-match leaves the string
// unchanged, exactly like sed.
var (
	reGit   = regexp.MustCompile(`^.*[:/]([^/]*/[^/]*)\.git$`)
	reNoGit = regexp.MustCompile(`^.*[:/]([^/]*/[^/]*)$`)
	// ticketRe matches the canonical branch shapes; group 2 is the ticket id.
	ticketRe = regexp.MustCompile(`^(feat|fix|chore|bug|refactor|hotfix)/([A-Za-z0-9-]+)_`)
)

// Resolve computes the slug/branch/ticket identity. It errors in exactly two
// cases: ErrNoRepo (git cannot resolve a worktree) and the env-conflict abort
// (BABYSIT_TICKET vs BBS_TICKET disagree). Every other path — no remote, no
// branch, no ticket — yields a populated Info, matching the bash script.
func Resolve() (*Info, error) {
	home := os.Getenv("HOME")
	cacheDir := filepath.Join(home, ".babysit", "slug-cache")

	// Key the cache by the repo's PRIMARY worktree, not the cwd. Outside a
	// repo, git fails and bin/bbs-slug's `set -e` aborts the whole script —
	// so we short-circuit before touching env, cache, or output.
	projectDir, ok := git.PrimaryWorktree()
	if !ok {
		return nil, ErrNoRepo
	}
	cacheKey := strings.ReplaceAll(projectDir, "/", "_")
	cacheFile := filepath.Join(cacheDir, cacheKey)

	// 1. Cached slug wins (consistency across sessions).
	slug := ""
	if b, err := os.ReadFile(cacheFile); err == nil {
		slug = strings.TrimRight(string(b), "\n")
	}

	// 2. Compute from the git remote.
	if slug == "" {
		if remote := git.RemoteURL("origin"); remote != "" {
			raw := reGit.ReplaceAllString(remote, "$1")
			raw = reNoGit.ReplaceAllString(raw, "$1")
			raw = strings.ReplaceAll(raw, "/", "-")
			slug = keep(raw, isSlugChar)
		}
	}

	// 3. Fallback to the cwd basename when there is no remote.
	if slug == "" {
		wd, _ := os.Getwd()
		slug = keep(filepath.Base(wd), isSlugChar)
	}

	// 4. Cache the slug atomically, failing silently.
	if slug != "" {
		writeCache(cacheDir, cacheFile, slug)
	}

	rawBranch := git.CurrentBranch()
	branch := keep(rawBranch, isBranchChar)
	if branch == "" {
		branch = "unknown"
	}

	ticket := ""
	if m := ticketRe.FindStringSubmatch(branch); m != nil {
		ticket = m[2]
	}
	// Env-first identity: BABYSIT_TICKET (canonical) / BBS_TICKET (legacy)
	// override the branch. Both set and disagreeing is a loud abort.
	bab, bbs := os.Getenv("BABYSIT_TICKET"), os.Getenv("BBS_TICKET")
	if bab != "" && bbs != "" && bab != bbs {
		return nil, fmt.Errorf("bbs-slug: BABYSIT_TICKET=%s conflicts with BBS_TICKET=%s; unset one to proceed.", bab, bbs)
	}
	if bab != "" {
		ticket = bab
	} else if bbs != "" {
		ticket = bbs
	}

	projectHome := os.Getenv("BABYSIT_PROJECT_HOME")
	if projectHome == "" {
		projectHome = filepath.Join(home, ".babysit", "projects", slug)
	}

	return &Info{Slug: slug, Branch: branch, Ticket: ticket, ProjectHome: projectHome}, nil
}

// writeCache mirrors the bash atomic write: mkdir -p, mktemp in the cache dir,
// write the slug (no newline), rename into place. Every error is swallowed.
func writeCache(dir, file, slug string) {
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".slug-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(slug)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(name, file) != nil {
		os.Remove(name)
	}
}

// keep drops every byte not accepted by ok, matching `tr -cd`.
func keep(s string, ok func(byte) bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if ok(s[i]) {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// isSlugChar is the `[a-zA-Z0-9._-]` set.
func isSlugChar(c byte) bool { return isAlnum(c) || c == '.' || c == '_' || c == '-' }

// isBranchChar is the `[a-zA-Z0-9._/-]` set (slashes kept for ticket parsing).
func isBranchChar(c byte) bool { return isSlugChar(c) || c == '/' }
