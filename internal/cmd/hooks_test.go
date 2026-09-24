package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hooks_test.go covers the Go port of bin/hooks/{pre-tool-gate,session-writer}:
// the gate's deny/ask/pass matrix, stage classification (incl. .exe and
// git -C), Grok's deny-only contract, and the session writer's minting,
// ticket derivation, throttle, started_at preservation, and stale sweep.

// gateEnv isolates identity + state for one gate evaluation: a temp HOME and
// BABYSIT_HOME, an empty BABYSIT_PROJECT_HOME, and no inherited ticket env.
// Returns the project home (verdicts live under it) and the babysit home.
func gateEnv(t *testing.T) (projectHome, babysitHome string) {
	t.Helper()
	root := t.TempDir()
	projectHome = filepath.Join(root, "projects", "testslug")
	babysitHome = filepath.Join(root, "babysit")
	home := filepath.Join(root, "home")
	for _, d := range []string{projectHome, babysitHome, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("BABYSIT_HOME", babysitHome)
	t.Setenv("BABYSIT_PROJECT_HOME", projectHome)
	for _, k := range []string{
		"BABYSIT_TICKET", "BBS_TICKET", "CODEX_SESSION_ID", "CODEX_THREAD_ID",
		"GROK_SESSION_ID", "GROK_HOOK_EVENT", "CLAUDE_CODE_SESSION_ID",
	} {
		t.Setenv(k, "")
	}
	return projectHome, babysitHome
}

// gitRepo makes a one-commit repo on the given branch and returns its path.
func gitRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", branch)
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-qm", "init")
	return dir
}

