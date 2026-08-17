package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/qaconfig"
)

// origPath is captured at load time, before any t.Setenv clobbers PATH, so the
// helpers that need real git or a real shell can put it back.
var origPath = os.Getenv("PATH")

// isolate cuts every input Resolve reads off from the developer's real machine:
// a temp dir for the global config, an empty PATH so RepoToplevel finds no git
// and no repo config is consulted, and no BABYSIT_AGENT inherited from the
// shell running the suite.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("BABYSIT_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("BABYSIT_AGENT", "")
}

// writeGlobal seeds ~/.babysit/config.yaml (redirected by BABYSIT_STATE_DIR).
func writeGlobal(t *testing.T, body string) {
	t.Helper()
	dir := os.Getenv("BABYSIT_STATE_DIR")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsToClaudeForBothRoles(t *testing.T) {
	isolate(t)
	for _, key := range []string{WorkerKey, ForemanKey} {
		p, err := Resolve(key, "")
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "claude" {
			t.Errorf("%s resolved to %q, want claude", key, p.Name)
		}
	}
}

// The whole reason the two keys exist. Moving workers to a cheaper agent is a
// throughput choice; the foreman audits their design gates and QA evidence, so
// it must stay put until someone says otherwise on purpose.
func TestWorkerAgentDoesNotDragTheForemanAlong(t *testing.T) {
	isolate(t)
	writeGlobal(t, "worker_agent: grok\n")

	worker, err := Resolve(WorkerKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if worker.Name != "grok" {
		t.Errorf("worker resolved to %q, want grok", worker.Name)
	}
	foreman, err := Resolve(ForemanKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if foreman.Name != "claude" {
		t.Errorf("`worker_agent: grok` moved the foreman to %q — it must stay claude", foreman.Name)
	}
}

func TestForemanAgentIsSelectableOnItsOwn(t *testing.T) {
	isolate(t)
	writeGlobal(t, "foreman_agent: grok\n")

	p, err := Resolve(ForemanKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "grok" {
		t.Errorf("foreman resolved to %q, want grok", p.Name)
	}
	// And it does not leak the other way either.
	w, err := Resolve(WorkerKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "claude" {
		t.Errorf("foreman_agent moved the workers to %q", w.Name)
	}
}

func TestPrecedenceFlagBeatsEnvBeatsGlobal(t *testing.T) {
	isolate(t)
	writeGlobal(t, "worker_agent: claude\n")

	if p, _ := Resolve(WorkerKey, ""); p.Name != "claude" {
		t.Fatalf("global config lost: %q", p.Name)
	}
	t.Setenv("BABYSIT_AGENT", "grok")
	if p, _ := Resolve(WorkerKey, ""); p.Name != "grok" {
		t.Errorf("BABYSIT_AGENT did not beat the global config")
	}
	if p, _ := Resolve(WorkerKey, "claude"); p.Name != "claude" {
		t.Errorf("--agent did not beat BABYSIT_AGENT")
	}
}

// The repo file is committed; the global one is not. A machine without a given
// CLI has to be able to opt out without editing tracked state.
func TestRepoConfigBeatsGlobalButNotEnv(t *testing.T) {
	isolate(t)
	repo := gitRepo(t)
	writeRepoConfig(t, repo, "workspace: acme\nworker_agent: grok\n")
	writeGlobal(t, "worker_agent: claude\n")

	if p, _ := Resolve(WorkerKey, ""); p.Name != "grok" {
		t.Errorf("repo config lost to the global default")
	}
	t.Setenv("BABYSIT_AGENT", "claude")
	if p, _ := Resolve(WorkerKey, ""); p.Name != "claude" {
		t.Errorf("BABYSIT_AGENT could not override the committed repo config")
	}
}

// An unknown name must fail where it is used, not where the file is read: an
// older bbs has to keep working in a repo that pins an agent it never heard of.
func TestUnknownAgentFailsWithTheSourceAndTheKnownNames(t *testing.T) {
	isolate(t)
	_, err := Resolve(WorkerKey, "gork")
	if err == nil {
		t.Fatal("want an error for an unknown agent")
	}
	for _, want := range []string{"gork", "--agent", "claude", "grok"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestRepoConfigWithAnUnknownAgentStillLoads(t *testing.T) {
	isolate(t)
	repo := gitRepo(t)
	writeRepoConfig(t, repo, "workspace: acme\nworker_agent: some-future-cli\n")

	_, err := Resolve(WorkerKey, "")
	if err == nil || !strings.Contains(err.Error(), "some-future-cli") {
		t.Fatalf("want the unknown name reported at resolve time, got %v", err)
	}
	// The foreman role reads a different key, so it is unaffected and still runs.
	if p, err := Resolve(ForemanKey, ""); err != nil || p.Name != "claude" {
		t.Errorf("an unknown worker agent broke foreman resolution: %v / %q", err, p.Name)
	}
}

func TestWorkerCommandCarriesTheYoloFlagPerAgent(t *testing.T) {
	claude, grok := profiles["claude"], profiles["grok"]
	if got := claude.WorkerCommand("/bbs:autopilot ship it"); got != `claude --dangerously-skip-permissions '/bbs:autopilot ship it'` {
		t.Errorf("claude worker command: %s", got)
	}
	if got := grok.WorkerCommand("/bbs:autopilot ship it"); got != `grok --always-approve '/bbs:autopilot ship it'` {
		t.Errorf("grok worker command: %s", got)
	}
}

// A requirement is free text and reaches a shell through
// `orca terminal create --command "<this>"`.
func TestWorkerCommandSurvivesAnApostropheInTheRequirement(t *testing.T) {
	got := profiles["grok"].WorkerCommand(`/bbs:autopilot don't break checkout`)
	if got != `grok --always-approve '/bbs:autopilot don'\''t break checkout'` {
		t.Errorf("apostrophe not escaped: %s", got)
	}
	// Round-trip it through a real shell: the agent must receive one argument
	// with the apostrophe intact.
	out := shOut(t, "set -- "+strings.TrimPrefix(got, "grok --always-approve ")+`; printf '%s|%s' "$#" "$1"`)
	if out != `1|/bbs:autopilot don't break checkout` {
		t.Errorf("shell parsed it as %q", out)
	}
}

// Foreman spawns are attended, so they carry no yolo flag — and the session
// flags are what make a foreman resumable.
func TestForemanCommandsBindAndResumeTheSession(t *testing.T) {
	p := profiles["grok"]
	if got := p.NewSessionCommand("uuid-1", "/bbs:foreman"); got != `grok --session-id uuid-1 '/bbs:foreman'` {
		t.Errorf("new session: %s", got)
	}
	if got := p.ResumeCommand("uuid-1", "/bbs:foreman"); got != `grok --resume uuid-1 '/bbs:foreman'` {
		t.Errorf("resume: %s", got)
	}
	if strings.Contains(p.NewSessionCommand("u", "/x"), p.Yolo) {
		t.Error("a foreman spawn must not skip permission prompts — it is attended")
	}
}

// A missing CLI has to be caught before Orca opens a terminal on it, and the
// message has to say what to install. For grok that includes the plugin store:
// the binary alone leaves it unable to resolve /bbs: prompts.
func TestPreflightNamesWhatToInstall(t *testing.T) {
	isolate(t)
	err := profiles["grok"].Preflight()
	if err == nil {
		t.Fatal("want a preflight failure with grok absent from PATH")
	}
	for _, want := range []string{"grok", "PATH", "grok plugin install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	dir := os.Getenv("PATH")
	if err := os.WriteFile(filepath.Join(dir, "grok"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := profiles["grok"].Preflight(); err != nil {
		t.Errorf("preflight failed with grok on PATH: %v", err)
	}
}

// gitRepo stands up a real git repo and moves the test into it, because repo
// config is reached through `git rev-parse --show-toplevel`. It returns the path
// git reports rather than the one t.TempDir gave: on macOS the temp dir is
// reached through a symlink and git answers with the physical path, so the two
// differ and only git's is where LoadRepoConfig will look.
func gitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("PATH", origPath)
	t.Chdir(t.TempDir())
	if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	top := qaconfig.RepoToplevel()
	if top == "" {
		t.Fatal("git reported no toplevel in a freshly initialized repo")
	}
	return top
}

func writeRepoConfig(t *testing.T, top, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(top, ".babysit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, ".babysit", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shOut runs a snippet through a real /bin/sh, which is the only honest way to
// assert that a quoted command line parses the way the code claims.
func shOut(t *testing.T, script string) string {
	t.Helper()
	t.Setenv("PATH", origPath)
	out, err := exec.Command("sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("sh -c %q: %v", script, err)
	}
	return string(out)
}

// The failure this pair of tests exists for: grok on PATH with
// `permission_mode = "always-approve"` already set still stops on "Do you trust
// the contents of this directory?" the first time it runs somewhere. Nobody is
// reading a worker's pane, so the ticket looks hung rather than blocked.
func TestUntrustedDirectoryIsCaughtBeforeTheWorkerHangs(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := profiles["grok"].PreflightDir("/Users/long/workspace/acme")
	if err == nil {
		t.Fatal("want a refusal for a directory absent from the trust file")
	}
	// The message has to name the directory, say why the yolo flag is not the
	// answer, and give the fix — it is read from a BLOCKED line, not a debugger.
	for _, want := range []string{"/Users/long/workspace/acme", "--always-approve", "trusted = true"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[folders.\"/Users/long/workspace/acme\"]\ntrusted = true\ndecided_at = 1785777942\n"
	if err := os.WriteFile(filepath.Join(home, ".grok", "trusted_folders.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := profiles["grok"].PreflightDir("/Users/long/workspace/acme"); err != nil {
		t.Errorf("a trusted directory was still refused: %v", err)
	}
	// Claude Code has no such gate, so it must never be refused on these grounds.
	if err := profiles["claude"].PreflightDir("/anywhere"); err != nil {
		t.Errorf("claude has no trust file and must not be gated: %v", err)
	}
}

func TestTrustScanReadsOnlyTheRequestedStanza(t *testing.T) {
	body := `[folders."/a"]
trusted = true
decided_at = 1

[folders."/b"]
trusted = false
decided_at = 2

[folders."/c"]
decided_at = 3
`
	cases := map[string]bool{
		"/a": true,  // trusted
		"/b": false, // explicitly declined — must not read /a's value
		"/c": false, // stanza with no verdict at all
		"/d": false, // absent
		// A trusted parent is deliberately not read as covering a child: a
		// permissive guess here brings the hang back.
		"/a/worktrees/x": false,
	}
	for dir, want := range cases {
		if got := trustedIn(body, dir); got != want {
			t.Errorf("trustedIn(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestNamesListsEveryRegisteredAgent(t *testing.T) {
	got := strings.Join(Names(), ",")
	if got != "claude,grok" {
		t.Errorf("Names() = %q, want claude,grok", got)
	}
}
