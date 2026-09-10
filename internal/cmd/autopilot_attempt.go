package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

type attemptRecord struct {
	SchemaVersion   int                    `json:"schema_version"`
	ID              string                 `json:"id"`
	Ticket          string                 `json:"ticket"`
	RunID           string                 `json:"run_id"`
	Gate            string                 `json:"gate"`
	State           string                 `json:"state"`
	Revision        int64                  `json:"revision"`
	Owner           string                 `json:"owner"`
	IdempotencyKey  string                 `json:"idempotency_key"`
	ExpectedState   int64                  `json:"expected_state_revision"`
	Assignment      map[string]interface{} `json:"assignment,omitempty"`
	Runtime         map[string]interface{} `json:"runtime,omitempty"`
	Result          interface{}            `json:"result,omitempty"`
	Failure         interface{}            `json:"failure,omitempty"`
	WaitingReason   string                 `json:"waiting_reason,omitempty"`
	CreatedAt       string                 `json:"created_at"`
	UpdatedAt       string                 `json:"updated_at"`
	TerminationSeen bool                   `json:"termination_confirmed,omitempty"`
}

func (a *apState) attemptV2(args []string) {
	if len(args) == 0 {
		failV2("USAGE", "attempt needs start|update|show", false, nil, 2)
	}
	switch args[0] {
	case "start":
		a.startAttempt(args[1:])
	case "update":
		a.updateAttempt(args[1:])
	case "show":
		a.showAttempt(args[1:])
	default:
		failV2("USAGE", "attempt needs start|update|show", false, nil, 2)
	}
}

func (a *apState) startAttempt(args []string) {
	ticketID := safeTicket(orDefault(argValue(args, "--ticket"), a.ticket))
	if ticketID == "" {
		failV2("TICKET_REQUIRED", "attempt start requires a ticket", false, nil, 3)
	}
	input, err := readJSONObjectArg(args)
	if err != nil {
		failV2("INVALID_ASSIGNMENT", err.Error(), false, nil, 2)
	}
	rec, err := prepareAttempt(a, ticketID, input)
	if err != nil {
		failV2("ATTEMPT_REJECTED", err.Error(), false, nil, 3)
	}
	printV2Envelope(rec)
}

func prepareAttempt(a *apState, ticketID string, input map[string]interface{}) (*attemptRecord, error) {
	gate := stringValue(input["gate"])
	owner := stringValue(input["owner"])
	key := stringValue(input["idempotency_key"])
	runID := stringValue(input["run_id"])
	if gate == "" || owner == "" || key == "" || runID == "" {
		return nil, fmt.Errorf("assignment needs gate, owner, idempotency_key, and run_id")
	}
	for name, value := range map[string]string{"gate": gate, "owner": owner, "idempotency_key": key, "run_id": runID} {
		if safeTicket(value) != value {
			return nil, fmt.Errorf("invalid %s", name)
		}
	}
	expected := int64Value(input["expected_state_revision"])
	assignment, _ := input["assignment"].(map[string]interface{})
	if assignment == nil {
		assignment = map[string]interface{}{}
	}
	if err := validateAttemptAssignment(assignment); err != nil {
		return nil, err
	}

	env := identity.Env{Slug: a.slug, Branch: a.branch, Ticket: ticketID, ProjectHome: a.stateRoot}
	st := ticket.New(env)
	var rec *attemptRecord
	err := withLock(st, func() error {
		cpPath := filepath.Join(st.Home(), "checkpoint.json")
		cp, err := readStrictObject(cpPath, true)
		if err != nil {
			return err
		}
		if contractVersion(cp) != 2 {
			return fmt.Errorf("attempts require a schema_version 2 checkpoint")
		}
		if stringValue(cp["run_id"]) != runID {
			return fmt.Errorf("assignment run_id does not match checkpoint")
		}
		attemptDir := filepath.Join(st.Home(), "attempts")
		if err := os.MkdirAll(attemptDir, 0o755); err != nil {
			return err
		}
		paths, _ := filepath.Glob(filepath.Join(attemptDir, "*.json"))
		for _, path := range paths {
			prior := readAttemptRecord(path)
			if prior == nil {
				continue
			}
			if prior.IdempotencyKey == key {
				if prior.RunID != runID || prior.Gate != gate || prior.Owner != owner || digestJSON(prior.Assignment) != digestJSON(assignment) {
					return fmt.Errorf("idempotency key %s was already used for a different assignment", key)
				}
				rec = prior
				return nil
			}
			if !terminalAttemptStateV2(prior.State) {
				return fmt.Errorf("active attempt %s already owns this ticket", prior.ID)
			}
		}
		if expected != int64Value(cp["revision"]) {
			return fmt.Errorf("state revision changed: expected %d, current %d", expected, int64Value(cp["revision"]))
		}
		idx := ticket.ReadDoc(st.IndexPath())
		if control := idx.Get("control.state"); control != "" {
			return fmt.Errorf("ticket is %s; no new attempt may dispatch", control)
		}
		now := isoNow()
		id := stringValue(input["id"])
		if id == "" {
			sum := sha256.Sum256([]byte(ticketID + "\x00" + runID + "\x00" + key))
			id = gate + "-" + hex.EncodeToString(sum[:6])
		}
		if safeTicket(id) != id {
			return fmt.Errorf("invalid attempt id")
		}
		rec = &attemptRecord{
			SchemaVersion: 2, ID: id, Ticket: ticketID, RunID: runID, Gate: gate,
			State: "prepared", Revision: 1, Owner: owner, IdempotencyKey: key,
			ExpectedState: expected, Assignment: assignment, CreatedAt: now, UpdatedAt: now,
		}
		if err := writeAttemptRecord(filepath.Join(attemptDir, id+".json"), rec); err != nil {
			return err
		}
		cp["active_attempt_id"] = id
		cp["revision"] = int64Value(cp["revision"]) + 1
		cp["updated_at"] = now
		return writeJSONAtomic(cpPath, cp)
	})
	return rec, err
}

