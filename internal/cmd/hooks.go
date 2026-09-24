package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
	"github.com/spf13/cobra"
)

// newHooksCmd is the Go port of bin/hooks/{pre-tool-gate,session-writer} as
// `bbs hooks <name>` — one compiled implementation for every OS, so the
// plugin hooks no longer require bash or jq on PATH (audit bs-b3m7rnkw #2).
// The bin/hooks scripts remain as thin `exec bbs hooks <name>` shims for
// manifests that still invoke them by path.
//
// Payload contract (unchanged): hook JSON arrives on stdin, or via
// `--payload <json>` / `--payload=<json>` for callers that cannot pipe stdin
// (OMP's pi.exec has no stdin channel — argv carries data, never shell code).
func newHooksCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "hooks",
		Short:              "plugin hooks (pre-tool-gate/session-writer)",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				hooksUsage()
				os.Exit(2)
			}
			payload := hookPayload(args[1:])
			switch args[0] {
			case "-h", "--help", "help":
				hooksUsage()
				os.Exit(0)
			case "pre-tool-gate":
				runPreToolGate(payload, os.Stdout, os.Stderr)
			case "session-writer":
				runSessionWriter(payload)
			default:
				fmt.Fprintf(os.Stderr, "unknown hook: %s\n", args[0])
				os.Exit(2)
			}
			return nil
		},
	}
}

func hooksUsage() {
	fmt.Fprint(os.Stderr, `usage: bbs hooks <hook> [--payload <json>]

  pre-tool-gate   PreToolUse(Bash) release gate — reads hook JSON, emits a
                  native deny/ask decision, or nothing when it has no objection
  session-writer  SessionStart + PostToolUse session tracker — refreshes
                  ~/.babysit/sessions/<agent>-<id>.yaml, always exits 0

Payload comes from stdin, or --payload when the caller cannot pipe stdin.
`)
}

// hookPayload returns the hook JSON: --payload wins (present even when empty),
// else stdin — but only when stdin is a pipe/file, so an interactive
// `bbs hooks <name>` never blocks on the terminal.
func hookPayload(args []string) []byte {
	for i, a := range args {
		if a == "--payload" {
			if i+1 < len(args) {
				return []byte(args[i+1])
			}
			return []byte{}
		}
		if strings.HasPrefix(a, "--payload=") {
			return []byte(strings.TrimPrefix(a, "--payload="))
		}
	}
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		if b, err := io.ReadAll(os.Stdin); err == nil {
			return b
		}
	}
	return nil
}

// ─── pre-tool-gate ─────────────────────────────────────────────────────────

// gateDecision is the gate's outcome: pass emits nothing (the call falls
// through to the host's own permission rules); deny/ask emit the host's
// native decision JSON. "defer" is never produced — Claude Code honors it as
// "pause the session" in every non-interactive context (github issue #21).
type gateDecision struct {
	action string // "pass" | "deny" | "ask"
	reason string // pass: stderr trail; deny/ask: decision reason
}

var gateExeRe = regexp.MustCompile(`(git|gh|glab)\.exe`)

// classifyGateStage mirrors the bash case block: merge before a bare push so
// `gh pr merge` isn't mis-read as a push. `.exe` suffixes are stripped first —
// Windows shells invoke git.exe / gh.exe and must not bypass the gate.
func classifyGateStage(cmd string) string {
	norm := gateExeRe.ReplaceAllString(cmd, "$1")
	switch {
	case strings.Contains(norm, "gh pr merge"), strings.Contains(norm, "glab mr merge"):
		return "merge"
	case strings.Contains(norm, "gh pr create"), strings.Contains(norm, "glab mr create"):
		return "pr"
	case strings.Contains(norm, "git push"), gateGitCPush(norm):
		return "push"
	}
	return ""
}

// gateGitCPush mirrors the bash glob `*"git -C "*" push"*` — a `git -C "…"`
// invocation whose remainder contains `" push"`. A bare `git -C "d" status`
// must not classify as a push.
func gateGitCPush(norm string) bool {
	i := strings.Index(norm, `git -C "`)
	return i >= 0 && strings.Contains(norm[i+len(`git -C "`):], `" push`)
}

// gateCommand extracts .tool_input.command (or the Grok/Codex spellings).
// jq picks ONE input object — (.tool_input // .toolInput // .input // {}) —
// then reads .command // .cmd inside it; a present-but-empty tool_input does
// not fall through to toolInput.
func gateCommand(payload map[string]any) string {
	for _, key := range []string{"tool_input", "toolInput", "input"} {
		ti, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if c, ok := ti["command"].(string); ok && c != "" {
			return c
		}
		if c, ok := ti["cmd"].(string); ok && c != "" {
			return c
		}
		return ""
	}
	return ""
}

