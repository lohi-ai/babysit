package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillRuntimeRecordsCorrelatedLocalEventsWithoutSource(t *testing.T) {
	state := t.TempDir()
	t.Setenv("BABYSIT_STATE_DIR", state)
	t.Setenv("BABYSIT_ANALYTICS_DIR", "")
	t.Setenv("BABYSIT_MODEL", "gpt-test")
	start := currentSkillRuntimeRecord("implement")
	start.Event, start.InvocationID, start.Session = "start", "skill-test", "skill-test"
	if err := appendSkillRuntime(start); err != nil {
		t.Fatal(err)
	}
	name, started := findSkillStart("skill-test")
	if name != "implement" || started.IsZero() {
		t.Fatalf("start correlation failed: name=%q started=%v", name, started)
	}
	end := currentSkillRuntimeRecord(name)
	end.Event, end.InvocationID, end.Session, end.Outcome = "end", "skill-test", "skill-test", "success"
	if err := appendSkillRuntime(end); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(state, "analytics", "skill-usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "\n") != 2 || strings.Contains(string(b), "requirement") {
		t.Fatalf("unexpected telemetry contents: %s", b)
	}
	var row map[string]interface{}
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(string(b)), "\n")[0]), &row); err != nil {
		t.Fatal(err)
	}
	usage := row["provider_usage"].(map[string]interface{})
	if usage["available"] != false || usage["input_tokens"] != nil || row["model"] != "gpt-test" {
		t.Fatalf("provider usage availability was fabricated: %#v", row)
	}
}

func TestSkillRuntimeTelemetryOffWritesNothing(t *testing.T) {
	state := t.TempDir()
	t.Setenv("BABYSIT_STATE_DIR", state)
	mustWrite(t, filepath.Join(state, "config.yaml"), "telemetry: off\n")
	rec := currentSkillRuntimeRecord("qa")
	rec.Event, rec.InvocationID = "start", "skill-off"
	if err := appendSkillRuntime(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "analytics", "skill-usage.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("telemetry=off wrote data: %v", err)
	}
}

func TestSkillRuntimeAcceptsOnlyObservedUsage(t *testing.T) {
	input, output := int64(12), int64(7)
	total := input + output
	rec := currentSkillRuntimeRecord("review-pr")
	rec.Usage = skillUsage{Available: true, InputTokens: &input, OutputTokens: &output, TotalTokens: &total}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"available":true`) || !strings.Contains(string(b), `"total_tokens":19`) {
		t.Fatalf("observed usage was not retained: %s", b)
	}
}

func TestSkillCommandIsRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"skill"})
	if err != nil || cmd.Name() != "skill" {
		t.Fatalf("skill command missing: cmd=%v err=%v", cmd, err)
	}
}
