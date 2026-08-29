// Package agent resolves which coding-agent CLI a foreman and its workers run
// on. Claude Code is the default; grok, omp and codex are the other supported
// agents.
//
// A profile is data, not a template language: the CLIs differ in the binary
// name, the flag that turns off permission prompts, how (or whether) a
// conversation can be given a durable handle, and how they namespace babysit's
// skills. Everything else a spawn needs — the prompt, the Orca terminal around
// it — is identical.
//
// Two of those axes are where the registry stopped being a pure widening
// exercise, and both are load-bearing:
//
//   - Not every agent can mint a session id. claude and grok take
//     `--session-id <uuid>`; omp and codex have no such flag. An agent without
//     one must never render a flagless command line, and must not have a uuid
//     recorded against it that it has never heard of — a later resume would
//     hand it an id it cannot find. See MintsSessionID and SessionDir.
//   - Not every agent namespaces skills the same way. claude and grok read
//     babysit's plugin manifest and expose `bbs:autopilot`; omp finds skills
//     through `skills.customDirectories`, which is a flat list, so the same
//     skill is bare `autopilot` there. A `/bbs:autopilot` prompt sent to omp
//     resolves to nothing at all. See SkillPrefix and SkillRef.
//
// Foremen and workers are chosen separately, by two config keys that do not
// inherit from each other:
//
//	worker_agent:  which CLI runs the per-ticket workers
//	foreman_agent: which CLI runs the foreman itself
//
// The independence is the point. A foreman reviews design gates, reads QA
// verdicts, and decides whether a worker's evidence holds up; setting workers
// to grok is a throughput choice and must not silently move that audit off the
// stronger reasoner. So `worker_agent: grok` alone leaves the foreman on Claude
// Code — moving it takes saying `foreman_agent: grok` on purpose.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/reallongnguyen/babysit/internal/qaconfig"
	"github.com/reallongnguyen/babysit/internal/workspace"
)

// Default is the agent used when nothing selects one, for both roles. Claude
// Code is the agent every babysit skill was written against.
const Default = "claude"

// Config keys, in the repo's snake_case. Both roles are named explicitly rather
// than one being the bare `agent`: an unqualified key would read as "the agent
// for everything", which is exactly the inheritance these two must not have.
const (
	WorkerKey  = "worker_agent"
	ForemanKey = "foreman_agent"
)

// Profile is one coding-agent CLI's spawn shape.
type Profile struct {
	// Name is the value users write in config or pass to --agent.
	Name string
	// Bin is the executable looked up on PATH.
	Bin string
	// Yolo is the flag that stops the agent asking for tool approval. A worker
	// runs unattended in an Orca terminal nobody is watching keystroke by
	// keystroke, so without it the run stalls on the first edit. Foreman spawns
	// deliberately omit it — a foreman is attended, and the human at the
	// sidebar is the approver.
	Yolo string
	// Session is the flag binding a NEW conversation to a uuid we minted, and
	// Resume re-opens one by that uuid. claude and grok happen to spell these
	// the same; they are fields rather than constants so an agent that spells
	// them differently stays a registry entry instead of a code path. Empty
	// Session means the agent cannot be told which conversation to start —
	// see MintsSessionID, and never render Session unset.
	Session string
	// Resume re-opens a conversation by the token recorded for it. It is not
	// necessarily a flag: codex spells it as the subcommand `codex resume
	// <id>`, which renders identically because the token follows the word.
	Resume string
	// SessionDir is the flag pointing an agent at a private conversation store
	// (omp: --session-dir). It is the weaker durable handle for an agent that
	// cannot mint an id: give one foreman its own directory and "the most
	// recent conversation in there" is unambiguously that foreman's, which is
	// the property Continue alone does not have in a shared checkout.
	SessionDir string
	// Continue re-opens the most recent conversation without naming it (omp:
	// --continue, codex: `resume --last`). It is the fallback for an agent with
	// no Session, and is only trustworthy when paired with SessionDir.
	Continue string
	// SkillPrefix is how this agent namespaces babysit's skills in a prompt.
	// "bbs:" for agents that read the plugin manifest; "" for agents that
	// discover skills through a flat directory list and expose them bare.
	// Getting this wrong is silent: the agent starts fine and then resolves
	// the prompt to no skill at all.
	SkillPrefix string
	// Install is the hint printed when Bin is not on PATH. It names what to
	// install, and for agents with their own plugin store, what else they need
	// before a babysit skill prompt resolves.
	Install string
	// TrustFile, when non-empty, is a path under $HOME recording the directories
	// the agent has been told to trust. Agents that keep one refuse to touch an
	// unlisted directory until a human answers a prompt, which is fatal for an
	// unattended worker: the pane sits on a question nobody reads. Empty means
	// the agent has no such gate.
	TrustFile string
	// TrustHint says how to grant that trust, named in the preflight failure.
	TrustHint string
}

