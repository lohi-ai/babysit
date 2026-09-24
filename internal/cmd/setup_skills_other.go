//go:build !windows

package cmd

import "os/exec"

// setupSkillsCmd execs the checkout's setup-skills directly: on POSIX the
// shebang makes it executable.
func setupSkillsCmd(script string) *exec.Cmd {
	return exec.Command(script)
}
