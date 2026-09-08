package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The status ladder is what a human reads at release time to tell which tickets
// are finished. Before `landed`, it topped out at in_review and only ever moved
// on a `pushed:` flag, so a batch that had genuinely closed itself out still
// read `planned` — the board could not distinguish delivered work from a plan
// nobody had started.
//
// Every case here is a shape that `git branch --merged` (the obvious
// implementation) gets wrong by reporting it finished.

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.test")
	out, err := c.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return string(out)
}

// ticketRepo builds a repo with one commit on main and a ticket home whose
// manifest points at it. Returns the repo dir and the ticket home.
func ticketRepo(t *testing.T, branch string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "init")

	th := t.TempDir()
	manifest := fmt.Sprintf(""+
		"version: 1\nticket: bs-x\nrepos:\n"+
		"  - name: r\n    branch: %s\n    canonical: .\n    worktree: %s\n    base: main\n    pushed: false\n",
		branch, repo)
	if err := os.WriteFile(filepath.Join(th, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	mergedCache = map[string]mergedEntry{}
	return repo, th
}

// A branch cut from base and not yet committed to shares base's tip, so base
// can trivially reach it. It is the state of every ticket the moment `ensure`
// runs — reading it as landed would mark the whole board done on creation.
func TestFreshBranchIsNotLanded(t *testing.T) {
	repo, th := ticketRepo(t, "feat/bs-x_thing")
	gitIn(t, repo, "branch", "feat/bs-x_thing")

	if landed(th) {
		t.Fatal("a branch with no commits of its own is not landed")
	}
	if got := reconcileTarget(th); got == "done" {
		t.Fatalf("reconcileTarget = %q, want anything but done", got)
	}
}

// The same vacuous reachability, but with base moved on independently: the
// branch still has no work, and "merged" now hides that behind a real-looking
// history.
func TestAbandonedBranchWithBaseAheadIsNotLanded(t *testing.T) {
	repo, th := ticketRepo(t, "feat/bs-x_thing")
	gitIn(t, repo, "branch", "feat/bs-x_thing")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "commit", "-qam", "base moves on")

	if landed(th) {
		t.Fatal("an abandoned branch is not landed just because base moved past it")
	}
}

// Trunk mode: the work goes straight onto base, so there is no merge to find.
func TestTrunkBranchIsNotLanded(t *testing.T) {
	_, th := ticketRepo(t, "main")
	if landed(th) {
		t.Fatal("branch == base is never landed")
	}
}

// The real thing: commits on the branch, merged --no-ff the way
// `bbs ticket land` does it.
func TestMergedBranchIsLandedAndReconcilesToDone(t *testing.T) {
	repo, th := ticketRepo(t, "feat/bs-x_thing")
	gitIn(t, repo, "checkout", "-q", "-b", "feat/bs-x_thing")
	if err := os.WriteFile(filepath.Join(repo, "g"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "feat: work")
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "Merge branch 'feat/bs-x_thing'", "feat/bs-x_thing")

	if !landed(th) {
		t.Fatal("a branch base merged is landed")
	}
	if got := reconcileTarget(th); got != "done" {
		t.Fatalf("reconcileTarget = %q, want done", got)
	}
}

// Committing again after the merge puts work on the branch that base does not
// have, so the ticket is no longer finished. reconcile never downgrades, but
// the derivation itself has to be honest.
func TestCommitAfterLandingIsNotLanded(t *testing.T) {
	repo, th := ticketRepo(t, "feat/bs-x_thing")
	gitIn(t, repo, "checkout", "-q", "-b", "feat/bs-x_thing")
	if err := os.WriteFile(filepath.Join(repo, "g"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "feat: work")
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "Merge branch 'feat/bs-x_thing'", "feat/bs-x_thing")
	gitIn(t, repo, "checkout", "-q", "feat/bs-x_thing")
	if err := os.WriteFile(filepath.Join(repo, "g"), []byte("more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "commit", "-qam", "feat: more")
	mergedCache = map[string]mergedEntry{}

	if landed(th) {
		t.Fatal("work added after the merge is not in base")
	}
}

// A ticket with no manifest at all (never cut a branch) must not resolve to a
// terminal rung reconcile can never move again.
func TestNoManifestIsNotLanded(t *testing.T) {
	if landed(t.TempDir()) {
		t.Fatal("no manifest is not landed")
	}
}

// in_progress is a middle rung of the documented ladder, not a terminal state.
// Left out of reconcileRank it read as terminal, so a ticket somebody moved to
// in_progress could never be advanced by the landing it later got — it sat on
// the board as unfinished work that was in fact already merged.
func TestInProgressStillAdvances(t *testing.T) {
	if _, ok := reconcileRank["in_progress"]; !ok {
		t.Fatal("in_progress must be a rank, or reconcile freezes the ticket there")
	}
	for _, terminal := range []string{"cancelled", "duplicate", "blocked"} {
		if _, ok := reconcileRank[terminal]; ok {
			t.Fatalf("%q is a human's verdict on the ticket, not a rung", terminal)
		}
	}
	if reconcileRank["in_progress"] >= reconcileRank["in_review"] {
		t.Fatal("in_progress must rank below in_review, or reconcile downgrades it")
	}
}
