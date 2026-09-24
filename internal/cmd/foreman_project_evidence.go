package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/orca"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

type projectDelivery struct {
	Ticket string `json:"ticket"`
	Policy string `json:"policy"`
	State  string `json:"state"`
	Head   string `json:"head"`
	URL    string `json:"url,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type projectCompletion struct {
	Version  int               `json:"version"`
	Subject  string            `json:"subject"`
	At       string            `json:"at"`
	Foreman  string            `json:"foreman"`
	Cleanup  bool              `json:"cleanup_verified"`
	Delivery []projectDelivery `json:"delivery"`
}

func projectEvidenceProblems(st *ticket.Store, s projectSnapshot, ev projectEvidence, checkedOut bool) []string {
	problems := []string{}
	if ev.Version != 1 || ev.Subject != s.Subject || ev.Producer == "" || (ev.Kind != "integration" && ev.Kind != "product" && ev.Kind != "journey") {
		return []string{"invalid or stale subject"}
	}
	if _, err := parseProjectTime(ev.Started); err != nil {
		problems = append(problems, "missing start time")
	}
	if ev.Finished != "" {
		start, _ := parseProjectTime(ev.Started)
		end, err := parseProjectTime(ev.Finished)
		if err != nil || end.Before(start) {
			problems = append(problems, "invalid completion time")
		}
	}
	if ev.Kind == "product" {
		for _, child := range s.Children {
			var seal projectSeal
			if readProjectJSON(filepath.Join(storeForTicket(st.Env, child.ID).Home(), "project-seal.json"), &seal) == nil && slices.Contains(seal.Owners, ev.Producer) {
				problems = append(problems, "product evaluator also produced child evidence: "+child.ID)
			}
		}
	}
	seen := map[string]bool{}
	for _, surface := range ev.Surfaces {
		if !filepath.IsAbs(surface.Dir) || surface.Ref == "" || !fullSHARe.MatchString(surface.Head) || !fullSHARe.MatchString(surface.BaseHead) {
			problems = append(problems, "invalid surface")
			continue
		}
		if seen[surface.Dir] {
			problems = append(problems, "duplicate surface")
		}
		seen[surface.Dir] = true
		if gitOutIn(surface.Dir, "rev-parse", "--verify", surface.Ref+"^{commit}") != surface.Head {
			problems = append(problems, "surface revision changed: "+surface.Ref)
		}
		if gitOutIn(surface.Dir, "rev-parse", "--verify", surface.BaseRef+"^{commit}") != surface.BaseHead {
			problems = append(problems, "source base changed: "+surface.BaseRef)
		}
		if checkedOut {
			head, err := gitOutputStrict(surface.Dir, "rev-parse", "HEAD")
			status, statusErr := gitOutputStrict(surface.Dir, "status", "--porcelain", "--untracked-files=all")
			if err != nil || statusErr != nil || head != surface.Head || status != "" {
				problems = append(problems, "tested surface not clean at recorded HEAD")
			}
		}
	}
	for _, child := range s.Children {
		for _, repo := range child.Repos {
			found := false
			for _, surface := range ev.Surfaces {
				if surface.Dir != repo.Canonical {
					continue
				}
				found = true
				if !gitOKIn(repo.Canonical, "merge-base", "--is-ancestor", repo.Head, surface.Head) {
					problems = append(problems, "surface omits "+child.ID)
				}
				if ev.Kind != "journey" {
					if repo.Finish == "land" && (surface.Ref != repo.Base || surface.BaseRef != repo.Base) {
						problems = append(problems, "land must test delivered base")
					}
					if repo.Finish != "land" && (!strings.HasPrefix(surface.Ref, "qa/"+st.Env.Ticket) || surface.Ref == repo.Base) {
						problems = append(problems, "review/pr must retain a project QA branch")
					}
					base := repo.Base
					if repo.Finish == "pr" {
						base = "origin/" + base
					}
					if surface.BaseRef != base {
						problems = append(problems, "incorrect integration source base")
					}
				}
			}
			if !found {
				problems = append(problems, "missing repository surface for "+child.ID)
			}
		}
	}
	if len(ev.Surfaces) == 0 && (s.Contract == nil || !s.Contract.ArtifactsOnly) {
		problems = append(problems, "no verified surfaces")
	}
	for _, check := range ev.Checks {
		if check.ExitCode == nil || *check.ExitCode != 0 || len(check.Command) == 0 {
			problems = append(problems, "failed or incomplete check: "+check.Criterion+"/"+check.Kind)
		}
		if d, ok := digestFile(check.Log); !ok || d != check.Digest {
			problems = append(problems, "missing or changed check evidence: "+check.Criterion+"/"+check.Kind)
		}
	}
	for _, finding := range ev.Findings {
		if finding.Severity != "minor" && finding.Severity != "nit" {
			problems = append(problems, "product finding: "+finding.Summary)
		}
	}
	if ev.Preview != "" {
		u, err := url.Parse(ev.Preview)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			problems = append(problems, "invalid preview URL")
		}
		// Runtime is an executable revision probe, persisted just like a check.
		matched := false
		for _, check := range ev.Checks {
			if check.Kind != "runtime" || check.ExitCode == nil || *check.ExitCode != 0 {
				continue
			}
			b, err := os.ReadFile(check.Log)
			if err == nil && strings.TrimSpace(string(b)) == ev.Runtime {
				matched = true
			}
		}
		head := false
		for _, surface := range ev.Surfaces {
			if surface.Head == ev.Runtime {
				head = true
			}
		}
		if !matched || !head {
			problems = append(problems, "preview needs a passing runtime probe containing the tested HEAD")
		}
	}
	return problems
}

func projectEvidenceCommand(st *ticket.Store, kv map[string]string) error {
	return withLock(st, func() error {
		s, err := projectRead(st)
		if err != nil {
			return err
		}
		if s.Contract == nil || (kv["begin"] == "1" && len(s.Dispatch) > 0) {
			return fmt.Errorf("project not ready: %v", s.Dispatch)
		}
		if kv["begin"] == "1" {
			var spec struct {
				Kind     string           `json:"kind"`
				Producer string           `json:"producer"`
				Surfaces []projectSurface `json:"surfaces"`
			}
			if err := readProjectJSON(kv["file"], &spec); err != nil {
				return err
			}
			ev := projectEvidence{Version: 1, ID: newInvocationID(), Kind: spec.Kind, Producer: spec.Producer, Subject: s.Subject, Started: time.Now().UTC().Format(time.RFC3339Nano), Surfaces: spec.Surfaces, Checks: []projectCheck{}, Findings: []projectFinding{}}
			for i := range ev.Surfaces {
				r := &ev.Surfaces[i]
				r.Dir, err = filepath.Abs(r.Dir)
				if err != nil {
					return err
				}
				r.Head = gitOutIn(r.Dir, "rev-parse", "--verify", r.Ref+"^{commit}")
				r.BaseHead = gitOutIn(r.Dir, "rev-parse", "--verify", r.BaseRef+"^{commit}")
				if r.RestoreRef == "" || !fullSHARe.MatchString(r.RestoreHead) || gitOutIn(r.Dir, "rev-parse", "--verify", r.RestoreRef+"^{commit}") != r.RestoreHead {
					return fmt.Errorf("surface needs original restore_ref and restore_head (delivered base for land)")
				}
			}
			if p := projectEvidenceProblems(st, s, ev, true); len(p) > 0 {
				return fmt.Errorf("cannot begin verification: %v", p)
			}
			if err := writeJSONAtomic(filepath.Join(st.Home(), "project-attempts", ev.ID+".json"), ev); err != nil {
				return err
			}
			projectEvent(st, "verification_started", ev.ID, ev.Producer, ev.Kind)
			printV2Envelope(ev)
			return nil
		}
		id := kv["attempt"]
		if !idRe.MatchString(id) {
			return fmt.Errorf("evidence needs --begin --file spec.json or --attempt ID --file results.json")
		}
		var ev projectEvidence
		if err := readProjectJSON(filepath.Join(st.Home(), "project-attempts", id+".json"), &ev); err != nil {
			return err
		}
		var results struct {
			Checks   []projectCheck   `json:"checks"`
			Findings []projectFinding `json:"findings"`
			Preview  string           `json:"preview"`
			Runtime  string           `json:"runtime"`
		}
		if err := readProjectJSON(kv["file"], &results); err != nil {
			return err
		}
		// Compare inputs before accepting results; late recollection cannot turn
		// an old test into proof for the current tree.
		if p := projectEvidenceProblems(st, s, ev, true); len(p) > 0 {
			return fmt.Errorf("verification inputs changed: %v", p)
		}
		ev.Checks, ev.Findings, ev.Preview, ev.Runtime = results.Checks, results.Findings, results.Preview, results.Runtime
		if len(ev.Checks) == 0 {
			return fmt.Errorf("verification needs executed checks")
		}
		for i := range ev.Checks {
			check := &ev.Checks[i]
			if check.ExitCode == nil || len(check.Command) == 0 || strings.TrimSpace(check.Command[0]) == "" {
				return fmt.Errorf("check needs command and exit_code")
			}
			known := false
			for _, ac := range s.Contract.Criteria {
				if ac.ID == check.Criterion && (slices.Contains(ac.Checks, check.Kind) || check.Kind == "product" || check.Kind == "runtime") {
					known = true
				}
			}
			if !known {
				return fmt.Errorf("unknown acceptance criterion or check: %s/%s", check.Criterion, check.Kind)
			}
			// Archive logs in the durable ticket, independent of worktree cleanup.
			b, err := os.ReadFile(check.Log)
			if err != nil {
				return err
			}
			if len(b) == 0 {
				return fmt.Errorf("empty check evidence")
			}
			check.Log = filepath.Join(st.Home(), "project-evidence", ev.ID, fmt.Sprintf("check-%d.log", i))
			if err := writeProjectImmutable(check.Log, b); err != nil {
				return err
			}
			check.Digest, _ = digestFile(check.Log)
		}
		path := filepath.Join(st.Home(), "project-evidence", ev.ID+".json")
		var prior projectEvidence
		if err := readProjectJSON(path, &prior); err == nil {
			// A lost response must be retryable without changing the receipt's
			// completion time. Immutable comparison still rejects new results.
			ev.Finished = prior.Finished
		} else if !os.IsNotExist(err) {
			return err
		} else {
			ev.Finished = time.Now().UTC().Format(time.RFC3339Nano)
		}
		body, _ := json.MarshalIndent(ev, "", "  ")
		if err := writeProjectImmutable(path, body); err != nil {
			return err
		}
		projectEvent(st, "verification_recorded", ev.ID, ev.Producer, ev.Kind)
		printV2Envelope(map[string]interface{}{"evidence": ev, "problems": projectEvidenceProblems(st, s, ev, true)})
		return nil
	})
}

func writeProjectImmutable(path string, body []byte) error {
	if prior, err := os.ReadFile(path); err == nil {
		if bytes.Equal(prior, body) {
			return nil
		}
		return fmt.Errorf("immutable evidence already differs: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Publish only a fully written file. A crash must not leave a partial
	// immutable receipt that prevents all subsequent recovery attempts.
	f, err := os.CreateTemp(filepath.Dir(path), ".evidence-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(body)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(f.Name(), path); err != nil {
		if prior, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(prior, body) {
			return nil
		}
		return err
	}
	return nil
}

func projectObserveDelivery(s projectSnapshot) []projectDelivery {
	rows := []projectDelivery{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, child := range s.Children {
		if s.Contract != nil && s.Contract.ArtifactsOnly {
			state := "ARTIFACTS_READY"
			reason := ""
			if len(child.Problems) > 0 {
				state = "UNKNOWN"
				reason = "artifact evidence missing or changed"
			}
			rows = append(rows, projectDelivery{Ticket: child.ID, Policy: "artifacts", State: state, Reason: reason})
			continue
		}
		for _, r := range child.Repos {
			d := projectDelivery{Ticket: child.ID, Policy: r.Finish, State: "UNKNOWN", Head: r.Head}
			switch r.Finish {
			case "land":
				if gitOKIn(r.Canonical, "merge-base", "--is-ancestor", r.Head, r.Base) {
					d.State = "LANDED_LOCAL"
				} else {
					d.Reason = "verified head absent from local base"
				}
			case "review":
				if gitOutIn(r.Canonical, "rev-parse", "--verify", r.Branch+"^{commit}") == r.Head {
					d.State = "REVIEW_READY"
				} else {
					d.Reason = "verified branch not retained"
				}
			case "pr":
				cmd := exec.CommandContext(ctx, "gh", "pr", "view", r.Branch, "--json", "url,state,headRefOid")
				cmd.Dir = r.Canonical
				b, err := cmd.Output()
				var pr struct {
					URL   string `json:"url"`
					State string `json:"state"`
					Head  string `json:"headRefOid"`
				}
				if err == nil && json.Unmarshal(b, &pr) == nil && pr.Head == r.Head && (pr.State == "OPEN" || pr.State == "MERGED") {
					d.URL = pr.URL
					d.State = "PR_READY"
					if pr.State == "MERGED" {
						d.State = "MERGED_REMOTE"
					}
				} else {
					d.Reason = "current PR head/state unavailable or mismatched"
				}
			}
			rows = append(rows, d)
		}
	}
	return rows
}

func projectCompletionSubject(s projectSnapshot) string {
	var receipts []projectEvidence
	for _, ev := range s.Evidence {
		if ev.Subject == s.Subject && (ev.Kind == "integration" || ev.Kind == "product") {
			ev.State = ""
			ev.Problems = nil
			receipts = append(receipts, ev)
		}
	}
	return digestJSON(map[string]interface{}{"subject": s.Subject, "evidence": receipts, "delivery": s.Delivery})
}

func projectCleanupProblems(st *ticket.Store, s projectSnapshot) []string {
	problems := []string{}
	doc := ticket.ReadDoc(st.IndexPath())
	if run := doc.Get("pointers.orca_run"); run != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		client, err := orca.PreflightContext(ctx)
		if err != nil {
			problems = append(problems, "cleanup: Orca runtime unavailable")
		} else {
			workers, err := client.ExitedWorkers(run)
			if err != nil {
				problems = append(problems, "cleanup: worker state unknown")
			} else {
				for _, id := range sortedBoolKeys(workers) {
					if !workers[id] {
						problems = append(problems, "cleanup: worker "+id+" remains live or unverified")
					}
				}
			}
		}
		cancel()
	}
	var resources struct {
		Version        int                     `json:"version"`
		Leases         []foreman.ResourceLease `json:"leases"`
		ReconcileAfter string                  `json:"reconcile_after"`
	}
	err := readProjectJSON(filepath.Join(identity.BabysitHome(), "resources", "foreman-leases.json"), &resources)
	if (err != nil && !os.IsNotExist(err)) || (err == nil && resources.Version != 1) {
		problems = append(problems, "cleanup: resource state unreadable")
	}
	for _, lease := range resources.Leases {
		belongs := lease.Ticket == st.Env.Ticket
		for _, child := range s.Children {
			if child.ID == lease.Ticket {
				belongs = true
			}
		}
		if belongs {
			problems = append(problems, "cleanup: resource lease still held: "+lease.ID)
		}
	}
	latest := projectEvidence{}
	for _, ev := range s.Evidence {
		if ev.Subject == s.Subject && ev.Kind == "integration" && projectTimeAfter(ev.Finished, latest.Finished) {
			latest = ev
		}
	}
	for _, ev := range []projectEvidence{latest} {
		if ev.Subject != s.Subject || ev.Kind != "integration" {
			continue
		}
		for _, surface := range ev.Surfaces {
			if surface.RestoreRef == "" || gitOutIn(surface.Dir, "branch", "--show-current") != surface.RestoreRef || gitOutIn(surface.Dir, "rev-parse", "HEAD") != surface.RestoreHead {
				problems = append(problems, "cleanup: restore recorded checkout in "+surface.Dir)
			}
			status, err := gitOutputStrict(surface.Dir, "status", "--porcelain", "--untracked-files=all")
			if err != nil || status != "" {
				problems = append(problems, "cleanup: checkout dirty or unavailable")
			}
			gitdir := gitOutIn(surface.Dir, "rev-parse", "--absolute-git-dir")
			if gitdir == "" {
				problems = append(problems, "cleanup: git directory unavailable")
				continue
			}
			if _, err := os.Stat(leaseDirOf(gitdir)); err == nil {
				problems = append(problems, "cleanup: surface lease still held")
			} else if !os.IsNotExist(err) {
				problems = append(problems, "cleanup: surface lease unreadable")
			}
		}
	}
	return problems
}

func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func projectComplete(st *ticket.Store, id string) error {
	if err := foreman.ValidID(id); err != nil {
		return fmt.Errorf("complete needs --foreman ID")
	}
	r, err := foreman.Load(id)
	if err != nil {
		return err
	}
	return withLock(st, func() error {
		if doc := ticket.ReadDoc(st.IndexPath()); doc.Get("assignee") != id {
			return fmt.Errorf("parent is not claimed by this foreman")
		}
		s, err := projectRead(st)
		if err != nil {
			return err
		}
		if !s.Ready {
			return fmt.Errorf("project not ready: %v", s.Blockers)
		}
		check, err := projectRead(st)
		if err != nil {
			return err
		}
		if !check.Ready || projectCompletionSubject(check) != projectCompletionSubject(s) {
			return fmt.Errorf("project changed during completion; re-read readiness")
		}
		receipt := projectCompletion{Version: 1, Subject: projectCompletionSubject(s), At: isoNow(), Foreman: id, Cleanup: true, Delivery: s.Delivery}
		if err := writeJSONAtomic(filepath.Join(st.Home(), "completion.json"), receipt); err != nil {
			return err
		}
		doc, err := ticket.ReadDocStrict(st.IndexPath())
		if err != nil {
			return err
		}
		status := "done"
		for _, delivery := range s.Delivery {
			if delivery.State == "REVIEW_READY" || delivery.State == "PR_READY" {
				status = "in_review"
			}
		}
		doc.Set("status", status)
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		// Other assigned projects must complete independently. A completed
		// project never closes a coordinator still responsible for more work.
		allDone := true
		ids, err := ticket.TicketIDs(st.Env.ProjectHome)
		if err != nil {
			return err
		}
		for _, ticketID := range ids {
			other := storeForTicket(st.Env, ticketID)
			d, e := ticket.ReadDocStrict(other.IndexPath())
			if e != nil {
				// A stub directory (no index.json) is not a ticket — skip it
				// rather than wedging completion on leftover state.
				var re *ticket.ReadError
				if errors.As(e, &re) && re.Kind == ticket.KindMissing {
					continue
				}
				return e
			}
			if d.Get("assignee") != id || ticketID == st.Env.Ticket || d.Get("parent") != "" {
				continue
			}
			otherState, e := projectRead(other)
			if e != nil || otherState.Completion != "verified" {
				allDone = false
			}
		}
		if allDone {
			r, err = foreman.Load(id)
			if err != nil {
				return err
			}
			r.Status = "done"
			r.Heartbeat = foreman.Now()
			r.Completion = filepath.Join(st.Home(), "completion.json")
			if err := foreman.Save(r); err != nil {
				return err
			}
		}
		projectEvent(st, "project_completed", "", id, s.Subject)
		printV2Envelope(receipt)
		return nil
	})
}

func foremanCompletionCurrent(r foreman.Record) error {
	if r.Completion == "" {
		return fmt.Errorf("validated project completion receipt required")
	}
	home := filepath.Dir(r.Completion)
	env := identity.Env{ProjectHome: filepath.Dir(filepath.Dir(home)), Ticket: filepath.Base(home)}
	current, err := projectRead(ticket.New(env))
	if err != nil {
		return err
	}
	if current.Completion != "verified" {
		return fmt.Errorf("completion receipt is no longer current: %v", current.Blockers)
	}
	ids, err := ticket.TicketIDs(env.ProjectHome)
	if err != nil {
		return err
	}
	for _, id := range ids {
		st := storeForTicket(env, id)
		doc, err := ticket.ReadDocStrict(st.IndexPath())
		if err != nil {
			return err
		}
		if doc.Get("assignee") != r.ID || doc.Get("parent") != "" {
			continue
		}
		s, err := projectRead(st)
		if err != nil {
			return err
		}
		if s.Completion != "verified" {
			return fmt.Errorf("project %s has no current completion receipt: %v", id, s.Blockers)
		}
	}
	var receipt projectCompletion
	if err := readProjectJSON(r.Completion, &receipt); err != nil {
		return err
	}
	if receipt.Foreman != r.ID || !receipt.Cleanup {
		return fmt.Errorf("completion owner/cleanup mismatch")
	}
	return nil
}
