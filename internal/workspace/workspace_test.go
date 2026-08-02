package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDirPanicsWithoutBabysitHome(t *testing.T) {
	// The guard that keeps a future test from writing into the human's real
	// ~/.babysit. Setting BABYSIT_STATE_DIR must NOT satisfy it: that variable
	// redirects internal/config, not this store.
	t.Setenv("BABYSIT_HOME", "")
	t.Setenv("BABYSIT_STATE_DIR", t.TempDir())
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Dir() did not panic with BABYSIT_HOME unset — the store is writable from a test that forgot to redirect it")
		}
		if !strings.Contains(r.(string), "BABYSIT_HOME") {
			t.Fatalf("panic should name the variable to set, got %v", r)
		}
	}()
	_ = Dir()
}

func TestCreateAndLoadRoundTrip(t *testing.T) {
	TestHome(t)
	if err := Create("acme"); err != nil {
		t.Fatal(err)
	}
	if !Exists("acme") {
		t.Fatal("workspace should exist after Create")
	}
	// Create is idempotent — it is also the implicit path inside AddRepo.
	if err := Create("acme"); err != nil {
		t.Fatalf("second Create should be a no-op, got %v", err)
	}
	w, err := Load("acme")
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "acme" || w.Version != 1 {
		t.Fatalf("got %+v", w)
	}
}

func TestAddRepoAppendsAndUpdatesByGitURL(t *testing.T) {
	TestHome(t)
	if err := AddRepo("acme", Repo{GitURL: "git@github.com:acme/web.git", Path: "/tmp/web", Role: "fe"}); err != nil {
		t.Fatal(err)
	}
	// Implicit create: no Create call preceded this.
	if err := AddRepo("acme", Repo{GitURL: "git@github.com:acme/api.git", Path: "/tmp/api", Role: "be"}); err != nil {
		t.Fatal(err)
	}
	w, _ := Load("acme")
	if len(w.Repos) != 2 {
		t.Fatalf("want 2 repos, got %d: %+v", len(w.Repos), w.Repos)
	}
	// Same git url updates in place rather than duplicating.
	if err := AddRepo("acme", Repo{GitURL: "git@github.com:acme/web.git", Path: "/tmp/web2", Role: "fe"}); err != nil {
		t.Fatal(err)
	}
	w, _ = Load("acme")
	if len(w.Repos) != 2 {
		t.Fatalf("re-adding a git url must not duplicate, got %d", len(w.Repos))
	}
	if w.Repos[0].Path != "/tmp/web2" {
		t.Fatalf("entry should be updated, got %q", w.Repos[0].Path)
	}
}

func TestAddRepoConcurrentDoesNotDropEntries(t *testing.T) {
	// The reason this store locks where foreman.Save does not: read-modify-write
	// on a shared list loses entries silently without one.
	TestHome(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = AddRepo("acme", Repo{GitURL: string(rune('a'+i)) + ".git", Path: "/tmp/" + string(rune('a'+i))})
		}(i)
	}
	wg.Wait()
	w, err := Load("acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Repos) != 8 {
		t.Fatalf("concurrent add-repo dropped entries: want 8, got %d", len(w.Repos))
	}
}

func TestValidNameRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"../escape", ".hidden", "a/b", "", "/abs"} {
		if err := ValidName(bad); err == nil {
			t.Fatalf("ValidName(%q) should reject", bad)
		}
	}
	if err := ValidName("acme-1.0_x"); err != nil {
		t.Fatalf("ValidName rejected a legal name: %v", err)
	}
}

func TestListSkipsUnparseableFiles(t *testing.T) {
	home := TestHome(t)
	if err := Create("good"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "workspaces", "bad.yaml"), []byte("{{{not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := List()
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("one bad file must not blank the list, got %+v", got)
	}
}
