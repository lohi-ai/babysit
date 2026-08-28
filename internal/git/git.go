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

// MergedTips is the set of commits that base took in through a merge: every
// non-first parent of every merge commit reachable from base, run in dir.
//
// This is the "landed" signal, and it is deliberately narrower than
// `git branch --merged`. That command answers "is this branch's tip reachable
// from base", which is vacuously true for a branch freshly cut from base and
// for one that never got a commit — so it reports every ticket as finished the
// moment its branch exists. A branch appears here only if base actually merged
// it, which is exactly what `bbs ticket land` does (--no-ff, so the merge is
// never fast-forwarded away).
//
// A squash-merged PR is not in this set: squashing discards the second parent,
// so nothing local records where the commit came from. Those tickets rest at
// in_review on their PR pointer.
//
// nil on any failure (not a repo, no such base) and callers must read that as
// "unknown", never as "not merged": reconcile only ever advances a status, so a
// missed signal costs one tick while a wrong one strands a live ticket at done.
func MergedTips(dir, base string) map[string]bool {
	if base == "" {
		return nil
	}
	out, ok := runIn(dir, "rev-list", "--merges", "--parents", base)
	if !ok {
		return nil
	}
	set := map[string]bool{}
	for _, ln := range strings.Split(out, "\n") {
		// "<merge> <parent1> <parent2>…" — parent1 is base's own history.
		if f := strings.Fields(ln); len(f) > 2 {
			for _, p := range f[2:] {
				set[p] = true
			}
		}
	}
	return set
}

// RevParseIn is `git rev-parse <ref>` in dir, or "" when the ref does not
// resolve.
func RevParseIn(dir, ref string) string {
	out, ok := runIn(dir, "rev-parse", "--verify", "--quiet", ref)
	if !ok {
		return ""
	}
	return strings.TrimSpace(out)
}
