package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/spf13/cobra"
)

// newUpgradeCmd ports bin/bbs-upgrade as `bbs upgrade`, matching its output
// bytes and exit codes exactly.
//
// Flag parsing is disabled: the bash never parses flags. `--snooze` is special
// only as `$1`, and anything else — `--help`, `-x`, junk — falls through to a
// real upgrade. Letting cobra near the arguments would turn `--help` into a
// usage dump and unknown flags into errors, neither of which the bash does.
func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "upgrade",
		Short:              "pull latest babysit, re-run setup, write just-upgraded marker",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runUpgrade(args)
		},
	}
}

func runUpgrade(args []string) error {
	stateDir := config.Dir()
	babysit := babysitDir()
	markerFile := filepath.Join(stateDir, "just-upgraded-from")
	snoozeFile := filepath.Join(stateDir, "update-snoozed")
	cacheFile := filepath.Join(stateDir, "last-update-check")
	versionFile := filepath.Join(babysit, "VERSION")

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return errSilent
	}

	if len(args) > 0 && args[0] == "--snooze" {
		return runSnooze(args, cacheFile, snoozeFile)
	}

	oldVersion := readVersion(versionFile)

	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, retarget("git is required for bbs-upgrade"))
		return errSilent
	}

	if err := os.Chdir(babysit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return errSilent
	}
	if exec.Command("git", "rev-parse", "--git-dir").Run() != nil {
		fmt.Fprintln(os.Stderr, "babysit was not installed via git clone — nothing here to pull.")
		for _, ln := range upgradeHints(babysit) {
			fmt.Fprintln(os.Stderr, ln)
		}
		return errSilent
	}

	fmt.Println("→ Pulling latest babysit...")
	pullArgs := []string{"pull", "--ff-only"}
	failMsg := "git pull failed — resolve conflicts then re-run bbs-upgrade"
	if !gitOK("rev-parse", "--abbrev-ref", "@{upstream}") {
		// No tracking branch. A bare `git pull` fails outright here, and git's
		// own reason ("no tracking information for the current branch") is
		// nothing like the conflict the message below sends the operator
		// looking for. Name the remote explicitly instead of failing.
		branch := gitOut("rev-parse", "--abbrev-ref", "HEAD")
		if branch == "" || branch == "HEAD" {
			fmt.Fprintln(os.Stderr, "cannot upgrade from a detached HEAD — check out a branch first")
			return errSilent
		}
		fmt.Printf("  (no upstream for '%s' — pulling origin/%s explicitly)\n", branch, branch)
		pullArgs = append(pullArgs, "origin", branch)
		failMsg = "git pull origin " + branch + " failed — see git's output above, then re-run bbs-upgrade"
	}
	pull := exec.Command("git", pullArgs...)
	// Inherited, not captured: git's progress and conflict output is the
	// operator's only view of why a pull failed.
	pull.Stdout, pull.Stderr = os.Stdout, os.Stderr
	if pull.Run() != nil {
		fmt.Fprintln(os.Stderr, retarget(failMsg))
		return errSilent
	}

	fmt.Println("→ Relinking skills...")
	setup := exec.Command(filepath.Join(babysit, "bin", "setup-skills"))
	setup.Stderr = os.Stderr // stdout is dropped, as in `setup-skills >/dev/null`
	if err := setup.Run(); err != nil {
		// `set -e` propagates setup-skills' own status, so exit with it rather
		// than flattening every failure to 1.
		os.Exit(setupExitCode(err))
	}

	newVersion := readVersion(versionFile)
	if oldVersion != "" && oldVersion != newVersion {
		if err := os.WriteFile(markerFile, []byte(oldVersion+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return errSilent
		}
	}
	// `rm -f` only forgives a missing file; anything else (a directory, an
	// unwritable parent) is an error, and `set -e` aborts on it.
	if err := removeF(cacheFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return errSilent
	}
	if err := removeF(snoozeFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return errSilent
	}

	suffix := ""
	if oldVersion != "" {
		suffix = fmt.Sprintf(": %s → %s", oldVersion, newVersion)
	}
	fmt.Printf("✓ babysit upgraded%s\n", suffix)
	return nil
}