func validateAttemptAssignment(a map[string]interface{}) error {
	for _, key := range []string{"repo_path", "skill_path"} {
		path := stringValue(a[key])
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("assignment.%s must be absolute", key)
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("assignment.%s is not readable: %v", key, err)
		}
	}
	if prohibited := interfaceSlice(a["prohibited_operations"]); len(prohibited) == 0 {
		return fmt.Errorf("assignment needs prohibited_operations")
	}
	return nil
}

func (a *apState) updateAttempt(args []string) {
	ticketID := safeTicket(orDefault(argValue(args, "--ticket"), a.ticket))
	id := safeTicket(argValue(args, "--id"))
	expectedText := argValue(args, "--expect-revision")
	expected, parseErr := strconv.ParseInt(expectedText, 10, 64)
	if ticketID == "" || id == "" || parseErr != nil || expected < 0 {
		failV2("USAGE", "attempt update needs --id, --expect-revision, and ticket scope", false, nil, 2)
	}
	patch, err := readJSONObjectArg(args)
	if err != nil {
		failV2("INVALID_UPDATE", err.Error(), false, nil, 2)
	}
	rec, err := mutateAttempt(a, ticketID, id, expected, patch)
	if err != nil {
		failV2("ATTEMPT_CONFLICT", err.Error(), false, nil, 3)
	}
	printV2Envelope(rec)
}

