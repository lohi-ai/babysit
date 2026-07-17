//go:build unix

package telemetry

import (
	"os"
	"syscall"
)

func isExecutable(path string) bool {
	fi, err := os.Stat(path) // follows symlinks: a broken link fails, as exec would
	if err != nil || fi.IsDir() {
		return false
	}
	// Ask the kernel the question the bash's shell-out asks — "can *we* exec
	// it" — not "does anyone hold an exec bit". `Perm()&0o111 != 0` answers the
	// latter and diverges: a 0645 file owned by the invoker is EACCES to exec
	// (no owner-x) yet matches on other-x, so the bit test reads the config and
	// honors `telemetry: off` where the bash's exec fails and TIER falls back
	// to "local", writing the event anyway.
	//
	// Access(2) tests the real uid while exec tests the effective uid; equal
	// here, since nothing runs this setuid.
	const xOK = 0x01
	return syscall.Access(path, xOK) == nil
}
