// Package git wraps the handful of read-only git invocations the ported bins
// need. Each helper returns "" on any failure, mirroring the bash scripts'
// `git ... 2>/dev/null || true` idiom — shelling out to git is intentional
// (the bins do), only jq/yq are replaced by native Go.
package git

import (
	"os/exec"
	"strings"
)

// RemoteURL returns `git remote get-url <name>`, or "" if git fails (no repo,
// no such remote). Trailing newline is stripped to match `$(...)` capture.
func RemoteURL(name string) string {
	out, err := exec.Command("git", "remote", "get-url", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// CurrentBranch returns `git rev-parse --abbrev-ref HEAD`, or "" outside a repo.
func CurrentBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// PrimaryWorktree returns the first entry of `git worktree list --porcelain`,
// which is always the primary checkout. This is the consistency anchor from
// bin/bbs-slug: a linked worktree must resolve the same project home as its
// primary checkout. The bool is false when git itself failed (not a repo) —
// bin/bbs-slug crashes here under `set -euo pipefail`, so callers replicate
// that hard exit rather than falling back.
func PrimaryWorktree() (string, bool) {
	out, err := exec.Command("git", "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", false
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(ln, "worktree "); ok {
			return v, true
		}
	}
	return "", true
}
