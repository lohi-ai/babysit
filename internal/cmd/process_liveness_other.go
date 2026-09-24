//go:build !unix && !windows

package cmd

// No liveness primitive exists on this platform: report every process-based
// attempt as dead rather than guessing. The release targets are darwin/linux
// and Windows; anything else is a from-source build that gets a loud, safe
// answer.
func processAlive(pid int) bool { return false }

func processStartIdentity(pid int) (string, bool) { return "", false }
