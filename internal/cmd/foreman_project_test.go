package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

func projectApprovalFixture(t *testing.T) (*ticket.Store, identity.Env) {
	t.Helper()
	env := identity.Env{ProjectHome: t.TempDir(), Ticket: "bs-project"}
	st := ticket.New(env)
	if err := ticket.WriteDoc(st.IndexPath(), ticket.Doc{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"requirement.md", "plan.md", "design.md", "prototype.html", "manifest.md"} {
		if err := os.WriteFile(filepath.Join(st.Home(), name), []byte("Project task list and navigation"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := approvalPublish(st, "project-plan", "Review project", "developer"); err != nil {
		t.Fatal(err)
	}
	return st, env
}

const projectRubric = `Coverage: parent AC1 maps to the task list journey
Host-page consistency: matches the existing application navigation
Reuse: existing list and toolbar components in the host
Prototype inspected: prototype.html includes the complete task journey
Scope: parent requirement acceptance criteria only`

func TestProjectApprovalNeedsExplicitAuto(t *testing.T) {
	st, env := projectApprovalFixture(t)
	rec := foreman.Record{ID: "fm-project"}
	if code, _, _ := selfResolveGate(st, env, rec, projectRubric); code != exitGrant {
		t.Fatalf("project self-approved without --auto: %d", code)
	}
	rec.Auto = true
	if code, _, reason := selfResolveGate(st, env, rec, projectRubric); code != 0 {
		t.Fatalf("explicit --auto rejected: %d %s", code, reason)
	}
	if code, _, _ := selfResolveGate(st, env, rec, "Coverage: complete parent task journey"); code != exitRubric {
		t.Fatalf("--auto bypassed incomplete rubric: %d", code)
	}
	rec.Hold = &foreman.Hold{}
	if code, _, _ := selfResolveGate(st, env, rec, projectRubric); code != exitGrant {
		t.Fatalf("--auto bypassed human hold: %d", code)
	}
	if err := os.WriteFile(filepath.Join(st.Home(), "design.md"), []byte("Stripe payments"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := selfResolveGate(st, env, rec, projectRubric); code != exitFloor {
		t.Fatalf("--auto bypassed non-delegable floor: %d", code)
	}
}

func TestProjectApprovalDoesNotChangeChildAutonomy(t *testing.T) {
	st, env := projectApprovalFixture(t)
	doc := ticket.ReadDoc(st.IndexPath())
	doc.Set("approval.kind", "plan")
	if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
		t.Fatal(err)
	}
	if code, _, reason := selfResolveGate(st, env, foreman.Record{ID: "fm-parent"}, projectRubric); code != 0 {
		t.Fatalf("child review now requires --auto: %d %s", code, reason)
	}
}

func TestProjectApprovalDetectsArtifactChangesAndRepublishes(t *testing.T) {
	for _, name := range []string{"requirement.md", "plan.md", "design.md", "prototype.html", "manifest.md"} {
		t.Run(name, func(t *testing.T) {
			st, _ := projectApprovalFixture(t)
			before := ticket.ReadDoc(st.IndexPath()).Get("approval.artifact_revision")
			if conflict, err := approvalPublish(st, "project-plan", "resume", "developer"); err != nil || conflict != "pending" {
				t.Fatalf("unchanged pending review not idempotent: %s %v", conflict, err)
			}
			if err := os.WriteFile(filepath.Join(st.Home(), name), []byte("Changed project journey"), 0o644); err != nil {
				t.Fatal(err)
			}
			if state, _, _ := approvalRead(st); state != "stale" {
				t.Fatalf("changed pending review reads %s", state)
			}
			if _, _, err := approvalResolve(st, "approve", "LGTM", "developer"); err == nil {
				t.Fatal("approved artifacts that were never reviewed")
			}
			if conflict, err := approvalPublish(st, "project-plan", "review changes", "developer"); err != nil || conflict != "" {
				t.Fatalf("cannot replace stale pending checkpoint: %s %v", conflict, err)
			}
			if ticket.ReadDoc(st.IndexPath()).Get("approval.artifact_revision") == before {
				t.Fatal("new checkpoint retained old revision")
			}
			if _, _, err := approvalResolve(st, "approve", "LGTM", "developer"); err != nil {
				t.Fatal(err)
			}
			if state, _, _ := approvalRead(st); state != "approved" {
				t.Fatalf("unchanged approval reads %s", state)
			}
			if err := os.Remove(filepath.Join(st.Home(), name)); err != nil {
				t.Fatal(err)
			}
			if state, _, _ := approvalRead(st); state != "stale" {
				t.Fatalf("removed approved artifact reads %s", state)
			}
		})
	}
}

func TestProjectApprovalTracksPointedArtifacts(t *testing.T) {
	st, _ := projectApprovalFixture(t)
	doc := ticket.ReadDoc(st.IndexPath())
	path := filepath.Join(t.TempDir(), "design.md")
	doc.Set("pointers.design", path)
	if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
		t.Fatal(err)
	}
	if _, err := approvalPublish(st, "project-plan", "review", "developer"); err == nil {
		t.Fatal("missing pointed design accepted")
	}
	if err := os.WriteFile(path, []byte("Design outside ticket root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := approvalPublish(st, "project-plan", "review", "developer"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Revised external design"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := approvalRead(st); state != "stale" {
		t.Fatalf("pointed design changed but reads %s", state)
	}
}

func TestForemanAutoSurvivesSpawnResume(t *testing.T) {
	log, titles := fakeOrcaFor(t)
	if _, err := foremanSpawn([]string{"fm-project", "--dir", trustedDir(t), "--auto"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{titles, log} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := spawnForeman("fm-project", "", "", ""); err != nil {
		t.Fatal(err)
	}
	rec, err := foreman.Load("fm-project")
	if err != nil || !rec.Auto {
		t.Fatalf("resume lost --auto: %+v %v", rec, err)
	}
	if !strings.Contains(readCalls(t, log), "--foreman-id fm-project --auto") {
		t.Fatal("resumed prompt lost --auto")
	}
}

func TestForemanAutoAdoptionIsExplicitAndDurable(t *testing.T) {
	log, _ := fakeOrcaFor(t)
	setFakeCurrent(t, log, "codex", "/repo")
	if err := foremanAdopt([]string{"fm-project"}); err != nil {
		t.Fatal(err)
	}
	if rec, err := foreman.Load("fm-project"); err != nil || rec.Auto {
		t.Fatalf("default adoption enabled auto: %+v %v", rec, err)
	}
	if err := foremanAdopt([]string{"fm-project", "--auto"}); err != nil {
		t.Fatal(err)
	}
	if err := foremanAdopt(nil); err != nil {
		t.Fatal(err)
	}
	if rec, err := foreman.Load("fm-project"); err != nil || !rec.Auto {
		t.Fatalf("bare adoption lost explicit auto: %+v %v", rec, err)
	}
}

func TestForemanReportWorksWithoutLiveCoordinator(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "tickets", "bs-project", "report.md")
	if err := readForemanReport(home, "bs-project"); err == nil {
		t.Fatal("missing report read as completion")
	}
	if err := ticket.WriteAtomic(path, []byte("Observed: yesterday\nDelivery: PR_READY, 0 merged\n")); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := readForemanReport(home, "bs-project"); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"not a live check", "Observed: yesterday", "PR_READY, 0 merged"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lost %q: %s", want, out)
		}
	}
	if err := foremanReport([]string{"../outside"}); err == nil {
		t.Fatal("report accepted a path as a ticket id")
	}
}
