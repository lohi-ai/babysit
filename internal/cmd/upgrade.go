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
		Use:                "upgrade [check|--snooze]",
		Short:              "pull latest babysit, re-run setup, write just-upgraded marker",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// `upgrade check` is the version probe — the surface that used to be
			// its own top-level `update-check`. Checking whether to upgrade and
			// upgrading are one concern; they now share one command.
			if len(args) > 0 && args[0] == "check" {
				return runUpdateCheckCmd(args[1:])
			}
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
	if !isBabysitCheckout(babysit) {
		return upgradeExternal(babysit)
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

	// The pull + relink above refreshed the checkout and the skills-dir symlinks
	// that point at it. It did NOT touch ~/.claude/plugins/cache/babysit — a
	// marketplace plugin is a copy, and an installed marketplace plugin wins the
	// name collision over the skills-dir shape. So a machine with both (checkout
	// on PATH, plugin installed from GitHub) runs a fresh CLI against whatever
	// skills the last `claude plugin update` left behind, and the old code path
	// did not even mention it. Silence there is what strands skills versions
	// behind the binary, so drive that half here too — and name it when it is
	// present but not driveable from this process.
	pluginDone := false
	switch driveable, ok := upgradePlugin(); {
	case ok:
		pluginDone = true
	case driveable:
		fmt.Fprintln(os.Stderr, "  The CLI upgraded, but the plugin half did not — re-run the command above.")
	default:
		if pluginCached() {
			fmt.Fprintln(os.Stderr, "A marketplace plugin is installed but `claude` is not on PATH — the skills half is still stale.")
			fmt.Fprintln(os.Stderr, hintSkills())
		}
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
	if pluginDone {
		fmt.Println("  Restart Claude Code — plugin changes only apply on restart.")
	}
	return nil
}

// isBabysitCheckout reports whether babysit is the ROOT of a git checkout, not
// merely a directory that happens to sit inside one.
//
// `git rev-parse --git-dir` walks up the tree, so it answers yes for a brew
// install: /opt/homebrew is itself a git clone, and the Cellar lives inside it.
// Testing only that made `bbs upgrade` run `git pull` against Homebrew's
// repository and then fail hunting for a setup-skills the formula never ships.
// Requiring the enclosing repo's toplevel to BE this directory is what
// distinguishes babysit's own clone from whatever repo it was dropped into.
//
// Deliberately not also requiring a VERSION file: a checkout without one is a
// supported upgrade shape (the version suffix is simply omitted).
func isBabysitCheckout(babysit string) bool {
	top := gitOut("rev-parse", "--show-toplevel")
	if top == "" {
		return false
	}
	// Compare resolved paths — /tmp vs /private/tmp on macOS, and any symlinked
	// install dir, would otherwise read as a mismatch on a healthy checkout.
	if r, err := filepath.EvalSymlinks(top); err == nil {
		top = r
	}
	if r, err := filepath.EvalSymlinks(babysit); err == nil {
		babysit = r
	}
	return top == babysit
}

// upgradeExternal upgrades an install with no checkout behind it: the Homebrew
// CLI and the Claude Code plugin. babysit ships as two halves that separate
// tools own, so `bbs upgrade` drives each one rather than making the operator
// remember both.
//
// Each half is only driven when this machine actually has it — a brew CLI next
// to a skills-dir plugin upgrades the CLI and prints the other half's
// instructions, instead of guessing. Whatever we can't drive is named, so the
// output is always a complete account of what is left to do.
//
// One caveat kept from when this only printed: the `claude` on PATH is not
// necessarily the binary running this session. Plugin changes need a restart
// regardless, which is why the restart line prints on every path.
func upgradeExternal(babysit string) error {
	var done, manual []string
	failed := false

	// runUpgrade chdir'd into the install dir for the checkout path. Here that
	// directory is the very thing being replaced — brew unlinks the old Cellar
	// keg — and a process whose cwd has been deleted breaks the child commands
	// it spawns. Step out first.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		_ = os.Chdir(home)
	}

	if strings.Contains(babysit, "/Cellar/") && hasCmd("brew") {
		fmt.Println("→ Upgrading the bbs CLI (brew)...")
		if err := runVisible("brew", "upgrade", "bbs"); err != nil {
			fmt.Fprintln(os.Stderr, "brew upgrade bbs failed — see the output above")
			failed = true
		} else {
			done = append(done, "CLI")
		}
	} else {
		manual = append(manual, hintCLI(babysit))
	}

	switch driveable, ok := upgradePlugin(); {
	case ok:
		done = append(done, "skills")
	case driveable:
		failed = true
	default:
		manual = append(manual, hintSkills())
	}

	if len(done) == 0 && !failed {
		// Neither half is driveable from here — this is the pre-existing
		// print-and-exit-nonzero path, header line first.
		fmt.Fprintln(os.Stderr, "babysit was not installed via git clone — nothing here to pull.")
	}
	for _, ln := range manual {
		fmt.Fprintln(os.Stderr, ln)
	}
	if failed || len(done) == 0 {
		fmt.Fprintln(os.Stderr, "  Then restart Claude Code — plugin changes only apply on restart.")
		return errSilent
	}
	fmt.Printf("✓ babysit upgraded (%s)\n", strings.Join(done, " + "))
	fmt.Println("  Restart Claude Code — plugin changes only apply on restart.")
	return nil
}

