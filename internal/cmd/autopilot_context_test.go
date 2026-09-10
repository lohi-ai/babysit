package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextProjectionIsBoundedKeepsRequiredReadsAndRedactsLogs(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "ap-05")
	mustMkdirAll(t, filepath.Join(home, "attempts"))
	mustMkdirAll(t, filepath.Join(home, "logs"))
	requirement := "MUST KEEP THIS FIRST\n" + strings.Repeat("requirement detail\n", 500) + "MUST KEEP THIS LAST\n"
	plan := "PLAN START\n" + strings.Repeat("plan detail\n", 500) + "PLAN END\n"
	mustWrite(t, filepath.Join(home, "index.json"), `{"id":"ap-05","origin":{"type":"standalone"},"control":null}`)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":2,"run_id":"run-05","revision":3,"ticket":"ap-05","workflow":"builder","branch":"main","active_attempt_id":"implement-1"}`)
	mustWrite(t, filepath.Join(home, "requirement.md"), requirement)
	mustWrite(t, filepath.Join(home, "plan.md"), plan)
	mustWrite(t, filepath.Join(home, "logs", "worker.log"), "normal line\nAPI_TOKEN=do-not-leak\nfinished\n")
	mustWrite(t, filepath.Join(home, "attempts", "implement-1.json"), `{"schema_version":2,"id":"implement-1","ticket":"ap-05","run_id":"run-05","gate":"implement","state":"waiting","revision":2,"owner":"worker","idempotency_key":"key","assignment":{"prompt":"TOP SECRET SOURCE BODY","prohibited_operations":["push"]},"runtime":{"kind":"native","handle":"h","transport_id":"t"},"result":{"log_path":"logs/worker.log","raw_output":"DO NOT EMIT"},"created_at":"2026-09-10T00:00:00Z","updated_at":"2026-09-10T00:01:00Z"}`)
	a := &apState{slug: "project", branch: "main", ticket: "ap-05", stateRoot: project}

	snapshot, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	projection := buildContextProjection(snapshot)
	total := 0
	foundRequired := false
	for _, artifact := range projection.Artifacts {
		total += len(artifact.Excerpt)
		if artifact.Role == "requirement" {
			foundRequired = artifact.RequiresRead && artifact.Truncated && strings.Contains(artifact.Excerpt, "MUST KEEP THIS FIRST") && strings.Contains(artifact.Excerpt, "MUST KEEP THIS LAST")
		}
	}
	if total > contextArtifactBudget || !foundRequired {
		t.Fatalf("bounded required artifact contract failed: bytes=%d artifacts=%+v", total, projection.Artifacts)
	}
	if len(projection.Logs) != 1 || strings.Contains(projection.Logs[0].Excerpt, "do-not-leak") || !strings.Contains(projection.Logs[0].Excerpt, "[REDACTED]") {
		t.Fatalf("safe log excerpt contract failed: %+v", projection.Logs)
	}
	if got := stringValue(snapshot.ActiveAttempt["liveness"]); got != "unknown" {
		t.Fatalf("native liveness must remain unknown without adapter evidence, got %q", got)
	}
	encoded, _ := json.Marshal(snapshot.ActiveAttempt)
	if strings.Contains(string(encoded), "TOP SECRET") || strings.Contains(string(encoded), "DO NOT EMIT") {
		t.Fatalf("snapshot leaked unbounded attempt bodies: %s", encoded)
	}
}

func TestContextCursorDeltaAndCacheAreBounded(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "ap-05")
	mustMkdirAll(t, home)
	mustWrite(t, filepath.Join(home, "index.json"), `{"id":"ap-05","origin":{"type":"standalone"},"control":null}`)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"schema_version":2,"run_id":"run-05","revision":3,"ticket":"ap-05","workflow":"builder","branch":"main"}`)
	mustWrite(t, filepath.Join(home, "requirement.md"), "requirement\n")
	mustWrite(t, filepath.Join(home, "plan.md"), "plan\n")
	a := &apState{slug: "project", branch: "main", ticket: "ap-05", stateRoot: project}

	snapshot, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	projection := buildContextProjection(snapshot)
	cursor := contextCursor("ap-05", "run-05", projection)
	rec := contextCacheRecord{SchemaVersion: 2, Ticket: "ap-05", RunID: "run-05", Projection: projection}
	if err := writeContextCache(a, "ap-05", rec, cursor); err != nil {
		t.Fatal(err)
	}
	prior, reason := readContextCache(a, "ap-05", "run-05", cursor)
	if prior == nil || reason != "" {
		t.Fatalf("cursor did not round-trip: reason=%q", reason)
	}
	changes := contextChanges(prior.Projection, projection)
	encoded, _ := json.Marshal(contextPacket{Kind: "delta", Cursor: cursor, SnapshotID: snapshot.SnapshotID, StateRevision: snapshot.StateRevision, Changes: changes})
	if len(changes) != 0 || len(encoded) > 512 {
		t.Fatalf("unchanged delta is not bounded: bytes=%d changes=%#v", len(encoded), changes)
	}
	if got, reason := readContextCache(a, "ap-05", "other-run", cursor); got != nil || reason != "cursor_scope_changed" {
		t.Fatalf("cross-run cursor was accepted: record=%+v reason=%q", got, reason)
	}
	if got, reason := readContextCache(a, "ap-05", "run-05", "garbage"); got != nil || reason != "unknown_cursor" {
		t.Fatalf("unknown cursor result: record=%+v reason=%q", got, reason)
	}

	mustWrite(t, filepath.Join(home, "plan.md"), "changed plan\n")
	changedSnapshot, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	changed := contextChanges(projection, buildContextProjection(changedSnapshot))
	if changed["snapshot"] == nil || changed["artifacts"] == nil {
		t.Fatalf("artifact change absent from delta: %#v", changed)
	}

	for i := 0; i < contextCacheLimit+3; i++ {
		name := "v2." + strings.Repeat(string(rune('a'+i)), 32)
		if err := writeContextCache(a, "ap-05", rec, name); err != nil {
			t.Fatal(err)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(home, "cache", "context", "v2.*.json"))
	if len(paths) > contextCacheLimit {
		t.Fatalf("context cache grew to %d entries", len(paths))
	}
}

func TestContextNoTicketDoesNotCreateCache(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	project := filepath.Join(t.TempDir(), "missing")
	a := &apState{slug: "project", branch: "main", stateRoot: project}
	snapshot, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = buildContextProjection(snapshot)
	if _, err := os.Stat(project); !os.IsNotExist(err) {
		t.Fatalf("no-ticket context initialized state: %v", err)
	}
}
