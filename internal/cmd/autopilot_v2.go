package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

type v2Envelope struct {
	SchemaVersion int         `json:"schema_version"`
	OK            bool        `json:"ok"`
	Data          interface{} `json:"data,omitempty"`
	Error         *v2Error    `json:"error,omitempty"`
}

type v2Error struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Retryable bool        `json:"retryable"`
	Details   interface{} `json:"details,omitempty"`
}

type snapshotTicket struct {
	ID             string `json:"id"`
	Project        string `json:"project"`
	CanonicalRepo  string `json:"canonical_repo"`
	Worktree       string `json:"worktree"`
	IdentitySource string `json:"identity_source"`
}

type snapshotRun struct {
	ID             string      `json:"id,omitempty"`
	Workflow       string      `json:"workflow,omitempty"`
	WorkflowDigest string      `json:"workflow_digest,omitempty"`
	Mode           string      `json:"mode,omitempty"`
	Control        interface{} `json:"control,omitempty"`
	StopAfter      string      `json:"stop_after,omitempty"`
	Contract       int         `json:"contract_version"`
}

type snapshotGit struct {
	Branch           string `json:"branch"`
	Head             string `json:"head"`
	TreeDigest       string `json:"tree_digest"`
	BaseRef          string `json:"base_ref"`
	BaseSHA          string `json:"base_sha"`
	Dirty            bool   `json:"dirty"`
	Upstream         string `json:"upstream,omitempty"`
	RemoteHead       string `json:"remote_head,omitempty"`
	RemoteObservedAt string `json:"remote_observed_at,omitempty"`
	PushedExactly    bool   `json:"pushed_exactly"`
}

type snapshotPolicy struct {
	Effective  map[string]string `json:"effective"`
	Provenance map[string]string `json:"provenance"`
	Digest     string            `json:"digest"`
}

type snapshotArtifact struct {
	Role        string   `json:"role"`
	Path        string   `json:"path"`
	Digest      string   `json:"digest,omitempty"`
	RequiredFor []string `json:"required_for,omitempty"`
	Exists      bool     `json:"exists"`
}

type snapshotGate struct {
	Name         string   `json:"name"`
	State        string   `json:"state"`
	AttemptID    string   `json:"attempt_id,omitempty"`
	ReasonCodes  []string `json:"reason_codes,omitempty"`
	EvidencePath string   `json:"evidence_path,omitempty"`
}

type snapshotObligation struct {
	Kind              string   `json:"kind"`
	Reason            string   `json:"reason"`
	RequiredArtifacts []string `json:"required_artifacts,omitempty"`
	AllowedActions    []string `json:"allowed_actions"`
}

type autopilotSnapshot struct {
	SchemaVersion int                    `json:"schema_version"`
	SnapshotID    string                 `json:"snapshot_id"`
	StateRevision int64                  `json:"state_revision"`
	Ticket        *snapshotTicket        `json:"ticket"`
	Run           *snapshotRun           `json:"run"`
	Git           snapshotGit            `json:"git"`
	Policy        snapshotPolicy         `json:"policy"`
	Artifacts     []snapshotArtifact     `json:"artifacts"`
	Gates         []snapshotGate         `json:"gates"`
	ActiveAttempt map[string]interface{} `json:"active_attempt"`
	Obligations   []snapshotObligation   `json:"obligations"`
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func argValue(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"=")
		}
	}
	return ""
}

func printV2Envelope(data interface{}) {
	_ = json.NewEncoder(os.Stdout).Encode(v2Envelope{SchemaVersion: 2, OK: true, Data: data})
}

func failV2(code, message string, retryable bool, details interface{}, exitCode int) {
	_ = json.NewEncoder(os.Stdout).Encode(v2Envelope{
		SchemaVersion: 2, OK: false,
		Error: &v2Error{Code: code, Message: message, Retryable: retryable, Details: details},
	})
	os.Exit(exitCode)
}

func (a *apState) snapshotV2(args []string) {
	s, err := collectAutopilotSnapshot(a, argValue(args, "--ticket"))
	if err != nil {
		writeSnapshotError(err)
	}
	printV2Envelope(s)
}

func writeSnapshotError(err error) {
	var se *snapshotError
	if errors.As(err, &se) {
		failV2(se.Code, se.Message, se.Retryable, se.Details, se.Exit)
	}
	failV2("IO_ERROR", err.Error(), false, nil, 1)
}

