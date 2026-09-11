package cmd

import (
	"fmt"
	"os"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// resolveEnv is the single identity entry point for ticket-scoped commands.
// It runs the canonical ladder — env → manifest.yaml cwd match → branch
// regex — via ticket.ResolveLadder, because the preamble's
// `eval "$(bbs ticket env)"` sets shell TICKET only: it neither exports
// BABYSIT_TICKET nor survives per-call shells, so each command re-resolves.
//
// A typed failure is a loud abort, never an empty identity: an env conflict
// or an ambiguous manifest match exits 2 rather than letting a command mint
// a new ticket or act on the wrong one.
func resolveEnv() identity.Env {
	env, err := ticket.ResolveLadder()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bbs: %v\n", err)
		os.Exit(2)
	}
	return env
}

// resolveProject is resolveEnv without the manifest cwd rung — for commands
// whose target ticket is an explicit argument. An ambiguous cwd must not
// reject `get-manifest <ticket>` or `switch <ticket>`; the env-conflict
// abort still applies.
func resolveProject() identity.Env {
	env, err := ticket.ResolveProject()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bbs: %v\n", err)
		os.Exit(2)
	}
	return env
}
