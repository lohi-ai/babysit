// Package workspace is the multi-repo registry: a named workspace owns a list
// of repos, and each repo declares its membership in a committed
// <repo>/.babysit/config.yaml.
//
// # Three things are called "workspace" in this codebase
//
// Only one of them lives here. Keep them apart when writing code, CLI output,
// or docs:
//
//  1. Orca terminal — a sidebar tab with its own agent session, what the
//     foreman skill drives. Held by foreman.Record's WorkspaceDir/WorkspaceRef/
//     WorkspaceTitle. Always written "Orca terminal" in output.
//  2. .babysit/worktrees/ — the per-ticket git worktree pool under
//     mode: worktree. Always written "worktree" in output, never "workspace".
//  3. this package — a named set of repos. Always written with its name
//     attached ("workspace acme"), never bare.
//
// This package deliberately adds no field to foreman.Record. A foreman already
// records the repo it works in (ProjectDir); the workspace it belongs to is
// derived from that repo's config.yaml, so there is exactly one place a
// membership is written down.
//
// # What is stored where
//
// The split is machine-locality. A git url is stable and machine-independent;
// a local folder path is neither and must never reach a committed file. So the
// repo list — which carries paths — lives in global state at
// ~/.babysit/workspaces/<name>.yaml, and the committed config.yaml carries only
// the back-pointer plus facts about the repo itself.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"gopkg.in/yaml.v3"
)

// Repo is one member of a workspace. GitURL is the identity — it is what makes
// an entry meaningful on another machine. Path is where this machine happens to
// keep it and may be empty (a workspace may list a repo that isn't cloned here).
// Role is the optional sibling role that RELATED_*_REPO used to be the only
// answer for; see Resolver.
type Repo struct {
	GitURL string `yaml:"git_url"`
	Path   string `yaml:"path,omitempty"`
	Role   string `yaml:"role,omitempty"`
}

// Workspace is the registry file: ~/.babysit/workspaces/<name>.yaml.
type Workspace struct {
	Version int    `yaml:"version"`
	Name    string `yaml:"name"`
	Repos   []Repo `yaml:"repos"`
}

// Dir is ~/.babysit/workspaces.
//
// The test guard is load-bearing, not a nicety. identity.BabysitHome() honors
// only BABYSIT_HOME while config.Dir() honors only BABYSIT_STATE_DIR, and
// neither reads the other: a test that redirects state the config way and then
// touches this store would write into the human's real ~/.babysit/workspaces
// and corrupt live state. Enforcing it here rather than in a helper each test
// must remember to call is the point — a future test cannot opt out of it.
func Dir() string {
	if testing.Testing() && os.Getenv("BABYSIT_HOME") == "" {
		panic("workspace: a test touched the workspace store without BABYSIT_HOME set — " +
			"call workspace.TestHome(t) (BABYSIT_STATE_DIR does NOT redirect this store)")
	}
	return filepath.Join(identity.BabysitHome(), "workspaces")
}

// Path is the registry file for one workspace name. Only ever call it with a
// name that passed ValidName — Join CLEANS its result, so a name carrying "../"
// resolves outside the workspaces directory entirely.
func Path(name string) string { return filepath.Join(Dir(), name+".yaml") }

// nameRe is what a workspace name may look like: a leading alphanumeric, then
// word characters, dot or dash. That excludes "/" and any leading dot, which is
// what keeps a name from escaping Dir(). Same shape as foreman.ValidID, for the
// same reason: the name reaches this package from the command line and from a
// committed config.yaml, so it is untrusted input on the way to a file name.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidName rejects a name that would resolve to a path outside Dir().
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("workspace name %q: must be letters, digits, dot, dash or underscore, starting alphanumeric", name)
	}
	return nil
}

// Load reads one workspace. A missing file is an error — callers that treat
// "not registered" as ordinary check with Exists first.
func Load(name string) (Workspace, error) { return loadFrom(Dir(), name) }

