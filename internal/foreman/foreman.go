// Package foreman persists the foreman records at ~/.babysit/foremen/<id>.yaml.
//
// A foreman is a long-running agent session bound to ONE project/workspace
// folder, watching the tickets assigned to it. The record is what makes it
// addressable from outside its own terminal: the dashboard reads it to list
// foremen, and writes through it to wake one.
//
// The pointer direction is one-way on purpose: a foreman names its current
// ~/.babysit/sessions/<id>.yaml, and the session file never names a foreman.
// Sessions are written by the session-writer hook on every SessionStart, so a
// back-pointer there would have to be maintained by a component that does not
// know foremen exist — and would go stale the moment a session restarted.
package foreman

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"gopkg.in/yaml.v3"
)

// StaleAfter is how long without a heartbeat before a foreman reads as dead.
// A foreman heartbeats on each watch tick; this allows several missed ticks
// before the dashboard stops offering it as an assignment target.
const StaleAfter = 10 * time.Minute

// Record is one foreman.
type Record struct {
	ID    string `yaml:"id"`
	Owner string `yaml:"owner"`
	// ProjectDir is the repo the foreman works in; WorkspaceDir is the folder
	// its cmux workspace is rooted at. They differ under worktree mode, where
	// the foreman sits in the primary checkout and workers get worktrees.
	ProjectDir   string `yaml:"project_dir"`
	WorkspaceDir string `yaml:"workspace_dir"`
	// WorkspaceTitle is the ONLY durable workspace handle. WorkspaceRef is the
	// ref as of the last write, kept for display and debugging — refs are
	// positional and churn as workspaces open and close, so anything that
	// actually talks to the workspace re-derives the ref from the title.
	WorkspaceRef   string `yaml:"workspace_ref"`
	WorkspaceTitle string `yaml:"workspace_title"`
	// Session is the ~/.babysit/sessions/<id>.yaml this foreman is currently
	// running as; empty until it reports one.
	Session   string `yaml:"session,omitempty"`
	Status    string `yaml:"status"`
	Heartbeat string `yaml:"heartbeat"`
	// Unreachable is the RFC3339 stamp of the last poke that could not be
	// delivered to this foreman's workspace, cleared on the next one that
	// lands. It is a separate field from Status because the two answer
	// different questions: Status is what the foreman said it was doing,
	// Unreachable is what the dashboard observed from outside. Overwriting
	// Status would erase the foreman's own report to record someone else's
	// failure to reach it.
	Unreachable string `yaml:"unreachable,omitempty"`
}

// MarkUnreachable records that a poke did not reach this foreman's workspace.
// The assignment it failed to announce stays on disk either way — the foreman
// re-derives its inbox from the tickets on its next tick — so this is a display
// fact, and a failure to persist it must not fail the assignment.
//
// Load-modify-Save with no lock, deliberately: this races the foreman's own
// heartbeat writer, and the loser's field is dropped. Each Save is atomic
// (temp + rename), so the file is never torn, and the only value at stake is
// one heartbeat stamp that the next beat rewrites — not worth a lock file per
// foreman on the poke path.
func MarkUnreachable(id string) {
	r, err := Load(id)
	if err != nil {
		return
	}
	r.Unreachable = Now()
	_ = Save(r)
}

// ClearUnreachable is the other half: a poke that landed proves the workspace
// is there.
func ClearUnreachable(id string) {
	r, err := Load(id)
	if err != nil || r.Unreachable == "" {
		return
	}
	r.Unreachable = ""
	_ = Save(r)
}

// Dir is ~/.babysit/foremen.
func Dir() string { return filepath.Join(identity.BabysitHome(), "foremen") }

// Path is the record file for one foreman id. Only ever call it with an id
// that passed ValidID — Join CLEANS its result, so an id carrying "../"
// resolves outside the foremen directory entirely.
func Path(id string) string { return filepath.Join(Dir(), id+".yaml") }

// idRe is what an id may look like: a leading alphanumeric, then word
// characters, dot or dash. That excludes "/" and any leading dot, which is
// what keeps an id from escaping Dir().
var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidID rejects an id that would resolve to a path outside Dir(). The id
// reaches this package from the command line today and over HTTP once the
// dashboard can spawn foremen, so it is untrusted input on the way to a file
// name: unchecked, `register ../../x` writes a record outside ~/.babysit and
// `retire ../../x` deletes whatever .yaml is there.
func ValidID(id string) error {
	if !idRe.MatchString(id) {
		return fmt.Errorf("foreman id %q: must be letters, digits, dot, dash or underscore, starting alphanumeric", id)
	}
	return nil
}

// Live reports whether the heartbeat is recent enough to act on. An unparseable
// or missing heartbeat is not live: the dashboard would otherwise route work to
// a foreman that never came up.
func (r Record) Live() bool {
	t, err := time.Parse(time.RFC3339, r.Heartbeat)
	if err != nil {
		return false
	}
	return time.Since(t) < StaleAfter
}

// Load reads one record.
func Load(id string) (Record, error) {
	var r Record
	if err := ValidID(id); err != nil {
		return r, err
	}
	b, err := os.ReadFile(Path(id))
	if err != nil {
		return r, fmt.Errorf("foreman %s: %w", id, err)
	}
	if err := yaml.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("foreman %s: %w", id, err)
	}
	return r, nil
}

// Save writes one record, creating the directory if needed. The write is
// atomic — the dashboard polls this directory, and a half-written record would
// read as a corrupt foreman rather than as a foreman mid-update.
func Save(r Record) error {
	if err := ValidID(r.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	// A per-call temp name, not "<id>.yaml.tmp": a foreman heartbeating while
	// the dashboard writes the same record would otherwise have both writers
	// interleaving into one file and renaming the mixture into place.
	tmp, err := os.CreateTemp(Dir(), "."+r.ID+".*.tmp")
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
	return os.Rename(tmp.Name(), Path(r.ID))
}

// List returns every record, id-sorted. A record that fails to parse is
// skipped rather than fatal: one bad file must not blank the foreman list.
func List() []Record {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return nil
	}
	var out []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if r, err := Load(strings.TrimSuffix(e.Name(), ".yaml")); err == nil && r.ID != "" {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Remove deletes a record. Missing is not an error.
func Remove(id string) error {
	if err := ValidID(id); err != nil {
		return err
	}
	err := os.Remove(Path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Now is the heartbeat stamp format.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }
