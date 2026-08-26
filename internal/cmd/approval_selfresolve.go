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
// approvalResolve call — what varies is only *whether the foreman may make
// that call*. Default posture is autonomous; a human hold (or a spent /
// expired / out-of-scope grant bound) is what blocks it.
//
// The gate runs in a fixed order and the order is the design:
//
//	floor → rubric → Allows → resolve
//
// Floor first because it is the one check no posture can satisfy, so a
// money/auth/irreversible-data change gets the same answer under default
// autonomy, a narrow grant, or an unbounded grant. Rubric before Allows
// because autonomy changes who resolves the checkpoint, never what evidence
// is required — checking Allows first would let a broad posture hide a
// rubric that was never filled.
//
// Each refusal has its own exit code because the foreman routes on them:
// floor and Allows refusals escalate to a human, an unfilled rubric is a
// feedback round to the worker (and BLOCKED once the rounds are spent).
const (
	exitFloor  = 3 // non-delegable path — escalate regardless of posture
	exitRubric = 4 // rubric not filled with named evidence — feedback round
	exitGrant  = 5 // human hold, or a grant bound does not cover this — escalate
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
//
// `nullable` marks the two lines that a non-UI change cannot answer as written.
// The rubric was authored for user-facing work: on a CLI, Go or markdown ticket
// "host-page consistency" and "prototype inspected" have no referent at all, so
// insisting a missing line is never a pass left those tickets with a rubric
// that could only be filled dishonestly or redirected forever. Those two accept
// an explicit `N/A — <reason>` instead.
//
// Coverage, reuse and scope are never nullable, and that asymmetry is the whole
// design: they are the lines that carry the actual review. Every acceptance
// criterion maps to a plan step, existing code is reused or a new thing is
// justified, and nothing exceeds the request — all three mean exactly as much
// for a Go package as for a screen. Making them nullable too would turn the
// escape hatch into a way to approve anything.
var rubricKeys = []struct {
	canon, match string
	nullable     bool
}{
	{canon: "coverage", match: "coverage"},
	{canon: "host-page consistency", match: "host", nullable: true},
	{canon: "reuse", match: "reuse"},
	{canon: "prototype inspected", match: "prototype", nullable: true},
	{canon: "scope", match: "scope"},
}

// naPattern matches a line that declines a nullable rubric key WITH a reason.
// A bare "N/A" stays noise: the reason is the evidence here, and it is what
// makes "this ticket has no UI surface" reviewable after the fact rather than
// a word that means nothing.
var naPattern = regexp.MustCompile(`(?i)^n/?a\b[\s:—–-]*(.+)$`)

// naReason returns the stated reason for declining a nullable key, and whether
// the line was an N/A at all. The reason has to be long enough to be a reason —
// the same bar the evidence lines are held to.
func naReason(val string) (string, bool) {
	m := naPattern.FindStringSubmatch(strings.TrimSpace(val))
	if m == nil {
		return "", false
	}
	reason := strings.TrimSpace(m[1])
	if len([]rune(reason)) < 8 {
		return "", false
	}
	return reason, true
}

// isNA reports whether a line declines its key at all, reasoned or bare. It is
// deliberately wider than naReason: naReason answers "may this stand as
// evidence", isNA answers "was this an attempt to decline", and a non-nullable
// key needs the second question so a well-worded refusal is not mistaken for a
// well-worded answer.
func isNA(val string) bool {
	v := strings.ToLower(strings.TrimSpace(val))
	return v == "n/a" || v == "na" || naPattern.MatchString(strings.TrimSpace(val))
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
			if !strings.HasPrefix(key, rk.match) || filled[rk.canon] != "" {
				continue
			}
			// A nullable key may be declined, but only with a stated reason —
			// which is then recorded as the evidence, so the decisions log shows
			// what was skipped and why. On a key that is never nullable an N/A
			// is a missing line, however well argued: "N/A — this is a backend
			// change" is long enough and specific enough to read as evidence to
			// a length check, and letting it through would hand back the
			// approve-anything hatch the nullable list exists to avoid.
			if isNA(val) {
				if reason, ok := naReason(val); ok && rk.nullable {
					filled[rk.canon] = "N/A — " + reason
				}
				continue
			}
			if !rubricNoise[strings.ToLower(val)] && len([]rune(val)) >= 8 {
				filled[rk.canon] = val
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

// designArtifacts are the ticket-root documents the design checkpoint is made
// of. prototype.html belongs in the list even though it is markup rather than
// prose: it is the artifact the rubric asks the foreman to inspect, and it is
// where a money surface actually becomes visible. A bland design.md sitting
// over a prototype full of Stripe buttons and a credit-card field must not
// clear the floor.
var designArtifacts = []string{"requirement.md", "plan.md", "design.md", "prototype.html"}

// designPointers are the index keys that may relocate one of those artifacts.
// design-ui writes design.md and then records the real path in
// pointers.design, which internal/cmd/design.go already treats as
// authoritative — so a floor that only globbed the ticket root would read
// nothing at all for exactly the tickets that have a design spec.
var designPointers = []string{"pointers.requirement", "pointers.plan", "pointers.design"}

// designText is the corpus the floor is checked against: what the foreman
// asserts in the rubric plus what the worker actually wrote. Reading the
// artifacts and not just the rubric is what keeps the floor from being
// something the foreman can talk its way past.
func designText(st *ticket.Store, rubric string) string {
	var b strings.Builder
	b.WriteString(rubric)
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(st.Home(), p)
		}
		if seen[p] {
			return
		}
		seen[p] = true
		if data, err := os.ReadFile(p); err == nil {
			b.WriteString("\n")
			b.Write(data)
		}
	}
	for _, name := range designArtifacts {
		add(name)
	}
	doc := ticket.ReadDoc(st.IndexPath())
	for _, key := range designPointers {
		add(strings.TrimSpace(doc.Get(key)))
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

	// Gate order is load-bearing (floor → rubric → Allows). selfResolveGate
	// owns it so tests can assert the order without going through os.Exit.
	code, filled, reason := selfResolveGate(st, env, rec, rubric)
	switch code {
	case exitFloor:
		fmt.Fprintf(os.Stderr, "FLOOR: %s touches a non-delegable path (%s)\n", env.Ticket, reason)
		fmt.Fprintln(os.Stderr, "money, auth and irreversible-data changes escalate to a human under any posture, including default autonomy and --unbounded grants.")
		fmt.Println("escalate")
		os.Exit(exitFloor)
	case exitRubric:
		fmt.Fprintf(os.Stderr, "RUBRIC: %s is not filled with named evidence — unfilled: %s\n",
			env.Ticket, reason)
		fmt.Fprintln(os.Stderr, "a line you cannot fill is a feedback round, never a pass.")
		fmt.Println("incomplete")
		os.Exit(exitRubric)
	case exitGrant:
		fmt.Fprintf(os.Stderr, "POSTURE: %s may not resolve %s — %s\n", fmID, env.Ticket, reason)
		fmt.Println("escalate")
		os.Exit(exitGrant)
	}

	// Resolve through the one mechanism, naming the foreman (and grant,
	// when present) so history.jsonl can tell a foreman approval from a
	// human one.
	note := fmt.Sprintf("auto-approved by foreman %s (default autonomy)", fmID)
	if rec.Grant != nil {
		note = fmt.Sprintf("auto-approved by foreman %s under grant from %s (%s)",
			fmID, rec.Grant.GrantedBy, rec.Grant.At)
	}
	state, missingRec, err := approvalResolve(st, "approve", note, fmID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "approval self-resolve: %v\n", err)
		os.Exit(1)
	}
	if missingRec {
		fmt.Fprintf(os.Stderr, "%s: no approval pending\n", env.Ticket)
		os.Exit(2)
	}
	if rec.Grant != nil {
		if err := foreman.Consume(fmID); err != nil {
			// The verdict is already written; refusing to report it because the
			// counter did not move would be worse than an over-spent budget.
			fmt.Fprintf(os.Stderr, "approval self-resolve: warning — grant budget not updated (%v)\n", err)
		}
	}
	logSelfResolvedApproval(env, fmID, rec, filled)
	fmt.Println(state)
	os.Exit(0)
}

// selfResolveGate is the ordered gate for self-resolve:
//
//	floor → rubric → Allows
//
// Return is (exit code, filled rubric on success, reason string for refusals).
// code 0 means the caller may resolve. Extracted so a test can pin the order
// — specifically that default autonomy + a filled rubric still cannot clear
// a floor hit — without depending on statement order in the CLI wrapper.
func selfResolveGate(st *ticket.Store, env identity.Env, rec foreman.Record, rubric string) (code int, filled map[string]string, reason string) {
	// 1. Floor — before Allows, so no posture (default autonomy or
	//    --unbounded grant) can reach a non-delegable path.
	if hits := floorHits(designText(st, rubric)); len(hits) > 0 {
		return exitFloor, nil, strings.Join(hits, ", ")
	}

	// 2. Rubric — before Allows, so default autonomy cannot stand in for
	//    evidence that was never gathered.
	filled, missing := parseRubric(rubric)
	if len(missing) > 0 {
		return exitRubric, nil, strings.Join(missing, ", ")
	}

	// 3. Allows — human hold, or a grant bound that does not cover this.
	if ok, why := rec.Allows(env.Ticket, time.Now()); !ok {
		return exitGrant, nil, why
	}
	return 0, filled, ""
}

// logSelfResolvedApproval writes the filled rubric to the decisions log. This
// is the audit surface for autonomous resolve: nobody watched the decision
// happen, so the evidence it rested on has to be recoverable afterwards.
func logSelfResolvedApproval(env identity.Env, fmID string, rec foreman.Record, filled map[string]string) {
	stateMap := map[string]interface{}{
		"foreman": fmID,
		"rubric":  filled,
	}
	choice := "auto-approved (default autonomy)"
	if rec.Grant != nil {
		stateMap["granted_by"] = rec.Grant.GrantedBy
		stateMap["granted_at"] = rec.Grant.At
		stateMap["unbounded"] = rec.Grant.Unbounded()
		choice = "auto-approved under grant"
	}
	state, _ := json.Marshal(stateMap)
	line, _ := json.Marshal(map[string]string{
		"ts":        learnings.Timestamp(),
		"skill":     "foreman",
		"tier":      "taste",
		"type":      "design-checkpoint",
		"ticket":    env.Ticket,
		"choice":    choice,
		"rationale": "rubric filled with named evidence; no non-delegable path",
		"state":     string(state),
	})
	learnings.Append(learnings.AnalyticsDir(), string(line)+"\n")
}
