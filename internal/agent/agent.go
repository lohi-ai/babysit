// Package agent resolves which coding-agent CLI a foreman and its workers run
// on. Claude Code is the default; grok is the second supported agent.
//
// The two CLIs are close enough that a profile is data, not a template
// language: they differ only in the binary name and the flag that turns off
// permission prompts. Everything else a spawn needs — the `/bbs:<skill>`
// prompt, the session flags, the cmux workspace around it — is identical,
// because grok reads babysit's plugin manifest and namespaces its skills the
// same way.
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
	// runs unattended in a cmux workspace nobody is watching keystroke by
	// keystroke, so without it the run stalls on the first edit. Foreman spawns
	// deliberately omit it — a foreman is attended, and the human at the
	// sidebar is the approver.
	Yolo string
	// Session is the flag binding a NEW conversation to a uuid we minted, and
	// Resume re-opens one by that uuid. Both CLIs happen to spell these the
	// same; they are fields rather than constants so a third agent that spells
	// them differently stays a registry entry instead of a code path.
	Session string
	Resume  string
	// Install is the hint printed when Bin is not on PATH. It names what to
	// install, and for agents with their own plugin store, what else they need
	// before a `/bbs:` prompt resolves.
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
		Session: "--session-id", Resume: "--resume",
		Install: "install Claude Code: https://claude.com/product/claude-code",
	},
	"grok": {
		Name: "grok", Bin: "grok",
		Yolo:    "--always-approve",
		Session: "--session-id", Resume: "--resume",
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
// the workspace is created, because cmux happily opens a workspace on a command
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
	if trustedIn(string(b), dir) {
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

// WorkerCommand renders the shell command line that runs one worker on the
// given prompt, e.g. `/bbs:autopilot ship the settings page`.
func (p Profile) WorkerCommand(prompt string) string {
	return p.Bin + " " + p.Yolo + " " + shellQuote(prompt)
}

// NewSessionCommand starts a fresh conversation bound to a uuid we minted, and
// ResumeCommand re-opens it. These are the foreman's spawn shapes: no Yolo,
// because a foreman is attended.
func (p Profile) NewSessionCommand(session, prompt string) string {
	return p.Bin + " " + p.Session + " " + session + " " + shellQuote(prompt)
}

func (p Profile) ResumeCommand(session, prompt string) string {
	return p.Bin + " " + p.Resume + " " + session + " " + shellQuote(prompt)
}

// shellQuote wraps s in single quotes, ending and reopening the quoted run
// around each embedded single quote — the only escape that is safe inside POSIX
// single quotes, where backslash is literal. It matters because a free-text
// requirement reaches `cmux workspace create --command "<this>"` and is parsed
// by a shell: an unquoted apostrophe in "don't break checkout" would truncate
// the requirement at best and split the command line at worst.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
