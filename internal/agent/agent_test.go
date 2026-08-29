package agent

import (
	"encoding/json"
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
}

// Claude Code has the same gate, recorded differently. This test replaces an
// assertion that claude had no such gate at all: it does, so an unattended
// worker dispatched into an untrusted directory sat on "Is this a project you
// trust?" forever — no log line, no verdict, a checkpoint that never advanced.
// The entry is not the answer, the flag is: Claude Code writes a project entry
// the first time it sees a directory and only flips the flag when a human
// accepts, so a presence check waves through precisely the directories that hang.
func TestClaudeUntrustedDirectoryIsCaughtBeforeTheWorkerHangs(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	const dir = "/Users/long/workspace/acme"

	if err := profiles["claude"].PreflightDir(dir); err == nil {
		t.Fatal("want a refusal when no trust record exists at all")
	}

	write := func(t *testing.T, accepted bool) {
		t.Helper()
		b, err := json.Marshal(map[string]any{"projects": map[string]any{
			dir:        map[string]any{"hasTrustDialogAccepted": accepted},
			"/other/p": map[string]any{"hasTrustDialogAccepted": true},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(t, false)
	err := profiles["claude"].PreflightDir(dir)
	if err == nil {
		t.Fatal("a recorded-but-unaccepted directory must still refuse")
	}
	for _, want := range []string{dir, "--dangerously-skip-permissions", "trust prompt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	write(t, true)
	if err := profiles["claude"].PreflightDir(dir); err != nil {
		t.Errorf("an accepted directory was still refused: %v", err)
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

func TestCurrentReadsGrokBeforeClaudeCodeEnv(t *testing.T) {
	t.Setenv("GROK_AGENT", "")
	t.Setenv("GROK_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if got := Current(); got != "" {
		t.Errorf("empty env: Current() = %q", got)
	}

	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-1")
	if got := Current(); got != "claude" {
		t.Errorf("claude session: Current() = %q", got)
	}

	t.Setenv("GROK_SESSION_ID", "gk-1")
	if got := Current(); got != "grok" {
		t.Errorf("both set: Current() = %q, want grok", got)
	}

	t.Setenv("GROK_SESSION_ID", "")
	t.Setenv("GROK_AGENT", "1")
	if got := Current(); got != "grok" {
		t.Errorf("GROK_AGENT: Current() = %q", got)
	}
}

func TestNamesListsEveryRegisteredAgent(t *testing.T) {
	got := strings.Join(Names(), ",")
	if got != "claude,codex,grok,omp" {
		t.Errorf("Names() = %q, want claude,codex,grok,omp", got)
	}
}

// ── Registry width: omp and codex ────────────────────────────────────────────

// The worker shape is what a foreman actually dispatches, and every agent has
// to render it with its own yolo flag and a safely quoted requirement. The
// apostrophe is the case that matters: the command line reaches
// `orca terminal create --command "<this>"` and is parsed by a shell.
func TestWorkerCommandRendersPerAgentWithSafeQuoting(t *testing.T) {
	const prompt = "/bbs:autopilot don't break checkout"
	for _, tc := range []struct{ name, want string }{
		{"claude", `claude --dangerously-skip-permissions '/bbs:autopilot don'\''t break checkout'`},
		{"grok", `grok --always-approve '/bbs:autopilot don'\''t break checkout'`},
		{"omp", `omp --auto-approve '/bbs:autopilot don'\''t break checkout'`},
		{"codex", `codex --dangerously-bypass-approvals-and-sandbox '/bbs:autopilot don'\''t break checkout'`},
	} {
		p, err := ByName(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.WorkerCommand(prompt); got != tc.want {
			t.Errorf("%s WorkerCommand:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

// The bug this pins: an agent with no --session-id used to render
// `omp  '<prompt>'` — a flag position with nothing in it — and have a uuid
// recorded against it that it had never heard of.
func TestNonMintingAgentsNeverRenderAFlaglessSessionSlot(t *testing.T) {
	for _, name := range Names() {
		p, err := ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		token := p.SessionToken("11111111-2222-3333-4444-555555555555", "/tmp/fm.sessions")
		for _, cmd := range []string{
			p.NewSessionCommand(token, "/bbs:foreman"),
			p.ResumeCommand(token, "/bbs:foreman"),
		} {
			if strings.Contains(cmd, "  ") {
				t.Errorf("%s rendered an empty argument slot: %q", name, cmd)
			}
			if !strings.HasSuffix(cmd, `'/bbs:foreman'`) {
				t.Errorf("%s lost the prompt: %q", name, cmd)
			}
		}
	}
}

func TestSessionTokenPicksTheStrongestHandleEachAgentSupports(t *testing.T) {
	const uuid, dir = "uuid-1", "/tmp/fm.sessions"
	for _, tc := range []struct{ name, want string }{
		{"claude", uuid}, // --session-id
		{"grok", uuid},   // --session-id
		{"omp", dir},     // no mint flag; --session-dir is the durable handle
		{"codex", ""},    // neither; resume falls back to "most recent"
	} {
		p, err := ByName(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.SessionToken(uuid, dir); got != tc.want {
			t.Errorf("%s SessionToken = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestForemanSessionCommandShapes(t *testing.T) {
	for _, tc := range []struct{ name, token, wantNew, wantResume string }{
		{
			"claude", "u1",
			`claude --session-id u1 '/bbs:foreman'`,
			`claude --resume u1 '/bbs:foreman'`,
		},
		{
			"grok", "u1",
			`grok --session-id u1 '/bbs:foreman'`,
			`grok --resume u1 '/bbs:foreman'`,
		},
		{
			// omp: private store, then "the most recent conversation in it".
			"omp", "/tmp/fm.sessions",
			`omp --session-dir '/tmp/fm.sessions' '/bbs:foreman'`,
			`omp --session-dir '/tmp/fm.sessions' --continue '/bbs:foreman'`,
		},
		{
			// codex: no handle at all — a fresh session is bare, and resume is
			// the `resume --last` subcommand.
			"codex", "",
			`codex '/bbs:foreman'`,
			`codex resume --last '/bbs:foreman'`,
		},
	} {
		p, err := ByName(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.NewSessionCommand(tc.token, "/bbs:foreman"); got != tc.wantNew {
			t.Errorf("%s new:\n got %s\nwant %s", tc.name, got, tc.wantNew)
		}
		if got := p.ResumeCommand(tc.token, "/bbs:foreman"); got != tc.wantResume {
			t.Errorf("%s resume:\n got %s\nwant %s", tc.name, got, tc.wantResume)
		}
	}
}

// omp reaches babysit's skills through skills.customDirectories, which is a
// FLAT list — the skills come out bare. A `/bbs:autopilot` prompt sent there
// resolves to no skill at all, and the worker starts cleanly and does nothing.
func TestSkillRefFollowsEachAgentsNamespacing(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"claude", "/bbs:autopilot"},
		{"grok", "/bbs:autopilot"},
		{"omp", "/autopilot"},
		{"codex", "/bbs:autopilot"},
	} {
		p, err := ByName(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.SkillRef("autopilot"); got != tc.want {
			t.Errorf("%s SkillRef = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A missing binary must name the exact fix, and for an agent whose skills need
// a separate install step the hint has to name that too — otherwise the worker
// comes up fine and then cannot resolve its own prompt, which reads as a hung
// ticket rather than a setup gap.
func TestPreflightNamesTheFixForEveryAgent(t *testing.T) {
	isolate(t) // empty PATH: nothing is installed
	for _, tc := range []struct {
		name string
		want []string
	}{
		{"claude", []string{"claude.com"}},
		{"grok", []string{"grok plugin install"}},
		{"omp", []string{"skills.customDirectories", "/autopilot, not /bbs:autopilot"}},
		{"codex", []string{"developers.openai.com/codex"}},
	} {
		p, err := ByName(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		err = p.Preflight()
		if err == nil {
			t.Fatalf("%s: Preflight passed on an empty PATH", tc.name)
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s Preflight message missing %q:\n%s", tc.name, want, err)
			}
		}
	}
}

// omp's install hint used to be `omp plugin install <git-url>`, which reports
// success under --dry-run and then fails for real with "package.json not
// found". Naming a fix that does not work is worse than naming none.
func TestOmpInstallHintDoesNotNameThePluginInstaller(t *testing.T) {
	p, err := ByName("omp")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.Install, "omp plugin install") {
		t.Errorf("omp hint names the npm-shaped installer, which fails on a Claude plugin repo:\n%s", p.Install)
	}
}

// Only agents that actually export a session marker may be detectable. omp and
// codex export none, and a plausible-looking guess would be silently wrong
// rather than silently safe.
func TestCurrentDetectsOnlyAgentsThatExportAMarker(t *testing.T) {
	for _, v := range []string{"GROK_AGENT", "GROK_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
		t.Setenv(v, "")
	}
	if got := Current(); got != "" {
		t.Errorf("Current() = %q with no markers set, want \"\"", got)
	}

	t.Setenv("CLAUDE_CODE_SESSION_ID", "abc")
	if got := Current(); got != "claude" {
		t.Errorf("Current() = %q, want claude", got)
	}

	// An omp or codex session started from a Claude Code terminal inherits
	// CLAUDE_CODE_SESSION_ID wholesale (verified by dumping omp's child env),
	// so grok's own marker has to win over the inherited one.
	t.Setenv("GROK_SESSION_ID", "def")
	if got := Current(); got != "grok" {
		t.Errorf("Current() = %q with both markers set, want grok — a nested session inherits the parent's", got)
	}
}
