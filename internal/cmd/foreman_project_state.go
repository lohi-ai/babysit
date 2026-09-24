package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/reallongnguyen/babysit/internal/ticket"
)

// Project scope is approved before runtime ticket IDs exist. Seed bindings
// live on children; children and dependency edges remain owned by the ticket DAG.
type projectContract struct {
	Version       int                `json:"version"`
	Audience      string             `json:"audience"`
	Outcome       string             `json:"outcome"`
	NonGoals      []string           `json:"non_goals"`
	Seeds         []string           `json:"seeds"`
	Criteria      []projectCriterion `json:"criteria"`
	FirstJourney  []string           `json:"first_journey"`
	ProductReview bool               `json:"product_review"`
	ArtifactsOnly bool               `json:"artifacts_only,omitempty"`
	Deadline      string             `json:"deadline,omitempty"`
}

type projectCriterion struct {
	ID     string   `json:"id"`
	Text   string   `json:"text"`
	Owner  string   `json:"owner"`
	Checks []string `json:"checks"`
}

type projectRepo struct {
	Canonical string `json:"canonical"`
	Branch    string `json:"branch"`
	Head      string `json:"head"`
	Base      string `json:"base"`
	BaseHead  string `json:"base_head"`
	Finish    string `json:"finish"`
	Policy    string `json:"policy"`
}

type projectSeal struct {
	Version   int               `json:"version"`
	Ticket    string            `json:"ticket"`
	At        string            `json:"at"`
	Inputs    string            `json:"inputs"`
	Repos     []projectRepo     `json:"repos"`
	Owners    []string          `json:"owners"`
	Artifacts map[string]string `json:"artifacts,omitempty"`
}

type projectChild struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Seed      string           `json:"seed"`
	State     string           `json:"state"`
	BlockedBy []string         `json:"blocked_by"`
	Repos     []projectRepo    `json:"repos"`
	Problems  []string         `json:"problems"`
	Progress  *projectProgress `json:"progress,omitempty"`
}

// A surface is captured while checked out for verification, then compared by
// retained ref. Restoring the primary checkout never invalidates the receipt.
type projectSurface struct {
	Dir         string `json:"dir"`
	Ref         string `json:"ref"`
	Head        string `json:"head"`
	BaseRef     string `json:"base_ref"`
	BaseHead    string `json:"base_head"`
	RestoreRef  string `json:"restore_ref"`
	RestoreHead string `json:"restore_head"`
}

type projectCheck struct {
	Criterion string   `json:"criterion"`
	Kind      string   `json:"kind"`
	Command   []string `json:"command"`
	ExitCode  *int     `json:"exit_code"`
	Log       string   `json:"log"`
	Digest    string   `json:"digest,omitempty"`
}

type projectFinding struct {
	Severity  string `json:"severity"`
	Summary   string `json:"summary"`
	Criterion string `json:"criterion"`
}

type projectEvidence struct {
	State    string           `json:"state,omitempty"`
	Problems []string         `json:"problems,omitempty"`
	Version  int              `json:"version"`
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Subject  string           `json:"subject"`
	Producer string           `json:"producer"`
	Started  string           `json:"started"`
	Finished string           `json:"finished,omitempty"`
	Surfaces []projectSurface `json:"surfaces"`
	Checks   []projectCheck   `json:"checks"`
	Findings []projectFinding `json:"findings"`
	// A preview is only published with a recorded revision probe in Checks.
	Preview string `json:"preview,omitempty"`
	Runtime string `json:"runtime,omitempty"`
}

type projectCoverage struct {
	projectCriterion
	State    string   `json:"state"`
	Evidence []string `json:"evidence"`
}

type projectSnapshot struct {
	Version     int               `json:"version"`
	Ticket      string            `json:"ticket"`
	Observed    string            `json:"observed_at"`
	Subject     string            `json:"subject"`
	Contract    *projectContract  `json:"contract"`
	Approval    string            `json:"approval"`
	Children    []projectChild    `json:"children"`
	Coverage    []projectCoverage `json:"coverage"`
	Evidence    []projectEvidence `json:"evidence"`
	Dispatch    []string          `json:"dispatch_blockers"`
	Blockers    []string          `json:"blockers"`
	Ready       bool              `json:"ready"`
	Completion  string            `json:"completion"`
	Coordinator map[string]string `json:"coordinator"`
	Delivery    []projectDelivery `json:"delivery"`
	Usage       skillUsage        `json:"provider_usage"`
}

