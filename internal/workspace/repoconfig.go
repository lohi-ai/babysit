package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Repo type values. The enum is not decoration: RepoType decides whether
// `bbs ticket serve` fans out to sibling repos at all. In a monorepo the
// siblings of a ticket live in the same checkout, so there is nothing to fan
// out to and an unresolvable sibling path is expected rather than a problem;
// in a polyrepo it is the problem. See Resolver.FanOut.
const (
	TypeMonorepo = "monorepo"
	TypePolyrepo = "polyrepo"
)

// RepoConfig is <repo>/.babysit/config.yaml — a committed file, so every field
// here must be a fact about the repo that holds true on any machine. Paths do
// not qualify and live in the registry instead.
type RepoConfig struct {
	// Workspace is the back-pointer, and the only required field.
	Workspace string `yaml:"workspace"`
	// HarnessVersion is the babysit VERSION that setup-project last configured
	// this repo with. A pointer because null is a real, ordinary value: it is
	// the correct reading for every repo that exists today, and it must never
	// warn or block. Absent and explicitly null are both nil.
	HarnessVersion *string `yaml:"harness_version"`
	Name           string  `yaml:"name,omitempty"`
	Description    string  `yaml:"description,omitempty"`
	RepoType       string  `yaml:"repo_type,omitempty"`
	// WorkerAgent names the coding-agent CLI this repo's per-ticket workers run
	// on, and ForemanAgent the one its foreman runs on; empty means "not stated
	// here", which falls through to ~/.babysit/config.yaml and then to claude.
	//
	// Neither is validated against the known-agent list here. Doing so would
	// make a repo that pins a newer agent break every command that reads this
	// file on an older bbs; the check belongs at the spawn that needs the name.
	// See internal/agent.Resolve.
	WorkerAgent  string `yaml:"worker_agent,omitempty"`
	ForemanAgent string `yaml:"foreman_agent,omitempty"`
}

// AgentFor reads the agent field for a role key — the WorkerKey/ForemanKey
// constants in internal/agent, not repeated as a dependency here because that
// package imports this one. The two are separate keys with no inheritance:
// moving workers to a cheaper agent must not silently move the foreman's audit
// of their work along with it. An unrecognized key selects nothing, which
// resolves to the default rather than guessing a role.
func (c RepoConfig) AgentFor(key string) string {
	switch key {
	case "worker_agent":
		return c.WorkerAgent
	case "foreman_agent":
		return c.ForemanAgent
	}
	return ""
}

// RepoConfigPath is <toplevel>/.babysit/config.yaml.
//
// Note the basename collides with the global ~/.babysit/config.yaml that
// `bbs config` reads. The two are kept apart at the CLI, not here: `bbs config`
// stays global-only and this file is reached through `bbs config repo`.
func RepoConfigPath(toplevel string) string {
	return filepath.Join(toplevel, ".babysit", "config.yaml")
}

// LoadRepoConfig reads a repo's config. The bool reports presence.
//
// A repo with no config.yaml is the normal case, not an error — that is every
// repo that exists today, and AC3 forbids giving them a new failure mode. Only
// a file that exists and is malformed errors.
func LoadRepoConfig(toplevel string) (RepoConfig, bool, error) {
	var c RepoConfig
	if toplevel == "" {
		return c, false, nil
	}
	b, err := os.ReadFile(RepoConfigPath(toplevel))
	if err != nil {
		return c, false, nil
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, true, fmt.Errorf("%s: %w", RepoConfigPath(toplevel), err)
	}
	if err := c.Validate(); err != nil {
		return c, true, fmt.Errorf("%s: %w", RepoConfigPath(toplevel), err)
	}
	return c, true, nil
}

// Validate checks the fields a present file must get right. harness_version is
// deliberately not checked for presence: a hand-edited file that omits the key
// reads as null, which is a legal state.
func (c RepoConfig) Validate() error {
	if c.Workspace == "" {
		return fmt.Errorf("workspace: is required")
	}
	if err := ValidName(c.Workspace); err != nil {
		return err
	}
	switch c.RepoType {
	case "", TypeMonorepo, TypePolyrepo:
	default:
		return fmt.Errorf("repo_type %q: must be %s or %s", c.RepoType, TypeMonorepo, TypePolyrepo)
	}
	return nil
}

// Stale reports whether this repo was configured by an older harness than
// current. Null is never stale — it is unknown, and unknown must stay silent.
func (c RepoConfig) Stale(current string) bool {
	return c.HarnessVersion != nil && *c.HarnessVersion != "" && *c.HarnessVersion != current
}

// SaveRepoConfig writes a repo's config, creating .babysit/ if needed. The
// write is whole-file: this file is small, committed, and hand-editable, and
// the writer (setup-project, or `bbs config repo set`) owns it.
func SaveRepoConfig(toplevel string, c RepoConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	dir := filepath.Join(toplevel, ".babysit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(RepoConfigPath(toplevel), b, 0o644)
}
