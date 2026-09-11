package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/reallongnguyen/babysit/internal/slug"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// runResolve ports bin/bbs-ticket.bash:922-1051 — the single identity entry
// point documented in docs/identity.md.
//
// Ladder: env → manifest.yaml cwd match → branch regex.
// Exit codes: 0 resolved (ticket id on stdout), 1 no resolution, 2 conflict.
func runResolve(args []string) {
	explainOn := false
	for _, a := range args {
		if a == "--explain" {
			explainOn = true
		}
	}
	explain := func(format string, v ...any) {
		if explainOn {
			fmt.Fprintf(os.Stderr, "resolve: "+format+"\n", v...)
		}
	}

	// ─── Step 1: env ─────────────────────────────────────────────────
	babysitTicket, bbsTicket := os.Getenv("BABYSIT_TICKET"), os.Getenv("BBS_TICKET")
	if babysitTicket != "" && bbsTicket != "" && babysitTicket != bbsTicket {
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: env conflict — BABYSIT_TICKET=%s, BBS_TICKET=%s\n", babysitTicket, bbsTicket)
		fmt.Fprintln(os.Stderr, "ATTEMPTED: env (both set, disagree)")
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: unset one of them, e.g. `unset BBS_TICKET` to use the BABYSIT_TICKET value.")
		os.Exit(2)
	}
	if babysitTicket != "" {
		explain("matched env BABYSIT_TICKET=%s", babysitTicket)
		fmt.Println(babysitTicket)
		os.Exit(0)
	}
	if bbsTicket != "" {
		explain("matched env BBS_TICKET=%s (legacy alias)", bbsTicket)
		fmt.Println(bbsTicket)
		os.Exit(0)
	}

	env, rerr := ticket.ResolveLadder()
	explain("bootstrap slug SLUG=%s → %s", env.Slug, env.ProjectHome)
	var amb *ticket.AmbiguousTicketError
	switch {
	case errors.As(rerr, &amb):
		pwd, _ := os.Getwd()
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: multiple manifest.yaml worktrees claim cwd %s\n", pwd)
		fmt.Fprintln(os.Stderr, "ATTEMPTED: step 2 — manifest.yaml cwd walk")
		fmt.Fprintf(os.Stderr, "CANDIDATES: %s\n", strings.Join(amb.Candidates, " "))
		fmt.Fprintln(os.Stderr, "RECOMMENDATION: cd into a more specific worktree, or set BABYSIT_TICKET explicitly.")
		os.Exit(2)
	case rerr != nil && !errors.Is(rerr, slug.ErrNoRepo):
		fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
		fmt.Fprintf(os.Stderr, "REASON: %v\n", rerr)
		os.Exit(2)
	}

	// ─── Steps 2+3 ran inside ResolveLadder; report which rung matched ──
	switch {
	case env.Ticket != "" && env.Ticket != env.DerivedTicket:
		explain("matched manifest.yaml cwd → %s", env.Ticket)
		fmt.Println(env.Ticket)
		os.Exit(0)
	case env.DerivedTicket != "":
		explain("matched branch regex → %s", env.DerivedTicket)
		fmt.Println(env.DerivedTicket)
		os.Exit(0)
	}
	explain("no resolution")
	os.Exit(1)
}