// gateWorkdir mirrors `.tool_input.workdir // .toolInput.workdir // .cwd`.
func gateWorkdir(payload map[string]any) string {
	for _, key := range []string{"tool_input", "toolInput"} {
		if ti, ok := payload[key].(map[string]any); ok {
			if w, ok := ti["workdir"].(string); ok && w != "" {
				return w
			}
		}
	}
	if w, ok := payload["cwd"].(string); ok {
		return w
	}
	return ""
}

func verdictReady(v string) bool   { return v == "DONE" || v == "DONE_WITH_CONCERNS" }
func verdictBlocked(v string) bool { return v == "BLOCKED" || v == "NEEDS_CONTEXT" }

// evaluateGate is the whole decision, separated from I/O so tests drive it
// directly. It may chdir into the payload's workdir — the same contract the
// bash hook had (hooks can run outside the tool's working directory).
func evaluateGate(payload []byte) gateDecision {
	var body map[string]any
	if len(payload) > 0 {
		// A malformed payload yields no command → pass, like the bash jq -r
		// extraction returning empty.
		_ = json.Unmarshal(payload, &body)
	}
	command := gateCommand(body)
	if command == "" {
		return gateDecision{"pass", "no command"}
	}
	stage := classifyGateStage(command)
	if stage == "" {
		return gateDecision{"pass", "not a hard-stage command"}
	}

	if wd := gateWorkdir(body); wd != "" {
		if err := os.Chdir(wd); err != nil {
			return gateDecision{"deny", fmt.Sprintf("pre-tool-gate: cannot inspect working directory %s", wd)}
		}
	}

	// Resolve the ticket in-process — the gate IS bbs, so the bash resolver's
	// "GATE OFFLINE" case (no runnable bbs-ticket anywhere) cannot occur: if
	// this code runs, the binary exists. Only identity conflicts deny.
	env, rerr := ticket.ResolveLadder()
	if rerr != nil {
		return gateDecision{"deny", fmt.Sprintf("pre-tool-gate: ticket identity could not be resolved (%v). Resolve the identity conflict before %s.", rerr, stage)}
	}
	if env.Ticket == "" {
		return gateDecision{"pass", "no ticket resolved"}
	}
	ticketID := env.Ticket

	// Version-2 runs use the shared typed evaluator. An evaluator error on a
	// v2 command is a closed gate, never an implicit legacy downgrade.
	v2Action := stage
	if v2Action == "merge" {
		v2Action = "land"
	}
	res, err := evaluateReadiness(env, v2Action)
	if err != nil {
		code := "READINESS_ERROR"
		var se *snapshotError
		if errors.As(err, &se) && se.Code != "" {
			code = se.Code
		}
		return gateDecision{"deny", fmt.Sprintf("Autopilot v2 readiness failed on %s (%s); blocking %s rather than falling back to stale verdict prose.", ticketID, code, stage)}
	}
	if res.Enforced {
		if !res.Ready {
			return gateDecision{"deny", fmt.Sprintf("Autopilot v2 is not ready for %s on %s (%s). Re-run the stale or missing gate on the current tree.", stage, ticketID, strings.Join(res.ReasonCodes, ","))}
		}
		return gateDecision{"pass", fmt.Sprintf("autopilot-v2 readiness=ready — ok for %s", stage)}
	}

	st := ticket.New(env)
	review := verdictStatus(st, "review-pr")
	qa := verdictStatus(st, "qa")

	// qaEvidenceClassify audits the qa verdict *body* (freshness, rubric, e2e
	// evidence), not just its STATUS token — consulted only once qa reads ready.
	qaEvidence := func() string { return qaEvidenceClassify(st.Home()) }

	switch stage {
	case "push":
		if verdictBlocked(review) {
			return gateDecision{"deny", fmt.Sprintf("review-pr verdict is %s on %s — resolve it before pushing.", review, ticketID)}
		}
		if !verdictReady(review) {
			return gateDecision{"ask", fmt.Sprintf("No ready review-pr verdict on %s (review-pr=%s). Run the review-pr skill before pushing.", ticketID, review)}
		}
		return gateDecision{"pass", fmt.Sprintf("review-pr=%s — ok to push", review)}
	case "pr":
		// PR creation is babysit's real handoff-to-human boundary, so QA gates
		// here, not only at merge.
		if verdictBlocked(review) || verdictBlocked(qa) {
			return gateDecision{"deny", fmt.Sprintf("Not ready for a PR on %s — review-pr=%s, qa=%s.", ticketID, review, qa)}
		}
		if !verdictReady(review) || !verdictReady(qa) {
			return gateDecision{"ask", fmt.Sprintf("Open PR for %s: review-pr=%s, qa=%s. Run the missing review-pr or qa check before creating the PR.", ticketID, review, qa)}
		}
		switch ev := qaEvidence(); {
		case strings.HasPrefix(ev, "contradiction:"):
			return gateDecision{"deny", fmt.Sprintf("qa=%s on %s contradicts its own rubric (%s) — not a real PASS. Re-run /bbs:qa before opening a PR.", qa, ticketID, ev)}
		case strings.HasPrefix(ev, "thin:") || ev == "unexplained":
			return gateDecision{"ask", fmt.Sprintf("qa=%s on %s but evidence is %s — no confirmed end-to-end web run. Run /bbs:qa or approve to proceed.", qa, ticketID, ev)}
		}
		return gateDecision{"pass", fmt.Sprintf("review-pr=%s qa=%s — ok to open PR", review, qa)}
	case "merge":
		if verdictBlocked(review) || verdictBlocked(qa) {
			return gateDecision{"deny", fmt.Sprintf("Not ready to merge %s — review-pr=%s, qa=%s.", ticketID, review, qa)}
		}
		if !verdictReady(review) || !verdictReady(qa) {
			return gateDecision{"ask", fmt.Sprintf("Merge %s: review-pr=%s, qa=%s. Run the missing review-pr or qa check before merging.", ticketID, review, qa)}
		}
		switch ev := qaEvidence(); {
		case strings.HasPrefix(ev, "contradiction:"):
			return gateDecision{"deny", fmt.Sprintf("qa=%s on %s contradicts its own rubric (%s) — not a real PASS. Re-run /bbs:qa before merging.", qa, ticketID, ev)}
		case strings.HasPrefix(ev, "thin:") || ev == "unexplained":
			return gateDecision{"ask", fmt.Sprintf("qa=%s on %s but evidence is %s — no confirmed end-to-end web run. Run /bbs:qa or approve to merge anyway.", qa, ticketID, ev)}
		}
		return gateDecision{"pass", fmt.Sprintf("review-pr=%s qa=%s — ok to merge", review, qa)}
	}
	return gateDecision{"pass", "no rule matched"}
}

