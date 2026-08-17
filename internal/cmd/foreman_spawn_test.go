package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/foreman"
)

// fakeOrcaFor stands up a stub `orca` and returns its call log plus the file
// holding the open terminal titles. Spawn shells out for everything, so the
// stub is the only way to see the command it launched. `open` seeds titles
// that are already live; the stub appends whatever `terminal create` makes.
// Truncating the titles file is how a test closes them.
func fakeOrcaFor(t *testing.T, open ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	titles := filepath.Join(dir, "titles")
	if err := os.WriteFile(titles, []byte(strings.Join(open, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
PATH=/bin:/usr/bin
printf '%s\n' "$*" >> ` + log + `
titles=` + titles + `
case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true}}}' ;;
  open) echo '{"ok":true,"result":{}}' ;;
  repo) echo '{"ok":true,"result":{}}' ;;
  terminal)
    case "$2" in
      list)
        python3 -c '
import json
titles=open("'"$titles"'").read().splitlines()
terms=[{"handle":"term_%d"%i,"title":t,"connected":True,"worktreePath":"/repo"}
       for i,t in enumerate(titles) if t]
print(json.dumps({"ok":True,"result":{"terminals":terms}}))
' ;;
      create)
        title=""
        while [ $# -gt 0 ]; do
          if [ "$1" = "--title" ]; then title="$2"; fi
          shift
        done
        printf '%s\n' "$title" >> "$titles"
        python3 -c 'import json,sys; print(json.dumps({"ok":True,"result":{"terminal":{"handle":"term_new","title":sys.argv[1]}}}))' "$title"
        ;;
      send|close|show|read) echo '{"ok":true,"result":{}}' ;;
      *) echo '{"ok":true,"result":{}}' ;;
    esac ;;
  worktree) echo '{"ok":true,"result":{}}' ;;
  *) echo '{"ok":true,"result":{}}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "orca"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Spawn preflights the agent binary before creating a terminal, and PATH
	// below is only this dir — so the agents have to exist here or every spawn
	// fails on a missing CLI rather than exercising what the test is about.
	for _, bin := range []string{"claude", "grok"} {
		if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	// PATH holds no git either, so RepoToplevel finds nothing and agent
	// resolution never reads a repo config — leaving these tests to the global
	// config under BABYSIT_STATE_DIR, which is a temp dir per test.
	t.Setenv("BABYSIT_STATE_DIR", t.TempDir())
	t.Setenv("BABYSIT_AGENT", "")
	t.Setenv("ORCA_CLI_COMMAND", "")
	t.Setenv("ORCA_DEV_REPO_ROOT", "")
	t.Setenv("BABYSIT_HOME", t.TempDir())
	// A fake HOME so the grok trust preflight reads a file this test owns rather
	// than the developer's real ~/.grok — and so it starts out empty, which is
	// the state that would refuse a spawn. Tests that spawn grok call trustDir.
	t.Setenv("HOME", t.TempDir())
	return log, titles
}

// trustDir records dir as trusted for grok in the fake HOME, standing in for the
// one-time prompt a human answers. Spawn resolves symlinks before the lookup, so
// the stanza is written under the physical path (macOS temp dirs are symlinked).
func trustDir(t *testing.T, dir string) {
	t.Helper()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	gdir := filepath.Join(os.Getenv("HOME"), ".grok")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[folders.\"" + dir + "\"]\ntrusted = true\n"
	if err := os.WriteFile(filepath.Join(gdir, "trusted_folders.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A fresh spawn must bind the conversation to an id we chose, and persist it —
// without that there is nothing for a later run to resume.
func TestSpawnMintsAndRecordsTheSession(t *testing.T) {
	log, _ := fakeOrcaFor(t)

	if _, err := spawnForeman("fm-a", t.TempDir(), "", ""); err != nil {
		t.Fatal(err)
	}
	rec, err := foreman.Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Session == "" {
		t.Fatal("spawn did not record a session id")
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "--session-id "+rec.Session) {
		t.Errorf("spawn did not launch claude with the recorded session:\n%s", calls)
	}
	if strings.Contains(calls, "--resume") {
		t.Errorf("a first spawn has nothing to resume:\n%s", calls)
	}
	// A workspace that comes up on a bare `claude` is a Claude session sitting
	// in a repo, not a foreman.
	if !strings.Contains(calls, "/bbs:foreman") {
		t.Errorf("spawn did not open the session on the foreman skill:\n%s", calls)
	}
}

// The bug this whole path exists for: re-spawning a foreman whose workspace was
// closed used to demand a retire, which drops the record and with it the only
// pointer back to the conversation.
func TestSpawnResumesARecordedSessionWhenTheWorkspaceIsGone(t *testing.T) {
	log, titles := fakeOrcaFor(t)
	dir := t.TempDir()

	if _, err := spawnForeman("fm-a", dir, "", ""); err != nil {
		t.Fatal(err)
	}
	first, _ := foreman.Load("fm-a")

	// The human closes the workspace; the record survives.
	if err := os.WriteFile(titles, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := spawnForeman("fm-a", dir, "", ""); err != nil {
		t.Fatalf("re-spawn with a closed workspace should resume, got: %v", err)
	}
	again, _ := foreman.Load("fm-a")
	if again.Session != first.Session {
		t.Errorf("resume changed the session: %q -> %q", first.Session, again.Session)
	}
	if !strings.Contains(readCalls(t, log), "--resume "+first.Session+" '/bbs:foreman'") {
		t.Errorf("re-spawn did not resume the recorded session on the skill:\n%s", readCalls(t, log))
	}
}

// A workspace that is still open is a real collision, not a restart: resuming
// would put a second Claude on the same conversation.
func TestSpawnStillRefusesALiveWorkspace(t *testing.T) {
	fakeOrcaFor(t, "bbs foreman fm-a")

	if err := foreman.Save(foreman.Record{
		ID: "fm-a", WorkspaceTitle: "bbs foreman fm-a", Session: "abc", Heartbeat: foreman.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := spawnForeman("fm-a", t.TempDir(), "", "")
	if err == nil || !strings.Contains(err.Error(), "already registered and running") {
		t.Fatalf("want a live-workspace refusal, got %v", err)
	}
}

// An explicit --command is the caller's business; injecting session flags into
// it would corrupt whatever they asked for.
func TestSpawnLeavesAnExplicitCommandAlone(t *testing.T) {
	log, _ := fakeOrcaFor(t)

	if _, err := spawnForeman("fm-a", t.TempDir(), "bash -l", ""); err != nil {
		t.Fatal(err)
	}
	calls := readCalls(t, log)
	if strings.Contains(calls, "--session-id") || strings.Contains(calls, "--resume") ||
		strings.Contains(calls, "/bbs:foreman") {
		t.Errorf("session flags or the skill prompt leaked into an explicit command:\n%s", calls)
	}
}

// setGlobalAgent writes ~/.babysit/config.yaml (redirected to a temp dir by
// fakeOrcaFor) with a role key.
func setGlobalAgent(t *testing.T, key, name string) {
	t.Helper()
	p := filepath.Join(os.Getenv("BABYSIT_STATE_DIR"), "config.yaml")
	if err := os.WriteFile(p, []byte(key+": "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// `worker_agent:` selects the workers. The foreman audits their design gates and QA
// evidence, so it must not follow them onto a different CLI by implication.
func TestSpawnKeepsTheForemanOnClaudeWhenOnlyWorkersMoved(t *testing.T) {
	log, _ := fakeOrcaFor(t)
	setGlobalAgent(t, "worker_agent", "grok")

	if _, err := spawnForeman("fm-a", t.TempDir(), "", ""); err != nil {
		t.Fatal(err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "claude --session-id") {
		t.Errorf("`worker_agent: grok` moved the foreman off claude:\n%s", calls)
	}
	if rec, _ := foreman.Load("fm-a"); rec.Agent != "claude" {
		t.Errorf("recorded agent %q, want claude", rec.Agent)
	}
}

func TestSpawnHonorsForemanAgent(t *testing.T) {
	log, _ := fakeOrcaFor(t)
	setGlobalAgent(t, "foreman_agent", "grok")
	dir := t.TempDir()
	trustDir(t, dir)

	if _, err := spawnForeman("fm-a", dir, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readCalls(t, log), "grok --session-id") {
		t.Errorf("foreman_agent: grok did not launch grok:\n%s", readCalls(t, log))
	}
	if rec, _ := foreman.Load("fm-a"); rec.Agent != "grok" {
		t.Errorf("spawn did not pin the agent on the record: %q", rec.Agent)
	}
}

// grok stops on a directory-trust prompt the first time it runs somewhere, and
// its always-approve flag does not answer that one. Catch it before Orca opens a
// terminal, or the pane sits on a question nobody is watching for.
func TestSpawnRefusesADirectoryTheAgentWouldStopAndAskAbout(t *testing.T) {
	log, _ := fakeOrcaFor(t)
	setGlobalAgent(t, "foreman_agent", "grok")

	_, err := spawnForeman("fm-a", t.TempDir(), "", "") // HOME has no trust file
	if err == nil {
		t.Fatal("want a refusal for an untrusted directory")
	}
	if !strings.Contains(err.Error(), "trust") {
		t.Errorf("error does not explain the trust gate: %v", err)
	}
	if strings.Contains(readCalls(t, log), "terminal create") {
		t.Error("a terminal was created for an agent that would have hung in it")
	}
	// claude has no trust gate, so the same directory must spawn fine.
	setGlobalAgent(t, "foreman_agent", "claude")
	if _, err := spawnForeman("fm-b", t.TempDir(), "", ""); err != nil {
		t.Errorf("claude was gated by grok's trust rule: %v", err)
	}
}

// The agent is pinned at spawn and read back on resume. A session uuid only
// means something to the CLI that minted it, so a config change between spawn
// and resume must not redirect the resume.
func TestResumeUsesThePinnedAgentNotCurrentConfig(t *testing.T) {
	log, titles := fakeOrcaFor(t)
	dir := t.TempDir()
	trustDir(t, dir)
	setGlobalAgent(t, "foreman_agent", "grok")

	if _, err := spawnForeman("fm-a", dir, "", ""); err != nil {
		t.Fatal(err)
	}
	first, _ := foreman.Load("fm-a")
	if err := os.WriteFile(titles, nil, 0o644); err != nil { // human closes it
		t.Fatal(err)
	}
	setGlobalAgent(t, "foreman_agent", "claude") // config changes underneath

	if _, err := spawnForeman("fm-a", dir, "", ""); err != nil {
		t.Fatal(err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "grok --resume "+first.Session) {
		t.Errorf("resume did not use the agent that minted the session:\n%s", calls)
	}
	if strings.Contains(calls, "claude --resume") {
		t.Errorf("resumed a grok session with claude:\n%s", calls)
	}
}

// Contradicting the pin explicitly is a mistake worth naming: neither honoring
// the flag (a uuid the new agent never saw) nor ignoring it is what was meant.
func TestSpawnRefusesAnAgentThatContradictsThePinnedSession(t *testing.T) {
	_, titles := fakeOrcaFor(t)
	dir := t.TempDir()

	if _, err := spawnForeman("fm-a", dir, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(titles, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := spawnForeman("fm-a", dir, "", "grok")
	if err == nil || !strings.Contains(err.Error(), "cannot resume it as grok") {
		t.Fatalf("want a refusal naming the conflict, got %v", err)
	}
}

// A missing CLI must be caught before Orca opens a terminal on it — otherwise
// the human gets a pane reading "command not found" and a foreman that never
// reports, which looks like a hang rather than a missing binary.
func TestSpawnPreflightsTheAgentBeforeCreatingAWorkspace(t *testing.T) {
	log, _ := fakeOrcaFor(t)
	if err := os.Remove(filepath.Join(filepath.Dir(log), "grok")); err != nil {
		t.Fatal(err)
	}
	setGlobalAgent(t, "foreman_agent", "grok")

	_, err := spawnForeman("fm-a", t.TempDir(), "", "")
	if err == nil || !strings.Contains(err.Error(), "grok plugin install") {
		t.Fatalf("want a preflight failure naming the install, got %v", err)
	}
	if strings.Contains(readCalls(t, log), "terminal create") {
		t.Error("a terminal was created for an agent that is not installed")
	}
	if _, err := foreman.Load("fm-a"); err == nil {
		t.Error("a record was written for a foreman that could not launch")
	}
}

// The skill asks bbs for the worker command rather than hardcoding a CLI, so
// agent selection has exactly one codepath.
func TestWorkerCommandRendersTheConfiguredAgent(t *testing.T) {
	fakeOrcaFor(t)
	setGlobalAgent(t, "worker_agent", "grok")

	out := captureStdout(t, func() {
		if err := foremanWorkerCommand([]string{"--prompt", "/bbs:autopilot ship it"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.TrimSpace(out) != `grok --always-approve '/bbs:autopilot ship it'` {
		t.Errorf("worker-command printed %q", out)
	}
}

func TestWorkerCommandDefaultsToClaudeAndNeedsAPrompt(t *testing.T) {
	fakeOrcaFor(t)

	out := captureStdout(t, func() {
		if err := foremanWorkerCommand([]string{"--prompt", "/bbs:autopilot x"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "claude --dangerously-skip-permissions ") {
		t.Errorf("default worker agent is not claude: %q", out)
	}
	if err := foremanWorkerCommand([]string{"--agent", "grok"}); err == nil {
		t.Error("want an error when --prompt is missing")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = saved
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readCalls(t *testing.T, log string) string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A poke lands in a session that may be many compactions past the point where
// it last read the skill. Prose alone assumes a protocol the receiver has
// forgotten; the invocation reloads it.
func TestWakePrefixesThePokeWithTheForemanSkill(t *testing.T) {
	log, _ := fakeOrcaFor(t, "bbs foreman fm-a")
	if err := foreman.Save(foreman.Record{
		ID: "fm-a", WorkspaceTitle: "bbs foreman fm-a", Heartbeat: foreman.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	s := &dashServer{stateDir: t.TempDir()}
	if got := s.wake("fm-a", "ticket bs-x was assigned to you."); got.state != "sent" {
		t.Fatalf("wake did not land: %+v", got)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "--text /bbs:foreman ticket bs-x was assigned to you.") {
		t.Errorf("poke was not prefixed with the skill:\n%s", calls)
	}
}

// A foreman is bound to one project. Resuming from wherever the human happened
// to be standing must not silently re-point it at that directory.
func TestResumeKeepsTheRecordedDirWhenNoneIsGiven(t *testing.T) {
	_, titles := fakeOrcaFor(t)
	bound := t.TempDir()

	if _, err := spawnForeman("fm-a", bound, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(titles, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := spawnForeman("fm-a", "", "", ""); err != nil {
		t.Fatal(err)
	}
	rec, _ := foreman.Load("fm-a")
	if rec.WorkspaceDir != bound {
		t.Errorf("resume re-pointed the foreman: %q, want %q", rec.WorkspaceDir, bound)
	}
}
