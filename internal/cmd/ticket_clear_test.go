package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearAllTicketsMovesEveryActiveTicketToTrash(t *testing.T) {
	state := t.TempDir()
	for _, path := range []string{
		"projects/one/tickets/bs-first/requirement.md",
		"projects/two/tickets/bs-second/requirement.md",
	} {
		full := filepath.Join(state, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("keep me\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(state, "config.yaml"), []byte("keep: config\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleared, err := clearAllTickets(state)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 2 {
		t.Fatalf("cleared = %d, want 2", cleared)
	}
	for _, path := range []string{
		"projects/one/tickets/bs-first",
		"projects/two/tickets/bs-second",
	} {
		if _, err := os.Stat(filepath.Join(state, path)); !os.IsNotExist(err) {
			t.Errorf("active ticket %s still exists: %v", path, err)
		}
	}
	for _, path := range []string{
		"trash/one/bs-first-*/requirement.md",
		"trash/two/bs-second-*/requirement.md",
	} {
		matches, err := filepath.Glob(filepath.Join(state, path))
		if err != nil || len(matches) != 1 {
			t.Errorf("trash record %s: matches=%v err=%v", path, matches, err)
		}
	}
	locks, err := filepath.Glob(filepath.Join(state, "trash", "one", "bs-first-*", ".index.lock"))
	if err != nil || len(locks) != 0 {
		t.Errorf("trash retained lock files: matches=%v err=%v", locks, err)
	}
	if _, err := os.Stat(filepath.Join(state, "config.yaml")); err != nil {
		t.Errorf("config was removed: %v", err)
	}
}

func TestClearAllTicketsWithoutStateIsANoop(t *testing.T) {
	cleared, err := clearAllTickets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 0 {
		t.Fatalf("cleared = %d, want 0", cleared)
	}
}
