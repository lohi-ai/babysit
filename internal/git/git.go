// Package git wraps the handful of read-only git invocations the ported bins
// need. Each helper returns "" on any failure, mirroring the bash scripts'
// `git ... 2>/dev/null || true` idiom — shelling out to git is intentional
// (the bins do), only jq/yq are replaced by native Go.
package git

import (
	"os/exec"
	"strings"
)

// runIn is `git <args>` executed in dir, or in the process cwd when dir is "".
// Every helper funnels through it so the dir-scoped and cwd-scoped forms cannot
// drift apart.
func runIn(dir string, args ...string) (string, bool) {
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

// RemoteURL returns `git remote get-url <name>`, or "" if git fails (no repo,
// no such remote). Trailing newline is stripped to match `$(...)` capture.
func RemoteURL(name string) string { return RemoteURLIn("", name) }

// RemoteURLIn is RemoteURL scoped to dir.
func RemoteURLIn(dir, name string) string {
	out, _ := runIn(dir, "remote", "get-url", name)
	return out
}

// CurrentBranch returns `git rev-parse --abbrev-ref HEAD`, or "" outside a repo.
func CurrentBranch() string { return CurrentBranchIn("") }

// CurrentBranchIn is CurrentBranch scoped to dir.
func CurrentBranchIn(dir string) string {
	out, _ := runIn(dir, "rev-parse", "--abbrev-ref", "HEAD")
	return out
}

// PrimaryWorktree returns the first entry of `git worktree list --porcelain`,
// which is always the primary checkout. This is the consistency anchor from
// bin/bbs-slug: a linked worktree must resolve the same project home as its
// primary checkout. The bool is false when git itself failed (not a repo) —
// bin/bbs-slug crashes here under `set -euo pipefail`, so callers replicate
// that hard exit rather than falling back.
func PrimaryWorktree() (string, bool) { return PrimaryWorktreeIn("") }

// PrimaryWorktreeIn is PrimaryWorktree scoped to dir.
func PrimaryWorktreeIn(dir string) (string, bool) {
	out, ok := runIn(dir, "worktree", "list", "--porcelain")
	if !ok {
		return "", false
	}
	for _, ln := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(ln, "worktree "); ok {
			return v, true
		}
	}
	return "", true
}
