package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSecretsSeedCreatesOwnerOnlyEnv(t *testing.T) {
	repo := t.TempDir()
	if err := secretsSeed([]string{"--repo-root", repo, "--", "QA_URL", "QA_TOKEN"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, ".babysit", ".env")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# QA_URL=") || !strings.Contains(string(body), "# QA_TOKEN=") {
		t.Fatalf("seeded file missing var placeholders:\n%s", body)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("secrets file must be owner-only, got %o", perm)
		}
	}
	gi, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil || !strings.Contains(string(gi), ".babysit/.env") {
		t.Fatalf("gitignore not seeded: %q err=%v", gi, err)
	}
}