// profiles is the registry. Adding a third agent is an entry here plus a row in
// the docs — there is no per-agent code path anywhere else.
var profiles = map[string]Profile{
	"claude": {
		Name: "claude", Bin: "claude",
		Yolo:    "--dangerously-skip-permissions",
		Session: "--session-id", Resume: "--resume", Continue: "--continue",
		SkillPrefix: "bbs:",
		Install:     "install Claude Code: https://claude.com/product/claude-code",
		// --dangerously-skip-permissions answers the *tool* prompts, not the
		// folder-trust dialog: a first run in a directory Claude Code has not
		// been trusted in stops on "Is this a project you trust?" with no log
		// line, no verdict and a checkpoint that never advances — the quietest
		// way an unattended worker can die.
		TrustFile: ".claude.json",
		TrustHint: "run `claude` there once and accept the trust prompt, " +
			"or dispatch the worker from a directory you have already trusted",
	},
	"grok": {
		Name: "grok", Bin: "grok",
		Yolo:    "--always-approve",
		Session: "--session-id", Resume: "--resume",
		SkillPrefix: "bbs:",
		// The second half is the failure this hint exists to prevent: grok finds
		// babysit's skills in the babysit repo itself (they are project skills
		// there) and nowhere else, so a worker dispatched in a product repo
		// comes up fine and then cannot resolve its own prompt.
		Install: "install grok, then give it babysit's skills: " +
			"grok plugin install https://github.com/lohi-ai/babysit " +
			"(grok has its own plugin store — without that install, /bbs:autopilot is not a skill grok can see)",
		// grok's directory trust is separate from its permission mode: with
		// `permission_mode = "always-approve"` already set, a first run in an
		// unlisted directory still stops on "Do you trust the contents of this
		// directory?" and --always-approve does not answer it.
		TrustFile: ".grok/trusted_folders.toml",
		TrustHint: "run `grok` there once and answer the trust prompt, " +
			"or add a `[folders.\"<dir>\"]` stanza with `trusted = true`",
	},
	"omp": {
		Name: "omp", Bin: "omp",
		Yolo: "--auto-approve",
		// No --session-id: omp resumes by id-prefix or path only, so a uuid we
		// minted would name a conversation it has never heard of. --session-dir
		// is the handle that does work — one directory per foreman makes
		// --continue unambiguous. Verified by round-trip on omp v18.0.6.
		Session: "", Resume: "--resume",
		SessionDir: "--session-dir", Continue: "--continue",
		// omp discovers Claude *user* and *project* skills but not Claude
		// *plugin* skills, and skills reached through customDirectories are a
		// FLAT list — they come out bare, not namespaced. So a `/bbs:autopilot`
		// prompt resolves to nothing here while `/autopilot` works.
		SkillPrefix: "",
		// `omp plugin install <git-url>` looks like the fix and is not: it is an
		// npm-shaped installer and fails with "package.json not found" on a
		// Claude plugin repo (--dry-run reports success anyway, which is what
		// makes it a trap). customDirectories is the verified fix, and it points
		// at the marketplace checkout rather than plugins/cache/<version>
		// because that path is stable across upgrades.
		Install: "install omp, then point it at babysit's skills: " +
			`omp config set skills.customDirectories '["$HOME/.claude/plugins/marketplaces/babysit/.claude/skills"]' ` +
			"(omp does not scan ~/.claude/plugins, so without this the worker comes up and " +
			"then cannot resolve its own prompt; note omp exposes them bare — /autopilot, not /bbs:autopilot)",
	},
	"codex": {
		Name: "codex", Bin: "codex",
		// Flags are from OpenAI's published CLI reference; codex was not
		// installed on the machine this profile was written on, so rendering and
		// quoting are tested but a live spawn is not. Preflight() already fails
		// loudly with the install hint when the binary is absent, which is the
		// correct behaviour for an agent nobody has installed.
		Yolo: "--dangerously-bypass-approvals-and-sandbox",
		// codex has no mint flag either, and its resume is a SUBCOMMAND taking a
		// positional id (`codex resume <id>`) rather than a flag — which renders
		// identically, because the token follows the word either way.
		Session: "", Resume: "resume", Continue: "resume --last",
		SkillPrefix: "bbs:",
		Install: "install codex: https://developers.openai.com/codex/cli " +
			"(then confirm it can resolve a babysit skill prompt — codex's skill discovery is " +
			"unverified here, and a worker that cannot resolve /bbs:autopilot comes up fine and then stalls)",
	},
}