func mutateAttempt(a *apState, ticketID, id string, expected int64, patch map[string]interface{}) (*attemptRecord, error) {
	env := identity.Env{Slug: a.slug, Branch: a.branch, Ticket: ticketID, ProjectHome: a.stateRoot}
	st := ticket.New(env)
	path := filepath.Join(st.Home(), "attempts", id+".json")
	var rec *attemptRecord
	err := withLock(st, func() error {
		rec = readAttemptRecord(path)
		if rec == nil {
			return fmt.Errorf("attempt %s not found", id)
		}
		if rec.Revision != expected {
			return fmt.Errorf("attempt revision changed: expected %d, current %d", expected, rec.Revision)
		}
		if terminalAttemptStateV2(rec.State) {
			return fmt.Errorf("attempt %s is terminal (%s)", id, rec.State)
		}
		cpPath := filepath.Join(st.Home(), "checkpoint.json")
		cp, err := readStrictObject(cpPath, true)
		if err != nil {
			return err
		}
		if contractVersion(cp) != 2 || stringValue(cp["run_id"]) != rec.RunID {
			return fmt.Errorf("attempt run no longer matches checkpoint")
		}
		if stringValue(cp["active_attempt_id"]) != id {
			return fmt.Errorf("checkpoint active attempt changed")
		}
		next := stringValue(patch["state"])
		if !validAttemptTransition(rec.State, next) {
			return fmt.Errorf("forbidden transition %s -> %s", rec.State, next)
		}
		if runtime, ok := patch["runtime"].(map[string]interface{}); ok {
			if err := validateAttemptRuntime(next, runtime); err != nil {
				return err
			}
			rec.Runtime = runtime
		}
		if next == "running" && len(rec.Runtime) == 0 {
			return fmt.Errorf("running attempt needs a bound runtime handle")
		}
		if next == "completed" && patch["result"] == nil {
			return fmt.Errorf("completed attempt needs result")
		}
		if next == "failed" && patch["failure"] == nil {
			return fmt.Errorf("failed attempt needs failure provenance")
		}
		if next == "cancelled" && patch["termination_confirmed"] != true {
			return fmt.Errorf("cancelled attempt needs termination_confirmed=true")
		}
		rec.State = next
		rec.Result = patch["result"]
		rec.Failure = patch["failure"]
		rec.WaitingReason = stringValue(patch["waiting_reason"])
		rec.TerminationSeen = patch["termination_confirmed"] == true
		rec.Revision++
		rec.UpdatedAt = isoNow()
		if err := writeAttemptRecord(path, rec); err != nil {
			return err
		}
		if terminalAttemptStateV2(next) {
			cp["active_attempt_id"] = nil
		}
		cp["revision"] = int64Value(cp["revision"]) + 1
		cp["updated_at"] = rec.UpdatedAt
		return writeJSONAtomic(cpPath, cp)
	})
	return rec, err
}

func validAttemptTransition(from, to string) bool {
	allowed := map[string]map[string]bool{
		"prepared": {"running": true, "failed": true, "cancelled": true},
		"running":  {"waiting": true, "completed": true, "failed": true, "cancelled": true},
		"waiting":  {"running": true, "completed": true, "failed": true, "cancelled": true},
	}
	return allowed[from][to]
}

func validateAttemptRuntime(state string, runtime map[string]interface{}) error {
	kind := stringValue(runtime["kind"])
	switch kind {
	case "process":
		pid := int(int64Value(runtime["pid"]))
		start := stringValue(runtime["process_start"])
		if pid <= 0 || start == "" {
			return fmt.Errorf("process runtime needs pid and process_start identity")
		}
		if state == "running" {
			actual, alive := processStartIdentity(pid)
			if !alive || actual != start {
				return fmt.Errorf("process runtime is not the claimed live incarnation")
			}
		}
	case "native":
		if stringValue(runtime["handle"]) == "" || stringValue(runtime["transport_id"]) == "" {
			return fmt.Errorf("native runtime needs handle and transport_id")
		}
	default:
		return fmt.Errorf("runtime.kind must be process or native")
	}
	return nil
}

func (a *apState) showAttempt(args []string) {
	ticketID := safeTicket(orDefault(argValue(args, "--ticket"), a.ticket))
	id := safeTicket(argValue(args, "--id"))
	if ticketID == "" || id == "" {
		failV2("USAGE", "attempt show needs --id and ticket scope", false, nil, 2)
	}
	path := filepath.Join(a.stateRoot, "tickets", ticketID, "attempts", id+".json")
	rec := readAttemptRecord(path)
	if rec == nil {
		failV2("ATTEMPT_NOT_FOUND", "attempt does not exist", false, map[string]string{"id": id}, 3)
	}
	data := map[string]interface{}{"attempt": rec, "liveness": attemptLiveness(rec)}
	printV2Envelope(data)
}

func attemptLiveness(rec *attemptRecord) string {
	if terminalAttemptStateV2(rec.State) {
		return "terminal"
	}
	if rec.State == "prepared" {
		return "not_dispatched"
	}
	kind := stringValue(rec.Runtime["kind"])
	if kind == "process" {
		pid := int(int64Value(rec.Runtime["pid"]))
		expected := stringValue(rec.Runtime["process_start"])
		actual, alive := processStartIdentity(pid)
		if !alive {
			return "dead"
		}
		if actual != expected {
			return "reused"
		}
		return "live"
	}
	if kind == "native" {
		if liveness := stringValue(rec.Runtime["liveness"]); liveness != "" {
			return liveness
		}
		return "unknown"
	}
	return "unknown"
}

