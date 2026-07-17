package cmd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// statusRe matches the first STATUS: line of a verdict body — the small fixed
// alphabet callers branch on instead of parsing prose.
var statusRe = regexp.MustCompile(`^STATUS:[[:space:]]*(DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT)\b`)

// runSetVerdict ports bin/bbs-ticket.bash:1411-1440.
//
// Unknown args fail loud: silently shifting them let callers run
// `--verdict PASS --note ...` and persist a `<no verdict>` body that
// verdict-status reads as none — a hollow verdict downstream gates re-ask about.
func runSetVerdict(args []string) {
	env := identity.Resolve()
	needTicket(env)

	var skill, body, bodyFile string
	haveBody := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--skill":
			skill, i = valueOf(args, i, "--skill"), i+1
		case "--body":
			body, i = valueOf(args, i, "--body"), i+1
			haveBody = true
		case "--body-file":
			bodyFile, i = valueOf(args, i, "--body-file"), i+1
		default:
			fmt.Fprintf(os.Stderr, "set-verdict: unknown arg '%s' (usage: set-verdict --skill S [--body MD | --body-file FILE])\n", args[i])
			os.Exit(2)
		}
	}
	if skill == "" {
		fmt.Fprintln(os.Stderr, "set-verdict: --skill required")
		os.Exit(2)
	}
	skill = safePathComponent("verdict", "skill", skill)

	st := ticket.New(env)
	st.EnsureDirs()
	vp := st.VerdictPath(skill)

	if bodyFile != "" {
		b, err := os.ReadFile(bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "set-verdict: --body-file '%s' not found\n", bodyFile)
			os.Exit(2)
		}
		if err := os.WriteFile(vp, b, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "set-verdict: cannot write %s: %v\n", vp, err)
			os.Exit(1)
		}
	} else {
		if !haveBody || body == "" {
			body = "<no verdict>"
		}
		if err := os.WriteFile(vp, []byte(body+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "set-verdict: cannot write %s: %v\n", vp, err)
			os.Exit(1)
		}
	}

	st.HistoryAppend("verdict", skill)
	fmt.Println(vp)
	os.Exit(0)
}

// runVerdictStatus ports bin/bbs-ticket.bash:1446-1473 — emit one of
// {none|DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT}.
func runVerdictStatus(args []string) {
	env := identity.Resolve()
	needTicket(env)

	var skill string
	for i := 0; i < len(args); i++ {
		if args[i] == "--skill" {
			skill, i = valueOf(args, i, "--skill"), i+1
		}
		// Unknown args are ignored, matching the bash `*) shift`.
	}
	if skill == "" {
		fmt.Fprintln(os.Stderr, "verdict-status: --skill required")
		os.Exit(2)
	}
	skill = safePathComponent("verdict", "skill", skill)

	fmt.Println(verdictStatus(ticket.New(env), skill))
	os.Exit(0)
}

// verdictStatus is the reusable read used by both verdict-status and board.
func verdictStatus(st *ticket.Store, skill string) string {
	f, err := os.Open(st.VerdictPath(skill))
	if err != nil {
		return "none"
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		// First STATUS: line wins. Verdict files are append-once-overwrite, so a
		// later duplicate would be a bug; taking the first is stable regardless.
		if m := statusRe.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
	}
	return "none"
}

// valueOf returns the value following a flag. A flag in last position is where
// bash dies on `"$2"` under `set -u` ("$2: unbound variable", exit 1) — so exit
// 1 here too. The message is ours; the exit code is the contract callers branch
// on, and 2 would misreport a crash as a usage error.
func valueOf(args []string, i int, flag string) string {
	if i+1 >= len(args) {
		fmt.Fprintf(os.Stderr, "bbs-ticket: %s requires a value\n", flag)
		os.Exit(1)
	}
	return strings.TrimSuffix(args[i+1], "\n")
}
