package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/reallongnguyen/babysit/internal/identity"
)

func TestCheckpointV2MigratesWithBackupAndPreservesUnknownFields(t *testing.T) {
	t.Setenv("BABYSIT_SKIP_COMMENT", "1")
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "ap-04")
	mustMkdirAll(t, home)
	legacy := `{"ticket":"ap-04","workflow":"builder","step":"implement","status":"in_progress","branch":"main","unknown":{"keep":true}}`
	mustWrite(t, filepath.Join(home, "checkpoint.json"), legacy)
	a := &apState{slug: "project", branch: "main", ticket: "ap-04", stateRoot: project}

	if err := a.checkpointV2(checkpointV2Input{Ticket: "ap-04", Workflow: "builder", Step: "implement", Status: "done_step"}); err != nil {
		t.Fatal(err)
	}
	cp := readJSONObject(filepath.Join(home, "checkpoint.json"))
	if int64Value(cp["schema_version"]) != 2 || int64Value(cp["revision"]) != 1 || stringValue(cp["run_id"]) == "" {
		t.Fatalf("migration omitted v2 fields: %#v", cp)
	}
	unknown, _ := cp["unknown"].(map[string]interface{})
	if unknown["keep"] != true {
		t.Fatalf("migration dropped unknown field: %#v", cp)
	}
	backup, err := os.ReadFile(filepath.Join(home, "checkpoint.v1.backup.json"))
	if err != nil || string(backup) != legacy {
		t.Fatalf("legacy backup mismatch: err=%v body=%q", err, backup)
	}

	beforeRun := stringValue(cp["run_id"])
	if err := a.refreshCheckpointV2("ap-04"); err != nil {
		t.Fatal(err)
	}
	refreshed := readJSONObject(filepath.Join(home, "checkpoint.json"))
	if int64Value(refreshed["revision"]) != 2 || stringValue(refreshed["run_id"]) != beforeRun || refreshed["unknown"] == nil {
		t.Fatalf("refresh did not preserve v2 state: %#v", refreshed)
	}
}

func TestCheckpointV2RejectsStaleWriterAndUnsupportedVersion(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "ap-04")
	mustMkdirAll(t, home)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":2,"revision":4,"ticket":"ap-04","branch":"main"}`)
	a := &apState{slug: "project", branch: "main", ticket: "ap-04", stateRoot: project}
	stale := int64(3)
	if err := a.checkpointV2(checkpointV2Input{Ticket: "ap-04", Workflow: "builder", Step: "implement", Status: "in_progress", ExpectedRevision: &stale}); err == nil {
		t.Fatal("stale checkpoint writer was accepted")
	}
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":3,"revision":4,"ticket":"ap-04","branch":"main"}`)
	if err := a.checkpointV2(checkpointV2Input{Ticket: "ap-04", Workflow: "builder", Step: "implement", Status: "in_progress"}); err == nil {
		t.Fatal("unsupported checkpoint writer was accepted")
	}
}
func TestCheckpointV2TicketInvariantNotBranch(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "ap-04")
	mustMkdirAll(t, home)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":2,"revision":4,"ticket":"ap-04","branch":"feat/ap-04_old"}`)

	// Same ticket on a different branch (trunk mode shares branches): both
	// write paths must succeed — branch is not identity.
	a := &apState{slug: "project", branch: "main", ticket: "ap-04", stateRoot: project}
	if err := a.refreshCheckpointV2("ap-04"); err != nil {
		t.Fatalf("refresh rejected same ticket on a different branch: %v", err)
	}
	if err := a.checkpointV2(checkpointV2Input{Ticket: "ap-04", Workflow: "builder", Step: "implement", Status: "in_progress", Force: true}); err != nil {
		t.Fatalf("checkpoint rejected same ticket on a different branch: %v", err)
	}

	// A checkpoint file naming a different ticket is still refused.
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":2,"revision":4,"ticket":"ap-99","branch":"main"}`)
	if err := a.refreshCheckpointV2("ap-04"); err == nil {
		t.Fatal("refresh accepted a checkpoint belonging to another ticket")
	}
	if err := a.checkpointV2(checkpointV2Input{Ticket: "ap-04", Workflow: "builder", Step: "implement", Status: "in_progress", Force: true}); err == nil {
		t.Fatal("checkpoint accepted a checkpoint belonging to another ticket")
	}
}