// upgradeHints names the commands that actually upgrade an install with no
// checkout behind it — a brew binary, or a Claude Code plugin installed from
// the GitHub marketplace.
//
// `bbs upgrade` deliberately does not do this work itself. A brew binary has no
// remote to pull, and a plugin is refreshed by `claude`, not by us: shelling out
// would couple babysit to a CLI it doesn't own, and the `claude` on PATH is not
// necessarily the one running this session. So print the exact commands, name
// the restart, and stop — a wrong guess about the install shape is worse than
// two commands the operator can read.
func upgradeHints(babysit string) []string {
	out := []string{"  CLI:    brew upgrade bbs"}
	if !strings.Contains(babysit, "/Cellar/") {
		out[0] += "   (or re-download from https://github.com/lohi-ai/babysit/releases)"
	}
	home, _ := os.UserHomeDir()
	switch {
	case home == "":
	case isDir(filepath.Join(home, ".claude", "plugins", "cache", "babysit")):
		out = append(out, "  Skills: claude plugin marketplace update babysit && claude plugin update bbs@babysit")
	case isDir(filepath.Join(home, ".claude", "skills", "babysit")):
		out = append(out, "  Skills: ~/.claude/skills/babysit is a skills-dir install — re-sync it from a checkout")
	default:
		out = append(out, "  Skills: claude plugin marketplace update babysit && claude plugin update bbs@babysit")
	}
	return append(out, "  Then restart Claude Code — plugin changes only apply on restart.")
}

// runSnooze silences upgrade prompts for the pending version without upgrading.
func runSnooze(args []string, cacheFile, snoozeFile string) error {
	level := "1" // `LEVEL="${2:-1}"` — an empty $2 defaults too
	if len(args) > 1 && args[1] != "" {
		level = args[1]
	}
	switch level {
	case "1", "2", "3":
	default:
		fmt.Fprintln(os.Stderr, "snooze level must be 1, 2, or 3")
		return errSilent
	}

	remote := cacheRemote(cacheFile)
	if remote == "" {
		fmt.Fprintln(os.Stderr, "no pending upgrade to snooze")
		return errSilent
	}

	line := fmt.Sprintf("%s %s %d\n", remote, level, time.Now().Unix())
	if err := os.WriteFile(snoozeFile, []byte(line), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return errSilent
	}
	fmt.Printf("Snoozed upgrade to %s (level %s).\n", remote, level)
	return nil
}

// cacheRemote is `awk '/^UPGRADE_AVAILABLE/{print $3}' "$CACHE_FILE"` captured
// through `$(...)`: field 3 of every line starting with UPGRADE_AVAILABLE,
// newline-joined with trailing newlines stripped. A missing or non-regular
// file yields "" (the bash guards with `[ -f ]`).
func cacheRemote(path string) string {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // awk records: a trailing newline ends the last one
	}
	var out strings.Builder
	for _, ln := range lines {
		if !strings.HasPrefix(ln, "UPGRADE_AVAILABLE") {
			continue
		}
		// awk's default FS splits on runs of blanks — space and tab only, so a
		// CR stays part of the field.
		fields := strings.FieldsFunc(ln, func(r rune) bool { return r == ' ' || r == '\t' })
		if len(fields) > 2 {
			out.WriteString(fields[2])
		}
		out.WriteByte('\n') // awk prints an empty line when $3 is unset
	}
	return strings.TrimRight(out.String(), "\n")
}

// readVersion is `[ -f "$f" ] && cat "$f" 2>/dev/null | tr -d '[:space:]'`.
// tr deletes whitespace everywhere, not just at the ends — an unreadable or
// missing file reads as "".
func readVersion(path string) string {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r':
			return -1
		}
		return r
	}, string(b))
}

// removeF is `rm -f`: a missing target is success, every other failure is not.
//
// Unlink rather than os.Remove — os.Remove falls back to rmdir and would
// silently succeed on an empty directory, where `rm -f` (no -r) fails EISDIR.
func removeF(path string) error {
	if err := syscall.Unlink(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &fs.PathError{Op: "unlink", Path: path, Err: err}
	}
	return nil
}

// setupExitCode maps a failed setup-skills run to the status bash would exit
// with: its own code when it ran, 128+N when a signal killed it, else 127/126
// as the shell reports a command it could not execute.
func setupExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return 1
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	if errors.Is(err, fs.ErrPermission) {
		fmt.Fprintln(os.Stderr, err)
		return 126
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}