func readProjectJSON(path string, into interface{}) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return requireJSONEOF(d)
}

func projectContractPath(st *ticket.Store) string { return filepath.Join(st.Home(), "project.json") }

func validateProjectContract(c projectContract) error {
	if c.Version != 1 || strings.TrimSpace(c.Audience) == "" || strings.TrimSpace(c.Outcome) == "" || len(c.Seeds) == 0 || len(c.Criteria) == 0 || len(c.FirstJourney) == 0 {
		return fmt.Errorf("project contract needs version 1, audience, outcome, seeds, criteria and first_journey")
	}
	seeds, ids := map[string]bool{}, map[string]bool{}
	for _, seed := range c.Seeds {
		if !idRe.MatchString(seed) || seeds[seed] {
			return fmt.Errorf("invalid or duplicate seed %q", seed)
		}
		seeds[seed] = true
	}
	for _, ac := range c.Criteria {
		if !idRe.MatchString(ac.ID) || ids[ac.ID] || !seeds[ac.Owner] || strings.TrimSpace(ac.Text) == "" || len(ac.Checks) == 0 {
			return fmt.Errorf("criterion %q needs a unique ID, text, seed owner and checks", ac.ID)
		}
		ids[ac.ID] = true
		seen := map[string]bool{}
		for _, check := range ac.Checks {
			if !idRe.MatchString(check) || seen[check] {
				return fmt.Errorf("invalid or duplicate check for %s", ac.ID)
			}
			seen[check] = true
		}
	}
	for _, ac := range c.FirstJourney {
		if !ids[ac] {
			return fmt.Errorf("first_journey references unknown criterion %s", ac)
		}
	}
	if c.Deadline != "" {
		if _, err := parseProjectTime(c.Deadline); err != nil {
			return err
		}
	}
	return nil
}

func projectChildInputs(st *ticket.Store) (string, []string, error) {
	cp, err := readStrictObject(filepath.Join(st.Home(), "checkpoint.json"), true)
	if err != nil {
		return "", nil, err
	}
	if contractVersion(cp) != 2 {
		return "", nil, fmt.Errorf("typed v2 evidence required; migrate and rerun gates")
	}
	if stringValue(cp["active_attempt_id"]) != "" {
		return "", nil, fmt.Errorf("active attempt still owns ticket")
	}
	rows := map[string]string{}
	for _, artifact := range collectSnapshotArtifacts(st.Home(), stringValue(cp["workflow"])) {
		if artifact.Role != "checkpoint" && artifact.Role != "manifest" {
			rows[artifact.Role] = artifact.Digest
		}
	}
	owners := []string{}
	accepted, _ := cp["accepted_evidence"].(map[string]interface{})
	for _, gate := range []string{"review-pr", "qa"} {
		id := stringValue(accepted[gate])
		if !idRe.MatchString(id) {
			return "", nil, fmt.Errorf("%s: missing accepted evidence", gate)
		}
		path := filepath.Join(st.Home(), "evidence", "verification", "attempts", id+".json")
		ev, err := readStrictObject(path, true)
		if err != nil {
			return "", nil, err
		}
		attempt, err := readAttemptRecordStrict(filepath.Join(st.Home(), "attempts", id+".json"))
		if err != nil {
			return "", nil, err
		}
		if attempt.State != "completed" || attempt.RunID != stringValue(cp["run_id"]) || attempt.Gate != gate || attempt.Ticket != st.Env.Ticket {
			return "", nil, fmt.Errorf("%s: accepted attempt no longer completed/current", gate)
		}
		// Sealing runs the full current-tree validator. Subsequent reads bind
		// its accepted bytes and durable logs without needing the old checkout.
		for _, raw := range interfaceSlice(ev["checks"]) {
			check, ok := raw.(map[string]interface{})
			if !ok {
				return "", nil, fmt.Errorf("malformed check")
			}
			log := stringValue(check["log_path"])
			if !filepath.IsAbs(log) {
				log = filepath.Join(st.Home(), log)
			}
			if digest, ok := digestFile(log); !ok || digest != stringValue(check["log_digest"]) {
				return "", nil, fmt.Errorf("%s: check log missing or changed", gate)
			}
		}
		rows[gate] = digestJSON(map[string]interface{}{"evidence": ev, "attempt": attempt})
		producer, _ := ev["producer"].(map[string]interface{})
		owner := stringValue(producer["owner"])
		if !slices.Contains(owners, owner) {
			owners = append(owners, owner)
		}
	}
	return digestJSON(rows), owners, nil
}
