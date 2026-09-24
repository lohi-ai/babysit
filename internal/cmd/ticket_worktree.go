package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// `bbs ticket worktree-remove <path>` is `git worktree remove` with a bounded
// retry: on NTFS a transient open handle (antivirus, indexer, a terminal that
// just closed) fails the removal outright, and the foreman's unattended
// close-out has nobody to re-run it. The retry window is short — a real
// blocker still fails loud, just a few seconds later.
const (
	worktreeRemoveAttempts = 10
	worktreeRemoveDelay    = 500 * time.Millisecond
)

func runWorktreeRemove(args []string) {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: bbs-ticket worktree-remove <worktreePath>")
		os.Exit(2)
	}
	target := args[0]
	// `worktree remove` must run from inside the repo, not from the doomed
	// worktree itself — running there holds the very handle that fails the
	// removal on NTFS. The common git dir answers for every shape: `<repo>/.git`
	// and a bare repo are both valid `-C` targets, and a repo using
	// --separate-git-dir resolves to the agent that owns the worktrees.
	common, err := exec.Command("git", "-C", target, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "worktree-remove: cannot find the checkout owning %s: %v\n", target, err)
		os.Exit(1)
	}
	repo := strings.TrimSpace(string(common))
	for attempt := range worktreeRemoveAttempts {
		if attempt > 0 {
			time.Sleep(worktreeRemoveDelay)
		}
		cmd := exec.Command("git", "-C", repo, "worktree", "remove", target)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if cmd.Run() == nil {
			return
		}
	}
	fmt.Fprintf(os.Stderr, "worktree-remove: still failing after %d attempts — close processes holding %s and retry\n", worktreeRemoveAttempts, target)
	os.Exit(1)
}