type snapshotError struct {
	Code, Message string
	Retryable     bool
	Details       interface{}
	Exit          int
}

func (e *snapshotError) Error() string { return e.Message }

// collectAutopilotSnapshot is intentionally mutation-free. Ticket files are
// sampled twice instead of taking the mkdir-based writer lock; that lock would
// itself violate the read-purity contract. One changed sample is retried and a
// persistently moving state is reported rather than returned as a mixed view.
func collectAutopilotSnapshot(a *apState, explicitTicket string) (*autopilotSnapshot, error) {
	for attempt := 0; attempt < 2; attempt++ {
		ticketID := a.ticket
		if explicitTicket != "" {
			ticketID = safeTicket(explicitTicket)
			if ticketID == "" || ticketID != explicitTicket {
				return nil, &snapshotError{Code: "INVALID_TICKET", Message: "ticket id contains unsupported characters", Exit: 2}
			}
		}
		var ticketHome string
		if ticketID != "" {
			ticketHome = filepath.Join(a.stateRoot, "tickets", ticketID)
			if explicitTicket != "" {
				if fi, err := os.Stat(ticketHome); err != nil || !fi.IsDir() {
					return nil, &snapshotError{Code: "TICKET_NOT_FOUND", Message: "explicit ticket does not exist", Details: map[string]string{"ticket": ticketID}, Exit: 3}
				}
			}
		}
		before, err := snapshotStateToken(ticketHome)
		if err != nil {
			return nil, err
		}
		s, err := collectAutopilotSnapshotOnce(a, ticketID, ticketHome)
		if err != nil {
			return nil, err
		}
		after, err := snapshotStateToken(ticketHome)
		if err != nil {
			return nil, err
		}
		if before == after {
			s.SnapshotID = snapshotContentDigest(s)
			return s, nil
		}
	}
	return nil, &snapshotError{Code: "STATE_CHANGED", Message: "ticket state changed while it was being read", Retryable: true, Exit: 3}
}

func collectAutopilotSnapshotOnce(a *apState, ticketID, ticketHome string) (*autopilotSnapshot, error) {
	top := gitOut("rev-parse", "--show-toplevel")
	if top == "" {
		return nil, &snapshotError{Code: "NOT_A_REPOSITORY", Message: "snapshot requires a git repository", Exit: 3}
	}
	policy, err := snapshotGitFlow(top)
	if err != nil {
		return nil, &snapshotError{Code: "POLICY_INVALID", Message: err.Error(), Exit: 3}
	}
	gitState, err := snapshotGitState(top, policy.Effective["base_branch"])
	if err != nil {
		var se *snapshotError
		if errors.As(err, &se) {
			return nil, se
		}
		return nil, &snapshotError{Code: "GIT_STATE_UNAVAILABLE", Message: err.Error(), Retryable: true, Exit: 3}
	}

	s := &autopilotSnapshot{SchemaVersion: 2, Git: gitState, Policy: policy}
	if ticketID == "" {
		s.Artifacts = []snapshotArtifact{}
		s.Gates = []snapshotGate{{Name: "review-pr", State: "missing"}, {Name: "qa", State: "missing"}}
		s.Obligations = []snapshotObligation{{Kind: "requirement", Reason: "no durable ticket; use conversation context", AllowedActions: []string{"direct-skill"}}}
		return s, nil
	}

	indexPath := filepath.Join(ticketHome, "index.json")
	checkpointPath := filepath.Join(ticketHome, "checkpoint.json")
	manifestPath := filepath.Join(ticketHome, "manifest.yaml")
	idx, err := readStrictObject(indexPath, false)
	if err != nil {
		return nil, err
	}
	cp, err := readStrictObject(checkpointPath, false)
	if err != nil {
		return nil, err
	}
	st := ticket.New(identity.Env{Slug: a.slug, Branch: a.branch, Ticket: ticketID, ProjectHome: a.stateRoot})
	canonical, worktree := top, top
	if m, err := ticket.ReadManifest(manifestPath); err == nil {
		if r := m.FindRepo(func(r ticket.Repo) bool {
			return r.Branch == a.branch || samePath(r.Worktree, top)
		}); r != nil {
			canonical, worktree = r.Canonical, r.Worktree
		}
	} else if !os.IsNotExist(err) {
		return nil, &snapshotError{Code: "STATE_MALFORMED", Message: "malformed required state: " + manifestPath, Details: map[string]string{"path": manifestPath}, Exit: 3}
	}
	s.Ticket = &snapshotTicket{ID: ticketID, Project: a.slug, CanonicalRepo: canonical, Worktree: worktree, IdentitySource: identitySource(ticketID)}
	s.StateRevision = int64Value(cp["revision"])
	s.Run = &snapshotRun{
		ID: stringValue(cp["run_id"]), Workflow: stringValue(cp["workflow"]),
		WorkflowDigest: stringValue(cp["workflow_digest"]), Mode: deriveMode(idx, ticketHome, top),
		Control: idx.Value("control"), StopAfter: stringValue(cp["stop_after"]), Contract: contractVersion(cp),
	}
	activeID := stringValue(cp["active_attempt_id"])
	if activeID != "" {
		if safeTicket(activeID) != activeID {
			return nil, &snapshotError{Code: "STATE_MALFORMED", Message: "checkpoint has invalid active_attempt_id", Exit: 3}
		}
		if _, err := readAttemptRecordStrict(filepath.Join(ticketHome, "attempts", activeID+".json")); err != nil {
			return nil, &snapshotError{Code: "STATE_MALFORMED", Message: "active attempt is malformed or unreadable", Details: map[string]string{"attempt_id": activeID}, Exit: 3}
		}
	}
	s.Artifacts = collectSnapshotArtifacts(ticketHome, s.Run.Workflow)
	s.Gates = collectSnapshotGates(ticketHome, cp)
	s.ActiveAttempt = activeAttempt(ticketHome, activeID)
	s.Obligations = deriveObligations(s, st)
	return s, nil
}

