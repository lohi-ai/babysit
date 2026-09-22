//go:build unix

package foreman

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// The kernel releases this lock even on SIGKILL. Never unlink it: waiters
// must all lock the same inode. The old mkdir lock is deliberately unused.
func lockResources(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for tries := 0; ; tries++ {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
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
