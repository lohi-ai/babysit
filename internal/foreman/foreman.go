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
	// Grant is the human's delegation of the design checkpoint to this
	// foreman. A pointer, not a value: nil is "no grant" and must serialize
	// to nothing at all, because a record written before grants existed and a
	// record whose grant was revoked have to be the same bytes on disk.
	Grant *Grant `yaml:"grant,omitempty"`
}

// Grant is permission for a foreman to resolve its own design checkpoint —
// to write the approval verdict a human would otherwise write in the
// dashboard. It is deliberately a record of *who said so and until when*
// rather than a boolean: an unattended approval nobody can attribute after
// the fact is indistinguishable from a bug.
//
// Every bound is "unset means unbounded", so the zero Grant is the most
// permissive one. That is the wrong default to reach by accident, which is
// why the CLI refuses to mint it without an explicit --unbounded.
type Grant struct {
	GrantedBy string `yaml:"granted_by"`
	At        string `yaml:"at"`
	// ExpiresAt is RFC3339; empty means no time bound.
	ExpiresAt string `yaml:"expires_at,omitempty"`
	// MaxApprovals is the approval budget; 0 means no budget bound.
	MaxApprovals int `yaml:"max_approvals,omitempty"`
	// Used counts approvals actually written under this grant.
	Used int `yaml:"used"`
	// Tickets scopes the grant to a ticket set; empty means any ticket.
	Tickets []string `yaml:"tickets,omitempty"`
}

// Unbounded reports a grant with no expiry, no budget and no ticket scope.
func (g *Grant) Unbounded() bool {
	return g != nil && g.ExpiresAt == "" && g.MaxApprovals == 0 && len(g.Tickets) == 0
}

// Allows reports whether this record's grant covers approving `ticket` at
// `now`, and when it does not, the reason — the reason is the point: it is
// what the foreman prints when it escalates instead, so a human reading the
// pane learns whether to extend the grant or answer the question themselves.
//
// It does NOT consider the non-delegable floor. That check reads the design
// artifacts rather than the record and lives with the caller, so that no
// future grant field can be mistaken for a way to switch the floor off.
func (r Record) Allows(ticket string, now time.Time) (bool, string) {
	g := r.Grant
	if g == nil {
		return false, "no grant"
	}
	if g.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, g.ExpiresAt)
		if err != nil {
			// An unparseable bound is not a missing bound: reading it as
			// "unbounded" would turn a corrupt field into unlimited authority.
			return false, fmt.Sprintf("grant expiry %q is unreadable", g.ExpiresAt)
		}
		if !now.Before(exp) {
			return false, "grant expired at " + g.ExpiresAt
		}
	}
	if g.MaxApprovals > 0 && g.Used >= g.MaxApprovals {
		return false, fmt.Sprintf("grant budget spent (%d/%d)", g.Used, g.MaxApprovals)
	}
	if len(g.Tickets) > 0 {
		if ticket == "" {
			return false, "grant is ticket-scoped and no ticket was named"
		}
		found := false
		for _, t := range g.Tickets {
			if t == ticket {
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Sprintf("%s is outside the grant's ticket scope (%s)", ticket, strings.Join(g.Tickets, ", "))
		}
	}
	return true, ""
}

// Consume records one approval spent against the grant. It re-Loads first so
// the increment lands on the current record rather than on a copy the caller
// may have been holding across a heartbeat.
//
// Like MarkUnreachable this is Load-modify-Save with no lock, and the same
// tradeoff applies with one difference worth stating: the value at stake is a
// budget, not a display field. Two resolvers racing can each read used=N and
// both write N+1, spending one approval more than the budget allows per
// racing caller. A foreman handles its checkpoints serially within one
// session, so the race needs two foremen sharing an id to appear at all.
func Consume(id string) error {
	r, err := Load(id)
	if err != nil {
		return err
	}
	if r.Grant == nil {
		return fmt.Errorf("foreman %s: no grant to consume", id)
	}
	r.Grant.Used++
	return Save(r)
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
func Load(id string) (Record, error) { return loadFrom(Dir(), id) }

func loadFrom(dir, id string) (Record, error) {
	var r Record
	if err := ValidID(id); err != nil {
		return r, err
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".yaml"))
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
func List() []Record { return ListIn(Dir()) }

// ListIn is List against an explicit directory. The dashboard composes a
// snapshot for a state dir it was handed (--state-dir, BABYSIT_STATE_DIR),
// which is not necessarily the identity's BabysitHome() — reading Dir() there
// would silently list the wrong machine's foremen.
func ListIn(dir string) []Record {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if r, err := loadFrom(dir, strings.TrimSuffix(e.Name(), ".yaml")); err == nil && r.ID != "" {
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
