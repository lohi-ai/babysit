package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/spf13/cobra"
)

// newTicketCmd ports the identity core of bin/bbs-ticket.bash as `bbs ticket`.
//
// This is a strangler port: the five subcommands below run natively, and every
// other subcommand — the base-ops family (merge-base/refresh/reset-base/switch/
// qa-lease) plus init/ensure/get/set-*/path/list/reconcile/… — is delegated to
// bin/bbs-ticket.bash, which stays the source of truth until it is ported. The
// bin/bbs-ticket compat symlink therefore serves every subcommand.
//
// Flag parsing is disabled and each subcommand hand-parses its argv, because
// the bash original hand-parses too and its quirks are part of the contract
// (resolve silently ignores unknown args; set-verdict rejects them with exit 2).
func newTicketCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "ticket",
		Short:              "ticket identity (resolve/board/verdicts/sessions)",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				delegate(args) // bash prints usage and exits 2
			}
			switch args[0] {
			case "resolve":
				runResolve(args[1:])
			case "verdict-status":
				runVerdictStatus(args[1:])
			case "set-verdict":
				runSetVerdict(args[1:])
			case "session":
				runSession(args[1:])
			case "board":
				runBoard(args[1:])
			default:
				delegate(args)
			}
			return nil
		},
	}
}

// delegate hands the invocation to bin/bbs-ticket.bash, which sits next to the
// real binary (bin/bbs) regardless of which bbs-* symlink we were called
// through. syscall.Exec replaces this process, so argv, env, cwd, stdio, signals
// and the exit code all pass through untouched.
func delegate(args []string) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bbs ticket: cannot locate own binary: %v\n", err)
		os.Exit(1)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	bash := filepath.Join(filepath.Dir(exe), "bbs-ticket.bash")
	if err := syscall.Exec(bash, append([]string{bash}, args...), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "bbs ticket: cannot exec %s: %v\n", bash, err)
		os.Exit(1)
	}
}

// ─── shared helpers ──────────────────────────────────────────────────────

// needTicket mirrors bash need_ticket() (bbs-ticket.bash:262-266).
func needTicket(env identity.Env) {
	if env.Ticket == "" {
		fmt.Fprintf(os.Stderr, "bbs-ticket: no ticket in scope (branch='%s'; set BBS_TICKET to override)\n", env.Branch)
		os.Exit(2)
	}
}

// safePathComponent mirrors bash _safe_path_component() (bbs-ticket.bash:690-708)
// with _PATH_KIND=verdict: traversal exits 3, other validation exits 2.
func safePathComponent(kind, label, value string) string {
	if value == "" {
		fmt.Fprintf(os.Stderr, "bbs-ticket path: %s: --%s is empty (try: bbs-ticket path)\n", kind, label)
		os.Exit(2)
	}
	if strings.Contains(value, "/") || strings.Contains(value, "..") || strings.HasPrefix(value, "/") {
		fmt.Fprintf(os.Stderr, "bbs-ticket path: %s: --%s '%s' rejected (path traversal)\n", kind, label, value)
		os.Exit(3)
	}
	cleaned := keepChars(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
	if cleaned != value {
		fmt.Fprintf(os.Stderr, "bbs-ticket path: %s: --%s '%s' contains forbidden characters (allowed: a-zA-Z0-9._-)\n", kind, label, value)
		os.Exit(2)
	}
	return value
}

func keepChars(s, allowed string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(allowed, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