// hintCLI names the command that upgrades the binary when we can't run it here.
func hintCLI(babysit string) string {
	out := "  CLI:    brew upgrade bbs"
	if !strings.Contains(babysit, "/Cellar/") {
		out += "   (or re-download from https://github.com/lohi-ai/babysit/releases)"
	}
	return out
}

// upgradePlugin drives the Claude Code marketplace plugin half — the *copy*
// under ~/.claude/plugins/cache that `claude plugin install` made.
//
// driveable reports whether this machine has that half and the `claude` command
// to update it; ok reports whether the update actually succeeded. Callers need
// both, because "no marketplace plugin here" and "the update failed" are
// different outcomes: the first is named as manual work, the second is a
// failure.
func upgradePlugin() (driveable, ok bool) {
	if !pluginCached() || !hasCmd("claude") {
		return false, false
	}
	fmt.Println("→ Updating the babysit plugin (claude)...")
	err := runVisible("claude", "plugin", "marketplace", "update", "babysit")
	if err == nil {
		err = runVisible("claude", "plugin", "update", "bbs@babysit")
	}
	if err != nil {
		// "see the output above" is not enough to act on: runVisible inherits the
		// child's output but never echoes the command, so the operator has no way
		// to retry by hand. Name both spellings — the shell one for a terminal,
		// the slash one for the session they are probably sitting in.
		fmt.Fprintln(os.Stderr, "claude plugin update failed — see the output above, then run:")
		fmt.Fprintln(os.Stderr, "    "+pluginCmds)
		fmt.Fprintln(os.Stderr, "    (inside Claude Code: /plugin marketplace update babysit)")
		return true, false
	}
	return true, true
}

// pluginCmds is the pair upgradePlugin runs, named once so a failure can quote
// exactly what to re-run and hintSkills can quote the same thing.
const pluginCmds = "claude plugin marketplace update babysit && claude plugin update bbs@babysit"

// hintSkills names the command that refreshes the skill pack for the plugin
// shape this machine has.
func hintSkills() string {
	const marketplace = "  Skills: " + pluginCmds
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return marketplace
	}
	dir := filepath.Join(home, ".claude", "skills", "babysit")
	if !isDir(dir) {
		return marketplace
	}
	// A skills-dir plugin is loaded in place, so it is often a symlink to the
	// operator's own checkout. That shape upgrades itself: `bbs upgrade` run
	// from the target pulls and relinks, and the plugin sees it immediately.
	// Telling them to "re-sync" would send them looking for a copy step that
	// does not exist.
	if target, err := filepath.EvalSymlinks(dir); err == nil && target != dir {
		return "  Skills: ~/.claude/skills/babysit links to " + target + " — run `bbs upgrade` from there"
	}
	return "  Skills: ~/.claude/skills/babysit is a skills-dir install — re-sync it from a checkout"
}

func pluginCached() bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	return isDir(filepath.Join(home, ".claude", "plugins", "cache", "babysit"))
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// runVisible runs a command with the operator watching: its output is the only
// explanation of a failure, so it is inherited rather than captured.
func runVisible(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
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
	lines := strings.Split(readRegular(path), "\n")
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
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r':
			return -1
		}
		return r
	}, readRegular(path))
}

// readRegular is the `[ -f "$f" ] && cat "$f" 2>/dev/null` both readers above
// open with: the contents of path, or "" when it is missing, unreadable, or not
// a regular file. The regular-file test is what keeps a directory from reading
// as an error rather than as absence.
func readRegular(path string) string {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
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
