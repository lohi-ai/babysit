package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This file ports the append-only artifact family of bin/bbs-ticket.bash:
// add-handoff, latest-handoff, set-review, set-evidence, evidence-status,
// qa-evidence. All are file I/O over the Layout C ticket home; none touch git.

// runAddHandoff ports add-handoff (bbs-ticket.bash:1340-1375): write the next
// numbered <NNN>-<skill>-<status>.md handoff, update handoffs/LATEST, and append
// a history row. Holds the index lock while allocating the sequence number.
func runAddHandoff(args []string) {
	env := identity.Resolve()
	needTicket(env)
	var skill, status, body, bodyFile string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--skill":
			skill, i = valueOf(args, i, "--skill"), i+1
		case "--status":
			status, i = valueOf(args, i, "--status"), i+1
		case "--body":
			body, i = valueOf(args, i, "--body"), i+1
		case "--body-file":
			bodyFile, i = valueOf(args, i, "--body-file"), i+1
		}
	}
	if skill == "" || status == "" {
		fmt.Fprintln(os.Stderr, "add-handoff: --skill and --status required")
		os.Exit(2)
	}
	skill = safePathComponent("handoff", "skill", skill)

	st := ticket.New(env)
	st.EnsureDirs()
	acquireOrDie(st) // released explicitly below — os.Exit skips defers, leaking the lock

	th := st.Home()
	seq := nextHandoffSeq(th)
	statusSlug := keepChars(strings.ToLower(status), "abcdefghijklmnopqrstuvwxyz0123456789_-")
	if statusSlug == "" {
		fmt.Fprintf(os.Stderr, "add-handoff: --status '%s' has no a-z0-9_- characters\n", status)
		os.Exit(2)
	}
	fn := seq + "-" + skill + "-" + statusSlug + ".md"
	hp := filepath.Join(th, "handoffs", fn)

	if bodyFile != "" && fileExists(bodyFile) {
		b, err := os.ReadFile(bodyFile)
		if err == nil {
			_ = os.WriteFile(hp, b, 0o644)
		}
	} else {
		if body == "" {
			body = "<no body>"
		}
		_ = os.WriteFile(hp, []byte(body+"\n"), 0o644)
	}
	// Plain-text LATEST pointer — symlinks are unreliable on some filesystems.
	_ = os.WriteFile(filepath.Join(th, "handoffs", "LATEST"), []byte(fn+"\n"), 0o644)
	st.HistoryAppendExtra("handoff", skill, fmt.Sprintf(`{"status":"%s","file":"%s"}`, status, fn))
	st.ReleaseLock()
	fmt.Println(hp)
	os.Exit(0)
}

// nextHandoffSeq mirrors bash next_handoff_seq: the max NNN prefix among
// handoffs/[0-9][0-9][0-9]-*.md, plus one, 0-padded to 3 digits.
func nextHandoffSeq(home string) string {
	max := 0
	matches, _ := filepath.Glob(filepath.Join(home, "handoffs", "[0-9][0-9][0-9]-*.md"))
	for _, f := range matches {
		base := filepath.Base(f)
		if i := strings.IndexByte(base, '-'); i > 0 {
			if n, err := strconv.Atoi(base[:i]); err == nil && n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("%03d", max+1)
}

// runLatestHandoff ports latest-handoff (bbs-ticket.bash:1377-1409): with --skill,
// the highest-NNN <NNN>-<skill>-*.md; else the handoffs/LATEST pointer (validated
// so a poisoned LATEST cannot escape the handoffs dir).
func runLatestHandoff(args []string) {
	env := identity.Resolve()
	needTicket(env)
	var filterSkill string
	for i := 0; i < len(args); i++ {
		if args[i] == "--skill" {
			filterSkill, i = valueOf(args, i, "--skill"), i+1
		}
	}
	th := ticket.New(env).Home()
	if filterSkill != "" {
		matches, _ := filepath.Glob(filepath.Join(th, "handoffs", "[0-9][0-9][0-9]-"+filterSkill+"-*.md"))
		sort.Strings(matches)
		if len(matches) > 0 {
			fmt.Println(matches[len(matches)-1])
		}
		os.Exit(0)
	}
	l := filepath.Join(th, "handoffs", "LATEST")
	if b, err := os.ReadFile(l); err == nil {
		name := strings.TrimRight(string(b), "\n")
		if invalidLatestName(name) {
			fmt.Fprintf(os.Stderr, retarget("bbs-ticket list handoff: LATEST contains invalid name '%s'\n"), name)
			os.Exit(3)
		}
		fmt.Printf("%s/handoffs/%s\n", th, name)
	}
	os.Exit(0)
}

// invalidLatestName mirrors the bash case guard ""|*/*|*..*|.* on a LATEST value.
func invalidLatestName(name string) bool {
	return name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") || strings.HasPrefix(name, ".")
}

// runSetReview ports set-review (bbs-ticket.bash:1475-1500): overwrite
// reviews/<skill>.md and append a history row. Unknown args fail loud (exit 2).
func runSetReview(args []string) {
	env := identity.Resolve()
	needTicket(env)
	var skill, body, bodyFile string
	haveBody := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--skill":
			skill, i = valueOf(args, i, "--skill"), i+1
		case "--body":
			body, i, haveBody = valueOf(args, i, "--body"), i+1, true
		case "--body-file":
			bodyFile, i = valueOf(args, i, "--body-file"), i+1
		default:
			fmt.Fprintf(os.Stderr, "set-review: unknown arg '%s' (usage: set-review --skill S [--body MD | --body-file FILE])\n", args[i])
			os.Exit(2)
		}
	}
	if skill == "" {
		fmt.Fprintln(os.Stderr, "set-review: --skill required")
		os.Exit(2)
	}
	skill = safePathComponent("review", "skill", skill)

	st := ticket.New(env)
	st.EnsureDirs()
	rp := filepath.Join(st.Home(), "reviews", skill+".md")
	if bodyFile != "" {
		b, err := os.ReadFile(bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "set-review: --body-file '%s' not found\n", bodyFile)
			os.Exit(2)
		}
		_ = os.WriteFile(rp, b, 0o644)
	} else {
		if !haveBody || body == "" {
			body = "<no review>"
		}
		_ = os.WriteFile(rp, []byte(body+"\n"), 0o644)
	}
	st.HistoryAppend("review", skill)
	fmt.Println(rp)
	os.Exit(0)
}

