package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/learnings"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This is the second resolver for the approval record bs-bfq34gq0 added: the
// foreman writing the verdict the dashboard would otherwise wait for a human
// to write. It is the same record, the same verdict shape and the same
// approvalResolve call — what is new here is only the question of *whether the
// foreman may make that call*, which is what a grant answers.
//
// The gate runs in a fixed order and the order is the design:
//
//	floor → rubric → grant → resolve
//
// Floor first because it is the one check no grant can satisfy, so a
// money/auth/irreversible-data change gets the same answer whether the grant
// is absent, narrow or unbounded. Rubric before grant because a grant changes
// who resolves the checkpoint, never what evidence is required — checking the
// grant first would let a broad grant hide a rubric that was never filled.
//
// Each refusal has its own exit code because the foreman routes on them:
// floor and grant refusals escalate to a human, an unfilled rubric is a
// feedback round to the worker (and BLOCKED once the rounds are spent).
const (
	exitFloor  = 3 // non-delegable path — escalate regardless of grant
	exitRubric = 4 // rubric not filled with named evidence — feedback round
	exitGrant  = 5 // no grant, or the grant does not cover this — escalate
)

// floorPattern matches the paths a grant may never cover. It is biased to
// escalate — the cost of the two error directions is wildly asymmetric, so a
// genuinely ambiguous mention counts as a hit and this list should get longer
// over time rather than shorter.
//
// Bias is not the same as imprecision, though, and a few words are ambiguous
// only in the abstract. Three of them are core *presentation* vocabulary in
// any repo with a design system, and matching them bare made the floor fire on
// essentially every user-facing ticket — the exact population this grant
// exists to automate, which left the grant inert rather than safe:
//
//   - "token" is a design token. The bs-bfq34gq0 artifacts contain 12, all
//     visual ("tokens come from", "token-skinned"), plus exactly one real auth
//     hit ("bearer token").
//   - "role" is an ARIA attribute. That same prototype contains 37, every one
//     of them role="tab" / role="img" / role="dialog" / role="link".
//   - "session" is a Claude Code session and ~/.babysit/sessions/.
//
// So these three require context — "bearer", "access token", "user role" —
// while every genuine money/auth/data phrase stays a bare match. Nothing is
// exempted: the auth path is still caught, it is just spelled the way an auth
// path is actually written.
var floorPattern = regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
	// money
	"payment", "payments", "billing", "invoice", "invoices", "charge", "charges",
	"refund", "refunds", "checkout", "subscription", "subscriptions", "stripe",
	"paypal", "paywall", "payout", "payouts", "currency", "credit card",
	"purchase", "purchases", "coupon", "discount",
	"price change", "pricing page", "update price", "prices",
	// auth
	"auth", "authentication", "authorization", "authenticate", "authenticates",
	"authenticated", "login", "logout", "signup", "sign-in", "sign-up",
	"password", "passwords", "credential", "credentials", "secret", "secrets",
	"oauth", "sso", "permission", "permissions", "access control", "mfa", "2fa",
	"jwt", "api key", "api keys", "bearer",
	"auth token", "access token", "refresh token", "api token", "session token",
	"user role", "admin role", "role change", "role-based", "rbac",
	"session cookie", "user session", "session hijack",
	// irreversible data
	"delete", "deletes", "deletion", "drop", "truncate", "purge", "destroy",
	"wipe", "erase", "irreversible", "migration", "migrations", "migrate",
	"backfill", "overwrite", "hard delete", "cascade",
}, "|") + `)\b`)

