package cmd

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/spf13/cobra"
)

type skillUsage struct {
	Available    bool   `json:"available"`
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
	TotalTokens  *int64 `json:"total_tokens"`
	Reason       string `json:"reason,omitempty"`
}

type skillRuntimeRecord struct {
	TS           string     `json:"ts"`
	Skill        string     `json:"skill"`
	Event        string     `json:"event"`
	InvocationID string     `json:"invocation_id"`
	Session      string     `json:"session"`
	Repo         string     `json:"repo"`
	Branch       string     `json:"branch"`
	Invoker      string     `json:"invoker"`
	Harness      string     `json:"harness"`
	Model        *string    `json:"model"`
	Outcome      string     `json:"outcome,omitempty"`
	DurationS    *float64   `json:"duration_s,omitempty"`
	Usage        skillUsage `json:"provider_usage"`
}

func newSkillRuntimeCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "skill {enter|exit} ...",
		Short:              "record local skill invocation lifecycle",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			runSkillRuntime(args)
			return nil
		},
	}
}

func runSkillRuntime(args []string) {
	if len(args) == 0 {
		failV2("USAGE", "skill needs enter|exit", false, nil, 2)
	}
	switch args[0] {
	case "enter":
		skillEnter(args[1:])
	case "exit":
		skillExit(args[1:])
	default:
		failV2("USAGE", "skill needs enter|exit", false, nil, 2)
	}
}

func skillEnter(args []string) {
	name := argValue(args, "--name")
	if !hasArg(args, "--json") || name == "" || safeTicket(name) != name {
		failV2("USAGE", "skill enter requires --name NAME --json", false, nil, 2)
	}
	rec := currentSkillRuntimeRecord(name)
	if harness := strings.TrimSpace(argValue(args, "--harness")); harness != "" {
		rec.Harness = harness
	}
	if model := strings.TrimSpace(argValue(args, "--model")); model != "" {
		rec.Model = &model
	}
	rec.Event = "start"
	rec.InvocationID = newInvocationID()
	rec.Session = rec.InvocationID
	if err := appendSkillRuntime(rec); err != nil {
		failV2("IO_ERROR", err.Error(), false, nil, 1)
	}
	printV2Envelope(map[string]interface{}{
		"invocation_id": rec.InvocationID, "started_at": rec.TS, "harness": rec.Harness,
		"model": rec.Model, "provider_usage": rec.Usage, "telemetry": telemetryMode(),
	})
}

func skillExit(args []string) {
	id, outcome := argValue(args, "--invocation"), argValue(args, "--outcome")
	if id == "" || outcome == "" || safeTicket(id) != id {
		failV2("USAGE", "skill exit requires --invocation ID --outcome STATUS", false, nil, 2)
	}
	name, started := findSkillStart(id)
	rec := currentSkillRuntimeRecord(name)
	rec.Event, rec.InvocationID, rec.Session, rec.Outcome = "end", id, id, outcome
	inputText, outputText := argValue(args, "--input-tokens"), argValue(args, "--output-tokens")
	if inputText != "" || outputText != "" {
		input, inputErr := strconv.ParseInt(inputText, 10, 64)
		output, outputErr := strconv.ParseInt(outputText, 10, 64)
		if inputErr != nil || outputErr != nil || input < 0 || output < 0 {
			failV2("USAGE", "observed usage requires non-negative --input-tokens and --output-tokens", false, nil, 2)
		}
		total := input + output
		rec.Usage = skillUsage{Available: true, InputTokens: &input, OutputTokens: &output, TotalTokens: &total}
	}
	if duration := argValue(args, "--duration-s"); duration != "" {
		value, err := strconv.ParseFloat(duration, 64)
		if err != nil || value < 0 {
			failV2("USAGE", "--duration-s must be non-negative", false, nil, 2)
		}
		rec.DurationS = &value
	} else if !started.IsZero() {
		value := time.Since(started).Seconds()
		rec.DurationS = &value
	}
	if err := appendSkillRuntime(rec); err != nil {
		failV2("IO_ERROR", err.Error(), false, nil, 1)
	}
	printV2Envelope(map[string]interface{}{
		"invocation_id": id, "outcome": outcome, "duration_s": rec.DurationS,
		"provider_usage": rec.Usage, "telemetry": telemetryMode(),
	})
}

func currentSkillRuntimeRecord(name string) skillRuntimeRecord {
	harness := strings.TrimSpace(os.Getenv("BABYSIT_AGENT"))
	if harness == "" && (os.Getenv("CODEX_SESSION_ID") != "" || os.Getenv("CODEX_THREAD_ID") != "") {
		harness = "codex"
	}
	if harness == "" && os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		harness = "claude"
	}
	if harness == "" && (os.Getenv("GROK_SESSION_ID") != "" || os.Getenv("GROK_AGENT") != "") {
		harness = "grok"
	}
	if harness == "" {
		harness = "unknown"
	}
	var model *string
	for _, key := range []string{"BABYSIT_MODEL", "CODEX_MODEL", "ANTHROPIC_MODEL", "OPENAI_MODEL"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			model = &value
			break
		}
	}
	repo, branch := filepath.Base(gitOut("rev-parse", "--show-toplevel")), gitOut("branch", "--show-current")
	if repo == "." || repo == "" {
		repo = "unknown"
	}
	if branch == "" {
		branch = "unknown"
	}
	return skillRuntimeRecord{
		TS: time.Now().UTC().Format(time.RFC3339), Skill: name, Repo: repo, Branch: branch,
		Invoker: actorRole(), Harness: harness, Model: model,
		Usage: skillUsage{Available: false, Reason: "provider_usage_unavailable"},
	}
}

func newInvocationID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err == nil {
		return "skill-" + hex.EncodeToString(b)
	}
	return fmt.Sprintf("skill-%d", time.Now().UnixNano())
}

func telemetryMode() string {
	if value, ok := config.Get("telemetry"); ok && value == "off" {
		return "off"
	}
	return "local"
}

func skillUsagePath() string {
	if path := os.Getenv("BABYSIT_ANALYTICS_DIR"); path != "" {
		return filepath.Join(path, "skill-usage.jsonl")
	}
	return filepath.Join(config.Dir(), "analytics", "skill-usage.jsonl")
}

func appendSkillRuntime(rec skillRuntimeRecord) error {
	if telemetryMode() == "off" {
		return nil
	}
	path := skillUsagePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func findSkillStart(id string) (string, time.Time) {
	if telemetryMode() == "off" {
		return "unknown", time.Time{}
	}
	f, err := os.Open(skillUsagePath())
	if err != nil {
		return "unknown", time.Time{}
	}
	defer f.Close()
	name := "unknown"
	var started time.Time
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row skillRuntimeRecord
		if json.Unmarshal(scanner.Bytes(), &row) == nil && row.InvocationID == id && row.Event == "start" {
			name = row.Skill
			started, _ = time.Parse(time.RFC3339, row.TS)
		}
	}
	return name, started
}