// Names lists the registered agents, sorted, for error messages and docs.
func Names() []string {
	out := make([]string, 0, len(profiles))
	for n := range profiles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Resolve picks the agent for a role — WorkerKey or ForemanKey — in precedence
// order:
//
//	--agent <name>              the caller's explicit override
//	BABYSIT_AGENT               this shell / this run
//	<repo>/.babysit/config.yaml the repo's committed default
//	~/.babysit/config.yaml      this machine's default
//	claude                      the built-in default
//
// The repo file is committed and the global one is not, so the order lets a
// machine without a given CLI installed opt out without editing a tracked file,
// and lets a repo state a team default without assuming every machine matches.
//
// An unrecognized name is an error here rather than in RepoConfig.Validate, and
// that placement is load-bearing: validating the enum at load time would make a
// repo that pins a newer agent break every command that reads repo config on an
// older bbs. Failing at the point of use keeps the blast radius at the spawn
// that actually needs the name.
func Resolve(key, flag string) (Profile, error) {
	name, source := selectName(key, flag)
	p, ok := profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown agent %q (from %s) — known agents: %s",
			name, source, strings.Join(Names(), ", "))
	}
	return p, nil
}

// currentEnv maps an agent to env vars that prove this process is running
// inside it. Only agents that actually export a marker appear here, and the
// list is short on purpose — see Current for why guessing is worse than "".
//
// Order is precedence, and claude is deliberately LAST because these
// environments nest: an omp session started from a Claude Code terminal
// inherits CLAUDECODE=1 and CLAUDE_CODE_SESSION_ID wholesale (verified by
// dumping the child env of `omp -p` under Claude Code). Claude Code's marker is
// therefore the one most likely to be somebody else's leftover, so anything
// with a marker of its own has to be tested first.
var currentEnv = []struct {
	name string
	vars []string
}{
	{"grok", []string{"GROK_AGENT", "GROK_SESSION_ID"}},
	{"claude", []string{"CLAUDE_CODE_SESSION_ID"}},
}

// Current names the CLI this process is running inside, or "".
//
// omp and codex are absent by design, not by omission: neither exports a
// session marker (probed — omp's child environment carries only Orca's and the
// parent CLI's vars), so there is nothing to detect them by. Inventing a
// plausible-looking variable would be worse than not detecting them at all,
// because the failure would be silent and wrong rather than silent and safe.
//
// The cost of "" is bounded and the callers are built for it: resolveGoalAgent
// falls through to BABYSIT_AGENT and then to the configured worker_agent, which
// is a stated preference rather than a guess. And every session babysit itself
// spawns is stamped with BABYSIT_AGENT=<name>, which outranks this function —
// so a nested `--auto` inside a babysit-spawned omp worker resolves to omp even
// though Current() cannot see it. The only case left is a human-started omp or
// codex session, where the configured default is the right answer anyway.
func Current() string {
	for _, c := range currentEnv {
		for _, v := range c.vars {
			if os.Getenv(v) != "" {
				return c.name
			}
		}
	}
	return ""
}