// floorHits returns the distinct non-delegable keywords present in text.
func floorHits(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range floorPattern.FindAllString(text, -1) {
		k := strings.ToLower(m)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// rubricKeys are the design-checkpoint rubric lines from foreman/SKILL.md.
// Every one must carry named evidence before any auto-approval — that is the
// bar a grant explicitly does not lower.
var rubricKeys = []struct{ canon, match string }{
	{"coverage", "coverage"},
	{"host-page consistency", "host"},
	{"reuse", "reuse"},
	{"prototype inspected", "prototype"},
	{"scope", "scope"},
}

// rubricNoise is evidence that is not evidence: a line filled in with a
// placeholder reads as filled to a `len(value) > 0` check, and the whole point
// of the rubric is that a line you cannot fill is a feedback round.
var rubricNoise = map[string]bool{
	"": true, "-": true, "n/a": true, "na": true, "none": true, "tbd": true,
	"ok": true, "yes": true, "no": true, "pass": true, "todo": true, "?": true,
}

// parseRubric reads `key: evidence` lines, tolerating the markdown a foreman
// naturally writes (`- **Coverage** — …`, `* Reuse: …`). It returns the
// evidence per canonical key and the keys still missing.
func parseRubric(text string) (filled map[string]string, missing []string) {
	filled = map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		l = strings.TrimLeft(l, "-*# \t")
		// Accept both "Coverage: x" and "**Coverage** — x".
		var key, val string
		if i := strings.IndexAny(l, ":—"); i >= 0 {
			key, val = l[:i], l[i:]
			val = strings.TrimLeft(val, ":—")
		} else {
			continue
		}
		key = strings.ToLower(strings.Trim(strings.TrimSpace(key), "*`_"))
		val = strings.TrimSpace(val)
		for _, rk := range rubricKeys {
			if strings.HasPrefix(key, rk.match) && filled[rk.canon] == "" {
				if !rubricNoise[strings.ToLower(val)] && len([]rune(val)) >= 8 {
					filled[rk.canon] = val
				}
			}
		}
	}
	for _, rk := range rubricKeys {
		if filled[rk.canon] == "" {
			missing = append(missing, rk.canon)
		}
	}
	return filled, missing
}

// designText is the corpus the floor is checked against: what the foreman
// asserts in the rubric plus what the worker actually wrote. Reading the
// artifacts and not just the rubric is what keeps the floor from being
// something the foreman can talk its way past.
func designText(st *ticket.Store, rubric string) string {
	var b strings.Builder
	b.WriteString(rubric)
	for _, name := range []string{"requirement.md", "plan.md", "design.md"} {
		if data, err := os.ReadFile(filepath.Join(st.Home(), name)); err == nil {
			b.WriteString("\n")
			b.Write(data)
		}
	}
	return b.String()
}

// runApprovalSelfResolve is `bbs ticket approval self-resolve`.
func runApprovalSelfResolve(st *ticket.Store, env identity.Env, args []string) {
	fmID, rubricPath, rubric := os.Getenv("BABYSIT_FOREMAN"), "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--foreman":
			fmID, i = valueOf(args, i, "--foreman"), i+1
		case "--rubric-file":
			rubricPath, i = valueOf(args, i, "--rubric-file"), i+1
		case "--rubric":
			rubric, i = valueOf(args, i, "--rubric"), i+1
		}
	}
	if fmID == "" {
		fmt.Fprintln(os.Stderr, "approval self-resolve: needs --foreman <id> (or BABYSIT_FOREMAN)")
		os.Exit(2)
	}
	if rubricPath != "" {
		b, err := os.ReadFile(rubricPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "approval self-resolve: %v\n", err)
			os.Exit(2)
		}
		rubric = string(b)
	}
	if strings.TrimSpace(rubric) == "" {
		fmt.Fprintln(os.Stderr, "approval self-resolve: needs --rubric or --rubric-file — an approval with no evidence is not an approval")
		os.Exit(2)
	}
	rec, err := foreman.Load(fmID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "approval self-resolve: %v\n", err)
		os.Exit(1)
	}

	// 1. Floor — before the grant, so --unbounded cannot reach it.
	if hits := floorHits(designText(st, rubric)); len(hits) > 0 {
		fmt.Fprintf(os.Stderr, "FLOOR: %s touches a non-delegable path (%s)\n", env.Ticket, strings.Join(hits, ", "))
		fmt.Fprintln(os.Stderr, "money, auth and irreversible-data changes escalate to a human under any grant, including --unbounded.")
		fmt.Println("escalate")
		os.Exit(exitFloor)
	}

	// 2. Rubric — before the grant, so a broad grant cannot stand in for
	//    evidence that was never gathered.
	filled, missing := parseRubric(rubric)
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "RUBRIC: %s is not filled with named evidence — unfilled: %s\n",
			env.Ticket, strings.Join(missing, ", "))
		fmt.Fprintln(os.Stderr, "a line you cannot fill is a feedback round, never a pass.")
		fmt.Println("incomplete")
		os.Exit(exitRubric)
	}

	// 3. Grant.
	if ok, reason := rec.Allows(env.Ticket, time.Now()); !ok {
		fmt.Fprintf(os.Stderr, "GRANT: %s may not resolve %s — %s\n", fmID, env.Ticket, reason)
		fmt.Println("escalate")
		os.Exit(exitGrant)
	}

	// 4. Resolve through the one mechanism, naming the grant so history.jsonl
	//    can tell a foreman approval from a human one.
	note := fmt.Sprintf("auto-approved by foreman %s under grant from %s (%s)",
		fmID, rec.Grant.GrantedBy, rec.Grant.At)
	state, missingRec, err := approvalResolve(st, "approve", note, fmID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "approval self-resolve: %v\n", err)
		os.Exit(1)
	}
	if missingRec {
		fmt.Fprintf(os.Stderr, "%s: no approval pending\n", env.Ticket)
		os.Exit(2)
	}
	if err := foreman.Consume(fmID); err != nil {
		// The verdict is already written; refusing to report it because the
		// counter did not move would be worse than an over-spent budget.
		fmt.Fprintf(os.Stderr, "approval self-resolve: warning — grant budget not updated (%v)\n", err)
	}
	logGrantedApproval(env, fmID, rec, filled)
	fmt.Println(state)
	os.Exit(0)
}

// logGrantedApproval writes the filled rubric to the decisions log. This is
// the audit surface the requirement asks for: under a grant nobody watched the
// decision happen, so the evidence it rested on has to be recoverable
// afterwards.
func logGrantedApproval(env identity.Env, fmID string, rec foreman.Record, filled map[string]string) {
	state, _ := json.Marshal(map[string]interface{}{
		"foreman":    fmID,
		"granted_by": rec.Grant.GrantedBy,
		"granted_at": rec.Grant.At,
		"unbounded":  rec.Grant.Unbounded(),
		"rubric":     filled,
	})
	line, _ := json.Marshal(map[string]string{
		"ts":        learnings.Timestamp(),
		"skill":     "foreman",
		"tier":      "taste",
		"type":      "design-checkpoint",
		"ticket":    env.Ticket,
		"choice":    "auto-approved under grant",
		"rationale": "rubric filled with named evidence; no non-delegable path",
		"state":     string(state),
	})
	learnings.Append(learnings.AnalyticsDir(), string(line)+"\n")
}
