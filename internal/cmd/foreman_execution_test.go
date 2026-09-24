package cmd

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

func executionFixture(t *testing.T) (*ticket.Store, *ticket.Store, string) {
	t.Helper()
	repo, env := initReadinessFixture(t)
	t.Chdir(repo)
	state := t.TempDir()
	mustMkdirAll(t, filepath.Join(state, "projects"))
	projectHome := filepath.Join(state, "projects", "project")
	if err := os.Rename(env.ProjectHome, projectHome); err != nil {
		t.Fatal(err)
	}
	env.ProjectHome = projectHome
	t.Setenv("BABYSIT_HOME", state)
	t.Setenv("BABYSIT_PROJECT_HOME", env.ProjectHome)
	t.Setenv("BABYSIT_TICKET", "ready")
	runGit(t, repo, "switch", "-c", env.Branch)
	mustWrite(t, filepath.Join(repo, "feature.txt"), "working journey\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "child behavior")
	child := ticket.New(env)
	if err := ticket.WriteManifest(child.ManifestPath(), env.Ticket, "App", []ticket.Repo{{Name: "app", Canonical: repo, Worktree: repo, Branch: env.Branch, Base: "main"}}); err != nil {
		t.Fatal(err)
	}
	doc := ticket.ReadDoc(child.IndexPath())
	doc.Set("origin.project_seed", "core")
	doc.Set("parent", "parent")
	doc.Set("status", "in_review")
	if err := ticket.WriteDoc(child.IndexPath(), doc); err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct{ gate, id string }{{"review-pr", "review-1"}, {"qa", "qa-1"}} {
		if _, err := setV2VerificationEvidence(env, evidenceForCurrentSnapshot(t, env, g.gate, g.id, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := projectSealChild(child); err != nil {
		t.Fatal(err)
	}
	parent := storeForTicket(env, "parent")
	if err := ticket.WriteDoc(parent.IndexPath(), ticket.Doc{"id": "parent", "status": "decomposed", "assignee": "fm-test", "children": []interface{}{"ready"}}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(parent.Home(), "requirement.md"), "Create and recover tasks\n")
	mustWrite(t, filepath.Join(parent.Home(), "plan.md"), "A real persisted task journey\n")
	c := projectContract{Version: 1, Audience: "small teams", Outcome: "Create a task and recover it after reload", NonGoals: []string{"billing"}, Seeds: []string{"core"}, Criteria: []projectCriterion{{ID: "AC1", Text: "Task persists across reload", Owner: "core", Checks: []string{"behavior", "failure"}}}, FirstJourney: []string{"AC1"}, ProductReview: true}
	if err := writeJSONAtomic(projectContractPath(parent), c); err != nil {
		t.Fatal(err)
	}
	if _, err := approvalPublish(parent, "project-plan", "review", "developer"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := approvalResolve(parent, "approve", "accepted", "developer"); err != nil {
		t.Fatal(err)
	}
	if err := foreman.Save(foreman.Record{ID: "fm-test", ProjectDir: repo, Status: "running"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "switch", "-c", "qa/parent")
	return parent, child, repo
}

func executionEvidence(t *testing.T, st *ticket.Store, repo, kind string) string {
	t.Helper()
	spec := filepath.Join(t.TempDir(), "spec.json")
	if err := writeJSONAtomic(spec, map[string]interface{}{"kind": kind, "producer": "fresh-evaluator", "surfaces": []projectSurface{{Dir: repo, Ref: "qa/parent", BaseRef: "main", RestoreRef: "main", RestoreHead: gitOutIn(repo, "rev-parse", "main")}}}); err != nil {
		t.Fatal(err)
	}
	var err error
	out := captureStdout(t, func() { err = projectEvidenceCommand(st, map[string]string{"begin": "1", "file": spec}) })
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data projectEvidence `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data.ID
}

func finishExecutionEvidence(t *testing.T, st *ticket.Store, id, kind string, failed bool) {
	t.Helper()
	code := 0
	if failed {
		code = 1
	}
	log := filepath.Join(t.TempDir(), "check.log")
	mustWrite(t, log, "Executed real fixture assertion: task survives reload\n")
	checks := []projectCheck{{Criterion: "AC1", Kind: "behavior", Command: []string{"fixture", "reload"}, ExitCode: &code, Log: log}, {Criterion: "AC1", Kind: "failure", Command: []string{"fixture", "invalid-task"}, ExitCode: &code, Log: log}}
	if kind == "product" {
		checks = []projectCheck{{Criterion: "AC1", Kind: "product", Command: []string{"fixture", "journey"}, ExitCode: &code, Log: log}}
	}
	result := filepath.Join(t.TempDir(), "results.json")
	if err := writeJSONAtomic(result, map[string]interface{}{"checks": checks, "findings": []projectFinding{}}); err != nil {
		t.Fatal(err)
	}
	var err error
	captureStdout(t, func() { err = projectEvidenceCommand(st, map[string]string{"attempt": id, "file": result}) })
	if err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() { err = projectEvidenceCommand(st, map[string]string{"attempt": id, "file": result}) })
	if err != nil {
		t.Fatalf("record retry after lost response: %v", err)
	}
}

func verifiedExecution(t *testing.T) (*ticket.Store, *ticket.Store, string) {
	t.Helper()
	parent, child, repo := executionFixture(t)
	for _, kind := range []string{"integration", "product"} {
		id := executionEvidence(t, parent, repo, kind)
		finishExecutionEvidence(t, parent, id, kind, false)
	}
	runGit(t, repo, "switch", "main")
	s, err := projectRead(parent)
	if err != nil || !s.Ready {
		t.Fatalf("valid project blocked: %+v %v", s.Blockers, err)
	}
	return parent, child, repo
}

func TestProjectCompletionAndCleanedChildRecovery(t *testing.T) {
	st, child, repo := verifiedExecution(t)
	var err error
	captureStdout(t, func() { err = projectComplete(st, "fm-test") })
	if err != nil {
		t.Fatal(err)
	}
	// The manifest still identifies retained source refs even when its worker
	// checkout no longer exists. Durable logs must remain sufficient.
	m, _ := ticket.ReadManifest(child.ManifestPath())
	m.Repos[0].Worktree = filepath.Join(t.TempDir(), "removed")
	if err := ticket.WriteManifest(child.ManifestPath(), m.Ticket, m.Title, m.Repos); err != nil {
		t.Fatal(err)
	}
	s, err := projectRead(st)
	if err != nil || !s.Ready || s.Completion != "verified" {
		t.Fatalf("cleanup lost verified result: %+v %v", s, err)
	}
	r, _ := foreman.Load("fm-test")
	if err := foremanCompletionCurrent(r); err != nil {
		t.Fatal(err)
	}
	if err := foremanHeartbeat([]string{"fm-test", "--status", "done"}); err != nil {
		t.Fatal(err)
	}
	// Subsequent unrelated checkout use doesn't erase an observed restoration.
	runGit(t, repo, "switch", "qa/parent")
	if s, _ := projectRead(st); s.Completion != "verified" {
		t.Fatal("later checkout switch invalidated completion")
	}
	doc := ticket.ReadDoc(st.IndexPath())
	doc.Set("assignee", "other")
	if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(st.Home(), "plan.md"), "changed after reassignment")
	if err := foremanCompletionCurrent(r); err == nil {
		t.Fatal("reassignment hid a stale completion receipt")
	}
}

// stubOrcaCLI puts a fake `orca` on ORCA_CLI_COMMAND that answers preflight
// and returns the WORKERS env payload for every orchestration call.
func stubOrcaCLI(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orca")
	stub := `#!/bin/sh
case "$1" in
 status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
 orchestration) printf '%s\n' "$WORKERS" ;;
esac
`
	if err := os.WriteFile(path, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCA_CLI_COMMAND", path)
}

// A settled dispatch whose terminal was reused by a later dispatch (retained)
// or already closed (released) has no liveness row left to read — its
// projection is unverifiable forever even though the dispatch itself is done.
// Completion must not wedge on that; live and genuinely unverifiable active
// workers still block.
func TestProjectCompleteSettledWorkerTerminals(t *testing.T) {
	for _, tc := range []struct {
		name, status, terminal, verdict string
		done                            bool
	}{
		{name: "retained terminal", status: "completed", terminal: "retained", verdict: "unverifiable", done: true},
		{name: "released terminal", status: "failed", terminal: "released", verdict: "unverifiable", done: true},
		{name: "live worker", status: "dispatched", terminal: "active", verdict: "live"},
		{name: "unverifiable active worker", status: "dispatched", terminal: "active", verdict: "unverifiable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, repo := executionFixture(t)
			stubOrcaCLI(t)
			t.Setenv("WORKERS", fmt.Sprintf(`{"ok":true,"result":{"workers":[{"dispatchId":"ctx-w","dispatchStatus":%q,"terminalState":%q,"projection":{"liveness":{"verdict":%q}}}],"page":{"hasMore":false}}}`, tc.status, tc.terminal, tc.verdict))
			doc := ticket.ReadDoc(p.IndexPath())
			doc.Set("pointers.orca_run", "run-fm")
			if err := ticket.WriteDoc(p.IndexPath(), doc); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"integration", "product"} {
				id := executionEvidence(t, p, repo, kind)
				finishExecutionEvidence(t, p, id, kind, false)
			}
			runGit(t, repo, "switch", "main")
			var err error
			captureStdout(t, func() { err = projectComplete(p, "fm-test") })
			if tc.done {
				if err != nil {
					t.Fatalf("settled worker with %s terminal wedged completion: %v", tc.terminal, err)
				}
				r, _ := foreman.Load("fm-test")
				if r.Status != "done" {
					t.Fatalf("foreman not marked done: %+v", r)
				}
				if err := foremanCompletionCurrent(r); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("completion ignored a live or unverifiable active worker")
			}
		})
	}
}

// A leftover ticket directory without index.json (e.g. bs-cli holding only
// review.rounds) is not a ticket: both the allDone scan inside projectComplete
// and the receipt check in foremanCompletionCurrent must skip it rather than
// wedge on leftover state.
func TestCompletionSkipsStubTicketDirs(t *testing.T) {
	p, _, _ := verifiedExecution(t)
	stub := filepath.Join(p.Env.ProjectHome, "tickets", "bs-stub")
	mustMkdirAll(t, stub)
	mustWrite(t, filepath.Join(stub, "review.rounds"), "1\n")
	var err error
	captureStdout(t, func() { err = projectComplete(p, "fm-test") })
	if err != nil {
		t.Fatalf("stub ticket dir wedged completion: %v", err)
	}
	r, _ := foreman.Load("fm-test")
	if r.Status != "done" {
		t.Fatalf("foreman not marked done: %+v", r)
	}
	if err := foremanCompletionCurrent(r); err != nil {
		t.Fatalf("stub ticket dir wedged receipt check: %v", err)
	}
}

// A ticket directory whose index.json exists but cannot be parsed or read is
// a real record in an unknown state — not a stub. Both completion scans must
// fail loudly on it instead of skipping it like a missing index.
func TestCompletionFailsOnMalformedTicketRecords(t *testing.T) {
	for _, tc := range []struct {
		name       string
		breakIndex func(t *testing.T, dir string)
	}{
		{name: "malformed index", breakIndex: func(t *testing.T, dir string) {
			mustWrite(t, filepath.Join(dir, "index.json"), "{not json")
		}},
		{name: "unreadable index", breakIndex: func(t *testing.T, dir string) {
			// A directory named index.json exists but cannot be read as a file.
			mustMkdirAll(t, filepath.Join(dir, "index.json"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := verifiedExecution(t)
			broken := filepath.Join(p.Env.ProjectHome, "tickets", "bs-broken")
			mustMkdirAll(t, broken)
			tc.breakIndex(t, broken)
			var err error
			captureStdout(t, func() { err = projectComplete(p, "fm-test") })
			if err == nil {
				t.Fatal("completion skipped a malformed ticket record")
			}
		})
		t.Run(tc.name+" receipt check", func(t *testing.T) {
			p, _, _ := verifiedExecution(t)
			var err error
			captureStdout(t, func() { err = projectComplete(p, "fm-test") })
			if err != nil {
				t.Fatal(err)
			}
			r, _ := foreman.Load("fm-test")
			broken := filepath.Join(p.Env.ProjectHome, "tickets", "bs-broken")
			mustMkdirAll(t, broken)
			tc.breakIndex(t, broken)
			if err := foremanCompletionCurrent(r); err == nil {
				t.Fatal("receipt check skipped a malformed ticket record")
			}
		})
	}
}

func TestProjectTimeOrderUsesInstant(t *testing.T) {
	if !projectTimeAfter("2026-09-24T06:00:00.1Z", "2026-09-24T06:00:00Z") || projectTimeAfter("2026-09-24T06:00:00Z", "2026-09-24T06:00:00.1Z") {
		t.Fatal("fractional timestamps were compared lexicographically")
	}
}

func TestProjectRejectsFalseCompletion(t *testing.T) {
	cases := map[string]func(*testing.T, *ticket.Store, *ticket.Store, string){
		"scope removed": func(t *testing.T, p, c *ticket.Store, repo string) {
			d := ticket.ReadDoc(p.IndexPath())
			d["children"] = []interface{}{}
			ticket.WriteDoc(p.IndexPath(), d)
		},
		"cancelled child": func(t *testing.T, p, c *ticket.Store, repo string) {
			d := ticket.ReadDoc(c.IndexPath())
			d.Set("status", "cancelled")
			ticket.WriteDoc(c.IndexPath(), d)
		},
		"stale approval": func(t *testing.T, p, c *ticket.Store, repo string) {
			mustWrite(t, filepath.Join(p.Home(), "plan.md"), "changed scope")
		},
		"changed child evidence": func(t *testing.T, p, c *ticket.Store, repo string) {
			mustWrite(t, filepath.Join(c.Home(), "evidence", "check.log"), "changed log")
		},
		"post QA code": func(t *testing.T, p, c *ticket.Store, repo string) {
			runGit(t, repo, "switch", "qa/parent")
			mustWrite(t, filepath.Join(repo, "feature.txt"), "regression")
			runGit(t, repo, "commit", "-am", "regression")
		},
		"base moved": func(t *testing.T, p, c *ticket.Store, repo string) {
			mustWrite(t, filepath.Join(repo, "tracked.txt"), "new base")
			runGit(t, repo, "commit", "-am", "base change")
		},
		"held lease": func(t *testing.T, p, c *ticket.Store, repo string) {
			leasePublish(leaseDirOf(gitOutIn(repo, "rev-parse", "--absolute-git-dir")), leaseBodyFor("parent", leaseLong, "10"))
		},
		"missing integration": func(t *testing.T, p, c *ticket.Store, repo string) {
			paths, _ := filepath.Glob(filepath.Join(p.Home(), "project-evidence", "*.json"))
			for _, path := range paths {
				var e projectEvidence
				readProjectJSON(path, &e)
				if e.Kind == "integration" {
					os.Remove(path)
				}
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p, c, repo := verifiedExecution(t)
			change(t, p, c, repo)
			s, err := projectRead(p)
			if err == nil && s.Ready {
				t.Fatalf("false completion: %+v", s)
			}
			if name == "stale approval" && (len(s.Coverage) == 0 || s.Coverage[0].State == "proven") {
				t.Fatal("stale approval retained apparently current coverage")
			}
			if err := projectComplete(p, "fm-test"); err == nil {
				t.Fatal("complete accepted stale/missing evidence")
			}
		})
	}
}

func TestProjectEvidenceCapturesInputsBeforeCheck(t *testing.T) {
	p, _, repo := executionFixture(t)
	id := executionEvidence(t, p, repo, "integration")
	mustWrite(t, filepath.Join(repo, "feature.txt"), "changed after begin")
	runGit(t, repo, "commit", "-am", "late change")
	result := filepath.Join(t.TempDir(), "results.json")
	mustWrite(t, result, `{"checks":[]}`)
	if err := projectEvidenceCommand(p, map[string]string{"attempt": id, "file": result}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("late results accepted: %v", err)
	}
}

func TestProjectFailedCheckAndProductFindings(t *testing.T) {
	p, _, repo := executionFixture(t)
	id := executionEvidence(t, p, repo, "integration")
	finishExecutionEvidence(t, p, id, "integration", true)
	id = executionEvidence(t, p, repo, "product")
	finishExecutionEvidence(t, p, id, "product", false)
	runGit(t, repo, "switch", "main")
	s, err := projectRead(p)
	if err != nil || s.Ready || s.Coverage[0].State == "proven" {
		t.Fatalf("failed check passed: %+v %v", s, err)
	}
}

func TestProjectHeartbeatCannotBypassCompletion(t *testing.T) {
	t.Setenv("BABYSIT_HOME", t.TempDir())
	if err := foreman.Save(foreman.Record{ID: "fm-test", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := foremanHeartbeat([]string{"fm-test", "--status", "DONE"}); err == nil {
		t.Fatal("raw done heartbeat bypassed validation")
	}
	r, _ := foreman.Load("fm-test")
	if r.Status != "running" {
		t.Fatal("rejected heartbeat mutated status")
	}
}

func TestProjectScopeContractInvalidatesApproval(t *testing.T) {
	p, _, _ := executionFixture(t)
	var c projectContract
	if err := readProjectJSON(projectContractPath(p), &c); err != nil {
		t.Fatal(err)
	}
	c.Criteria[0].Text = "New accepted behavior"
	if err := writeJSONAtomic(projectContractPath(p), c); err != nil {
		t.Fatal(err)
	}
	s, err := projectRead(p)
	if err != nil || s.Approval != "stale" || len(s.Dispatch) == 0 {
		t.Fatalf("changed scope kept approval: %+v %v", s, err)
	}
	if err := statusSet(p, "done", "developer"); err == nil {
		t.Fatal("status command bypassed project completion")
	}
}

func TestProjectEvidenceOnlyDelivery(t *testing.T) {
	p, child, repo := executionFixture(t)
	// Evidence-only work has no production commits and stores its outputs on
	// the durable ticket. Re-approve the explicitly different contract.
	runGit(t, repo, "switch", "main")
	runGit(t, repo, "branch", "-f", child.Env.Branch, "main")
	var c projectContract
	readProjectJSON(projectContractPath(p), &c)
	c.ArtifactsOnly = true
	c.ProductReview = false
	writeJSONAtomic(projectContractPath(p), c)
	if _, err := approvalPublish(p, "project-plan", "artifact scope", "developer"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := approvalResolve(p, "approve", "accepted", "developer"); err != nil {
		t.Fatal(err)
	}
	d := ticket.ReadDoc(child.IndexPath())
	d.Set("pointers.workflow", "prototype")
	ticket.WriteDoc(child.IndexPath(), d)
	mustWrite(t, child.VerdictPath("prototype"), "STATUS: DONE\nLearning validated with named observations\n")
	artifact := filepath.Join(child.Home(), "experiment.md")
	mustWrite(t, artifact, "Measured assumption and bounded production recommendation")
	input := filepath.Join(t.TempDir(), "artifacts.json")
	writeJSONAtomic(input, map[string]interface{}{"paths": []string{artifact}})
	if err := projectSealArtifacts(child, input); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeJSONAtomic(spec, map[string]interface{}{"kind": "integration", "producer": "evaluator", "surfaces": []interface{}{}})
	var err error
	out := captureStdout(t, func() { err = projectEvidenceCommand(p, map[string]string{"begin": "1", "file": spec}) })
	if err != nil {
		t.Fatal(err)
	}
	var e struct {
		Data projectEvidence `json:"data"`
	}
	json.Unmarshal([]byte(out), &e)
	finishExecutionEvidence(t, p, e.Data.ID, "integration", false)
	s, err := projectRead(p)
	if err != nil || !s.Ready || s.Delivery[0].State != "ARTIFACTS_READY" {
		t.Fatalf("artifacts rejected: %+v %v", s, err)
	}
	mustWrite(t, artifact, "changed conclusion")
	s, _ = projectRead(p)
	if s.Ready {
		t.Fatal("changed artifact remained accepted")
	}
}

func TestProjectWatchIgnoresSpinnerAndPreservesBoundedWait(t *testing.T) {
	p, _, repo := executionFixture(t)
	originalPath := os.Getenv("PATH")
	client, r, pane, _ := watchFixture(t)
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+originalPath)
	r.ProjectDir = repo
	now := time.Now().UTC().Truncate(time.Second)
	watchTick(client, r, testWatchOpts(), now)
	write(t, pane, "spinner keeps moving")
	if line := watchTick(client, r, testWatchOpts(), now.Add(11*time.Minute)); !strings.HasPrefix(line, "NUDGED") {
		t.Fatalf("spinner counted as progress: %s", line)
	}
	report := projectProgress{Version: 1, Ticket: p.Env.Ticket, Producer: r.ID, Phase: "qa", Summary: "Waiting for check", Updated: now.Format(time.RFC3339), WaitKind: "test", WaitReason: "browser check still running", WaitUntil: now.Add(13 * time.Minute).Format(time.RFC3339)}
	writeJSONAtomic(filepath.Join(p.Home(), "progress.json"), report)
	if line := watchTick(client, r, testWatchOpts(), now.Add(12*time.Minute)); !strings.HasPrefix(line, "WAITING") {
		t.Fatalf("valid bounded wait nudged: %s", line)
	}
	if s := watchLoad(r.ID); s.Nudges != 1 || s.RuntimeObservedAt == "" {
		t.Fatalf("wait changed retry budget or lost reachability: %+v", s)
	}
}

func TestProjectDashboardUsesSharedRead(t *testing.T) {
	p, _, _ := verifiedExecution(t)
	srv := &dashServer{stateDir: filepath.Dir(filepath.Dir(p.Env.ProjectHome))}
	// Server expects <state>/projects/<slug>; the test fixture can name its
	// state directory directly without changing process cwd in the handler.
	slug := filepath.Base(p.Env.ProjectHome)
	r := httptest.NewRequest("GET", "/api/tickets/"+slug+"/parent/project", nil)
	r.SetPathValue("project", slug)
	r.SetPathValue("ticket", "parent")
	w := httptest.NewRecorder()
	srv.handleProject(w, r)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got projectSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want, err := projectRead(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != want.Subject || got.Ready != want.Ready || strings.Join(got.Blockers, "|") != strings.Join(want.Blockers, "|") {
		t.Fatalf("API diverged: %+v / %+v", got, want)
	}
}

func TestProjectProgressWaitsAndEvidence(t *testing.T) {
	p, _, _ := executionFixture(t)
	file := filepath.Join(t.TempDir(), "progress.json")
	evidence := filepath.Join(t.TempDir(), "milestone.md")
	mustWrite(t, evidence, "Verified task survives reload")
	input := projectProgress{Producer: "fm-test", Phase: "integration", Summary: "First journey demonstrated", Evidence: evidence}
	writeJSONAtomic(file, input)
	if err := projectProgressCommand(p, map[string]string{"file": file}); err != nil {
		t.Fatal(err)
	}
	var first projectProgress
	readProjectJSON(filepath.Join(p.Home(), "progress.json"), &first)
	if first.Health != "progressing" {
		t.Fatal(first)
	}
	input.WaitKind = "test"
	input.WaitReason = "Browser fixture running"
	input.WaitUntil = time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	writeJSONAtomic(file, input)
	if err := projectProgressCommand(p, map[string]string{"file": file}); err != nil {
		t.Fatal(err)
	}
	var next projectProgress
	readProjectJSON(filepath.Join(p.Home(), "progress.json"), &next)
	if next.Health != "reported_wait" || next.Advanced != first.Advanced {
		t.Fatal("activity invented progress")
	}
	projectProgressHealth(&next, time.Now().Add(6*time.Minute))
	if next.Health != "wait_expired" {
		t.Fatal(next)
	}
}