func loadFrom(dir, name string) (Workspace, error) {
	var w Workspace
	if err := ValidName(name); err != nil {
		return w, err
	}
	b, err := os.ReadFile(filepath.Join(dir, name+".yaml"))
	if err != nil {
		return w, fmt.Errorf("workspace %s: %w", name, err)
	}
	if err := yaml.Unmarshal(b, &w); err != nil {
		return w, fmt.Errorf("workspace %s: %w", name, err)
	}
	return w, nil
}

// Exists reports whether a workspace is registered.
func Exists(name string) bool {
	if ValidName(name) != nil {
		return false
	}
	_, err := os.Stat(Path(name))
	return err == nil
}

// Save writes one workspace atomically. Callers mutating an existing file must
// hold the lock across their read-modify-write (see AddRepo) — Save alone is
// atomic per file, not serialized against a concurrent reader-modifier.
func Save(w Workspace) error {
	if err := ValidName(w.Name); err != nil {
		return err
	}
	if w.Version == 0 {
		w.Version = 1
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(w)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(Dir(), "."+w.Name+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), Path(w.Name))
}

// List returns every registered workspace, name-sorted. A file that fails to
// parse is skipped rather than fatal: one bad file must not blank the list.
func List() []Workspace { return ListIn(Dir()) }

// ListIn is List against an explicit directory, for a caller that was handed a
// state dir rather than deriving one (the dashboard's --state-dir).
func ListIn(dir string) []Workspace {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Workspace
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if w, err := loadFrom(dir, strings.TrimSuffix(e.Name(), ".yaml")); err == nil && w.Name != "" {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Create registers an empty workspace. Existing is not an error — creation is
// idempotent so `create` and the implicit create inside AddRepo are the same
// operation.
func Create(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if Exists(name) {
		return nil
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	// Under the same lock as AddRepo, and re-checking Exists inside it: without
	// that, a create racing an add-repo can write its empty workspace over the
	// entry the add just made.
	unlock, err := acquireLock(name)
	if err != nil {
		return err
	}
	defer unlock()
	if Exists(name) {
		return nil
	}
	return Save(Workspace{Version: 1, Name: name})
}

// AddRepo appends or updates a repo entry, keyed by git url. It creates the
// workspace if absent.
//
// This takes a lock where foreman.Save deliberately does not, and the
// difference is the operation, not the storage. A foreman record is written
// whole by the process that owns it, so a lost race costs one heartbeat field.
// This is a read-modify-write on a list shared by every repo in the workspace:
// two concurrent add-repo calls with no lock silently drop one entry, and
// nothing later reveals the loss. Rarity is not a safety argument — it only
// makes the bug harder to reproduce.
func AddRepo(name string, r Repo) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if r.GitURL == "" {
		return fmt.Errorf("workspace %s: repo needs a git url", name)
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	unlock, err := acquireLock(name)
	if err != nil {
		return err
	}
	defer unlock()

	w, err := Load(name)
	if err != nil {
		// Only an absent file starts a fresh list. A file that exists but does
		// not parse must abort: overwriting it here would silently delete every
		// repo already registered because someone left a tab in the yaml.
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		w = Workspace{Version: 1, Name: name}
	}
	w.Name = name
	found := false
	for i := range w.Repos {
		if w.Repos[i].GitURL == r.GitURL {
			w.Repos[i] = r
			found = true
			break
		}
	}
	if !found {
		w.Repos = append(w.Repos, r)
	}
	return Save(w)
}

// acquireLock takes <name>.lock via mkdir (atomic on POSIX) — the same
// mechanism as ticket.Store.AcquireLock. ~50 retries at 100ms ≈ 5s.
func acquireLock(name string) (func(), error) {
	lockPath := filepath.Join(Dir(), name+".lock")
	for tries := 0; ; tries++ {
		if err := os.Mkdir(lockPath, 0o755); err == nil {
			return func() { _ = os.RemoveAll(lockPath) }, nil
		}
		if tries >= 50 {
			return nil, fmt.Errorf("workspace %s: failed to acquire lock after 5s (%s)\n"+
				"  If no other bbs config workspace command is running, that directory is a leftover from a killed process — remove it.", name, lockPath)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
