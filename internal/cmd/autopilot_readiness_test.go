package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/identity"
)

func TestV2EvidenceAndReadinessAreBoundToCurrentSubject(t *testing.T) {
	repo, env := initReadinessFixture(t)
	t.Chdir(repo)

	review := evidenceForCurrentSnapshot(t, env, "review-pr", "review-1", 0)
	if _, err := setV2VerificationEvidence(env, review); err != nil {
		t.Fatalf("accept review evidence: %v", err)
	}
	push, err := evaluateReadiness(env, "push")
	if err != nil {
		t.Fatal(err)
	}
	if !push.Enforced || !push.Ready {
		t.Fatalf("fresh review did not make push ready: %+v", push)
	}
	land, err := evaluateReadiness(env, "land")
	if err != nil {
		t.Fatal(err)
	}
	if land.Ready || !containsString(land.ReasonCodes, "qa:missing") {
		t.Fatalf("land ignored missing QA: %+v", land)
	}

	qa := evidenceForCurrentSnapshot(t, env, "qa", "qa-1", 0)
	if _, err := setV2VerificationEvidence(env, qa); err != nil {
		t.Fatalf("accept qa evidence: %v", err)
	}
	land, err = evaluateReadiness(env, "land")
	if err != nil {
		t.Fatal(err)
	}
	if !land.Ready {
		t.Fatalf("fresh review+qa did not make land ready: %+v", land)
	}

	mustWrite(t, filepath.Join(env.ProjectHome, "tickets", env.Ticket, "requirement.md"), "changed requirement\n")
	stale, err := evaluateReadiness(env, "land")
	if err != nil {
		t.Fatal(err)
	}
	if stale.Ready || !containsString(stale.ReasonCodes, "qa:stale_requirement_digest") || !containsString(stale.ReasonCodes, "review-pr:stale_requirement_digest") {
		t.Fatalf("changed requirement reused stale evidence: %+v", stale)
	}
}

func TestV2EvidenceRejectsContradictionAndImmutableRewrite(t *testing.T) {
	repo, env := initReadinessFixture(t)
	t.Chdir(repo)

	contradiction := evidenceForCurrentSnapshot(t, env, "review-pr", "review-1", 1)
	if _, err := setV2VerificationEvidence(env, contradiction); err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("PASS + failed check error=%v", err)
	}

	accepted := evidenceForCurrentSnapshot(t, env, "review-pr", "review-1", 0)
	if _, err := setV2VerificationEvidence(env, accepted); err != nil {
		t.Fatal(err)
	}
	var rewritten map[string]interface{}
	if err := json.Unmarshal(accepted, &rewritten); err != nil {
		t.Fatal(err)
	}
	rewritten["result"] = "FAIL"
	rewrittenBody, _ := json.Marshal(rewritten)
	if _, err := setV2VerificationEvidence(env, rewrittenBody); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable rewrite error=%v", err)
	}
}

func TestV2ReadinessUsesImmutableAcceptedEvidence(t *testing.T) {
	repo, env := initReadinessFixture(t)
	t.Chdir(repo)

	var failed map[string]interface{}
	if err := json.Unmarshal(evidenceForCurrentSnapshot(t, env, "review-pr", "review-1", 1), &failed); err != nil {
		t.Fatal(err)
	}
	failed["result"] = "FAIL"
	body, _ := json.Marshal(failed)
	if _, err := setV2VerificationEvidence(env, body); err != nil {
		t.Fatal(err)
	}
	gatePath := filepath.Join(env.ProjectHome, "tickets", env.Ticket, "evidence", "verification", "gates", "review-pr.json")
	failed["result"], failed["status"] = "PASS", "DONE"
	for _, raw := range failed["checks"].([]interface{}) {
		raw.(map[string]interface{})["exit_code"] = float64(0)
	}
	forged, _ := json.Marshal(failed)
	mustWrite(t, gatePath, string(forged))

	result, err := evaluateReadiness(env, "push")
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready || !containsString(result.ReasonCodes, "review-pr:result_not_pass") {
		t.Fatalf("mutable gate projection overrode immutable failure: %+v", result)
	}
	immutablePath := filepath.Join(env.ProjectHome, "tickets", env.Ticket, "evidence", "verification", "attempts", "review-1.json")
	immutable := readJSONObject(immutablePath)
	immutable["producer"].(map[string]interface{})["owner"] = "different-worker"
	tampered, _ := json.Marshal(immutable)
	mustWrite(t, immutablePath, string(tampered))
	if _, err := evaluateReadiness(env, "push"); err == nil || !strings.Contains(err.Error(), "owner mismatch") {
		t.Fatalf("tampered immutable evidence was not rejected: %v", err)
	}
}