// writeVerdict seeds <projectHome>/tickets/<ticket>/verdicts/<skill>.md.
func writeVerdict(t *testing.T, projectHome, ticketID, skill, body string) {
	t.Helper()
	dir := filepath.Join(projectHome, "tickets", ticketID, "verdicts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, skill+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gatePayload(command, workdir string) []byte {
	b, _ := json.Marshal(map[string]any{
		"tool_input": map[string]any{"command": command, "workdir": workdir},
		"cwd":        workdir,
	})
	return b
}

func TestClassifyGateStage(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"git push origin HEAD", "push"},
		{"git.exe push origin HEAD", "push"},
		{`git -C "/some/dir" push`, "push"},
		{"gh pr create --fill", "pr"},
		{"gh.exe pr create --fill", "pr"},
		{"glab mr create --fill", "pr"},
		{"gh pr merge 12", "merge"},
		{"glab mr merge 12", "merge"},
		{"ls -la", ""},
		{"git status", ""},
		{"echo git push", "push"}, // substring classifier, same as bash — not a parser
	}
	for _, c := range cases {
		if got := classifyGateStage(c.cmd); got != c.want {
			t.Errorf("classifyGateStage(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestGateNoCommandAndNonHardStage(t *testing.T) {
	gateEnv(t)
	if d := evaluateGate(nil); d.action != "pass" {
		t.Fatalf("empty payload = %v, want pass", d)
	}
	if d := evaluateGate(gatePayload("ls -la", "")); d.action != "pass" || d.reason != "not a hard-stage command" {
		t.Fatalf("non-hard-stage = %v, want pass/not a hard-stage command", d)
	}
}

func TestGateNoTicketPasses(t *testing.T) {
	gateEnv(t)
	repo := gitRepo(t, "main")
	d := evaluateGate(gatePayload("git push origin HEAD", repo))
	if d.action != "pass" || d.reason != "no ticket resolved" {
		t.Fatalf("no-ticket push = %v, want pass/no ticket resolved", d)
	}
}

func TestGateBadWorkdirDenies(t *testing.T) {
	gateEnv(t)
	d := evaluateGate(gatePayload("git push", filepath.Join(t.TempDir(), "does not exist")))
	if d.action != "deny" || !strings.Contains(d.reason, "cannot inspect working directory") {
		t.Fatalf("bad workdir = %v, want deny/cannot inspect", d)
	}
}

func TestGateEnvConflictDenies(t *testing.T) {
	gateEnv(t)
	t.Setenv("BABYSIT_TICKET", "bs-a")
	t.Setenv("BBS_TICKET", "bs-b")
	repo := gitRepo(t, "main")
	d := evaluateGate(gatePayload("git push", repo))
	if d.action != "deny" {
		t.Fatalf("env conflict = %v, want deny", d)
	}
}

// gateMatrix runs the verdict matrix: review-pr/qa verdict files under the
// project home, BABYSIT_TICKET pinned, payload workdir = temp repo.
func TestGateVerdictMatrix(t *testing.T) {
	projectHome, _ := gateEnv(t)
	t.Setenv("BABYSIT_TICKET", "bs-test")
	repo := gitRepo(t, "main")

	seed := func(review, qa, qaBody string) {
		t.Helper()
		th := filepath.Join(projectHome, "tickets", "bs-test")
		os.RemoveAll(th)
		// The ticket dir must exist or v2 readiness denies TICKET_NOT_FOUND.
		if err := os.MkdirAll(filepath.Join(th, "verdicts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if review != "" {
			writeVerdict(t, projectHome, "bs-test", "review-pr", "STATUS: "+review+"\n")
		}
		if qa != "" {
			writeVerdict(t, projectHome, "bs-test", "qa", "STATUS: "+qa+"\n"+qaBody)
		}
	}
	gate := func(command string) gateDecision {
		t.Helper()
		return evaluateGate(gatePayload(command, repo))
	}

	// push: only review-pr gates.
	seed("", "", "")
	if d := gate("git push origin HEAD"); d.action != "ask" {
		t.Fatalf("push no-verdict = %v, want ask", d)
	}
	seed("BLOCKED", "", "")
	if d := gate("git push origin HEAD"); d.action != "deny" {
		t.Fatalf("push blocked-review = %v, want deny", d)
	}
	seed("DONE", "", "")
	if d := gate("git push origin HEAD"); d.action != "pass" {
		t.Fatalf("push ready-review = %v, want pass", d)
	}

	// pr: review + qa both gate, then the qa evidence body.
	seed("DONE", "", "")
	if d := gate("gh pr create --fill"); d.action != "ask" {
		t.Fatalf("pr missing-qa = %v, want ask", d)
	}
	seed("DONE", "BLOCKED", "")
	if d := gate("gh pr create --fill"); d.action != "deny" {
		t.Fatalf("pr blocked-qa = %v, want deny", d)
	}
	// qa verdict claims PASS but cites no e2e evidence → ask.
	seed("DONE", "DONE", "VERDICT: PASS\nEVIDENCE: none\n")
	if d := gate("gh pr create --fill"); d.action != "ask" {
		t.Fatalf("pr thin-qa-evidence = %v, want ask", d)
	}
	// Real e2e evidence with an existing artifact → pass. The artifact is
	// seeded after seed() because seed wipes the ticket dir.
	seed("DONE", "DONE", "VERDICT: PASS\nEVIDENCE: browser journey ok, screenshot evidence/qa/shot.png\n")
	if err := os.MkdirAll(filepath.Join(projectHome, "tickets", "bs-test", "evidence", "qa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectHome, "tickets", "bs-test", "evidence", "qa", "shot.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := gate("gh pr create --fill"); d.action != "pass" {
		t.Fatalf("pr ready = %v, want pass", d)
	}

	// merge: same contract, plus the contradiction deny.
	seed("DONE", "DONE", "VERDICT: PASS\nRUBRIC: freshness=B\nEVIDENCE: browser journey ok, screenshot evidence/qa/shot.png\n")
	if d := gate("gh pr merge 12"); d.action != "deny" || !strings.Contains(d.reason, "contradicts its own rubric") {
		t.Fatalf("merge contradiction = %v, want deny/contradiction", d)
	}
	seed("DONE", "DONE", "VERDICT: PASS\nEVIDENCE: browser journey ok, screenshot evidence/qa/shot.png\n")
	if err := os.MkdirAll(filepath.Join(projectHome, "tickets", "bs-test", "evidence", "qa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectHome, "tickets", "bs-test", "evidence", "qa", "shot.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := gate("gh pr merge 12"); d.action != "pass" {
		t.Fatalf("merge ready = %v, want pass", d)
	}
}

// TestGateV2Readiness exercises the enforced path: a schema_version 2
// checkpoint makes the typed evaluator authoritative — missing/stale evidence
// denies, accepted current evidence passes, and legacy verdict files are
// never consulted.
func TestGateV2Readiness(t *testing.T) {
	gateEnv(t)
	repo, env := initReadinessFixture(t)
	t.Setenv("BABYSIT_TICKET", env.Ticket)
	t.Setenv("BABYSIT_PROJECT_HOME", env.ProjectHome)
	t.Chdir(repo)

	// Enforced with no accepted evidence → deny naming the missing gates.
	d := evaluateGate(gatePayload("gh pr create --fill", repo))
	if d.action != "deny" || !strings.Contains(d.reason, "missing") {
		t.Fatalf("v2 missing evidence = %v, want deny/missing", d)
	}

	// Accept current evidence for both gates → ready → pass, even with a
	// BLOCKED legacy verdict file present (v2 is authoritative).
	for _, row := range []struct{ gate, attempt string }{{"review-pr", "review-1"}, {"qa", "qa-1"}} {
		if _, err := setV2VerificationEvidence(env, evidenceForCurrentSnapshot(t, env, row.gate, row.attempt, 0)); err != nil {
			t.Fatalf("record %s evidence: %v", row.gate, err)
		}
	}
	writeVerdict(t, env.ProjectHome, env.Ticket, "review-pr", "STATUS: BLOCKED\n")
	if d := evaluateGate(gatePayload("gh pr create --fill", repo)); d.action != "pass" {
		t.Fatalf("v2 ready = %v, want pass", d)
	}

	// A commit after the evidence was accepted makes it stale → deny.
	runGit(t, repo, "commit", "--allow-empty", "-m", "new revision")
	if d := evaluateGate(gatePayload("gh pr create --fill", repo)); d.action != "deny" || !strings.Contains(d.reason, "head_sha") {
		t.Fatalf("v2 stale = %v, want deny/head_sha", d)
	}
}

func TestRunPreToolGateEmit(t *testing.T) {
	projectHome, _ := gateEnv(t)
	t.Setenv("BABYSIT_TICKET", "bs-test")
	repo := gitRepo(t, "main")
	writeVerdict(t, projectHome, "bs-test", "review-pr", "STATUS: BLOCKED\n")

	var out, errb bytes.Buffer
	runPreToolGate(gatePayload("git push", repo), &out, &errb)
	var dec struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
		t.Fatalf("decision JSON: %v\n%s", err, out.String())
	}
	if dec.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("decision = %q, want deny", dec.HookSpecificOutput.PermissionDecision)
	}

	// Pass prints nothing on stdout.
	out.Reset()
	errb.Reset()
	writeVerdict(t, projectHome, "bs-test", "review-pr", "STATUS: DONE\n")
	runPreToolGate(gatePayload("git push", repo), &out, &errb)
	if out.Len() != 0 {
		t.Fatalf("pass stdout = %q, want empty", out.String())
	}
	if !strings.Contains(errb.String(), "pre-tool-gate:") {
		t.Fatalf("pass stderr missing trail: %q", errb.String())
	}

	// Grok payload → {decision:"deny"} shape, never hookSpecificOutput.
	out.Reset()
	writeVerdict(t, projectHome, "bs-test", "review-pr", "STATUS: BLOCKED\n")
	grokPayload, _ := json.Marshal(map[string]any{
		"hookEventName": "PreToolUse",
		"toolInput":     map[string]any{"command": "git push", "workdir": repo},
	})
	runPreToolGate(grokPayload, &out, &errb)
	var grokDec struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(out.Bytes(), &grokDec); err != nil || grokDec.Decision != "deny" {
		t.Fatalf("grok decision = %q err=%v, want deny", out.String(), err)
	}
}

// ─── session-writer ────────────────────────────────────────────────────────

func sessionPayload(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	b, _ := json.Marshal(fields)
	return b
}

func readSession(t *testing.T, babysitHome, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(babysitHome, "sessions", name))
	if err != nil {
		t.Fatalf("read session %s: %v", name, err)
	}
	return string(b)
}

func TestSessionWriterMintsYAML(t *testing.T) {
	_, babysitHome := gateEnv(t)
	wt := filepath.Join(t.TempDir(), ".babysit", "worktrees", "bs-x_slug")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	runSessionWriter(sessionPayload(t, map[string]any{"session_id": "s1", "cwd": wt}))
	got := readSession(t, babysitHome, "cc-s1.yaml")
	for _, want := range []string{"version: 1", "session_id: cc-s1", "ticket: bs-x", "started_at:", "last_seen_at:", "cwd: " + wt} {
		if !strings.Contains(got, want) {
			t.Fatalf("session yaml missing %q:\n%s", want, got)
		}
	}
}

func TestSessionWriterAgentPrefixes(t *testing.T) {
	_, babysitHome := gateEnv(t)
	for agent, prefix := range map[string]string{"claude": "cc", "codex": "cx", "grok": "grok", "omp": "omp"} {
		runSessionWriter(sessionPayload(t, map[string]any{"session_id": "s-" + agent, "agent": agent, "cwd": t.TempDir()}))
		readSession(t, babysitHome, fmt.Sprintf("%s-s-%s.yaml", prefix, agent))
	}
}

func TestSessionWriterRejectsBadIDs(t *testing.T) {
	_, babysitHome := gateEnv(t)
	for _, sid := range []string{"", "..", "a/b", "a b", "x:yaml"} {
		runSessionWriter(sessionPayload(t, map[string]any{"session_id": sid, "cwd": t.TempDir()}))
	}
	entries, err := os.ReadDir(filepath.Join(babysitHome, "sessions"))
	if os.IsNotExist(err) {
		return // nothing minted at all — the strongest form of rejection
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".session.") {
			t.Fatalf("bad session id minted %s", e.Name())
		}
	}
}

func TestSessionWriterThrottleAndStartedAt(t *testing.T) {
	_, babysitHome := gateEnv(t)
	cwd := t.TempDir()
	payload := sessionPayload(t, map[string]any{"session_id": "s1", "cwd": cwd})

	runSessionWriter(payload)
	first := readSession(t, babysitHome, "cc-s1.yaml")

	// Within 60s: no rewrite — plant a marker the writer would overwrite.
	marked := first + "marker: 1\n"
	sf := filepath.Join(babysitHome, "sessions", "cc-s1.yaml")
	if err := os.WriteFile(sf, []byte(marked), 0o644); err != nil {
		t.Fatal(err)
	}
	runSessionWriter(payload)
	if got := readSession(t, babysitHome, "cc-s1.yaml"); got != marked {
		t.Fatalf("throttled write rewrote session file:\n%s", got)
	}

	// Older than 60s: rewrite preserves the original started_at.
	old := time.Now().Add(-61 * time.Second)
	if err := os.Chtimes(sf, old, old); err != nil {
		t.Fatal(err)
	}
	runSessionWriter(payload)
	got := readSession(t, babysitHome, "cc-s1.yaml")
	var started string
	for _, ln := range strings.Split(first, "\n") {
		if strings.HasPrefix(ln, "started_at:") {
			started = ln
		}
	}
	if !strings.Contains(got, started) {
		t.Fatalf("started_at not preserved: want %q in\n%s", started, got)
	}
	if strings.Contains(got, "marker: 1") {
		t.Fatalf("stale session not refreshed:\n%s", got)
	}
}

func TestSessionWriterBranchTicket(t *testing.T) {
	_, babysitHome := gateEnv(t)
	repo := gitRepo(t, "feat/bs-br_some-slug")
	runSessionWriter(sessionPayload(t, map[string]any{"session_id": "s-br", "cwd": repo}))
	if got := readSession(t, babysitHome, "cc-s-br.yaml"); !strings.Contains(got, "ticket: bs-br\n") {
		t.Fatalf("branch ticket not derived:\n%s", got)
	}
}

func TestSessionWriterSubTicketBranch(t *testing.T) {
	_, babysitHome := gateEnv(t)
	repo := gitRepo(t, "feat/bs-parent/001_bs-child_slug")
	runSessionWriter(sessionPayload(t, map[string]any{"session_id": "s-sub", "cwd": repo}))
	if got := readSession(t, babysitHome, "cc-s-sub.yaml"); !strings.Contains(got, "ticket: bs-child\n") {
		t.Fatalf("sub-ticket not derived:\n%s", got)
	}
}

func TestSessionWriterSweepsStale(t *testing.T) {
	_, babysitHome := gateEnv(t)
	sessDir := filepath.Join(babysitHome, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(sessDir, "cc-old.yaml")
	fresh := filepath.Join(sessDir, "cc-new.yaml")
	dot := filepath.Join(sessDir, ".hidden")
	for _, f := range []string{stale, fresh, dot} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-121 * time.Minute)
	for _, f := range []string{stale, dot} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	runSessionWriter(sessionPayload(t, map[string]any{"session_id": "s1", "cwd": t.TempDir()}))
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale session not swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh session swept")
	}
	if _, err := os.Stat(dot); err != nil {
		t.Fatal("dotfile swept")
	}
}
