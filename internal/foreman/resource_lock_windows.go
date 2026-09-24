//go:build windows

package foreman

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Windows analogue of the unix flock: LockFileEx on byte 0 of the lock file.
// The handle owns the lock, so closing it — including on process death —
// releases it, matching the "kernel releases even on SIGKILL" contract. Never
// unlink the file: waiters must all lock the same path.
func lockResources(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	// The OVERLAPPED carries the locked offset even for synchronous handles.
	ol := &windows.Overlapped{}
	for tries := 0; ; tries++ {
		err = windows.LockFileEx(windows.Handle(f.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, ol)
		if err == nil {
			return func() {
				_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) && !errors.Is(err, windows.ERROR_IO_PENDING) {
			f.Close()
			return nil, err
		}
		if tries >= resourceLockRetries {
			f.Close()
			return nil, fmt.Errorf("resource broker busy after 5s")
		}
		time.Sleep(resourceLockDelay)
	}
}
