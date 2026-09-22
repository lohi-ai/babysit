//go:build unix

package foreman

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResourceLockSurvivesKilledHolder(t *testing.T) {
	if dir := os.Getenv("BBS_RESOURCE_LOCK_CHILD"); dir != "" {
		broker := &ResourceBroker{Dir: dir}
		err := broker.withLock(func() error {
			if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0600); err != nil {
				return err
			}
			time.Sleep(time.Hour)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	// Old versions leave this directory behind after an interrupted command.
	if err := os.Mkdir(filepath.Join(dir, ".foreman-leases.lock"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestResourceLockSurvivesKilledHolder$")
	cmd.Env = append(os.Environ(), "BBS_RESOURCE_LOCK_CHILD="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never acquired resource lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	broker := &ResourceBroker{Dir: dir}
	if err := broker.withLock(func() error { return nil }); err != nil {
		t.Fatalf("killed holder blocked next command: %v", err)
	}
}
