package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerificationProducerAndCrashRetry(t *testing.T) {
	repo, env := initReadinessFixture(t)
	t.Chdir(repo)
	a := &apState{slug: env.Slug, branch: env.Branch, ticket: env.Ticket, stateRoot: env.ProjectHome}
	for _, gate := range []string{"review-pr", "qa"} {
		var err error
		out := captureStdout(t, func() {
			err = a.verificationCommand([]string{"begin", "--gate", gate, "--owner", "worker", "--handle", "session-test", "--transport", "test", "--harness", "test"})
		})
		if err != nil {
			t.Fatal(err)
		}
		var e struct {
			Data attemptRecord `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &e); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "result.json")
		if err := writeJSONAtomic(file, map[string]interface{}{"checks": []map[string]interface{}{{"argv": []string{"git", "diff", "--check"}, "cwd": repo, "exit_code": 0, "log_path": filepath.Join(env.ProjectHome, "tickets", env.Ticket, "evidence", "check.log")}}, "unresolved_findings": []interface{}{}, "limitations": []interface{}{}}); err != nil {
			t.Fatal(err)
		}
		args := []string{"record", "--attempt", e.Data.ID, "--file", file}
		captureStdout(t, func() { err = a.verificationCommand(args) })
		if err != nil {
			t.Fatal(err)
		}
		if gate == "qa" {
			// Simulate a crash after writing pending evidence but before the
			// running attempt was advanced to completed.
			home := filepath.Join(env.ProjectHome, "tickets", env.Ticket)
			if err := writeAttemptRecord(filepath.Join(home, "attempts", e.Data.ID+".json"), &e.Data); err != nil {
				t.Fatal(err)
			}
			cp, err := readStrictObject(filepath.Join(home, "checkpoint.json"), true)
			if err != nil {
				t.Fatal(err)
			}
			cp["active_attempt_id"] = e.Data.ID
			if err := writeJSONAtomic(filepath.Join(home, "checkpoint.json"), cp); err != nil {
				t.Fatal(err)
			}
		}
		captureStdout(t, func() { err = a.verificationCommand(args) })
		if err != nil {
			t.Fatalf("crash retry failed: %v", err)
		}
	}
	ready, err := evaluateReadiness(env, "review")
	if err != nil || !ready.Enforced || !ready.Ready {
		t.Fatalf("produced evidence unusable: %+v %v", ready, err)
	}
	// Even a new commit with an identical tree is a different delivered revision.
	runGit(t, repo, "commit", "--allow-empty", "-m", "new revision")
	ready, err = evaluateReadiness(env, "review")
	if err != nil || ready.Ready || !strings.Contains(strings.Join(ready.ReasonCodes, ","), "head_sha") {
		t.Fatalf("post gate commit accepted: %+v %v", ready, err)
	}
}

func TestVerificationProducerRejectsRepairDuringGate(t *testing.T) {
	repo, env := initReadinessFixture(t)
	t.Chdir(repo)
	a := &apState{slug: env.Slug, branch: env.Branch, ticket: env.Ticket, stateRoot: env.ProjectHome}
	var err error
	out := captureStdout(t, func() {
		err = a.verificationCommand([]string{"begin", "--gate", "qa", "--owner", "worker", "--handle", "test", "--transport", "test"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var e struct {
		Data attemptRecord `json:"data"`
	}
	json.Unmarshal([]byte(out), &e)
	mustWrite(t, filepath.Join(repo, "tracked.txt"), "repair during gate")
	file := filepath.Join(t.TempDir(), "result.json")
	writeJSONAtomic(file, map[string]interface{}{"checks": []map[string]interface{}{{"argv": []string{"test"}, "cwd": repo, "exit_code": 0, "log_path": filepath.Join(env.ProjectHome, "tickets", env.Ticket, "evidence", "check.log")}}})
	if err := a.verificationCommand([]string{"record", "--attempt", e.Data.ID, "--file", file}); err == nil || !strings.Contains(err.Error(), "subject changed") {
		t.Fatalf("stale check accepted: %v", err)
	}
	if rec := readAttemptRecord(filepath.Join(env.ProjectHome, "tickets", env.Ticket, "attempts", e.Data.ID+".json")); rec == nil || rec.State != "failed" {
		t.Fatal("stale attempt retained writer ownership")
	}
}
