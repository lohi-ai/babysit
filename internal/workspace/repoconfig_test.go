package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRepoConfig writes a raw config.yaml, bypassing SaveRepoConfig so tests
// can exercise hand-edited and malformed files.
func writeRepoConfig(t *testing.T, body string) string {
	t.Helper()
	top := t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, ".babysit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RepoConfigPath(top), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return top
}

func TestLoadRepoConfigAbsentIsNotAnError(t *testing.T) {
	// AC3: every repo that exists today has no config.yaml and must behave
	// exactly as it does now — no error, no warning, no new failure mode.
	cfg, present, err := LoadRepoConfig(t.TempDir())
	if err != nil {
		t.Fatalf("missing config.yaml must not error, got %v", err)
	}
	if present {
		t.Fatal("present should be false")
	}
	if cfg.Workspace != "" {
		t.Fatalf("zero config expected, got %+v", cfg)
	}
}

func TestHarnessVersionNullIsValid(t *testing.T) {
	// AC2: null is the correct value for every repo configured before
	// setup-project learned to write it.
	for _, body := range []string{
		"workspace: acme\nharness_version: null\n",
		"workspace: acme\n", // key absent reads the same way
	} {
		cfg, present, err := LoadRepoConfig(writeRepoConfig(t, body))
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if !present {
			t.Fatalf("%q: should be present", body)
		}
		if cfg.HarnessVersion != nil {
			t.Fatalf("%q: want nil harness_version, got %q", body, *cfg.HarnessVersion)
		}
		if cfg.Stale("1.57.1") {
			t.Fatalf("%q: null must never read as stale", body)
		}
	}
}

func TestStaleDetectsOlderHarness(t *testing.T) {
	// AC5: a repo configured by an older harness is detectable.
	old := "1.50.0"
	cfg := RepoConfig{Workspace: "acme", HarnessVersion: &old}
	if !cfg.Stale("1.57.1") {
		t.Fatal("older harness_version should read stale")
	}
	cur := "1.57.1"
	cfg.HarnessVersion = &cur
	if cfg.Stale("1.57.1") {
		t.Fatal("current harness_version should not read stale")
	}
}

func TestValidateRejectsBadFields(t *testing.T) {
	if err := (RepoConfig{}).Validate(); err == nil {
		t.Fatal("workspace is required")
	}
	if err := (RepoConfig{Workspace: "../escape"}).Validate(); err == nil {
		t.Fatal("workspace name must be path-safe")
	}
	if err := (RepoConfig{Workspace: "acme", RepoType: "hybrid"}).Validate(); err == nil {
		t.Fatal("repo_type enum must be enforced")
	}
	for _, ok := range []string{"", TypeMonorepo, TypePolyrepo} {
		if err := (RepoConfig{Workspace: "acme", RepoType: ok}).Validate(); err != nil {
			t.Fatalf("repo_type %q should be legal: %v", ok, err)
		}
	}
}

func TestMalformedPresentFileErrors(t *testing.T) {
	_, present, err := LoadRepoConfig(writeRepoConfig(t, "workspace: acme\nrepo_type: hybrid\n"))
	if !present || err == nil {
		t.Fatal("a present but invalid file must error rather than read as absent")
	}
}

func TestSaveRepoConfigRoundTrip(t *testing.T) {
	top := t.TempDir()
	v := "1.57.1"
	in := RepoConfig{Workspace: "acme", HarnessVersion: &v, Name: "web", RepoType: TypePolyrepo}
	if err := SaveRepoConfig(top, in); err != nil {
		t.Fatal(err)
	}
	out, present, err := LoadRepoConfig(top)
	if err != nil || !present {
		t.Fatalf("present=%v err=%v", present, err)
	}
	if out.Workspace != "acme" || out.Name != "web" || out.RepoType != TypePolyrepo {
		t.Fatalf("got %+v", out)
	}
	if out.HarnessVersion == nil || *out.HarnessVersion != "1.57.1" {
		t.Fatalf("harness_version lost in round trip: %+v", out.HarnessVersion)
	}
}

