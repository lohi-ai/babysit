//go:build !unix

package telemetry

import "os"

// isExecutable on non-unix platforms. syscall.Access is unix-only, and Windows
// has no exec-bit concept (executability is decided by file extension), so the
// faithful real-uid check in isexec_unix.go has no analog. This build exists
// only to satisfy the cross-compile regression guard in build.yml — the
// release ships darwin + linux only and these bins never run on Windows — so
// the check degrades to "the file exists and is not a directory".
func isExecutable(path string) bool {
	fi, err := os.Stat(path) // follows symlinks: a broken link fails, as exec would
	return err == nil && !fi.IsDir()
}
