package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

const (
	contextArtifactBudget = 8 * 1024
	contextLogBudget      = 2 * 1024
	contextCacheLimit     = 8
)

type contextArtifact struct {
	Role         string   `json:"role"`
	Path         string   `json:"path"`
	Digest       string   `json:"digest,omitempty"`
	RequiredFor  []string `json:"required_for,omitempty"`
	Exists       bool     `json:"exists"`
	Excerpt      string   `json:"excerpt,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
	RequiresRead bool     `json:"requires_read,omitempty"`
}

type contextLog struct {
	Path      string `json:"path"`
	Excerpt   string `json:"excerpt"`
	Truncated bool   `json:"truncated,omitempty"`
}

type contextProjection struct {
	Snapshot    *autopilotSnapshot   `json:"snapshot"`
	Artifacts   []contextArtifact    `json:"artifacts"`
	Logs        []contextLog         `json:"logs"`
	Obligations []snapshotObligation `json:"obligations"`
}

type contextPacket struct {
	Kind          string                 `json:"kind"`
	Cursor        string                 `json:"cursor,omitempty"`
	ResetReason   string                 `json:"reset_reason,omitempty"`
	SnapshotID    string                 `json:"snapshot_id"`
	StateRevision int64                  `json:"state_revision"`
	Projection    *contextProjection     `json:"projection,omitempty"`
	Changes       map[string]interface{} `json:"changes,omitempty"`
}

type contextCacheRecord struct {
	SchemaVersion int               `json:"schema_version"`
	Ticket        string            `json:"ticket"`
	RunID         string            `json:"run_id"`
	Projection    contextProjection `json:"projection"`
}

func (a *apState) contextV2(args []string) {
	if !hasArg(args, "--json") {
		failV2("USAGE", "context requires --json", false, nil, 2)
	}
	snapshot, err := collectAutopilotSnapshot(a, argValue(args, "--ticket"))
	if err != nil {
		writeSnapshotError(err)
	}
	projection := buildContextProjection(snapshot)
	packet := contextPacket{Kind: "full", SnapshotID: snapshot.SnapshotID, StateRevision: snapshot.StateRevision, Projection: &projection}
	if snapshot.Ticket == nil {
		if argValue(args, "--since") != "" {
			packet.ResetReason = "no_ticket_cache"
		}
		printV2Envelope(packet)
		return
	}
	ticketID, runID := snapshot.Ticket.ID, ""
	if snapshot.Run != nil {
		runID = snapshot.Run.ID
	}
	cursor := contextCursor(ticketID, runID, projection)
	packet.Cursor = cursor
	if since := argValue(args, "--since"); since != "" {
		prior, reason := readContextCache(a, ticketID, runID, since)
		if prior == nil {
			packet.ResetReason = reason
		} else {
			packet.Kind = "delta"
			packet.Projection = nil
			packet.Changes = contextChanges(prior.Projection, projection)
		}
	}
	if err := writeContextCache(a, ticketID, contextCacheRecord{SchemaVersion: 2, Ticket: ticketID, RunID: runID, Projection: projection}, cursor); err != nil {
		failV2("IO_ERROR", err.Error(), false, nil, 1)
	}
	printV2Envelope(packet)
}

func (a *apState) recoverV2(args []string) {
	if argValue(args, "--since") != "" {
		failV2("USAGE", "recover --json does not accept --since", false, nil, 2)
	}
	snapshot, err := collectAutopilotSnapshot(a, argValue(args, "--ticket"))
	if err != nil {
		writeSnapshotError(err)
	}
	projection := buildContextProjection(snapshot)
	action := "continue"
	if snapshot.ActiveAttempt != nil {
		action = "reconcile_active_attempt"
	} else if snapshot.Run == nil {
		action = "direct_skill"
	}
	printV2Envelope(map[string]interface{}{
		"action": action, "snapshot_id": snapshot.SnapshotID,
		"state_revision": snapshot.StateRevision, "projection": projection,
	})
}

func buildContextProjection(snapshot *autopilotSnapshot) contextProjection {
	projection := contextProjection{Snapshot: snapshot, Obligations: snapshot.Obligations, Artifacts: []contextArtifact{}, Logs: []contextLog{}}
	remaining := contextArtifactBudget
	for _, artifact := range orderedContextArtifacts(snapshot.Artifacts) {
		item := contextArtifact{Role: artifact.Role, Path: artifact.Path, Digest: artifact.Digest, RequiredFor: artifact.RequiredFor, Exists: artifact.Exists}
		if artifact.Exists && remaining > 0 {
			limit := 2400
			if remaining < limit {
				limit = remaining
			}
			item.Excerpt, item.Truncated = boundedFileExcerpt(artifact.Path, limit)
			remaining -= len(item.Excerpt)
		}
		item.RequiresRead = item.Exists && (item.Truncated || item.Excerpt == "")
		projection.Artifacts = append(projection.Artifacts, item)
	}
	if snapshot.Ticket != nil && snapshot.ActiveAttempt != nil {
		if log := contextAttemptLog(snapshot); log != nil {
			projection.Logs = append(projection.Logs, *log)
		}
	}
	return projection
}

func orderedContextArtifacts(in []snapshotArtifact) []snapshotArtifact {
	priority := map[string]int{"requirement": 0, "plan": 1, "workflow": 2, "latest_handoff": 3, "checkpoint": 4, "manifest": 5}
	out := append([]snapshotArtifact(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return priority[out[i].Role] < priority[out[j].Role] })
	return out
}

func boundedFileExcerpt(path string, limit int) (string, bool) {
	if limit <= 0 {
		return "", true
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", false
	}
	if info.Size() <= int64(limit) {
		b, _ := io.ReadAll(io.LimitReader(f, int64(limit)))
		return string(b), false
	}
	marker := "\n… [truncated; read full artifact at path] …\n"
	if limit <= len(marker) {
		return "", true
	}
	each := (limit - len(marker)) / 2
	head := make([]byte, each)
	_, _ = io.ReadFull(f, head)
	tail := make([]byte, each)
	_, _ = f.ReadAt(tail, info.Size()-int64(each))
	return string(head) + marker + string(tail), true
}

var sensitiveLogLine = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|authorization|private key)`)