// runPreToolGate emits the decision: pass → stderr trail only, empty stdout;
// deny/ask → the host's native decision JSON. Grok has no ask response, so
// under Grok both become {decision:"deny"} — the missing check goes back to
// the agent instead of silently allowing or inventing a confirmation channel.
func runPreToolGate(payload []byte, stdout, stderr io.Writer) {
	d := evaluateGate(payload)
	if d.action == "pass" {
		fmt.Fprintf(stderr, "pre-tool-gate: %s\n", d.reason)
		return
	}
	grok := os.Getenv("GROK_HOOK_EVENT") != ""
	if !grok {
		var body map[string]any
		if json.Unmarshal(payload, &body) == nil {
			_, grok = body["hookEventName"]
		}
	}
	if grok {
		out, _ := json.Marshal(map[string]any{"decision": "deny", "reason": d.reason})
		fmt.Fprintln(stdout, string(out))
		return
	}
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       d.action,
			"permissionDecisionReason": d.reason,
		},
	})
	fmt.Fprintln(stdout, string(out))
}

// ─── session-writer ────────────────────────────────────────────────────────

// sessionIDRe is the bash `*[!a-zA-Z0-9._-]*` rejection: session IDs become
// filenames, so a path or YAML fragment is never accepted.
var sessionIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// sessionPrefix maps the payload/env agent hints to the file prefix, in the
// bash order: env defaults first, then the payload's explicit agent wins.
func sessionPrefix(payload map[string]any) string {
	prefix := "cc"
	if os.Getenv("CODEX_SESSION_ID") != "" || os.Getenv("CODEX_THREAD_ID") != "" {
		prefix = "cx"
	}
	if os.Getenv("GROK_SESSION_ID") != "" {
		prefix = "grok"
	}
	// jq `.agent // (...)`: null/false falls through to the hookEventName
	// probe; any other non-string value prints verbatim and matches no case,
	// keeping the env prefix.
	agent := ""
	if av, present := payload["agent"]; present && av != nil && av != false {
		s, ok := av.(string)
		if !ok {
			return prefix
		}
		agent = s
	}
	if agent == "" {
		if _, ok := payload["hookEventName"]; ok {
			agent = "grok"
		} else if _, ok := payload["turn_id"]; ok {
			agent = "codex"
		}
	}
	switch agent {
	case "claude":
		prefix = "cc"
	case "codex":
		prefix = "cx"
	case "grok":
		prefix = "grok"
	case "omp":
		prefix = "omp"
	}
	return prefix
}

