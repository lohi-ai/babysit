package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

func parseProjectTime(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

func projectTimeAfter(value, previous string) bool {
	v, err := parseProjectTime(value)
	if err != nil {
		return false
	}
	p, err := parseProjectTime(previous)
	return err != nil || v.After(p)
}

func projectStore(id string) (*ticket.Store, error) {
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("foreman: needs a plain ticket ID")
	}
	env := identity.Resolve()
	env.Ticket = id
	st := ticket.New(env)
	_, err := ticket.ReadDocStrict(st.IndexPath())
	return st, err
}

func projectRead(st *ticket.Store) (projectSnapshot, error) {
	s := projectSnapshot{Version: 1, Ticket: st.Env.Ticket, Observed: isoNow(), Children: []projectChild{}, Coverage: []projectCoverage{}, Evidence: []projectEvidence{}, Dispatch: []string{}, Blockers: []string{}, Completion: "pending", Usage: skillUsage{Reason: "provider usage not observed"}}
	doc, err := ticket.ReadDocStrict(st.IndexPath())
	if err != nil {
		return s, err
	}
	s.Coordinator = map[string]string{"agent_liveness": "unknown"}
	if r, err := foreman.Load(doc.Get("assignee")); err == nil {
		s.Coordinator["id"] = r.ID
		s.Coordinator["heartbeat"] = r.Heartbeat
		s.Coordinator["runtime_observed_at"] = watchLoad(r.ID).RuntimeObservedAt
	}
	var c projectContract
	if err := readProjectJSON(projectContractPath(st), &c); err != nil {
		s.Dispatch = append(s.Dispatch, "contract: missing or unreadable project.json")
	} else if err := validateProjectContract(c); err != nil {
		s.Dispatch = append(s.Dispatch, "contract: "+err.Error())
	} else {
		s.Contract = &c
	}
	s.Approval, _, _ = approvalRead(st)
	if s.Approval != "approved" || doc.Get("approval.kind") != "project-plan" {
		s.Dispatch = append(s.Dispatch, "approval: current project-plan approval required")
	}
	if control := doc.Get("control.state"); control != "" {
		s.Dispatch = append(s.Dispatch, "control: "+control)
	}
	if c.Deadline != "" {
		if deadline, err := parseProjectTime(c.Deadline); err == nil && time.Now().After(deadline) {
			s.Dispatch = append(s.Dispatch, "budget: project deadline exceeded; finish verified work or revise scope")
		}
	}
	g, err := ticket.BuildGraph(st.Env.ProjectHome, st.Env.Ticket)
	if err != nil {
		return s, err
	}
	if len(g.Cycles) > 0 {
		s.Dispatch = append(s.Dispatch, "graph: dependency cycle")
	}
	bindings := map[string]string{}
	// Only material, stable inputs enter the subject. Status, progress and
	// worktree removal must not rewrite an accepted product revision.
	inputs := map[string]interface{}{"contract": c, "approval": doc.Get("approval.artifact_revision"), "approval_state": s.Approval, "run": doc.Get("pointers.orca_run")}
	for _, node := range g.Nodes {
		cs := storeForTicket(st.Env, node.ID)
		cd, readErr := ticket.ReadDocStrict(cs.IndexPath())
		if readErr != nil || node.Dangling {
			s.Blockers = append(s.Blockers, "child "+node.ID+": missing or unreadable")
			inputs[node.ID] = "missing"
			continue
		}
		if node.External {
			if node.Status != "done" || cd.Get("control.state") != "" {
				s.Blockers = append(s.Blockers, "dependency "+node.ID+": not completed")
			}
			// External dependency revisions must also invalidate integrated QA.
			m, me := ticket.ReadManifest(cs.ManifestPath())
			refs := map[string]string{}
			if me != nil || len(m.Repos) == 0 {
				s.Blockers = append(s.Blockers, "dependency "+node.ID+": revision unavailable")
			} else {
				for _, r := range m.Repos {
					refs[r.Name] = gitOutIn(r.Canonical, "rev-parse", "--verify", r.Branch+"^{commit}")
					if refs[r.Name] == "" {
						s.Blockers = append(s.Blockers, "dependency "+node.ID+": revision unavailable")
					}
				}
			}
			inputs[node.ID] = refs
			continue
		}
		if control := cd.Get("control.state"); control != "" {
			s.Blockers = append(s.Blockers, "child "+node.ID+": "+control)
		}
		if node.Status == "cancelled" || node.Status == "duplicate" {
			s.Blockers = append(s.Blockers, "child "+node.ID+": required scope was "+node.Status)
		}
		if len(node.Children) != 0 {
			inputs[node.ID] = node.Children
			continue
		}
		child := projectChild{ID: node.ID, Title: cd.Get("title"), Seed: cd.Get("origin.project_seed"), State: node.State, BlockedBy: node.BlockedBy, Repos: []projectRepo{}, Problems: []string{}}
		if child.Seed == "" || !slices.Contains(c.Seeds, child.Seed) {
			child.Problems = append(child.Problems, "scope: seed is not in approved contract")
		} else if prior := bindings[child.Seed]; prior != "" {
			child.Problems = append(child.Problems, "scope: seed also bound to "+prior)
		} else {
			bindings[child.Seed] = child.ID
		}
		var seal projectSeal
		if err := readProjectJSON(filepath.Join(cs.Home(), "project-seal.json"), &seal); err != nil {
			child.Problems = append(child.Problems, "verification: seal required before delivery or cleanup")
		} else {
			child.Repos = seal.Repos
			if c.ArtifactsOnly != (len(seal.Artifacts) > 0) {
				child.Problems = append(child.Problems, "seal does not match approved delivery type")
			}
			child.Problems = append(child.Problems, validateProjectSeal(cs, seal)...)
		}
		var progress projectProgress
		if readProjectJSON(filepath.Join(cs.Home(), "progress.json"), &progress) == nil {
			projectProgressHealth(&progress, time.Now())
			child.Progress = &progress
		}
		for _, p := range child.Problems {
			s.Blockers = append(s.Blockers, "child "+child.ID+": "+p)
		}
		s.Children = append(s.Children, child)
		inputs[child.ID] = map[string]interface{}{"seed": child.Seed, "dependencies": child.BlockedBy, "seal": seal}
	}
	for _, seed := range c.Seeds {
		if bindings[seed] == "" {
			s.Blockers = append(s.Blockers, "scope: no child bound to "+seed)
		}
	}
	for _, child := range s.Children {
		for _, dependency := range child.BlockedBy {
			for _, upstream := range s.Children {
				if upstream.ID != dependency {
					continue
				}
				for _, depRepo := range upstream.Repos {
					for _, repo := range child.Repos {
						if repo.Canonical == depRepo.Canonical && !gitOKIn(repo.Canonical, "merge-base", "--is-ancestor", depRepo.Head, repo.Head) {
							s.Blockers = append(s.Blockers, "dependency "+dependency+": current revision not verified in "+child.ID)
						}
					}
				}
			}
		}
	}
	s.Subject = digestJSON(inputs)
	// Evidence is immutable; only current receipts contribute coverage. Failed
	// attempts remain visible, but a later current pass may repair them.
	paths, err := filepath.Glob(filepath.Join(st.Home(), "project-evidence", "*.json"))
	if err != nil {
		return s, err
	}
	for _, path := range paths {
		var ev projectEvidence
		if err := readProjectJSON(path, &ev); err != nil {
			s.Blockers = append(s.Blockers, "evidence: unreadable "+filepath.Base(path))
			continue
		}
		ev.Problems = projectEvidenceProblems(st, s, ev, false)
		ev.State = "passed"
		if len(ev.Problems) > 0 {
			ev.State = "failed"
		}
		if ev.Subject != s.Subject {
			ev.State = "stale"
		}
		s.Evidence = append(s.Evidence, ev)
	}
	latest := map[string]projectEvidence{}
	for _, ev := range s.Evidence {
		if ev.Subject == s.Subject && projectTimeAfter(ev.Finished, latest[ev.Kind].Finished) {
			latest[ev.Kind] = ev
		}
	}
	integration, product := latest["integration"], latest["product"]
	if integration.State != "passed" {
		integration = projectEvidence{}
	}
	if product.State != "passed" {
		product = projectEvidence{}
	}
	for _, kind := range []string{"integration", "product"} {
		if kind == "product" && !c.ProductReview {
			continue
		}
		ev, ok := latest[kind]
		if !ok {
			s.Blockers = append(s.Blockers, kind+": current evidence required")
			continue
		}
		for _, p := range ev.Problems {
			s.Blockers = append(s.Blockers, kind+": "+p)
		}
	}
	for _, ac := range c.Criteria {
		row := projectCoverage{projectCriterion: ac, State: "pending", Evidence: []string{}}
		covered := true
		for _, kind := range ac.Checks {
			if projectCheckPassed(integration, ac.ID, kind) {
				row.Evidence = append(row.Evidence, integration.ID+":"+kind)
			} else {
				covered = false
			}
		}
		if c.ProductReview && !projectCheckPassed(product, ac.ID, "product") {
			covered = false
		}
		if covered {
			row.State = "proven"
		} else {
			s.Blockers = append(s.Blockers, "criterion "+ac.ID+": missing required checks")
		}
		s.Coverage = append(s.Coverage, row)
	}
	s.Delivery = projectObserveDelivery(s)
	for _, delivery := range s.Delivery {
		if delivery.State == "UNKNOWN" {
			s.Blockers = append(s.Blockers, "delivery "+delivery.Ticket+": "+delivery.Reason)
		}
	}
	// An expired work budget prevents new starts, never acceptance of already
	// verified work. All other dispatch prerequisites also gate completion.
	for _, p := range s.Dispatch {
		if !strings.HasPrefix(p, "budget:") {
			s.Blockers = append(s.Blockers, p)
		}
	}
	var receipt projectCompletion
	completionRead := readProjectJSON(filepath.Join(st.Home(), "completion.json"), &receipt) == nil
	currentReceipt := completionRead && receipt.Version == 1 && receipt.Cleanup && receipt.Subject == projectCompletionSubject(s)
	if !currentReceipt {
		s.Blockers = append(s.Blockers, projectCleanupProblems(st, s)...)
	}
	s.Ready = len(s.Blockers) == 0
	if completionRead {
		s.Completion = "stale"
		if s.Ready && currentReceipt {
			s.Completion = "verified"
		}
	}
	return s, nil
}

