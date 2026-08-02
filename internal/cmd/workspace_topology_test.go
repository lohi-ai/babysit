package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/workspace"
)

// repoWith writes a checkout with the given .babysit/config.yaml (skipped when
// empty) and .babysit/.env (skipped when empty).
func repoWith(t *testing.T, cfg, env string) string {
	t.Helper()
	top := t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, ".babysit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		if err := os.WriteFile(filepath.Join(top, ".babysit", "config.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if env != "" {
		if err := os.WriteFile(filepath.Join(top, ".babysit", ".env"), []byte(env), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return top
}

func TestRelatedRepoPathUnregisteredFallsBackToEnv(t *testing.T) {
	// AC3: a repo with no config.yaml resolves siblings exactly as it does
	// today — through .babysit/.env, with no workspace involved.
	workspace.TestHome(t)
	top := repoWith(t, "", "RELATED_BACKEND_REPO=/tmp/api\n")
	got, ok, err := relatedRepoPathVia(workspace.NewResolver(top, ""), "be", top)
	if err != nil || !ok || got != "/tmp/api" {
		t.Fatalf("want /tmp/api, got %q ok=%v err=%v", got, ok, err)
	}
}

func TestRelatedRepoPathWorkspaceWins(t *testing.T) {
	workspace.TestHome(t)
	top := repoWith(t, "workspace: acme\n", "")
	if err := workspace.AddRepo("acme", workspace.Repo{GitURL: "web.git", Path: top}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.AddRepo("acme", workspace.Repo{GitURL: "api.git", Path: "/tmp/api", Role: "be"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := relatedRepoPathVia(workspace.NewResolver(top, ""), "be", top)
	if err != nil || !ok || got != "/tmp/api" {
		t.Fatalf("workspace should answer: got %q ok=%v err=%v", got, ok, err)
	}
}

func TestRelatedRepoPathConflictBlocks(t *testing.T) {
	// The failure this ticket exists to prevent: two sources naming different
	// directories for the same role. Neither wins — the caller stops.
	workspace.TestHome(t)
	top := repoWith(t, "workspace: acme\n", "RELATED_BACKEND_REPO=/tmp/api-old\n")
	if err := workspace.AddRepo("acme", workspace.Repo{GitURL: "web.git", Path: top}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.AddRepo("acme", workspace.Repo{GitURL: "api.git", Path: "/tmp/api-new", Role: "be"}); err != nil {
		t.Fatal(err)
	}
	_, _, err := relatedRepoPathVia(workspace.NewResolver(top, ""), "be", top)
	if err == nil {
		t.Fatal("disagreeing sources must BLOCK, not pick one")
	}
	for _, want := range []string{"/tmp/api-new", "/tmp/api-old", "RELATED_BACKEND_REPO"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("conflict message must name %q, got: %v", want, err)
		}
	}
}

func TestRelatedRepoPathAgreementIsNotAConflict(t *testing.T) {
	// A repo that registered its siblings and kept the old .env entries is a
	// normal migration state, not an error.
	workspace.TestHome(t)
	top := repoWith(t, "workspace: acme\n", "RELATED_BACKEND_REPO=/tmp/api\n")
	if err := workspace.AddRepo("acme", workspace.Repo{GitURL: "web.git", Path: top}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.AddRepo("acme", workspace.Repo{GitURL: "api.git", Path: "/tmp/api", Role: "be"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := relatedRepoPathVia(workspace.NewResolver(top, ""), "be", top)
	if err != nil || !ok || got != "/tmp/api" {
		t.Fatalf("agreement should resolve cleanly: got %q ok=%v err=%v", got, ok, err)
	}
}

func TestSiblingSourceNameStaysAccurate(t *testing.T) {
	// The serve message names whichever authority was actually consulted.
	workspace.TestHome(t)
	unreg := repoWith(t, "", "")
	if got := siblingSourceName(workspace.NewResolver(unreg, "")); !strings.Contains(got, "no .babysit/config.yaml") {
		t.Fatalf("unregistered repo should say so, got %q", got)
	}
	top := repoWith(t, "workspace: acme\n", "")
	if err := workspace.AddRepo("acme", workspace.Repo{GitURL: "web.git", Path: top}); err != nil {
		t.Fatal(err)
	}
	if got := siblingSourceName(workspace.NewResolver(top, "")); got != "workspace acme" {
		t.Fatalf("registered repo should name its workspace, got %q", got)
	}
}