var evidenceReq = map[string][]string{
	"verification": {"result"},
	"risk-gate":    {"high_risk"},
	"adversarial":  {"disproven", "unverified"},
}

// runSetEvidence ports set-evidence (bbs-ticket.bash:1506-1547): validate a typed
// JSON evidence blob on write (so a malformed blob never lands silently) and
// persist it at evidence/<kind>/result.json.
func runSetEvidence(args []string) {
	env := identity.Resolve()
	needTicket(env)
	var kind, jsonStr, jsonFile string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--kind":
			kind, i = valueOf(args, i, "--kind"), i+1
		case "--json":
			jsonStr, i = valueOf(args, i, "--json"), i+1
		case "--json-file":
			jsonFile, i = valueOf(args, i, "--json-file"), i+1
		}
	}
	if _, ok := evidenceReq[kind]; !ok {
		fmt.Fprintln(os.Stderr, "set-evidence: --kind must be verification|risk-gate|adversarial")
		os.Exit(2)
	}
	if jsonFile != "" && fileExists(jsonFile) {
		if b, err := os.ReadFile(jsonFile); err == nil {
			jsonStr = string(b)
		}
	}
	if jsonStr == "" {
		fmt.Fprintln(os.Stderr, "set-evidence: --json or --json-file required")
		os.Exit(2)
	}
	if msg := validateEvidence(kind, jsonStr); msg != "" {
		fmt.Fprint(os.Stderr, msg)
		os.Exit(2)
	}

	st := ticket.New(env)
	st.EnsureDirs()
	_ = os.MkdirAll(filepath.Join(st.Home(), "evidence", kind), 0o755)
	ep := filepath.Join(st.Home(), "evidence", kind, "result.json")
	_ = os.WriteFile(ep, []byte(jsonStr+"\n"), 0o644)
	st.HistoryAppend("evidence", kind)
	fmt.Println(ep)
	os.Exit(0)
}

// validateEvidence returns "" when the blob is valid, else the exact stderr text
// (with trailing newline) the bash python validator emits.
func validateEvidence(kind, jsonStr string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return fmt.Sprintf("set-evidence: not valid JSON: %v\n", err)
	}
	d, ok := v.(map[string]interface{})
	if !ok {
		return "set-evidence: top-level JSON must be an object\n"
	}
	var missing []string
	for _, k := range evidenceReq[kind] {
		if _, present := d[k]; !present {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Sprintf("set-evidence: %s missing field(s): %s\n", kind, strings.Join(missing, ", "))
	}
	if kind == "verification" {
		res := strings.ToUpper(scalarStr(d["result"]))
		if res != "PASS" && res != "FAIL" {
			return "set-evidence: verification.result must be PASS or FAIL\n"
		}
	}
	return ""
}

