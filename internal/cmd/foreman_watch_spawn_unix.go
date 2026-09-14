//go:build unix

package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// acquireWatchLock takes a single-instance flock for the watch loop. The
// unscoped watcher uses daemon.lock; a scoped watcher uses its own lock so a
// manual `watch <id>` cannot block monitoring of other foremen.
func acquireWatchLock(id string) (func(), error) {
	if err := os.MkdirAll(watchDir(), 0o755); err != nil {
		return nil, err
	}
	name := "daemon.lock"
	if id != "" {
		name = id + ".lock"
	}
	f, err := os.OpenFile(filepath.Join(watchDir(), name), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errWatchRunning
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}

// startWatcher launches `bbs foreman watch` detached: own session, output to
// watch.log, parent released. The child exits on its own when no foreman has
// an open terminal, so there is no stop path to wire. Under go test the
// executable is the test binary — spawning it recursively would run the suite
// as a watcher, so tests get no auto-start.
func startWatcher() error {
	if testing.Testing() {
		return nil
	}
	exe := selfBin()
	var out *os.File
	var err error
	if err := os.MkdirAll(watchDir(), 0o755); err == nil {
		out, _ = os.OpenFile(filepath.Join(watchDir(), "watch.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	if out == nil {
		if out, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0); err != nil {
			return err
		}
	}
	defer out.Close()
	cmd := exec.Command(exe, "foreman", "watch")
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