// ByName resolves a recorded agent name with no config ladder. Spawn uses it on
// the resume path: the conversation being re-opened was minted by a specific
// CLI, and the config may have changed since. Resuming a Claude session with
// grok would hand it a uuid it has never heard of.
func ByName(name string) (Profile, error) {
	p, ok := profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown agent %q — known agents: %s",
			name, strings.Join(Names(), ", "))
	}
	return p, nil
}

// selectName returns the winning name and where it came from, so an error can
// tell the user which of the four places to go fix.
//
// BABYSIT_AGENT is shared by both roles on purpose: it is the "just this run,
// on this machine" override, and a run that sets it means it.
func selectName(key, flag string) (name, source string) {
	if flag != "" {
		return flag, "--agent"
	}
	if v := os.Getenv("BABYSIT_AGENT"); v != "" {
		return v, "BABYSIT_AGENT"
	}
	if top := qaconfig.RepoToplevel(); top != "" {
		// A malformed or absent repo config is not this function's problem: the
		// commands that own that file report it. Here it simply does not select
		// an agent, and resolution falls through to the global default.
		if c, _, err := workspace.LoadRepoConfig(top); err == nil {
			if v := c.AgentFor(key); v != "" {
				return v, workspace.RepoConfigPath(top)
			}
		}
	}
	if v, _ := config.Get(key); v != "" {
		return v, config.Path()
	}
	return Default, "built-in default"
}

// Preflight reports whether this agent can actually be spawned. It runs before
// the terminal is created, because Orca happily opens a terminal on a command
// that does not exist: the human gets a pane containing "command not found"
// and a session that never reports, which reads as a hung ticket rather than a
// missing binary.
func (p Profile) Preflight() error {
	if _, err := exec.LookPath(p.Bin); err != nil {
		return fmt.Errorf("agent %q needs %q on PATH — %s", p.Name, p.Bin, p.Install)
	}
	return nil
}

// PreflightDir reports whether this agent will start in dir without first
// asking a question no unattended worker can answer. It is the second half of
// Preflight: a binary on PATH still hangs on the trust prompt, and a hung pane
// is the failure that costs a whole overnight batch.
//
// Trust is checked for the exact directory only. A trusted parent is not read as
// covering a child: guessing permissively re-creates the hang this exists to
// prevent, while guessing strictly costs one refusal that names a one-time fix.
func (p Profile) PreflightDir(dir string) error {
	if p.TrustFile == "" || dir == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // cannot locate the record; let the agent speak for itself
	}
	b, err := os.ReadFile(filepath.Join(home, p.TrustFile))
	if err != nil {
		// No trust file at all means the agent has never run anywhere, which is
		// exactly the state that hangs — report it rather than hoping.
		return p.untrusted(dir)
	}
	// grok resolves symlinks before recording, so compare on the physical path
	// (on macOS /tmp and /var are symlinked, and a worktree may be too).
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	trusted := trustedIn
	if strings.HasSuffix(p.TrustFile, ".json") {
		trusted = trustedInClaudeJSON
	}
	if trusted(string(b), dir) {
		return nil
	}
	return p.untrusted(dir)
}

func (p Profile) untrusted(dir string) error {
	return fmt.Errorf("agent %q has not been told to trust %s — it would stop there on a "+
		"trust prompt that no unattended worker can answer (%s is separate from %s). Fix: %s",
		p.Name, dir, p.TrustFile, p.Yolo, p.TrustHint)
}

// trustedIn scans the trust file for dir's stanza. The file is machine-written
// TOML in a fixed shape, so it is scanned rather than parsed — a TOML dependency
// for one lookup of one key would cost more than it explains.
func trustedIn(body, dir string) bool {
	header := `[folders."` + dir + `"]`
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) != header {
			continue
		}
		// Read this stanza's keys, stopping at the next table header.
		for _, kv := range lines[i+1:] {
			kv = strings.TrimSpace(kv)
			if strings.HasPrefix(kv, "[") {
				break
			}
			if strings.HasPrefix(kv, "trusted") {
				return strings.HasSuffix(kv, "true")
			}
		}
		return false
	}
	return false
}