// readStrictObject decodes one JSON-object state file through the record
// layer's strict read, mapping its ReadError onto the snapshot error contract:
// unreadable → STATE_UNREADABLE, malformed → STATE_MALFORMED. A missing file
// is only an error when required; otherwise the empty record stands in.
func readStrictObject(path string, required bool) (ticket.Doc, error) {
	d, err := ticket.ReadDocStrict(path)
	if err == nil {
		return d, nil
	}
	var re *ticket.ReadError
	if errors.As(err, &re) && re.Kind == ticket.KindMissing && os.IsNotExist(re.Err) && !required {
		return ticket.Doc{}, nil
	}
	if errors.As(err, &re) && re.Kind == ticket.KindMalformed {
		return nil, &snapshotError{Code: "STATE_MALFORMED", Message: "malformed required state: " + path, Details: map[string]string{"path": path}, Exit: 3}
	}
	return nil, &snapshotError{Code: "STATE_UNREADABLE", Message: err.Error(), Exit: 3}
}

func requireJSONEOF(dec *json.Decoder) error {
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func snapshotGitFlow(dir string) (snapshotPolicy, error) {
	p, err := resolveGitFlow(dir)
	if err != nil {
		return snapshotPolicy{}, err
	}
	effective := map[string]string{
		"profile": p.Profile, "base_branch": p.BaseBranch, "mode": p.Mode,
		"land": p.Land, "finish": p.Finish, "push": p.Push,
		"rigor": p.Rigor, "review_effort": p.ReviewEffort,
	}
	provenance := gitFlowProvenance(dir)
	digest := digestJSON(map[string]interface{}{"effective": effective, "provenance": provenance})
	return snapshotPolicy{Effective: effective, Provenance: provenance, Digest: digest}, nil
}

func snapshotGitState(dir, base string) (snapshotGit, error) {
	for attempt := 0; attempt < 2; attempt++ {
		first, err := snapshotGitStateOnce(dir, base)
		if err != nil {
			return snapshotGit{}, err
		}
		second, err := snapshotGitStateOnce(dir, base)
		if err != nil {
			return snapshotGit{}, err
		}
		if first == second {
			return second, nil
		}
	}
	return snapshotGit{}, &snapshotError{Code: "STATE_CHANGED", Message: "git state changed while it was being read", Retryable: true, Exit: 3}
}

func snapshotGitStateOnce(dir, base string) (snapshotGit, error) {
	head, err := gitOutputStrict(dir, "rev-parse", "HEAD")
	if err != nil {
		return snapshotGit{}, err
	}
	branch := gitOutIn(dir, "branch", "--show-current")
	statusRaw, err := gitOutputBytes(dir, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return snapshotGit{}, err
	}
	treeDigest, err := workingTreeDigest(dir, statusRaw)
	if err != nil {
		return snapshotGit{}, err
	}
	baseRef := base
	if gitOKIn(dir, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+base) {
		baseRef = "origin/" + base
	}
	baseSHA := gitOutIn(dir, "rev-parse", baseRef)
	upstream := gitOutIn(dir, "rev-parse", "--abbrev-ref", "@{u}")
	remoteHead := ""
	observedAt := ""
	if upstream != "" {
		remoteHead = gitOutIn(dir, "rev-parse", upstream)
		if gitDir := gitOutIn(dir, "rev-parse", "--git-common-dir"); gitDir != "" {
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(dir, gitDir)
			}
			refPath := filepath.Join(filepath.Clean(gitDir), "refs", "remotes", filepath.FromSlash(upstream))
			if fi, statErr := os.Stat(refPath); statErr == nil {
				observedAt = fi.ModTime().UTC().Format(time.RFC3339)
			}
		}
	}
	return snapshotGit{
		Branch: branch, Head: head, TreeDigest: treeDigest, BaseRef: baseRef, BaseSHA: baseSHA,
		Dirty: len(statusRaw) > 0, Upstream: upstream, RemoteHead: remoteHead,
		RemoteObservedAt: observedAt, PushedExactly: remoteHead != "" && remoteHead == head,
	}, nil
}

