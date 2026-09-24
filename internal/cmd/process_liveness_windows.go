//go:build windows

package cmd

import (
	"errors"
	"strconv"

	"golang.org/x/sys/windows"
)

// winStillActive is the exit code a running process reports via
// GetExitCodeProcess; x/sys does not name it.
const winStillActive = 259

// processAlive opens the process with the minimum query right and asks for
// its exit code. Signal(0) is a no-op on Windows, so the POSIX probe always
// read "alive" for dead PIDs and "dead" never — this is the native check.
//
// ERROR_INVALID_PARAMETER means no such process. ERROR_ACCESS_DENIED means
// the process exists but we may not query it — that is alive, not dead.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == winStillActive
}

// processStartIdentity returns the process creation time as the incarnation
// token — the Windows analogue of `ps -o lstart=`. A PID alone cannot
// distinguish a reused PID; the creation timestamp can. ("", true) means the
// process is alive but its start time is not queryable (ACCESS_DENIED on
// GetProcessTimes), which attemptLiveness reports as "unknown" rather than
// "reused".
func processStartIdentity(pid int) (string, bool) {
	if pid <= 0 || !processAlive(pid) {
		return "", false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Alive per the probe but unqueryable here — ACCESS_DENIED.
		return "", true
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return "", true
	}
	return strconv.FormatUint(uint64(creation.HighDateTime)<<32|uint64(creation.LowDateTime), 10), true
}