// trustedInClaudeJSON reads ~/.claude.json, where Claude Code records one
// entry per directory it has opened. The entry existing is not the answer:
// it is written on first sight and the flag only flips once a human accepts
// the dialog, so most recorded projects are untrusted. Read the flag.
func trustedInClaudeJSON(body, dir string) bool {
	var doc struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return false
	}
	return doc.Projects[dir].HasTrustDialogAccepted
}

// WorkerCommand renders the shell command line that runs one worker on the
// given prompt, e.g. `/bbs:autopilot ship the settings page`.
func (p Profile) WorkerCommand(prompt string) string {
	return p.Bin + " " + p.Yolo + " " + shellQuote(prompt)
}

// MintsSessionID reports whether a NEW conversation can be bound to a handle
// this side chooses. When it is false the caller must not mint a uuid and
// record it: the agent has never heard of that id and the next resume would
// hand it one it cannot find. SessionToken says what to record instead.
func (p Profile) MintsSessionID() bool { return p.Session != "" }

// SessionToken is the durable handle to record for a foreman's conversation,
// given a freshly minted uuid and a private directory this foreman may own.
// Three shapes, strongest first:
//
//	uuid  — the agent takes --session-id (claude, grok)
//	dir   — the agent only has a private session store (omp)
//	""    — neither; resume falls back to "the most recent conversation" (codex)
//
// The caller supplies both candidates rather than this deciding how to build a
// path, so the directory stays the caller's layout concern.
func (p Profile) SessionToken(uuid, dir string) string {
	switch {
	case p.MintsSessionID():
		return uuid
	case p.SessionDir != "":
		return dir
	}
	return ""
}

// NewSessionCommand starts a fresh conversation carrying the durable handle
// SessionToken chose, and ResumeCommand re-opens it. These are the foreman's
// spawn shapes: no Yolo, because a foreman is attended.
func (p Profile) NewSessionCommand(session, prompt string) string {
	return p.Bin + p.sessionArgs(session, false) + " " + shellQuote(prompt)
}

func (p Profile) ResumeCommand(session, prompt string) string {
	return p.Bin + p.sessionArgs(session, true) + " " + shellQuote(prompt)
}

// sessionArgs renders the session half of a foreman command line, and its one
// hard rule is that it never emits a flag with nothing after it. An agent with
// no Session used to render `omp  '<prompt>'` — two spaces where a uuid should
// have been — which starts a conversation nobody can ever find again.
func (p Profile) sessionArgs(session string, resume bool) string {
	switch {
	case p.MintsSessionID() && session != "":
		flag := p.Session
		if resume {
			flag = p.Resume
		}
		return " " + flag + " " + session
	case p.SessionDir != "" && session != "":
		// The directory is a path we built, but it still reaches a shell.
		out := " " + p.SessionDir + " " + shellQuote(session)
		if resume && p.Continue != "" {
			out += " " + p.Continue
		}
		return out
	case resume && p.Continue != "":
		return " " + p.Continue
	}
	return ""
}

// SkillRef renders a babysit skill invocation the way THIS agent resolves it —
// `/bbs:autopilot` where the plugin manifest is read, `/autopilot` where skills
// arrive through a flat directory list. Every prompt naming a skill must go
// through here; a hard-coded `/bbs:` prefix is the failure that comes up fine
// and then resolves to nothing.
func (p Profile) SkillRef(skill string) string {
	return "/" + p.SkillPrefix + skill
}

// shellQuote wraps s in single quotes, ending and reopening the quoted run
// around each embedded single quote — the only escape that is safe inside POSIX
// single quotes, where backslash is literal. It matters because a free-text
// requirement reaches `orca terminal create --command "<this>"` and is parsed
// by a shell: an unquoted apostrophe in "don't break checkout" would truncate
// the requirement at best and split the command line at worst.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