func TestAttemptLifecycleIsIdempotentCASCheckedAndTerminal(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	a, env := attemptFixture(t, repo)
	input := attemptAssignment(env, 1, "dispatch-1")
	first, err := prepareAttempt(a, env.Ticket, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != "prepared" || first.Revision != 1 {
		t.Fatalf("unexpected prepared attempt: %+v", first)
	}
	// The checkpoint advanced, but an acknowledgement-loss replay with the same
	// key still resolves to the already-persisted assignment.
	replay, err := prepareAttempt(a, env.Ticket, input)
	if err != nil || replay.ID != first.ID || replay.Revision != first.Revision {
		t.Fatalf("idempotent replay failed: attempt=%+v err=%v", replay, err)
	}
	other := attemptAssignment(env, 2, "dispatch-2")
	if _, err := prepareAttempt(a, env.Ticket, other); err == nil {
		t.Fatal("second active writer was accepted")
	}

	running, err := mutateAttempt(a, env.Ticket, first.ID, 1, map[string]interface{}{
		"state":   "running",
		"runtime": map[string]interface{}{"kind": "native", "handle": "worker-1", "transport_id": "dispatch-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutateAttempt(a, env.Ticket, first.ID, 1, map[string]interface{}{"state": "failed", "failure": "old writer"}); err == nil {
		t.Fatal("stale attempt writer was accepted")
	}
	completed, err := mutateAttempt(a, env.Ticket, first.ID, running.Revision, map[string]interface{}{
		"state": "completed", "result": map[string]interface{}{"handoff": "ready"},
	})
	if err != nil || completed.State != "completed" {
		t.Fatalf("completion failed: attempt=%+v err=%v", completed, err)
	}
	if _, err := mutateAttempt(a, env.Ticket, first.ID, completed.Revision, map[string]interface{}{"state": "failed", "failure": "rewrite"}); err == nil {
		t.Fatal("terminal attempt was mutated")
	}
	cp := readJSONObject(filepath.Join(env.ProjectHome, "tickets", env.Ticket, "checkpoint.json"))
	if cp["active_attempt_id"] != nil {
		t.Fatalf("terminal attempt retained checkout ownership: %#v", cp)
	}
}

func TestAttemptReplayRepairsPreparedOwnershipAfterCheckpointWriteLoss(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	a, env := attemptFixture(t, repo)
	input := attemptAssignment(env, 1, "dispatch-repair")
	first, err := prepareAttempt(a, env.Ticket, input)
	if err != nil {
		t.Fatal(err)
	}
	cpPath := filepath.Join(env.ProjectHome, "tickets", env.Ticket, "checkpoint.json")
	mustWrite(t, cpPath, `{"schema_version":2,"run_id":"run-04","revision":1,"ticket":"ap-04","branch":"main"}`)
	replay, err := prepareAttempt(a, env.Ticket, input)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay did not repair ownership: attempt=%+v err=%v", replay, err)
	}
	cp := readJSONObject(cpPath)
	if stringValue(cp["active_attempt_id"]) != first.ID || int64Value(cp["revision"]) != 2 {
		t.Fatalf("checkpoint ownership not repaired: %#v", cp)
	}
}

func TestAttemptStartFailsClosedOnMalformedExistingAttempt(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	a, env := attemptFixture(t, repo)
	mustMkdirAll(t, filepath.Join(env.ProjectHome, "tickets", env.Ticket, "attempts"))
	mustWrite(t, filepath.Join(env.ProjectHome, "tickets", env.Ticket, "attempts", "broken.json"), "{")
	if _, err := prepareAttempt(a, env.Ticket, attemptAssignment(env, 1, "dispatch")); err == nil {
		t.Fatal("malformed existing attempt was ignored")
	}
}

func TestAttemptStartRequiresExplicitRevision(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	a, env := attemptFixture(t, repo)
	input := attemptAssignment(env, 1, "dispatch")
	delete(input, "expected_state_revision")
	if _, err := prepareAttempt(a, env.Ticket, input); err == nil {
		t.Fatal("missing expected_state_revision was treated as revision zero")
	}
}

func TestAttemptLivenessUsesProcessIncarnationAndCancellationConfirmation(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	a, env := attemptFixture(t, repo)
	rec, err := prepareAttempt(a, env.Ticket, attemptAssignment(env, 1, "dispatch-process"))
	if err != nil {
		t.Fatal(err)
	}
	start, alive := processStartIdentity(os.Getpid())
	if !alive {
		t.Fatal("test process has no start identity")
	}
	running, err := mutateAttempt(a, env.Ticket, rec.ID, 1, map[string]interface{}{
		"state":   "running",
		"runtime": map[string]interface{}{"kind": "process", "pid": os.Getpid(), "process_start": start},
	})
	if err != nil || attemptLiveness(running) != "live" {
		t.Fatalf("live incarnation not recognized: attempt=%+v err=%v", running, err)
	}
	running.Runtime["process_start"] = "different incarnation"
	if got := attemptLiveness(running); got != "reused" {
		t.Fatalf("PID reuse mismatch reported %q", got)
	}
	if _, err := mutateAttempt(a, env.Ticket, rec.ID, running.Revision, map[string]interface{}{"state": "cancelled"}); err == nil {
		t.Fatal("unconfirmed cancellation was accepted")
	}
	if _, err := mutateAttempt(a, env.Ticket, rec.ID, running.Revision, map[string]interface{}{"state": "cancelled", "termination_confirmed": true}); err != nil {
		t.Fatal(err)
	}
}

func attemptFixture(t *testing.T, _ string) (*apState, identity.Env) {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	env := identity.Env{Slug: "project", Branch: "main", Ticket: "ap-04", ProjectHome: project}
	home := filepath.Join(project, "tickets", env.Ticket)
	mustMkdirAll(t, home)
	mustWrite(t, filepath.Join(home, "index.json"), `{"id":"ap-04","control":null}`)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":2,"run_id":"run-04","revision":1,"ticket":"ap-04","branch":"main"}`)
	return &apState{slug: env.Slug, branch: env.Branch, ticket: env.Ticket, stateRoot: env.ProjectHome}, env
}

func attemptAssignment(env identity.Env, revision int64, key string) map[string]interface{} {
	return map[string]interface{}{
		"gate": "implement", "owner": "worker-1", "idempotency_key": key,
		"run_id": "run-04", "expected_state_revision": revision,
		"assignment": map[string]interface{}{
			"repo_path":             env.ProjectHome,
			"prohibited_operations": []interface{}{"push", "land"},
		},
	}
}

func jsonNumber(v int) string {
	b, _ := json.Marshal(v)
	return string(b)
}