// deriveSessionTicket mirrors the bash ladder: $BABYSIT_TICKET, then the
// cwd's worktree dir name (<ticket>_<slug>), then the branch regex incl. the
// sub-ticket shape <prefix>/<parent>/<pos>_<ticket>_<slug>.
func deriveSessionTicket(cwd string) string {
	if t := os.Getenv("BABYSIT_TICKET"); t != "" {
		return t
	}
	// Native Windows agents report C:\… paths — normalize to slashes so the
	// worktree pattern matches on either separator.
	norm := strings.ReplaceAll(cwd, "\\", "/")
	// bash ${var##*/.babysit/worktrees/} strips the LONGEST prefix → last
	// occurrence; ${WT%%/*} keeps up to the first '/'; ${WT%%_*} keeps up to
	// the first '_' — and yields the whole dirname when there is none.
	if i := strings.LastIndex(norm, "/.babysit/worktrees/"); i >= 0 {
		wt := norm[i+len("/.babysit/worktrees/"):]
		if j := strings.Index(wt, "/"); j >= 0 {
			wt = wt[:j]
		}
		if j := strings.Index(wt, "_"); j >= 0 {
			return wt[:j]
		}
		return wt
	}
	if cwd == "" {
		return ""
	}
	br := gitOutIn(cwd, "branch", "--show-current")
	switch strings.SplitN(br, "/", 2)[0] {
	case "feat", "fix", "chore", "bug", "refactor", "hotfix":
	default:
		return ""
	}
	seg := br[strings.LastIndex(br, "/")+1:]
	// Sub-ticket: drop the leading <pos>_ segment (three digits + underscore).
	if len(seg) > 4 && seg[3] == '_' && seg[0] >= '0' && seg[0] <= '9' && seg[1] >= '0' && seg[1] <= '9' && seg[2] >= '0' && seg[2] <= '9' {
		seg = seg[4:]
	}
	if j := strings.Index(seg, "_"); j >= 0 {
		return seg[:j]
	}
	return seg
}

// sweepStaleSessions removes session files older than 120 minutes — the Go
// port of `find "$SESS_DIR" -mmin +120 -type f ! -name ".*" -delete`, with no
// find.exe to shadow on Windows (audit bs-b3m7rnkw #9).
func sweepStaleSessions(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > 120*time.Minute {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// jsonScalar renders a JSON scalar the way jq -r prints it (jq's number
// formatting trims trailing zeros, so 1.5 → "1.5" and 2.0 → "2").
func jsonScalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	}
	return ""
}

// runSessionWriter persists ~/.babysit/sessions/<prefix>-<sid>.yaml, throttled
// to one write per 60s per session. Always exits 0 — tracking is advisory and
// must never block tool execution.
func runSessionWriter(payload []byte) {
	var body map[string]any
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &body)
	}
	// jq -r prints scalars verbatim — accept a numeric session id the way
	// `jq -r '.session_id'` would print it.
	sid := jsonScalar(body["session_id"])
	if sid == "" {
		sid = jsonScalar(body["sessionId"])
	}
	if sid == "" || sid == "." || sid == ".." || !sessionIDRe.MatchString(sid) {
		return
	}
	cwd, _ := body["cwd"].(string)

	sessDir := filepath.Join(identity.BabysitHome(), "sessions")
	sf := filepath.Join(sessDir, sessionPrefix(body)+"-"+sid+".yaml")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return
	}

	// Throttle: PostToolUse fires on every shell call; once a minute is plenty
	// for a last-seen heartbeat.
	if info, err := os.Stat(sf); err == nil && time.Since(info.ModTime()) < 60*time.Second {
		return
	}

	ticketID := deriveSessionTicket(cwd)

	// Preserve started_at across refreshes; mint it on first write.
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	started := now
	if b, err := os.ReadFile(sf); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "started_at:") {
				started = ln
				break
			}
		}
	}
	if !strings.HasPrefix(started, "started_at:") {
		started = "started_at: " + now
	}

	var sb strings.Builder
	sb.WriteString("version: 1\n")
	sb.WriteString("session_id: " + sessionPrefix(body) + "-" + sid + "\n")
	sb.WriteString("ticket: " + ticketID + "\n")
	sb.WriteString(started + "\n")
	sb.WriteString("last_seen_at: " + now + "\n")
	sb.WriteString("pid: \n")
	sb.WriteString("cwd: " + cwd + "\n")

	tmp, err := os.CreateTemp(sessDir, ".session.*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(sb.String()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	// os.Rename refuses to replace an existing file on Windows; the advisory
	// session file tolerates the remove+rename fallback there.
	if err := os.Rename(tmpName, sf); err != nil {
		_ = os.Remove(sf)
		if err := os.Rename(tmpName, sf); err != nil {
			os.Remove(tmpName)
			return
		}
	}

	sweepStaleSessions(sessDir, time.Now())
}