func processStartIdentity(pid int) (string, bool) {
	if pid <= 0 || !processAlive(pid) {
		return "", false
	}
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", false
	}
	start := strings.Join(strings.Fields(string(out)), " ")
	return start, start != ""
}

func readJSONObjectArg(args []string) (map[string]interface{}, error) {
	path := argValue(args, "--json-file")
	body := argValue(args, "--json")
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		body = string(b)
	}
	if body == "" {
		return nil, fmt.Errorf("--json-file or --json is required")
	}
	var out map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil || out == nil {
		return nil, fmt.Errorf("input must be a JSON object")
	}
	return out, nil
}

func readAttemptRecord(path string) *attemptRecord {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rec attemptRecord
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&rec) != nil || rec.SchemaVersion != 2 {
		return nil
	}
	return &rec
}

func writeAttemptRecord(path string, rec *attemptRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return ticket.WriteAtomic(path, append(b, '\n'))
}

func writeJSONAtomic(path string, value interface{}) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return ticket.WriteAtomic(path, append(b, '\n'))
}

type checkpointV2Input struct {
	Ticket, Workflow, Step, Status, Note, Depth, StopAfter string
	ExpectedRevision                                       *int64
	Force                                                  bool
}

func (a *apState) checkpointV2(input checkpointV2Input) error {
	if safeTicket(input.Ticket) != input.Ticket || input.Ticket == "" {
		return fmt.Errorf("invalid ticket")
	}
	if input.Depth != "" {
		depth, err := strconv.Atoi(input.Depth)
		if err != nil || depth < 0 || depth > 3 || !isAllDigits(input.Depth) {
			return fmt.Errorf("depth must be a non-negative integer no greater than 3")
		}
	}
	env := identity.Env{Slug: a.slug, Branch: a.branch, Ticket: input.Ticket, ProjectHome: a.stateRoot}
	st := ticket.New(env)
	var ts string
	err := withLock(st, func() error {
		path := filepath.Join(st.Home(), "checkpoint.json")
		cp, err := readStrictObject(path, false)
		if err != nil {
			return err
		}
		version := contractVersion(cp)
		if raw := int(int64Value(cp["schema_version"])); raw > 2 {
			return fmt.Errorf("unsupported checkpoint schema_version %d", raw)
		}
		if input.ExpectedRevision != nil && int64Value(cp["revision"]) != *input.ExpectedRevision {
			return fmt.Errorf("checkpoint revision changed: expected %d, current %d", *input.ExpectedRevision, int64Value(cp["revision"]))
		}
		if branch := stringValue(cp["branch"]); branch != "" && branch != a.branch {
			return fmt.Errorf("branch/checkpoint divergence: branch=%q checkpoint=%q", a.branch, branch)
		}
		if !input.Force {
			if workflowPath, ok := resolveWorkflowPath(input.Workflow); ok {
				steps := stepList(workflowPath)
				oldPos, newPos := indexOf(steps, stringValue(cp["step"])), indexOf(steps, input.Step)
				if oldPos > 0 && newPos > 0 && newPos < oldPos {
					return fmt.Errorf("step regression: %s (%d) < %s (%d)", input.Step, newPos, stringValue(cp["step"]), oldPos)
				}
			}
		}
		if version == 1 && len(cp) > 0 {
			backup := filepath.Join(st.Home(), "checkpoint.v1.backup.json")
			if _, statErr := os.Stat(backup); os.IsNotExist(statErr) {
				if b, readErr := os.ReadFile(path); readErr == nil {
					if err := ticket.WriteAtomic(backup, b); err != nil {
						return err
					}
				}
			}
		}
		now := isoNow()
		ts = now
		cp["schema_version"] = 2
		if stringValue(cp["run_id"]) == "" {
			sum := sha256.Sum256([]byte(input.Ticket + "\x00" + now))
			cp["run_id"] = "run-" + hex.EncodeToString(sum[:6])
		}
		cp["revision"] = int64Value(cp["revision"]) + 1
		if input.Force {
			cp["iteration_count"], cp["consecutive_same_step"], cp["first_iteration_at"] = 1, 1, now
		} else {
			cp["iteration_count"] = int64Value(cp["iteration_count"]) + 1
			if stringValue(cp["step"]) == input.Step {
				cp["consecutive_same_step"] = int64Value(cp["consecutive_same_step"]) + 1
			} else {
				cp["consecutive_same_step"] = 1
			}
			if stringValue(cp["first_iteration_at"]) == "" {
				cp["first_iteration_at"] = now
			}
		}
		cp["ticket"], cp["workflow"], cp["step"], cp["status"] = input.Ticket, input.Workflow, input.Step, input.Status
		cp["note"], cp["branch"], cp["head_sha"], cp["slug"], cp["depth"] = jsonSafe(input.Note), a.branch, gitOut("rev-parse", "HEAD"), a.slug, input.Depth
		cp["updated_at"] = now
		if input.StopAfter != "" {
			cp["stop_after"] = input.StopAfter
		}
		if path, ok := resolveWorkflowPath(input.Workflow); ok {
			if digest, exists := digestFile(path); exists {
				cp["workflow_digest"] = digest
			}
		}
		if input.Status == "done_step" || input.Status == "done" {
			cp["last_completed_milestone"] = input.Step
		}
		return writeJSONAtomic(path, cp)
	})
	if err != nil {
		return err
	}
	hist := fmt.Sprintf(`{"ts":%q,"ticket":%q,"workflow":%q,"step":%q,"status":%q,"note":%q,"branch":%q,"actor":"work"}`+"\n",
		ts, input.Ticket, input.Workflow, input.Step, input.Status, input.Note, a.branch)
	appendFile(filepath.Join(st.Home(), "history.jsonl"), hist)
	a.appendTimeline(input.Ticket, input.Workflow, input.Step, input.Status, input.Note)
	a.bumpSession(ts)
	var comment string
	switch input.Status {
	case "in_progress":
		comment = fmt.Sprintf("[WORK] %s:%s — started", jsonSafe(input.Workflow), jsonSafe(input.Step))
	case "done_step":
		comment = fmt.Sprintf("[WORK] %s:%s — done%s", jsonSafe(input.Workflow), jsonSafe(input.Step), suffixNote(input.Note))
	case "done":
		comment = fmt.Sprintf("[WORK] %s complete%s", jsonSafe(input.Workflow), suffixNote(input.Note))
	case "blocked":
		comment = fmt.Sprintf("[WORK] %s:%s — BLOCKED%s", jsonSafe(input.Workflow), jsonSafe(input.Step), suffixNote(input.Note))
	}
	a.postComment(input.Ticket, comment)
	return nil
}