func validateProjectSeal(st *ticket.Store, seal projectSeal) []string {
	problems := []string{}
	if len(seal.Artifacts) > 0 {
		return validateArtifactSeal(st, seal)
	}
	if seal.Version != 1 || seal.Ticket != st.Env.Ticket || len(seal.Repos) == 0 {
		return []string{"invalid seal"}
	}
	inputs, _, err := projectChildInputs(st)
	if err != nil {
		problems = append(problems, "evidence: "+err.Error())
	} else if inputs != seal.Inputs {
		problems = append(problems, "evidence: changed since seal")
	}
	m, err := ticket.ReadManifest(st.ManifestPath())
	if err != nil || len(m.Repos) != len(seal.Repos) {
		return append(problems, "manifest changed or unavailable")
	}
	for _, r := range seal.Repos {
		mr := m.FindRepo(func(m ticket.Repo) bool { return m.Canonical == r.Canonical && m.Branch == r.Branch })
		if mr == nil {
			problems = append(problems, "manifest changed")
			continue
		}
		p, err := snapshotGitFlow(r.Canonical)
		if err != nil || p.Digest != r.Policy {
			problems = append(problems, "policy changed or unavailable")
		}
		head := gitOutIn(r.Canonical, "rev-parse", "--verify", r.Branch+"^{commit}")
		landed := r.Finish == "land" && gitOKIn(r.Canonical, "merge-base", "--is-ancestor", r.Head, r.Base)
		if head != r.Head && !(head == "" && landed) {
			problems = append(problems, "branch revision changed or unavailable: "+r.Branch)
		}
		if mr.Worktree != "" {
			if fi, err := os.Stat(mr.Worktree); err == nil && fi.IsDir() {
				if gitOutIn(mr.Worktree, "branch", "--show-current") == r.Branch && gitOutIn(mr.Worktree, "status", "--porcelain", "--untracked-files=all") != "" {
					problems = append(problems, "worktree dirty: "+r.Branch)
				}
			}
		}
	}
	return problems
}