func workingTreeDigest(dir string, status []byte) (string, error) {
	index, err := gitOutputBytes(dir, "ls-files", "-s", "-z")
	if err != nil {
		return "", err
	}
	pathsRaw, err := gitOutputBytes(dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	paths := strings.Split(string(pathsRaw), "\x00")
	sort.Strings(paths)
	h := sha256.New()
	_, _ = h.Write(index)
	_, _ = h.Write(status)
	for _, rel := range paths {
		if rel == "" {
			continue
		}
		_, _ = io.WriteString(h, "\x00"+rel+"\x00")
		path := filepath.Join(dir, filepath.FromSlash(rel))
		fi, statErr := os.Lstat(path)
		if statErr != nil {
			_, _ = io.WriteString(h, "<missing>")
			continue
		}
		_, _ = io.WriteString(h, fi.Mode().String()+"\x00")
		if fi.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return "", readErr
			}
			_, _ = io.WriteString(h, target)
			continue
		}
		if fi.Mode().IsRegular() {
			f, openErr := os.Open(path)
			if openErr != nil {
				return "", openErr
			}
			_, copyErr := io.Copy(h, f)
			_ = f.Close()
			if copyErr != nil {
				return "", copyErr
			}
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func gitOutputStrict(dir string, args ...string) (string, error) {
	b, err := gitOutputBytes(dir, args...)
	return strings.TrimSpace(string(b)), err
}

func gitOutputBytes(dir string, args ...string) ([]byte, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	b, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return b, nil
}

func collectSnapshotArtifacts(ticketHome, workflow string) []snapshotArtifact {
	items := []struct {
		role, path string
		required   []string
	}{
		{"requirement", filepath.Join(ticketHome, "requirement.md"), []string{"implement", "review-pr", "qa"}},
		{"plan", filepath.Join(ticketHome, "plan.md"), []string{"implement", "review-pr", "qa"}},
		{"checkpoint", filepath.Join(ticketHome, "checkpoint.json"), []string{"recover"}},
		{"manifest", filepath.Join(ticketHome, "manifest.yaml"), []string{"identity", "finish"}},
	}
	if workflow != "" {
		if path, ok := resolveWorkflowPath(workflow); ok {
			items = append(items, struct {
				role, path string
				required   []string
			}{"workflow", path, []string{"route", "recover"}})
		}
	}
	latest := filepath.Join(ticketHome, "handoffs", "LATEST")
	if b, err := os.ReadFile(latest); err == nil {
		name := strings.TrimSpace(string(b))
		if !invalidLatestName(name) {
			items = append(items, struct {
				role, path string
				required   []string
			}{"latest_handoff", filepath.Join(ticketHome, "handoffs", name), []string{"recover"}})
		}
	}
	out := make([]snapshotArtifact, 0, len(items))
	for _, item := range items {
		digest, exists := digestFile(item.path)
		out = append(out, snapshotArtifact{Role: item.role, Path: item.path, Digest: digest, RequiredFor: item.required, Exists: exists})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	return out
}

func collectSnapshotGates(ticketHome string, cp map[string]interface{}) []snapshotGate {
	out := make([]snapshotGate, 0, 2)
	accepted, _ := cp["accepted_evidence"].(map[string]interface{})
	for _, name := range []string{"review-pr", "qa"} {
		gate := snapshotGate{Name: name, State: "missing"}
		attemptID := stringValue(accepted[name])
		if attemptID != "" && safeTicket(attemptID) == attemptID {
			path := filepath.Join(ticketHome, "evidence", "verification", "attempts", attemptID+".json")
			ev, err := readStrictObject(path, true)
			if err != nil {
				gate.State, gate.ReasonCodes = "unknown", []string{"malformed_evidence"}
			} else if stringValue(ev["attempt_id"]) != attemptID || stringValue(ev["gate"]) != name {
				gate.State, gate.ReasonCodes = "unknown", []string{"evidence_identity_mismatch"}
			} else {
				gate.State = evidenceGateState(ev)
				gate.AttemptID = attemptID
				gate.EvidencePath = path
			}
		} else if attemptID != "" {
			gate.State, gate.ReasonCodes = "unknown", []string{"invalid_attempt_id"}
		}
		out = append(out, gate)
	}
	return out
}

func evidenceGateState(ev map[string]interface{}) string {
	if len(stringSlice(ev["limitations"])) > 0 {
		return "limited"
	}
	if strings.EqualFold(stringValue(ev["result"]), "FAIL") || hasMaterialFindings(interfaceSlice(ev["unresolved_findings"])) {
		return "fail"
	}
	status := stringValue(ev["status"])
	if strings.EqualFold(stringValue(ev["result"]), "PASS") && (status == "DONE" || status == "DONE_WITH_CONCERNS") {
		return "pass"
	}
	return "unknown"
}

func activeAttempt(ticketHome, requested string) map[string]interface{} {
	if requested != "" {
		if rec := readJSONObject(filepath.Join(ticketHome, "attempts", requested+".json")); rec != nil && !terminalAttemptStateV2(stringValue(rec["state"])) {
			return projectActiveAttempt(rec, filepath.Join(ticketHome, "attempts", requested+".json"))
		}
	}
	paths, _ := filepath.Glob(filepath.Join(ticketHome, "attempts", "*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	for _, path := range paths {
		if rec := readJSONObject(path); rec != nil && !terminalAttemptStateV2(stringValue(rec["state"])) {
			return projectActiveAttempt(rec, path)
		}
	}
	return nil
}

// projectActiveAttempt keeps snapshots/context bounded and prevents arbitrary
// assignment/result bodies from becoming telemetry-like context output.
func projectActiveAttempt(rec map[string]interface{}, path string) map[string]interface{} {
	out := map[string]interface{}{}
	for _, key := range []string{"id", "ticket", "run_id", "gate", "state", "revision", "owner", "updated_at"} {
		if value, ok := rec[key]; ok {
			out[key] = value
		}
	}
	if runtime, ok := rec["runtime"].(map[string]interface{}); ok {
		projected := map[string]interface{}{}
		for _, key := range []string{"kind", "pid", "process_start", "handle", "transport_id"} {
			if value, ok := runtime[key]; ok {
				projected[key] = value
			}
		}
		out["runtime"] = projected
	}
	for _, key := range []string{"result", "failure"} {
		if value, ok := rec[key].(map[string]interface{}); ok {
			if logPath := stringValue(value["log_path"]); logPath != "" {
				out[key] = map[string]interface{}{"log_path": logPath}
			}
		}
	}
	if rec := readAttemptRecord(path); rec != nil {
		out["liveness"] = attemptLiveness(rec)
	}
	return out
}

func readJSONObject(path string) map[string]interface{} {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&out) != nil || out == nil || requireJSONEOF(dec) != nil {
		return nil
	}
	return out
}

func terminalAttemptStateV2(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled"
}

func deriveObligations(s *autopilotSnapshot, _ *ticket.Store) []snapshotObligation {
	var out []snapshotObligation
	if s.Run != nil && s.Run.Control != nil {
		if control, ok := s.Run.Control.(map[string]interface{}); ok && stringValue(control["state"]) != "" {
			out = append(out, snapshotObligation{Kind: "control", Reason: "ticket is " + stringValue(control["state"]), AllowedActions: []string{"reconcile-control"}})
			return out
		}
	}
	if s.ActiveAttempt != nil {
		out = append(out, snapshotObligation{Kind: "attempt", Reason: "active attempt requires liveness/result reconciliation", AllowedActions: []string{"wait", "reconcile", "cancel"}})
	}
	for _, gate := range s.Gates {
		if gate.State != "pass" {
			out = append(out, snapshotObligation{Kind: "gate", Reason: gate.Name + " is " + gate.State, RequiredArtifacts: []string{"requirement", "plan"}, AllowedActions: []string{"run-" + gate.Name}})
		}
	}
	if len(out) == 0 {
		out = append(out, snapshotObligation{Kind: "finish", Reason: "all recorded gates pass; recheck readiness at the requested boundary", AllowedActions: []string{"readiness"}})
	}
	return out
}

func deriveMode(idx ticket.Doc, ticketHome, top string) string {
	if idx.Get("origin.type") == "sub_ticket" {
		return "child"
	}
	if fi, err := os.Stat(filepath.Join(ticketHome, "manifest.md")); err == nil && !fi.IsDir() {
		return "orchestrate"
	}
	if fileNonEmpty(filepath.Join(ticketHome, "plan.md")) {
		return "implement"
	}
	if fileNonEmpty(filepath.Join(ticketHome, "requirement.md")) {
		return "build"
	}
	base := baseBranchIn(top)
	if gitOutIn(top, "rev-list", "--count", base+"..HEAD") != "0" {
		return "verify"
	}
	return ""
}

func identitySource(ticketID string) string {
	if os.Getenv("BABYSIT_TICKET") == ticketID {
		return "env:BABYSIT_TICKET"
	}
	if os.Getenv("BBS_TICKET") == ticketID {
		return "env:BBS_TICKET"
	}
	return "canonical-resolver"
}

func snapshotStateToken(ticketHome string) (string, error) {
	if ticketHome == "" {
		return "", nil
	}
	var paths []string
	for _, rel := range []string{"index.json", "checkpoint.json", "manifest.yaml", "requirement.md", "plan.md", filepath.Join("handoffs", "LATEST")} {
		paths = append(paths, filepath.Join(ticketHome, rel))
	}
	if latest, err := os.ReadFile(filepath.Join(ticketHome, "handoffs", "LATEST")); err == nil {
		name := strings.TrimSpace(string(latest))
		if !invalidLatestName(name) {
			paths = append(paths, filepath.Join(ticketHome, "handoffs", name))
		}
	}
	for _, pattern := range []string{
		filepath.Join(ticketHome, "attempts", "*.json"),
		filepath.Join(ticketHome, "evidence", "verification", "attempts", "*.json"),
		filepath.Join(ticketHome, "evidence", "verification", "gates", "*.json"),
	} {
		matches, _ := filepath.Glob(pattern)
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		_, _ = io.WriteString(h, path+"\x00")
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				_, _ = io.WriteString(h, "<missing>")
				continue
			}
			return "", &snapshotError{Code: "STATE_UNREADABLE", Message: err.Error(), Exit: 3}
		}
		_, _ = h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func snapshotContentDigest(s *autopilotSnapshot) string {
	clone := *s
	clone.SnapshotID = ""
	clone.Git.RemoteObservedAt = ""
	return digestJSON(clone)
}

func digestJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), true
}

func contractVersion(cp map[string]interface{}) int {
	v := int(int64Value(cp["schema_version"]))
	if v == 0 {
		return 1
	}
	return v
}

func int64Value(v interface{}) int64 {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Int64()
		return n
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

func exactInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		n := int64(x)
		return n, float64(n) == x
	}
	return 0, false
}

func stringValue(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func stringSlice(v interface{}) []string {
	var out []string
	if xs, ok := v.([]interface{}); ok {
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && filepath.Clean(aa) == filepath.Clean(bb)
}