func (a *apState) refreshCheckpointV2(ticketID string) error {
	env := identity.Env{Slug: a.slug, Branch: a.branch, Ticket: ticketID, ProjectHome: a.stateRoot}
	st := ticket.New(env)
	return withLock(st, func() error {
		path := filepath.Join(st.Home(), "checkpoint.json")
		cp, err := readStrictObject(path, true)
		if err != nil {
			return err
		}
		if contractVersion(cp) != 2 {
			return fmt.Errorf("checkpoint is not schema_version 2")
		}
		if branch := stringValue(cp["branch"]); branch != "" && branch != a.branch {
			return fmt.Errorf("branch/checkpoint divergence: branch=%q checkpoint=%q", a.branch, branch)
		}
		cp["revision"] = int64Value(cp["revision"]) + 1
		cp["branch"] = a.branch
		cp["head_sha"] = gitOut("rev-parse", "HEAD")
		cp["updated_at"] = isoNow()
		return writeJSONAtomic(path, cp)
	})
}

func sortedAttemptPaths(home string) []string {
	paths, _ := filepath.Glob(filepath.Join(home, "attempts", "*.json"))
	sort.Strings(paths)
	return paths
}

func processIdentityPath(pidPath string) string { return pidPath + ".identity.json" }

func writeProcessIdentity(pidPath string, pid int) {
	start, alive := processStartIdentity(pid)
	if !alive {
		return
	}
	_ = writeJSONAtomic(processIdentityPath(pidPath), map[string]interface{}{
		"schema_version": 2, "pid": pid, "process_start": start, "recorded_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func readMatchingProcessIdentity(pidPath string, pid int) (known, matches bool) {
	rec := readJSONObject(processIdentityPath(pidPath))
	if rec == nil {
		return false, false
	}
	actual, alive := processStartIdentity(pid)
	return true, alive && int(int64Value(rec["pid"])) == pid && stringValue(rec["process_start"]) == actual
}