func contextAttemptLog(snapshot *autopilotSnapshot) *contextLog {
	result, _ := snapshot.ActiveAttempt["result"].(map[string]interface{})
	path := stringValue(result["log_path"])
	if path == "" {
		failure, _ := snapshot.ActiveAttempt["failure"].(map[string]interface{})
		path = stringValue(failure["log_path"])
	}
	if path == "" || snapshot.Ticket == nil {
		return nil
	}
	ticketHome := ""
	for _, artifact := range snapshot.Artifacts {
		if artifact.Role == "checkpoint" {
			ticketHome = filepath.Dir(artifact.Path)
			break
		}
	}
	if ticketHome == "" {
		return nil
	}
	resolved, err := resolveEvidencePath(path, ticketHome, snapshot.Ticket.Worktree)
	if err != nil {
		return nil
	}
	excerpt, truncated := boundedFileExcerpt(resolved, contextLogBudget)
	lines := strings.Split(excerpt, "\n")
	for i, line := range lines {
		if sensitiveLogLine.MatchString(line) {
			lines[i] = "[REDACTED]"
		}
	}
	return &contextLog{Path: resolved, Excerpt: strings.Join(lines, "\n"), Truncated: truncated}
}

func contextCursor(ticketID, runID string, projection contextProjection) string {
	identity := map[string]interface{}{
		"snapshot_id": projection.Snapshot.SnapshotID,
		"artifacts":   projection.Artifacts,
		"logs":        projection.Logs,
		"obligations": projection.Obligations,
	}
	sum := sha256.Sum256([]byte(ticketID + "\x00" + runID + "\x00" + digestJSON(identity)))
	return "v2." + hex.EncodeToString(sum[:16])
}

func contextCachePath(a *apState, ticketID, cursor string) string {
	return filepath.Join(a.stateRoot, "tickets", ticketID, "cache", "context", cursor+".json")
}

func readContextCache(a *apState, ticketID, runID, cursor string) (*contextCacheRecord, string) {
	if !regexp.MustCompile(`^v2\.[0-9a-f]{32}$`).MatchString(cursor) {
		return nil, "unknown_cursor"
	}
	b, err := os.ReadFile(contextCachePath(a, ticketID, cursor))
	if err != nil {
		return nil, "expired_cursor"
	}
	var rec contextCacheRecord
	if json.Unmarshal(b, &rec) != nil || rec.SchemaVersion != 2 {
		return nil, "invalid_cursor_cache"
	}
	if rec.Ticket != ticketID || rec.RunID != runID {
		return nil, "cursor_scope_changed"
	}
	return &rec, ""
}

func writeContextCache(a *apState, ticketID string, rec contextCacheRecord, cursor string) error {
	env := identity.Env{Slug: a.slug, Branch: a.branch, Ticket: ticketID, ProjectHome: a.stateRoot}
	st := ticket.New(env)
	return withLock(st, func() error {
		path := contextCachePath(a, ticketID, cursor)
		if err := writeJSONAtomic(path, rec); err != nil {
			return err
		}
		paths, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "v2.*.json"))
		if len(paths) <= contextCacheLimit {
			return nil
		}
		sort.Slice(paths, func(i, j int) bool {
			a, _ := os.Stat(paths[i])
			b, _ := os.Stat(paths[j])
			return a.ModTime().Before(b.ModTime())
		})
		for _, old := range paths[:len(paths)-contextCacheLimit] {
			_ = os.Remove(old)
		}
		return nil
	})
}

func contextChanges(old, next contextProjection) map[string]interface{} {
	changes := map[string]interface{}{}
	if old.Snapshot.SnapshotID != next.Snapshot.SnapshotID {
		changes["snapshot"] = next.Snapshot
	}
	if digestJSON(old.Artifacts) != digestJSON(next.Artifacts) {
		changes["artifacts"] = next.Artifacts
	}
	if digestJSON(old.Logs) != digestJSON(next.Logs) {
		changes["logs"] = next.Logs
	}
	if digestJSON(old.Obligations) != digestJSON(next.Obligations) {
		changes["obligations"] = next.Obligations
	}
	return changes
}
