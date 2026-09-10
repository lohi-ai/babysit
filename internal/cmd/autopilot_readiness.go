package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

var (
	fullSHARe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	digestRe  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type readinessResult struct {
	ContractVersion int                    `json:"contract_version"`
	Enforced        bool                   `json:"enforced"`
	Action          string                 `json:"action"`
	Ready           bool                   `json:"ready"`
	ReasonCodes     []string               `json:"reason_codes"`
	Gates           []snapshotGate         `json:"gates"`
	CurrentSubject  map[string]interface{} `json:"current_subject"`
}

func evidenceSchemaVersion(body string) int {
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	_ = json.Unmarshal([]byte(body), &head)
	return head.SchemaVersion
}

func runReadiness(args []string) {
	action := argValue(args, "--action")
	if action != "push" && action != "land" && action != "pr" {
		failV2("INVALID_ACTION", "readiness needs --action push|land|pr", false, nil, 2)
	}
	env := identity.Resolve()
	if env.Ticket == "" {
		failV2("TICKET_REQUIRED", "readiness requires a resolved ticket", false, nil, 3)
	}
	result, err := evaluateReadiness(env, action)
	if err != nil {
		writeSnapshotError(err)
	}
	printV2Envelope(result)
}

func evaluateReadiness(env identity.Env, action string) (readinessResult, error) {
	a := &apState{slug: env.Slug, branch: env.Branch, ticket: env.Ticket, stateRoot: env.ProjectHome}
	s, err := collectAutopilotSnapshot(a, env.Ticket)
	if err != nil {
		return readinessResult{}, err
	}
	result := readinessResult{
		ContractVersion: 1, Action: action, Gates: s.Gates,
		CurrentSubject: currentEvidenceSubject(s),
	}
	if s.Run != nil {
		result.ContractVersion = s.Run.Contract
	}
	result.Enforced = result.ContractVersion == 2
	needed := []string{"review-pr"}
	if action != "push" {
		needed = append(needed, "qa")
	}

	if !result.Enforced {
		st := ticket.New(env)
		for _, gate := range needed {
			status := verdictStatus(st, gate)
			if status != "DONE" && status != "DONE_WITH_CONCERNS" {
				result.ReasonCodes = append(result.ReasonCodes, gate+":legacy_verdict_"+strings.ToLower(status))
			}
		}
		result.Ready = len(result.ReasonCodes) == 0
		return result, nil
	}

	if s.Run != nil {
		if control, ok := s.Run.Control.(map[string]interface{}); ok && stringValue(control["state"]) != "" {
			result.ReasonCodes = append(result.ReasonCodes, "control:"+stringValue(control["state"]))
		}
	}
	ticketHome := ticket.New(env).Home()
	cp, err := readStrictObject(filepath.Join(ticketHome, "checkpoint.json"), true)
	if err != nil {
		return readinessResult{}, err
	}
	accepted, _ := cp["accepted_evidence"].(map[string]interface{})
	for _, gate := range needed {
		path := filepath.Join(ticketHome, "evidence", "verification", "gates", gate+".json")
		ev := readJSONObject(path)
		if ev == nil {
			result.ReasonCodes = append(result.ReasonCodes, gate+":missing")
			continue
		}
		attemptID := stringValue(ev["attempt_id"])
		if accepted == nil || stringValue(accepted[gate]) != attemptID {
			result.ReasonCodes = append(result.ReasonCodes, gate+":unaccepted")
			continue
		}
		for _, reason := range evidenceReadinessReasons(ev, gate, result.CurrentSubject) {
			result.ReasonCodes = append(result.ReasonCodes, gate+":"+reason)
		}
	}
	result.Ready = len(result.ReasonCodes) == 0
	return result, nil
}

func currentEvidenceSubject(s *autopilotSnapshot) map[string]interface{} {
	artifacts := map[string]string{}
	for _, artifact := range s.Artifacts {
		artifacts[artifact.Role] = artifact.Digest
	}
	repoID := ""
	if s.Ticket != nil {
		repoID = digestJSON(s.Ticket.CanonicalRepo)
	}
	return map[string]interface{}{
		"repo_id": repoID, "base_sha": s.Git.BaseSHA, "head_sha": s.Git.Head,
		"tree_digest": s.Git.TreeDigest, "requirement_digest": artifacts["requirement"],
		"plan_digest": artifacts["plan"], "policy_digest": s.Policy.Digest,
		"surface_fingerprint": currentSurfaceFingerprint(),
	}
}

func currentSurfaceFingerprint() string {
	top := gitOut("rev-parse", "--show-toplevel")
	var rows []map[string]string
	for _, rel := range []string{
		"go.mod", "go.sum", ".babysit/qa.yaml", "package.json", "package-lock.json",
		"pnpm-lock.yaml", "yarn.lock", "Cargo.lock", "requirements.txt", "uv.lock",
	} {
		path := filepath.Join(top, filepath.FromSlash(rel))
		if digest, ok := digestFile(path); ok {
			rows = append(rows, map[string]string{"path": rel, "digest": digest})
		}
	}
	return digestJSON(rows)
}

func evidenceReadinessReasons(ev map[string]interface{}, gate string, current map[string]interface{}) []string {
	var reasons []string
	if stringValue(ev["gate"]) != gate {
		reasons = append(reasons, "gate_mismatch")
	}
	status := stringValue(ev["status"])
	if status != "DONE" && status != "DONE_WITH_CONCERNS" {
		reasons = append(reasons, "status_"+strings.ToLower(status))
	}
	if !strings.EqualFold(stringValue(ev["result"]), "PASS") {
		reasons = append(reasons, "result_not_pass")
	}
	if len(interfaceSlice(ev["limitations"])) > 0 {
		reasons = append(reasons, "limited")
	}
	if hasMaterialFindings(interfaceSlice(ev["unresolved_findings"])) {
		reasons = append(reasons, "material_findings")
	}
	subject, _ := ev["subject"].(map[string]interface{})
	for _, key := range []string{"repo_id", "base_sha", "tree_digest", "requirement_digest", "plan_digest", "policy_digest"} {
		if stringValue(subject[key]) != stringValue(current[key]) {
			reasons = append(reasons, "stale_"+key)
		}
	}
	if gate == "qa" {
		surface, _ := ev["surface"].(map[string]interface{})
		if stringValue(surface["fingerprint"]) != stringValue(current["surface_fingerprint"]) {
			reasons = append(reasons, "stale_surface")
		}
	}
	return reasons
}

func hasMaterialFindings(findings []interface{}) bool {
	for _, raw := range findings {
		if finding, ok := raw.(map[string]interface{}); ok {
			severity := strings.ToLower(stringValue(finding["severity"]))
			if severity == "minor" || severity == "nit" || severity == "low" {
				continue
			}
		}
		return true
	}
	return false
}

func interfaceSlice(v interface{}) []interface{} {
	if xs, ok := v.([]interface{}); ok {
		return xs
	}
	return nil
}

func setV2VerificationEvidence(env identity.Env, body []byte) (string, error) {
	var ev map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&ev); err != nil || ev == nil {
		return "", fmt.Errorf("version-2 verification evidence is not a JSON object")
	}
	if err := validateV2VerificationEvidence(ev, env); err != nil {
		return "", err
	}

	a := &apState{slug: env.Slug, branch: env.Branch, ticket: env.Ticket, stateRoot: env.ProjectHome}
	s, err := collectAutopilotSnapshot(a, env.Ticket)
	if err != nil {
		return "", err
	}
	if s.Run == nil || s.Run.Contract != 2 {
		return "", fmt.Errorf("version-2 evidence needs a schema_version 2 checkpoint")
	}
	gate := stringValue(ev["gate"])
	if reasons := evidenceReadinessReasons(ev, gate, currentEvidenceSubject(s)); containsStaleReason(reasons) {
		return "", fmt.Errorf("evidence is stale for current state: %s", strings.Join(reasons, ","))
	}

	st := ticket.New(env)
	attemptID := stringValue(ev["attempt_id"])
	ticketHome := st.Home()
	var resultPath string
	err = withLock(st, func() error {
		attempt := readJSONObject(filepath.Join(ticketHome, "attempts", attemptID+".json"))
		if attempt == nil {
			return fmt.Errorf("attempt %s does not exist", attemptID)
		}
		if stringValue(attempt["state"]) != "completed" {
			return fmt.Errorf("attempt %s is %s, not completed", attemptID, stringValue(attempt["state"]))
		}
		producer, _ := ev["producer"].(map[string]interface{})
		for key, pair := range map[string][2]string{
			"ticket": {stringValue(ev["ticket"]), env.Ticket},
			"run":    {stringValue(ev["run_id"]), stringValue(attempt["run_id"])},
			"gate":   {gate, stringValue(attempt["gate"])},
			"owner":  {stringValue(producer["owner"]), stringValue(attempt["owner"])},
		} {
			if pair[0] != pair[1] {
				return fmt.Errorf("%s mismatch: evidence=%q attempt=%q", key, pair[0], pair[1])
			}
		}

		attemptDir := filepath.Join(ticketHome, "evidence", "verification", "attempts")
		gateDir := filepath.Join(ticketHome, "evidence", "verification", "gates")
		if err := os.MkdirAll(attemptDir, 0o755); err != nil {
			return err
		}
		if err := os.MkdirAll(gateDir, 0o755); err != nil {
			return err
		}
		canonical, _ := json.MarshalIndent(ev, "", "  ")
		canonical = append(canonical, '\n')
		immutable := filepath.Join(attemptDir, attemptID+".json")
		if prior, readErr := os.ReadFile(immutable); readErr == nil {
			if !bytes.Equal(prior, canonical) {
				return fmt.Errorf("immutable evidence for attempt %s already differs", attemptID)
			}
		} else if !os.IsNotExist(readErr) {
			return readErr
		} else if err := ticket.WriteAtomic(immutable, canonical); err != nil {
			return err
		}

		cpPath := filepath.Join(ticketHome, "checkpoint.json")
		cp, cpErr := readStrictObject(cpPath, true)
		if cpErr != nil {
			return cpErr
		}
		if contractVersion(cp) != 2 || stringValue(cp["run_id"]) != stringValue(ev["run_id"]) {
			return fmt.Errorf("checkpoint run/version changed before evidence acceptance")
		}
		accepted, _ := cp["accepted_evidence"].(map[string]interface{})
		if accepted == nil {
			accepted = map[string]interface{}{}
			cp["accepted_evidence"] = accepted
		}
		accepted[gate] = attemptID
		cp["revision"] = int64Value(cp["revision"]) + 1
		cp["updated_at"] = isoNow()
		cpBody, _ := json.Marshal(cp)
		cpBody = append(cpBody, '\n')
		if err := ticket.WriteAtomic(cpPath, cpBody); err != nil {
			return err
		}
		resultPath = filepath.Join(ticketHome, "evidence", "verification", "result.json")
		if err := ticket.WriteAtomic(filepath.Join(gateDir, gate+".json"), canonical); err != nil {
			return err
		}
		if err := ticket.WriteAtomic(resultPath, canonical); err != nil {
			return err
		}
		projection := fmt.Sprintf("STATUS: %s\nVERDICT: %s\nSUMMARY: Accepted immutable v2 %s evidence for attempt %s.\nEVIDENCE: %s\n",
			stringValue(ev["status"]), strings.ToUpper(stringValue(ev["result"])), gate, attemptID, immutable)
		if err := ticket.WriteAtomic(st.VerdictPath(gate), []byte(projection)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	st.HistoryAppend("evidence", gate)
	return resultPath, nil
}

func validateV2VerificationEvidence(ev map[string]interface{}, env identity.Env) error {
	required := []string{"ticket", "run_id", "attempt_id", "gate", "status", "result", "subject", "producer", "checks", "unresolved_findings", "limitations", "started_at", "completed_at"}
	for _, key := range required {
		if _, ok := ev[key]; !ok {
			return fmt.Errorf("version-2 verification missing field %s", key)
		}
	}
	if int(int64Value(ev["schema_version"])) != 2 {
		return fmt.Errorf("unsupported verification schema_version")
	}
	if stringValue(ev["ticket"]) != env.Ticket {
		return fmt.Errorf("evidence ticket %q does not match current ticket %q", stringValue(ev["ticket"]), env.Ticket)
	}
	for name, value := range map[string]string{"run_id": stringValue(ev["run_id"]), "attempt_id": stringValue(ev["attempt_id"])} {
		if value == "" || safeTicket(value) != value {
			return fmt.Errorf("invalid %s", name)
		}
	}
	gate := stringValue(ev["gate"])
	if gate != "review-pr" && gate != "qa" {
		return fmt.Errorf("gate must be review-pr or qa")
	}
	status := stringValue(ev["status"])
	if status != "DONE" && status != "DONE_WITH_CONCERNS" && status != "BLOCKED" {
		return fmt.Errorf("invalid evidence status %q", status)
	}
	result := strings.ToUpper(stringValue(ev["result"]))
	if result != "PASS" && result != "FAIL" {
		return fmt.Errorf("verification result must be PASS or FAIL")
	}
	subject, ok := ev["subject"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("subject must be an object")
	}
	for _, key := range []string{"repo_id", "tree_digest", "requirement_digest", "policy_digest"} {
		if !digestRe.MatchString(stringValue(subject[key])) {
			return fmt.Errorf("subject.%s must be a sha256 digest", key)
		}
	}
	if plan := stringValue(subject["plan_digest"]); plan != "" && !digestRe.MatchString(plan) {
		return fmt.Errorf("subject.plan_digest must be null/empty or a sha256 digest")
	}
	for _, key := range []string{"base_sha", "head_sha"} {
		if !fullSHARe.MatchString(stringValue(subject[key])) {
			return fmt.Errorf("subject.%s must be a full git SHA", key)
		}
	}
	producer, ok := ev["producer"].(map[string]interface{})
	if !ok || stringValue(producer["harness"]) == "" || stringValue(producer["owner"]) == "" || stringValue(producer["isolation"]) == "" {
		return fmt.Errorf("producer needs harness, isolation, and owner")
	}
	checks, ok := ev["checks"].([]interface{})
	if !ok || (gate == "qa" && len(checks) == 0) {
		return fmt.Errorf("qa evidence needs at least one check")
	}
	for i, raw := range checks {
		check, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("checks[%d] must be an object", i)
		}
		argv := interfaceSlice(check["argv"])
		if len(argv) == 0 || stringValue(check["cwd"]) == "" {
			return fmt.Errorf("checks[%d] needs argv and cwd", i)
		}
		if strings.EqualFold(result, "PASS") && int64Value(check["exit_code"]) != 0 {
			return fmt.Errorf("PASS contradicts checks[%d].exit_code", i)
		}
		if logPath := stringValue(check["log_path"]); logPath != "" {
			resolved, err := resolveEvidencePath(logPath, ticket.New(env).Home(), stringValue(check["cwd"]))
			if err != nil {
				return fmt.Errorf("checks[%d].log_path: %w", i, err)
			}
			claimed := stringValue(check["log_digest"])
			if !digestRe.MatchString(claimed) {
				return fmt.Errorf("checks[%d].log_digest must be a sha256 digest", i)
			}
			actual, _ := digestFile(resolved)
			if actual != claimed {
				return fmt.Errorf("checks[%d].log_digest does not match log_path", i)
			}
		}
	}
	if _, ok := ev["unresolved_findings"].([]interface{}); !ok {
		return fmt.Errorf("unresolved_findings must be an array")
	}
	if _, ok := ev["limitations"].([]interface{}); !ok {
		return fmt.Errorf("limitations must be an array")
	}
	start, startErr := time.Parse(time.RFC3339, stringValue(ev["started_at"]))
	end, endErr := time.Parse(time.RFC3339, stringValue(ev["completed_at"]))
	if startErr != nil || endErr != nil || end.Before(start) {
		return fmt.Errorf("started_at/completed_at must be ordered RFC3339 timestamps")
	}
	return nil
}

func resolveEvidencePath(path string, roots ...string) (string, error) {
	if strings.Contains(path, "\x00") {
		return "", fmt.Errorf("contains NUL")
	}
	for _, root := range roots {
		candidate := path
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		absRoot, _ := filepath.Abs(root)
		absCandidate, _ := filepath.Abs(candidate)
		if absCandidate != absRoot && !strings.HasPrefix(absCandidate, absRoot+string(os.PathSeparator)) {
			continue
		}
		resolvedRoot, rootErr := filepath.EvalSymlinks(absRoot)
		resolvedCandidate, candidateErr := filepath.EvalSymlinks(absCandidate)
		if rootErr == nil && candidateErr == nil && (resolvedCandidate == resolvedRoot || strings.HasPrefix(resolvedCandidate, resolvedRoot+string(os.PathSeparator))) {
			return resolvedCandidate, nil
		}
	}
	return "", fmt.Errorf("must resolve inside ticket or check cwd and exist")
}

func containsStaleReason(reasons []string) bool {
	for _, reason := range reasons {
		if strings.HasPrefix(reason, "stale_") || reason == "gate_mismatch" {
			return true
		}
	}
	return false
}

// readinessReasonText is stable, shell-safe summary text for hook adapters.
func readinessReasonText(reasons []string) string {
	copyReasons := append([]string(nil), reasons...)
	sort.Strings(copyReasons)
	return strings.Join(copyReasons, ",")
}

// ticketV2Readiness evaluates a ticket against the worktree recorded in its
// manifest. Land can run from the primary checkout, whose tree is intentionally
// not the ticket tree; evaluating there would make every fresh ticket stale.
func ticketV2Readiness(primary string, base identity.Env, ticketID, action string) (bool, bool, []string, error) {
	st := storeForTicket(base, ticketID)
	dir := primary
	if manifest, err := ticket.ReadManifest(st.ManifestPath()); err == nil {
		branch := ticketBranch(ticketID)
		for _, repo := range manifest.Repos {
			if repo.Branch == branch && repo.Worktree != "" {
				dir = repo.Worktree
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(primary, dir)
				}
				break
			}
		}
	}
	cmd := exec.Command(selfBin(), "ticket", "readiness", "--json", "--action", action)
	cmd.Dir = dir
	cmd.Env = overlayProcessEnv(map[string]string{
		"BABYSIT_TICKET":       ticketID,
		"BBS_TICKET":           "",
		"BABYSIT_PROJECT_HOME": base.ProjectHome,
	})
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return false, false, nil, fmt.Errorf("readiness exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(out)))
		}
		return false, false, nil, err
	}
	var envelope struct {
		SchemaVersion int             `json:"schema_version"`
		OK            bool            `json:"ok"`
		Data          readinessResult `json:"data"`
		Error         *v2Error        `json:"error"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return false, false, nil, fmt.Errorf("invalid readiness response: %w", err)
	}
	if envelope.SchemaVersion != 2 || !envelope.OK {
		if envelope.Error != nil {
			return false, false, nil, fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
		}
		return false, false, nil, fmt.Errorf("unsupported readiness response")
	}
	return envelope.Data.Enforced, envelope.Data.Ready, envelope.Data.ReasonCodes, nil
}

func overlayProcessEnv(values map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(values))
	for _, pair := range os.Environ() {
		key := pair
		if i := strings.IndexByte(pair, '='); i >= 0 {
			key = pair[:i]
		}
		if _, replaced := values[key]; !replaced {
			out = append(out, pair)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}
