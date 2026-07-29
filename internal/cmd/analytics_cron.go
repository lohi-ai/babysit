package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newAnalyticsCronCmd ports bin/bbs-analytics-cron as `bbs analytics-cron`,
// matching its stdout, the scheduler artifacts it writes, and its exit codes.
//
// DisableFlagParsing: the bash inspects only "$1" (--install / --uninstall /
// --dry-run) and passes everything else through untouched. Cobra's parser would
// reject the unknown flags, so it is switched off (same rationale as `bbs env`).
func newAnalyticsCronCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "analytics-cron",
		Short:              "weekly unattended /bbs:analytics-review dispatch",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runAnalyticsCron(args)
		},
	}
}

const analyticsLabel = "dev.babysit.analytics"

func analyticsEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// analyticsSelf mirrors the bash self_path() *in intent* — a stable path the
// scheduler can re-invoke — but deliberately does NOT resolve the final symlink
// the way the bash does. The bash entrypoint bin/bbs-analytics-cron was a real
// file, so its chain ended at itself; the Go entrypoint is a compat symlink to
// the multicall bin/bbs, and resolving it would point the scheduler at bare
// `bbs` (no subcommand), losing the analytics-cron dispatch. Preserving the
// invoked basename keeps the multicall's argv[0] routing intact. A bare name
// (no separator) is recovered via PATH, matching what the bash's BASH_SOURCE
// would have carried.
func analyticsSelf() string {
	argv0 := os.Args[0]
	if !strings.ContainsRune(argv0, filepath.Separator) {
		if p, err := exec.LookPath(argv0); err == nil {
			argv0 = p
		}
	}
	// Abs, not EvalSymlinks: `cd "$(dirname)" && pwd` is a logical walk that
	// leaves symlinked ancestors (e.g. macOS /var → /private/var) untouched.
	abs, err := filepath.Abs(argv0)
	if err != nil {
		return argv0
	}
	return abs
}

func isDarwin() bool {
	out, err := exec.Command("uname").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Darwin"
}

func plistPath() string {
	return filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", analyticsLabel+".plist")
}

func installSchedule() error {
	self := analyticsSelf()
	babysitHome := analyticsEnvOr("BABYSIT_HOME", filepath.Join(os.Getenv("HOME"), ".babysit"))
	if isDarwin() {
		plist := plistPath()
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return errSilent
		}
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array><string>%s</string></array>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Weekday</key><integer>1</integer>
    <key>Hour</key><integer>9</integer>
    <key>Minute</key><integer>0</integer>
  </dict>
  <key>StandardOutPath</key><string>%s/analytics/reviews/cron.out.log</string>
  <key>StandardErrorPath</key><string>%s/analytics/reviews/cron.err.log</string>
</dict>
</plist>
`, analyticsLabel, self, babysitHome, babysitHome)
		if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
			return errSilent
		}
		_ = exec.Command("launchctl", "unload", plist).Run() // || true
		if err := exec.Command("launchctl", "load", plist).Run(); err != nil {
			return errSilent
		}
		fmt.Printf("installed launchd agent %s (Mondays 09:00) → %s\n", analyticsLabel, plist)
		return nil
	}

	line := "0 9 * * 1 " + self
	if crontabContains(self) {
		fmt.Printf("cron entry for %s already present\n", self)
		return nil
	}
	existing := currentCrontab()
	next := existing
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	next += line + "\n"
	if err := writeCrontab(next); err != nil {
		return errSilent
	}
	fmt.Printf("installed cron entry (Mondays 09:00): %s\n", line)
	return nil
}

func uninstallSchedule() error {
	if isDarwin() {
		plist := plistPath()
		_ = exec.Command("launchctl", "unload", plist).Run() // || true
		_ = os.Remove(plist)                                 // rm -f: absent is fine
		fmt.Printf("removed launchd agent %s\n", analyticsLabel)
		return nil
	}

	self := analyticsSelf()
	var kept []string
	for _, l := range strings.Split(currentCrontab(), "\n") {
		if l == "" || strings.Contains(l, self) {
			continue
		}
		kept = append(kept, l)
	}
	out := ""
	if len(kept) > 0 {
		out = strings.Join(kept, "\n") + "\n"
	}
	if err := writeCrontab(out); err != nil {
		return errSilent
	}
	fmt.Printf("removed cron entry for %s\n", self)
	return nil
}

// currentCrontab mirrors `crontab -l 2>/dev/null` — empty string on any error
// (no crontab yet, command missing).
func currentCrontab() string {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func crontabContains(needle string) bool {
	return strings.Contains(currentCrontab(), needle)
}

// writeCrontab mirrors `… | crontab -`.
func writeCrontab(content string) error {
	c := exec.Command("crontab", "-")
	c.Stdin = strings.NewReader(content)
	return c.Run()
}

func runAnalyticsCron(args []string) error {
	arg1 := ""
	if len(args) > 0 {
		arg1 = args[0]
	}

	switch arg1 {
	case "--install":
		return installSchedule()
	case "--uninstall":
		return uninstallSchedule()
	}

	babysitHome := analyticsEnvOr("BABYSIT_HOME", filepath.Join(os.Getenv("HOME"), ".babysit"))
	reviewDir := filepath.Join(babysitHome, "analytics", "reviews")
	logPath := filepath.Join(babysitHome, "analytics", "skill-usage.jsonl")
	claudeBin := analyticsEnvOr("CLAUDE_BIN", "claude")
	allowedTools := analyticsEnvOr("BBS_ANALYTICS_TOOLS", "Bash Read Glob Grep Skill")

	if _, err := exec.LookPath(claudeBin); err != nil {
		fmt.Fprintln(os.Stderr, retarget("bbs-analytics-cron: claude CLI not found in PATH"))
		return errSilent
	}

	_ = os.MkdirAll(reviewDir, 0o755)
	stamp := time.Now().Format("2006-01-02")
	out := filepath.Join(reviewDir, stamp+".md")

	// AGENT_ROLE unset → report mode, no orchestrator relay (would hang headless).
	os.Unsetenv("AGENT_ROLE")
	os.Unsetenv("GT_ROLE")

	if arg1 == "--dry-run" {
		fmt.Printf("would write: %s\n", out)
		fmt.Printf("would run:   %s -p '/bbs:analytics-review' --allowedTools %s\n", claudeBin, allowedTools)
		return nil
	}

	result := "success"
	outFile, err := os.Create(out)
	if err != nil {
		result = "error"
	} else {
		runArgs := append([]string{"-p", "/bbs:analytics-review", "--allowedTools"}, strings.Fields(allowedTools)...)
		c := exec.Command(claudeBin, runArgs...)
		c.Stdout = outFile
		errFile, ferr := os.Create(out + ".err")
		if ferr == nil {
			c.Stderr = errFile
		}
		runErr := c.Run()
		outFile.Close()
		if errFile != nil {
			errFile.Close()
		}
		os.Remove(out + ".err")
		if runErr != nil {
			result = "error"
		}
	}

	ts := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "{\"ts\":\"%s\",\"event\":\"analytics-cron\",\"outcome\":\"%s\",\"report\":\"%s\"}\n", ts, result, out)
		f.Close()
	}

	fmt.Printf("analytics review (%s) → %s\n", result, out)
	if result != "success" {
		return errSilent
	}
	return nil
}
