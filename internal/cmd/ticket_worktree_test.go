package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runWorktreeRemove os.Exits on failure, so tests only cover paths that
// return: a real removal, and a removal that succeeds after transient
// failures (the NTFS open-handle case the retry exists for).
func TestWorktreeRemoveRemovesARealWorktree(t *testing.T) {
	repo := initSnapshotRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "wt-branch", wt)
	runWorktreeRemove([]string{wt})
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree still present after remove: %v", err)
	}
}

func TestWorktreeRemoveRetriesTransientFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a shell script")
	}
	repo := initSnapshotRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "wt-branch", wt)

	// Fake git fails the first two `worktree remove` calls, then delegates.
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	bin := t.TempDir()
	counter := filepath.Join(bin, "count")
	script := "#!/bin/sh\n" +
		"if [ \"$3\" = \"worktree\" ] && [ \"$4\" = \"remove\" ]; then\n" +
		"  n=$(cat \"" + counter + "\" 2>/dev/null || echo 0); n=$((n+1)); echo $n > \"" + counter + "\"\n" +
		"  if [ $n -lt 3 ]; then echo 'fatal: file in use' >&2; exit 128; fi\n" +
		"fi\n" +
		"exec \"" + real + "\" \"$@\"\n"
	fake := filepath.Join(bin, "git")
	mustWrite(t, fake, script)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	runWorktreeRemove([]string{wt})
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree still present after retried remove: %v", err)
	}
	n, err := os.ReadFile(counter)
	if err != nil || strings.TrimSpace(string(n)) != "3" {
		t.Fatalf("expected 3 remove attempts, got %q err=%v", n, err)
	}
}
