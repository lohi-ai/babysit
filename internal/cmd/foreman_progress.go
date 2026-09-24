package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/slug"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

type projectProgress struct {
	Version    int    `json:"version"`
	Ticket     string `json:"ticket"`
	Run        string `json:"run_id"`
	Dispatch   string `json:"dispatch_id"`
	Attempt    string `json:"attempt_id"`
	Producer   string `json:"producer"`
	Phase      string `json:"phase"`
	Summary    string `json:"summary"`
	Evidence   string `json:"evidence"`
	Digest     string `json:"digest"`
	Updated    string `json:"updated_at"`
	Advanced   string `json:"progress_at"`
	WaitKind   string `json:"wait_kind"`
	WaitUntil  string `json:"wait_until"`
	WaitReason string `json:"wait_reason"`
	Health     string `json:"health"`
}

func projectProgressHealth(p *projectProgress, now time.Time) {
	p.Health = "unknown"
	updated, err := parseProjectTime(p.Updated)
	if err != nil || now.Sub(updated) > 15*time.Minute || updated.After(now.Add(time.Minute)) {
		p.Health = "stale"
		return
	}
	if p.WaitKind != "" {
		until, err := parseProjectTime(p.WaitUntil)
		if err == nil && now.Before(until) && until.Sub(updated) <= 15*time.Minute {
			p.Health = "reported_wait"
			return
		}
		p.Health = "wait_expired"
		return
	}
	if d, ok := digestFile(p.Evidence); ok && d == p.Digest {
		advanced, err := parseProjectTime(p.Advanced)
		if err == nil && now.Sub(advanced) < 15*time.Minute {
			p.Health = "progressing"
			return
		}
	}
	p.Health = "no_recent_progress"
}

func projectProgressCommand(st *ticket.Store, kv map[string]string) error {
	var p projectProgress
	if err := readProjectJSON(kv["file"], &p); err != nil {
		return err
	}
	if strings.TrimSpace(p.Producer) == "" || strings.TrimSpace(p.Phase) == "" || strings.TrimSpace(p.Summary) == "" {
		return fmt.Errorf("progress needs producer, phase and summary")
	}
	if p.WaitKind != "" {
		switch p.WaitKind {
		case "test", "dependency", "approval", "resource":
		default:
			return fmt.Errorf("wait_kind must be test|dependency|approval|resource")
		}
		until, err := parseProjectTime(p.WaitUntil)
		if err != nil || !until.After(time.Now()) || time.Until(until) > 15*time.Minute || strings.TrimSpace(p.WaitReason) == "" {
			return fmt.Errorf("wait needs a reason and expiry within 15 minutes")
		}
	}
	return withLock(st, func() error {
		if p.Attempt != "" {
			cp, err := readStrictObject(filepath.Join(st.Home(), "checkpoint.json"), true)
			if err != nil {
				return err
			}
			if p.Attempt != stringValue(cp["active_attempt_id"]) || p.Run != stringValue(cp["run_id"]) {
				return fmt.Errorf("progress does not belong to the current attempt/run")
			}
			attempt, err := readAttemptRecordStrict(filepath.Join(st.Home(), "attempts", p.Attempt+".json"))
			if err != nil || attempt.Owner != p.Producer {
				return fmt.Errorf("progress producer does not own the attempt")
			}
		}
		path := filepath.Join(st.Home(), "progress.json")
		var prior projectProgress
		_ = readProjectJSON(path, &prior)
		p.Version, p.Ticket, p.Updated, p.Advanced = 1, st.Env.Ticket, isoNow(), prior.Advanced
		p.Digest = ""
		if p.Evidence != "" {
			var ok bool
			p.Evidence, _ = filepath.Abs(p.Evidence)
			p.Digest, ok = digestFile(p.Evidence)
			if !ok {
				return fmt.Errorf("progress evidence is unreadable")
			}
			if p.Digest != prior.Digest {
				p.Advanced = p.Updated
			}
		}
		projectProgressHealth(&p, time.Now())
		if err := writeJSONAtomic(path, p); err != nil {
			return err
		}
		projectEvent(st, "progress", p.Attempt, p.Producer, p.Phase)
		printV2Envelope(p)
		return nil
	})
}

// Lifecycle events come from successful state transitions, never guessed token
// counts. They share the existing analytics opt-out and correlation vocabulary.
func projectEvent(st *ticket.Store, event, attempt, producer, detail string) {
	if telemetryMode() == "off" {
		return
	}
	doc := ticket.ReadDoc(st.IndexPath())
	var progress projectProgress
	_ = readProjectJSON(filepath.Join(st.Home(), "progress.json"), &progress)
	runtime := currentSkillRuntimeRecord("foreman")
	row := map[string]interface{}{"schema_version": 1, "harness": runtime.Harness, "model": runtime.Model, "invocation_id": orDefault(attempt, progress.Dispatch), "ts": isoNow(), "skill": "foreman", "event": event, "project": st.Env.Slug, "ticket": st.Env.Ticket, "parent": doc.Get("parent"), "run_id": progress.Run, "dispatch_id": progress.Dispatch, "attempt_id": attempt, "producer": producer, "detail": detail, "provider_usage": skillUsage{Reason: "provider usage not observed"}}
	b, err := json.Marshal(row)
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(skillUsagePath()), "project-events.jsonl")
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// Managed projects use durable milestone digests instead of pane motion. This
// is still observation, not authority to reclaim a worker or its lease.
func foremanProjectProgress(r foreman.Record, now time.Time) (string, string, bool) {
	info, err := slug.ResolveIn(r.ProjectDir)
	if err != nil {
		return "", "", false
	}
	ids, err := ticket.TicketIDs(info.ProjectHome)
	if err != nil {
		return "", "", false
	}
	rows := map[string]string{}
	wait := ""
	managed := false
	managedCount, waitingCount := 0, 0
	for _, id := range ids {
		st := ticket.New(identity.Env{ProjectHome: info.ProjectHome, Ticket: id})
		doc := ticket.ReadDoc(st.IndexPath())
		if doc.Get("assignee") != r.ID {
			continue
		}
		if _, err := os.Stat(projectContractPath(st)); err != nil {
			continue
		}
		managed = true
		managedCount++
		var p projectProgress
		if readProjectJSON(filepath.Join(st.Home(), "progress.json"), &p) != nil {
			continue
		}
		projectProgressHealth(&p, now)
		if p.Health == "reported_wait" {
			waitingCount++
			wait = p.WaitKind + ": " + p.WaitReason
		}
		if d, ok := digestFile(p.Evidence); ok && d == p.Digest {
			rows[id] = p.Digest
		}
	}
	if waitingCount != managedCount {
		wait = ""
	}
	return digestJSON(rows), wait, managed
}
