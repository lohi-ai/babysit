package cmd

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/reallongnguyen/babysit/internal/slug"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// manifestOnlyFixture builds a repo on a non-ticket branch whose only claim
// to a ticket is a manifest.yaml worktree entry — the isolated-worktree
// resume shape the ladder exists for. (Trunk tickets share the checkout and
// record worktree "." — intentionally unresolvable by cwd; they need
// BABYSIT_TICKET per command.)
func manifestOnlyFixture(t *testing.T, ticketID string) (repo, projectHome string) {
	t.Helper()
	repo = initSnapshotRepo(t) // on main, no ticket in the branch name
	projectHome = filepath.Join(t.TempDir(), "project")
	th := filepath.Join(projectHome, "tickets", ticketID)
	mustMkdirAll(t, th)
	if err := ticket.WriteManifest(filepath.Join(th, "manifest.yaml"), ticketID, "t",
		[]ticket.Repo{{Name: "project", Worktree: repo}}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	t.Setenv("HOME", t.TempDir()) // isolate the slug cache
	t.Setenv("BABYSIT_TICKET", "")
	t.Setenv("BBS_TICKET", "")
	t.Setenv("BABYSIT_PROJECT_HOME", projectHome)
	return repo, projectHome
}

func TestResolveLadderManifestOnlyCheckout(t *testing.T) {
	manifestOnlyFixture(t, "ap-77")
	env, err := ticket.ResolveLadder()
	if err != nil {
		t.Fatal(err)
	}
	if env.Ticket != "ap-77" {
		t.Fatalf("manifest cwd match did not resolve ticket: %q", env.Ticket)
	}
}

// ambiguousFixture claims one cwd for two tickets — the shape where inferred
// identity must abort but explicit-ticket commands must still work.
func ambiguousFixture(t *testing.T) (repo, projectHome string) {
	t.Helper()
	repo = initSnapshotRepo(t)
	projectHome = filepath.Join(t.TempDir(), "project")
	for _, tid := range []string{"ap-11", "ap-22"} {
		th := filepath.Join(projectHome, "tickets", tid)
		mustMkdirAll(t, th)
		if err := ticket.WriteManifest(filepath.Join(th, "manifest.yaml"), tid, "t",
			[]ticket.Repo{{Name: "project", Worktree: repo}}); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(repo)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BABYSIT_TICKET", "")
	t.Setenv("BBS_TICKET", "")
	t.Setenv("BABYSIT_PROJECT_HOME", projectHome)
	return repo, projectHome
}

func TestResolveLadderAmbiguousCwdAborts(t *testing.T) {
	ambiguousFixture(t)
	if _, err := ticket.ResolveLadder(); err == nil {
		t.Fatal("ambiguous cwd resolved silently")
	} else {
		var amb *ticket.AmbiguousTicketError
		if !errors.As(err, &amb) {
			t.Fatalf("expected AmbiguousTicketError, got %v", err)
		}
	}
}

func TestResolveProjectAmbiguousCwdSucceeds(t *testing.T) {
	ambiguousFixture(t)
	env, err := ticket.ResolveProject()
	if err != nil {
		t.Fatalf("explicit-ticket path blocked by unrelated cwd ambiguity: %v", err)
	}
	if env.ProjectHome == "" {
		t.Fatal("project home lost")
	}
}

// Bare trunk resume: several tickets share main (manifest "." rows are
// skipped by the ladder), so the active pair in current.txt is the only
// durable discriminator. The ticket must exist on disk with a checkpoint
// naming it.
func TestCurrentTicketColdResumeOnTrunk(t *testing.T) {
	repo, projectHome := ambiguousFixture(t) // two manifests claim this cwd
	th := filepath.Join(projectHome, "tickets", "ap-55")
	mustMkdirAll(t, th)
	mustWrite(t, filepath.Join(th, "checkpoint.json"), `{"schema_version":2,"revision":1,"ticket":"ap-55","branch":"main"}`)
	mustWrite(t, filepath.Join(projectHome, "current.txt"), "builder ap-55\n")
	_ = repo

	// The ladder genuinely cannot pick — ambiguity is the regression shape.
	if _, err := ticket.ResolveLadder(); err == nil {
		t.Fatal("expected ambiguous cwd")
	}
	// Project-only resolution (what `current`/`recover` use) still works.
	a := resolveAPProject()
	if got := a.currentTicket(); got != "ap-55" {
		t.Fatalf("current.txt resume failed under ambiguity: %q", got)
	}
}

func TestCurrentTicketRejectsStalePair(t *testing.T) {
	repo := initSnapshotRepo(t)
	projectHome := filepath.Join(t.TempDir(), "project")
	mustMkdirAll(t, projectHome)
	// current.txt names a ticket whose dir/checkpoint is gone.
	mustWrite(t, filepath.Join(projectHome, "current.txt"), "builder ap-99\n")
	t.Chdir(repo)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BABYSIT_TICKET", "")
	t.Setenv("BBS_TICKET", "")
	t.Setenv("BABYSIT_PROJECT_HOME", projectHome)

	a := resolveAPProject()
	if got := a.currentTicket(); got != "" {
		t.Fatalf("stale current.txt resurrected deleted ticket: %q", got)
	}
}

func TestResolveLadderEnvConflictOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir()) // not a git repo — slug.Resolve returns ErrNoRepo
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BABYSIT_TICKET", "ap-01")
	t.Setenv("BBS_TICKET", "ap-02")
	if _, err := ticket.ResolveLadder(); err == nil {
		t.Fatal("conflicting env outside a repo resolved silently")
	} else {
		var conflict *slug.EnvConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("expected EnvConflictError, got %v", err)
		}
	}
}

func TestResolveLadderEnvWinsOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BABYSIT_TICKET", "ap-09")
	t.Setenv("BBS_TICKET", "")
	env, err := ticket.ResolveLadder()
	if err != nil {
		t.Fatal(err)
	}
	if env.Ticket != "ap-09" {
		t.Fatalf("env ticket lost outside a repo: %q", env.Ticket)
	}
}

func TestResolveAPManifestOnlyCheckout(t *testing.T) {
	manifestOnlyFixture(t, "ap-77")
	a := resolveAP()
	if a.ticket != "ap-77" {
		t.Fatalf("resolveAP lost manifest-only identity: %q", a.ticket)
	}
}

func TestResolveEnvManifestOnlyCheckout(t *testing.T) {
	manifestOnlyFixture(t, "ap-77")
	env := resolveEnv() // shared by `ticket env`, `ensure`, readiness
	if env.Ticket != "ap-77" {
		t.Fatalf("resolveEnv lost manifest-only identity: %q", env.Ticket)
	}
}