func projectCheckPassed(ev projectEvidence, criterion, kind string) bool {
	for _, check := range ev.Checks {
		if check.Criterion == criterion && check.Kind == kind && check.ExitCode != nil && *check.ExitCode == 0 {
			if digest, ok := digestFile(check.Log); ok && digest == check.Digest {
				return true
			}
		}
	}
	return false
}

func projectSealChild(st *ticket.Store) error {
	return withLock(st, func() error {
		inputs, owners, err := projectChildInputs(st)
		if err != nil {
			return err
		}
		m, err := ticket.ReadManifest(st.ManifestPath())
		if err != nil {
			return err
		}
		if len(m.Repos) != 1 {
			return fmt.Errorf("one verified repository per child is required; split cross-repo work into dependent children")
		}
		seal := projectSeal{Version: 1, Ticket: st.Env.Ticket, At: isoNow(), Inputs: inputs, Owners: owners, Repos: []projectRepo{}}
		for _, r := range m.Repos {
			if !filepath.IsAbs(r.Canonical) || !filepath.IsAbs(r.Worktree) {
				return fmt.Errorf("manifest needs absolute canonical and worktree paths")
			}
			env := st.Env
			env.Branch = r.Branch
			ready, err := evaluateReadinessIn(env, "review", r.Worktree)
			if err != nil {
				return err
			}
			if !ready.Enforced || !ready.Ready {
				return fmt.Errorf("child not verified: %v", ready.ReasonCodes)
			}
			p, err := snapshotGitFlow(r.Canonical)
			if err != nil {
				return err
			}
			head := gitOutIn(r.Worktree, "rev-parse", "HEAD")
			if head == "" || head != gitOutIn(r.Canonical, "rev-parse", "--verify", r.Branch+"^{commit}") || gitOutIn(r.Worktree, "branch", "--show-current") != r.Branch {
				return fmt.Errorf("worktree is not on the declared child revision")
			}
			seal.Repos = append(seal.Repos, projectRepo{Canonical: r.Canonical, Branch: r.Branch, Head: head, Base: p.Effective["base_branch"], BaseHead: stringValue(ready.CurrentSubject["base_sha"]), Finish: p.Effective["finish"], Policy: p.Digest})
		}
		if err := writeJSONAtomic(filepath.Join(st.Home(), "project-seal.json"), seal); err != nil {
			return err
		}
		projectEvent(st, "child_sealed", "", "", "")
		return nil
	})
}

