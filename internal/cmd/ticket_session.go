package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/ticket"
)

const sessionRowFmt = "%-40s %-14s %-6s %s\n"

// runSession ports bin/bbs-ticket.bash:2621-2667 — inspect/rehydrate the
// session files written by the session-writer hook (docs/identity.md).
func runSession(args []string) {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
		args = args[1:]
	}
	switch verb {
	case "list":
		sessionList()
	case "attach":
		sessionAttach(sessionID(args, "attach"))
	case "end":
		sessionEnd(sessionID(args, "end"))
	default:
		fmt.Fprintln(os.Stderr, "usage: bbs-ticket session <list|attach|end> [args]")
		os.Exit(2)
	}
	os.Exit(0)
}

func sessionList() {
	dir := ticket.SessionsDir()
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		os.Exit(0) // No sessions dir yet: no header, no error.
	}
	now := time.Now().Unix()
	cutoff := now - ticket.SessionWindow

	fmt.Printf(sessionRowFmt, "SESSION_ID", "TICKET", "AGE", "CWD")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		f := filepath.Join(dir, e.Name())
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		mt := fi.ModTime().Unix()
		if mt < cutoff {
			continue
		}
		fmt.Printf(sessionRowFmt,
			ticket.SessionField(f, "session_id"),
			ticket.SessionField(f, "ticket"),
			fmt.Sprintf("%dm", (now-mt)/60),
			ticket.SessionField(f, "cwd"))
	}
}

func sessionAttach(id string) {
	f := filepath.Join(ticket.SessionsDir(), id+".yaml")
	if fi, err := os.Stat(f); err != nil || fi.IsDir() {
		fmt.Fprintf(os.Stderr, "session attach: no session file at %s\n", f)
		fmt.Fprintln(os.Stderr, "Fix: run 'bbs-ticket session list' to see active sessions.")
		os.Exit(1)
	}
	fmt.Printf("export BABYSIT_TICKET=%s\n", ticket.SessionField(f, "ticket"))
	fmt.Printf("export BABYSIT_SESSION=%s\n", id)
}

func sessionEnd(id string) {
	os.Remove(filepath.Join(ticket.SessionsDir(), id+".yaml"))
}

// sessionID validates the id argument shared by attach and end: the empty
// usage error comes first, then the charset guard that keeps the id from
// escaping SessionsDir().
func sessionID(args []string, verb string) string {
	if len(args) == 0 || args[0] == "" {
		fmt.Fprintf(os.Stderr, "usage: bbs-ticket session %s <session-id>\n", verb)
		os.Exit(2)
	}
	if !ticket.ValidSessionID(args[0]) {
		fmt.Fprintf(os.Stderr, "session %s: invalid session id (allowed: [A-Za-z0-9_-])\n", verb)
		os.Exit(2)
	}
	return args[0]
}
