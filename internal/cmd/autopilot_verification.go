package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This producer captures the subject before a gate and owns the mechanical
// envelope/attempt transitions. Skills supply executed checks and judgments;
// they never reconstruct hashes from memory or upgrade old verdict prose.
func (a *apState) verification(args []string) {
	if err := a.verificationCommand(args); err != nil {
		failV2("VERIFICATION_REJECTED", err.Error(), false, nil, 3)
	}
}

func (a *apState) verificationCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("verification needs begin|record")
	}
	env := identity.Env{Slug: a.slug, Branch: a.branch, Ticket: a.ticket, ProjectHome: a.stateRoot}
	st := ticket.New(env)
	if args[0] == "begin" {
		gate, owner := argValue(args, "--gate"), argValue(args, "--owner")
		if gate != "review-pr" && gate != "qa" {
			return fmt.Errorf("gate must be review-pr or qa")
		}
		runtime := map[string]interface{}{"kind": "native", "handle": argValue(args, "--handle"), "transport_id": argValue(args, "--transport")}
		if err := validateAttemptRuntime("running", runtime); err != nil {
			return err
		}
		s, err := collectAutopilotSnapshot(a, a.ticket)
		if err != nil {
			return err
		}
		if s.Run == nil || s.Run.Contract != 2 || s.Git.Dirty {
			return fmt.Errorf("verification needs a v2 ticket and a clean committed checkout")
		}
		rec, err := prepareAttempt(a, a.ticket, map[string]interface{}{
			"gate": gate, "owner": owner, "idempotency_key": newInvocationID(), "run_id": s.Run.ID, "expected_state_revision": s.StateRevision,
			"assignment": map[string]interface{}{"prohibited_operations": []interface{}{"push", "land", "dispatch"}, "subject": currentEvidenceSubject(s), "started_at": isoNow(), "harness": argValue(args, "--harness")},
		})
		if err != nil {
			return err
		}
		rec, err = mutateAttempt(a, a.ticket, rec.ID, rec.Revision, map[string]interface{}{"state": "running", "runtime": runtime})
		if err != nil {
			return err
		}
		printV2Envelope(rec)
		return nil
	}
	if args[0] != "record" {
		return fmt.Errorf("verification needs begin|record")
	}
	id := argValue(args, "--attempt")
	if !idRe.MatchString(id) {
		return fmt.Errorf("record needs --attempt ID")
	}
	rec, err := readAttemptRecordStrict(filepath.Join(st.Home(), "attempts", id+".json"))
	if err != nil {
		return err
	}
	if rec.Gate != "review-pr" && rec.Gate != "qa" {
		return fmt.Errorf("attempt is not a verification gate")
	}
	pendingPath := filepath.Join(st.Home(), "verification-pending", id+".json")
	var body []byte
	pending, pendingErr := os.ReadFile(pendingPath)
	if rec.State == "completed" || pendingErr == nil {
		if pendingErr != nil {
			return pendingErr
		}
		body = pending
		if rec.State != "completed" {
			ev, err := readStrictObject(pendingPath, true)
			if err != nil {
				return err
			}
			if err := validateV2VerificationEvidence(ev, env); err != nil {
				return err
			}
			s, err := collectAutopilotSnapshot(a, a.ticket)
			if err != nil {
				return err
			}
			if s.Git.Dirty || containsStaleReason(evidenceReadinessReasons(ev, rec.Gate, currentEvidenceSubject(s))) {
				return fmt.Errorf("pending verification is stale; recover the failed attempt before retry")
			}
			_, err = mutateAttempt(a, a.ticket, id, rec.Revision, map[string]interface{}{"state": "completed", "result": map[string]interface{}{"evidence": pendingPath}})
			if err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(pendingErr) {
		return pendingErr
	} else {
		var result struct {
			Checks []struct {
				Argv []string `json:"argv"`
				CWD  string   `json:"cwd"`
				Exit *int     `json:"exit_code"`
				Log  string   `json:"log_path"`
			} `json:"checks"`
			Findings    []interface{} `json:"unresolved_findings"`
			Limitations []interface{} `json:"limitations"`
		}
		if err := readProjectJSON(argValue(args, "--file"), &result); err != nil {
			return err
		}
		if len(result.Checks) == 0 {
			return fmt.Errorf("record needs executed checks with logs")
		}
		s, err := collectAutopilotSnapshot(a, a.ticket)
		if err != nil {
			return err
		}
		if s.Git.Dirty || digestJSON(rec.Assignment["subject"]) != digestJSON(currentEvidenceSubject(s)) {
			_, _ = mutateAttempt(a, a.ticket, id, rec.Revision, map[string]interface{}{"state": "failed", "failure": map[string]interface{}{"reason": "verification subject changed"}})
			return fmt.Errorf("verification subject changed; commit repairs and begin a new gate")
		}
		checks := []interface{}{}
		passed := len(result.Limitations) == 0 && !hasMaterialFindings(result.Findings)
		for i, check := range result.Checks {
			if len(check.Argv) == 0 || !filepath.IsAbs(check.CWD) || check.Exit == nil {
				return fmt.Errorf("check needs argv, absolute cwd and exit_code")
			}
			log, err := os.ReadFile(check.Log)
			if err != nil {
				return err
			}
			if len(log) == 0 {
				return fmt.Errorf("check log is empty")
			}
			path := filepath.Join(st.Home(), "evidence", "verification", "logs", id, fmt.Sprintf("%d.log", i))
			if err := writeProjectImmutable(path, log); err != nil {
				return err
			}
			digest, _ := digestFile(path)
			argv := make([]interface{}, len(check.Argv))
			for j, arg := range check.Argv {
				argv[j] = arg
			}
			checks = append(checks, map[string]interface{}{"argv": argv, "cwd": check.CWD, "exit_code": *check.Exit, "log_path": path, "log_digest": digest})
			if *check.Exit != 0 {
				passed = false
			}
		}
		status, verdict := "BLOCKED", "FAIL"
		if passed {
			status, verdict = "DONE", "PASS"
		}
		findings, limitations := result.Findings, result.Limitations
		if findings == nil {
			findings = []interface{}{}
		}
		if limitations == nil {
			limitations = []interface{}{}
		}
		subject, _ := rec.Assignment["subject"].(map[string]interface{})
		ev := map[string]interface{}{"schema_version": 2, "ticket": a.ticket, "run_id": rec.RunID, "attempt_id": rec.ID, "gate": rec.Gate, "status": status, "result": verdict, "subject": subject,
			"producer": map[string]interface{}{"owner": rec.Owner, "harness": orDefault(stringValue(rec.Assignment["harness"]), "unknown"), "isolation": "current-session"},
			"checks":   checks, "unresolved_findings": findings, "limitations": limitations, "started_at": rec.Assignment["started_at"], "completed_at": isoNow(),
			"surface": map[string]interface{}{"fingerprint": subject["surface_fingerprint"]},
		}
		if err := validateV2VerificationEvidence(ev, env); err != nil {
			return err
		}
		body, _ = json.MarshalIndent(ev, "", "  ")
		if err := writeProjectImmutable(pendingPath, body); err != nil {
			return err
		}
		_, err = mutateAttempt(a, a.ticket, id, rec.Revision, map[string]interface{}{"state": "completed", "result": map[string]interface{}{"evidence": pendingPath}})
		if err != nil {
			return err
		}
	}
	path, err := setV2VerificationEvidence(env, body)
	if err != nil {
		return err
	}
	printV2Envelope(map[string]string{"evidence": path, "attempt_id": id})
	return nil
}