func foremanProjectCommand(verb string, args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	st, err := projectStore(id)
	if err != nil {
		return err
	}
	switch verb {
	case "contract":
		var c projectContract
		if err := readProjectJSON(kv["file"], &c); err != nil {
			return err
		}
		if err := validateProjectContract(c); err != nil {
			return err
		}
		return withLock(st, func() error {
			doc, err := ticket.ReadDocStrict(st.IndexPath())
			if err != nil {
				return err
			}
			if doc.Get("approval.state") == "pending" {
				return fmt.Errorf("resolve or redirect the pending project review before replacing scope")
			}
			if err := writeJSONAtomic(projectContractPath(st), c); err != nil {
				return err
			}
			doc.Set("pointers.project_contract", "project.json")
			if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
				return err
			}
			projectEvent(st, "contract_written", "", "", "")
			return nil
		})
	case "bind":
		if !idRe.MatchString(kv["child"]) {
			return fmt.Errorf("bind needs --child and --seed")
		}
		s, err := projectRead(st)
		if err != nil {
			return err
		}
		if len(s.Dispatch) > 0 {
			return fmt.Errorf("project not approved: %v", s.Dispatch)
		}
		if !slices.Contains(s.Contract.Seeds, kv["seed"]) {
			return fmt.Errorf("seed is outside approved scope")
		}
		found := false
		for _, child := range s.Children {
			if child.ID == kv["child"] {
				found = true
			}
			if child.Seed == kv["seed"] && child.ID != kv["child"] {
				return fmt.Errorf("seed already bound to %s", child.ID)
			}
		}
		if !found {
			return fmt.Errorf("child must already belong to the ticket DAG")
		}
		cs := storeForTicket(st.Env, kv["child"])
		return withLock(cs, func() error {
			doc, err := ticket.ReadDocStrict(cs.IndexPath())
			if err != nil {
				return err
			}
			doc.Set("origin.project_seed", kv["seed"])
			return ticket.WriteDoc(cs.IndexPath(), doc)
		})
	case "seal":
		if kv["file"] != "" {
			return projectSealArtifacts(st, kv["file"])
		}
		return projectSealChild(st)
	case "evidence":
		return projectEvidenceCommand(st, kv)
	case "progress":
		return projectProgressCommand(st, kv)
	case "complete":
		return projectComplete(st, kv["foreman"])
	case "snapshot", "readiness":
		s, err := projectRead(st)
		if err != nil {
			return err
		}
		if verb == "readiness" {
			action := kv["action"]
			if action != "dispatch" && action != "finish" {
				return fmt.Errorf("readiness requires --action dispatch|finish")
			}
			reasons := s.Blockers
			if action == "dispatch" {
				reasons = s.Dispatch
			}
			printV2Envelope(map[string]interface{}{"action": action, "ready": len(reasons) == 0, "reason_codes": reasons, "subject": s.Subject})
		} else {
			printV2Envelope(s)
		}
		return nil
	}
	return fmt.Errorf("unknown project command")
}