func scalarStr(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// runEvidenceStatus ports evidence-status (bbs-ticket.bash:1552-1577): {none|
// valid|malformed} for a typed evidence artifact, by set-evidence's rules.
func runEvidenceStatus(args []string) {
	env := identity.Resolve()
	needTicket(env)
	var kind string
	for i := 0; i < len(args); i++ {
		if args[i] == "--kind" {
			kind, i = valueOf(args, i, "--kind"), i+1
		}
	}
	if _, ok := evidenceReq[kind]; !ok {
		fmt.Fprintln(os.Stderr, "evidence-status: --kind must be verification|risk-gate|adversarial")
		os.Exit(2)
	}
	ep := filepath.Join(ticket.New(env).Home(), "evidence", kind, "result.json")
	b, err := os.ReadFile(ep)
	if err != nil {
		fmt.Println("none")
		os.Exit(0)
	}
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		fmt.Println("malformed")
		os.Exit(0)
	}
	d, ok := v.(map[string]interface{})
	if !ok {
		fmt.Println("malformed")
		os.Exit(0)
	}
	for _, k := range evidenceReq[kind] {
		if _, present := d[k]; !present {
			fmt.Println("malformed")
			os.Exit(0)
		}
	}
	fmt.Println("valid")
	os.Exit(0)
}

var (
	qaStatusRe   = regexp.MustCompile(`^STATUS:[[:space:]]*([A-Z_]+)`)
	qaVerdictRe  = regexp.MustCompile(`^VERDICT:[[:space:]]*([A-Z_]+)`)
	qaFreshRe    = regexp.MustCompile(`freshness=([A-D])`)
	qaBadDimRe   = regexp.MustCompile(`[a-z_]+=[CD]\b`)
	qaThinNoEvid = regexp.MustCompile(`(?i)^(none|n/?a|-|tbd)$`)
	qaE2ERe      = regexp.MustCompile(`(?i)e2e|browser|agent-browser|journey|click|navigat|snapshot|screenshot|\.png|curl|api|cli|request`)
)

// runQAEvidence ports qa-evidence (bbs-ticket.bash:1589-1632): audit the persisted
// qa verdict against the coverage rubric it claims. Classifies, never scores.
func runQAEvidence(args []string) {
	env := identity.Resolve()
	needTicket(env)
	vp := filepath.Join(ticket.New(env).Home(), "verdicts", "qa.md")
	b, err := os.ReadFile(vp)
	if err != nil {
		fmt.Println("none")
		os.Exit(0)
	}
	lines := strings.Split(string(b), "\n")
	var status, verdict, rubric, evid, summ string
	firstMatch := func(re *regexp.Regexp) string {
		for _, ln := range lines {
			if m := re.FindStringSubmatch(ln); m != nil {
				return m[1]
			}
		}
		return ""
	}
	firstLinePrefix := func(prefix string) string {
		for _, ln := range lines {
			if strings.HasPrefix(ln, prefix) {
				return ln
			}
		}
		return ""
	}
	// bash strips the prefix + following whitespace via sed (leading only —
	// trailing whitespace is preserved), so left-trim rather than TrimSpace.
	stripPrefix := func(prefix string) string {
		return strings.TrimLeft(strings.TrimPrefix(firstLinePrefix(prefix), prefix), " \t")
	}
	status = firstMatch(qaStatusRe)
	verdict = firstMatch(qaVerdictRe)
	rubric = firstLinePrefix("RUBRIC:")
	evid = stripPrefix("EVIDENCE:")
	summ = stripPrefix("SUMMARY:")

	passCheck := func() string {
		if m := qaFreshRe.FindStringSubmatch(rubric); m != nil && m[1] != "A" {
			return "contradiction:freshness=" + m[1]
		}
		if m := qaBadDimRe.FindString(rubric); m != "" {
			return "contradiction:" + m
		}
		if evid == "" || qaThinNoEvid.MatchString(evid) {
			return "thin:no-evidence"
		}
		if !qaE2ERe.MatchString(evid) {
			return "thin:no-e2e"
		}
		return "ok"
	}

	switch status {
	case "BLOCKED", "NEEDS_CONTEXT":
		fmt.Println("ok")
	case "DONE", "DONE_WITH_CONCERNS":
		switch verdict {
		case "PASS", "FIXED":
			fmt.Println(passCheck())
		default:
			if summ+evid != "" {
				fmt.Println("ok")
			} else {
				fmt.Println("unexplained")
			}
		}
	default:
		switch verdict {
		case "PASS", "FIXED":
			fmt.Println(passCheck())
		default:
			fmt.Println("ok")
		}
	}
	os.Exit(0)
}