func TestResolverMembershipConflictBlocks(t *testing.T) {
	TestHome(t)
	top := writeRepoConfig(t, "workspace: acme\n")

	// Workspace not registered at all.
	if r := NewResolver(top, ""); r.Err() == nil {
		t.Fatal("repo naming an unregistered workspace is a conflict")
	}

	// Registered but does not list this repo.
	if err := AddRepo("acme", Repo{GitURL: "other.git", Path: "/tmp/other"}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(top, "")
	if r.Err() == nil {
		t.Fatal("repo says acme, acme omits repo — must BLOCK, not guess")
	}
	if r.Registered() {
		t.Fatal("a conflicted resolver must not read as registered")
	}

	// Listed by path resolves the conflict.
	if err := AddRepo("acme", Repo{GitURL: "web.git", Path: top, Role: "fe"}); err != nil {
		t.Fatal(err)
	}
	r = NewResolver(top, "")
	if r.Err() != nil {
		t.Fatalf("membership by path should resolve: %v", r.Err())
	}
	if r.Name() != "acme" {
		t.Fatalf("got workspace %q", r.Name())
	}
}

func TestResolverMembershipByGitURL(t *testing.T) {
	TestHome(t)
	top := writeRepoConfig(t, "workspace: acme\n")
	// Entry carries no local path — a repo the workspace knows but this machine
	// has not cloned to that location. Matching falls back to the git url.
	if err := AddRepo("acme", Repo{GitURL: "web.git"}); err != nil {
		t.Fatal(err)
	}
	if r := NewResolver(top, "web.git"); r.Err() != nil {
		t.Fatalf("git url should establish membership: %v", r.Err())
	}
}

func TestResolverRolePathAndFanOut(t *testing.T) {
	TestHome(t)
	top := writeRepoConfig(t, "workspace: acme\nrepo_type: polyrepo\n")
	if err := AddRepo("acme", Repo{GitURL: "web.git", Path: top, Role: "fe"}); err != nil {
		t.Fatal(err)
	}
	if err := AddRepo("acme", Repo{GitURL: "api.git", Path: "/tmp/api", Role: "be"}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(top, "")
	if p, ok := r.RolePath("be"); !ok || p != "/tmp/api" {
		t.Fatalf("role be should resolve to /tmp/api, got %q ok=%v", p, ok)
	}
	if _, ok := r.RolePath("shared"); ok {
		t.Fatal("unlisted role must not resolve")
	}
	if !r.FanOut() {
		t.Fatal("polyrepo must keep fan-out on")
	}
}

func TestMonorepoDisablesFanOut(t *testing.T) {
	// AC6: repo_type changes a concrete behavior.
	TestHome(t)
	top := writeRepoConfig(t, "workspace: acme\nrepo_type: monorepo\n")
	if err := AddRepo("acme", Repo{GitURL: "mono.git", Path: top}); err != nil {
		t.Fatal(err)
	}
	if NewResolver(top, "").FanOut() {
		t.Fatal("monorepo must skip sibling fan-out")
	}
}

func TestUnconfiguredRepoResolverIsInert(t *testing.T) {
	// The today-repo path: no config.yaml, so no error, no membership, and
	// fan-out behaves exactly as before.
	TestHome(t)
	r := NewResolver(t.TempDir(), "")
	if r.Err() != nil || r.Registered() || r.Name() != "" {
		t.Fatalf("unconfigured repo must be inert, got err=%v registered=%v name=%q", r.Err(), r.Registered(), r.Name())
	}
	if !r.FanOut() {
		t.Fatal("unconfigured repo must keep today's fan-out behavior")
	}
	if _, ok := r.RolePath("be"); ok {
		t.Fatal("unconfigured repo resolves no roles")
	}
}