func TestV2EvidenceRejectsIncompleteChecksAndEscapingCWD(t *testing.T) {
	repo, env := initReadinessFixture(t)
	t.Chdir(repo)
	var ev map[string]interface{}
	if err := json.Unmarshal(evidenceForCurrentSnapshot(t, env, "review-pr", "review-1", 0), &ev); err != nil {
		t.Fatal(err)
	}
	check := ev["checks"].([]interface{})[0].(map[string]interface{})
	delete(check, "exit_code")
	missing, _ := json.Marshal(ev)
	if _, err := setV2VerificationEvidence(env, missing); err == nil || !strings.Contains(err.Error(), "exit_code") {
		t.Fatalf("missing exit code accepted: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.log")
	mustWrite(t, outside, "ok\n")
	check["exit_code"] = float64(0)
	check["cwd"] = filepath.Dir(outside)
	check["log_path"] = outside
	check["log_digest"], _ = digestFile(outside)
	escaping, _ := json.Marshal(ev)
	if _, err := setV2VerificationEvidence(env, escaping); err == nil || !strings.Contains(err.Error(), ".cwd") {
		t.Fatalf("escaping check cwd accepted: %v", err)
	}
}

func TestV2ReadinessRejectsCodeBaseAndPolicyChanges(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		mutate       func(*testing.T, string)
	}{
		{"code tree", "qa:stale_tree_digest", func(t *testing.T, repo string) {
			mustWrite(t, filepath.Join(repo, "tracked.txt"), "changed\n")
		}},
		{"same-tree base", "qa:stale_base_sha", func(t *testing.T, repo string) {
			runGit(t, repo, "commit", "--allow-empty", "-m", "new base")
		}},
		{"policy", "qa:stale_policy_digest", func(t *testing.T, repo string) {
			mustMkdirAll(t, filepath.Join(repo, ".babysit"))
			mustWrite(t, filepath.Join(repo, ".babysit", "git-flow.yaml"), "profile: startup\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, env := initReadinessFixture(t)
			t.Chdir(repo)
			for _, row := range []struct{ gate, attempt string }{{"review-pr", "review-1"}, {"qa", "qa-1"}} {
				if _, err := setV2VerificationEvidence(env, evidenceForCurrentSnapshot(t, env, row.gate, row.attempt, 0)); err != nil {
					t.Fatal(err)
				}
			}
			tc.mutate(t, repo)
			result, err := evaluateReadiness(env, "land")
			if err != nil {
				t.Fatal(err)
			}
			if result.Ready || !containsString(result.ReasonCodes, tc.reason) {
				t.Fatalf("mutation did not invalidate evidence with %s: %+v", tc.reason, result)
			}
		})
	}
}

func TestLegacyReadinessPreservesVerdictContract(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "legacy")
	mustMkdirAll(t, filepath.Join(home, "verdicts"))
	mustWrite(t, filepath.Join(home, "index.json"), `{"id":"legacy","control":null}`)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"ticket":"legacy","workflow":"builder"}`)
	mustWrite(t, filepath.Join(home, "verdicts", "review-pr.md"), "STATUS: DONE\n")
	mustWrite(t, filepath.Join(home, "verdicts", "qa.md"), "STATUS: DONE_WITH_CONCERNS\n")
	env := identity.Env{Slug: "project", Branch: "feat/legacy_x", Ticket: "legacy", ProjectHome: project}
	ready, err := evaluateReadiness(env, "land")
	if err != nil {
		t.Fatal(err)
	}
	if ready.Enforced || !ready.Ready {
		t.Fatalf("legacy verdict behavior changed: %+v", ready)
	}
}

func initReadinessFixture(t *testing.T) (string, identity.Env) {
	t.Helper()
	repo := initSnapshotRepo(t)
	t.Setenv("BBS_BASE_BRANCH", "main")
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "ready")
	mustMkdirAll(t, filepath.Join(home, "attempts"))
	mustMkdirAll(t, filepath.Join(home, "evidence"))
	mustWrite(t, filepath.Join(home, "index.json"), `{"id":"ready","origin":{"type":"standalone"},"control":null}`)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":2,"ticket":"ready","run_id":"run-1","revision":1,"workflow":"builder"}`)
	mustWrite(t, filepath.Join(home, "requirement.md"), "requirement\n")
	mustWrite(t, filepath.Join(home, "plan.md"), "plan\n")
	mustWrite(t, filepath.Join(home, "evidence", "check.log"), "ok\n")
	for _, row := range []struct{ id, gate string }{{"review-1", "review-pr"}, {"qa-1", "qa"}} {
		body, _ := json.Marshal(map[string]interface{}{
			"schema_version": 2, "id": row.id, "ticket": "ready", "run_id": "run-1",
			"gate": row.gate, "state": "completed", "owner": "worker-1", "revision": 2,
		})
		mustWrite(t, filepath.Join(home, "attempts", row.id+".json"), string(body))
	}
	return repo, identity.Env{Slug: "project", Branch: "feat/ready_x", Ticket: "ready", ProjectHome: project}
}

func evidenceForCurrentSnapshot(t *testing.T, env identity.Env, gate, attemptID string, exitCode int) []byte {
	t.Helper()
	a := &apState{slug: env.Slug, branch: env.Branch, ticket: env.Ticket, stateRoot: env.ProjectHome}
	s, err := collectAutopilotSnapshot(a, env.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(env.ProjectHome, "tickets", env.Ticket, "evidence", "check.log")
	logDigest, _ := digestFile(logPath)
	ev := map[string]interface{}{
		"schema_version": 2, "ticket": env.Ticket, "run_id": "run-1", "attempt_id": attemptID,
		"gate": gate, "status": "DONE", "result": "PASS", "subject": currentEvidenceSubject(s),
		"producer": map[string]interface{}{"harness": "test", "model": nil, "isolation": "fresh-native-context", "owner": "worker-1"},
		"checks": []interface{}{map[string]interface{}{
			"argv": []interface{}{"go", "test", "./internal/cmd"}, "cwd": gitOut("rev-parse", "--show-toplevel"),
			"exit_code": exitCode, "log_path": logPath, "log_digest": logDigest, "acceptance_ids": []interface{}{"AP-03"},
		}},
		"surface":             map[string]interface{}{"kind": "local-test", "fingerprint": currentSurfaceFingerprint()},
		"unresolved_findings": []interface{}{}, "limitations": []interface{}{},
		"started_at": "2026-09-10T00:00:00Z", "completed_at": "2026-09-10T00:01:00Z",
	}
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
