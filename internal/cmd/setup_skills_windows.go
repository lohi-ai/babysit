//go:build windows

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
)

// setupSkillsCmd runs the checkout's setup-skills through bash: Windows
// CreateProcess cannot exec a shebang script, and Git for Windows does not
// add bash to PATH by default — so fall back to the bash.exe that ships next
// to the git.exe the upgrade already required.
func setupSkillsCmd(script string) *exec.Cmd {
	return exec.Command(findBash(), script)
}

func findBash() string {
	// Git's own bash first: a WSL bash.exe earlier on PATH would receive a
	// C:\ path it cannot resolve, and the upgrade already required git, so
	// this candidate is always resolvable when we get here.
	if git, err := exec.LookPath("git"); err == nil {
		// <Git>/cmd/git.exe → <Git>/bin/bash.exe
		candidate := filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	// Let exec fail with its own "not found" error rather than inventing one.
	return "bash"
}
