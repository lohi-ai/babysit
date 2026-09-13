package ticket

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/slug"
)

// AmbiguousTicketError is returned when more than one manifest.yaml worktree
// claims the current working directory — the ladder cannot pick one.
type AmbiguousTicketError struct {
	Candidates []string
}

func (e *AmbiguousTicketError) Error() string {
	return fmt.Sprintf("multiple manifest.yaml worktrees claim this directory: %s", strings.Join(e.Candidates, " "))
}

// ResolveLadder is the canonical ticket identity ladder shared by every
// ticket-scoped command: env (BABYSIT_TICKET / BBS_TICKET) → manifest.yaml
// cwd match → branch regex. It exists because the preamble's
// `eval "$(bbs ticket env)"` sets shell TICKET only — it neither exports
// BABYSIT_TICKET nor survives per-call shells — so each command must
// re-resolve on its own.
func ResolveLadder() (identity.Env, error) {
	// Env conflict is checked before any repo work: slug.ResolveIn returns
	// ErrNoRepo at the worktree check, before it ever reads the env vars —
	// so outside a repo the conflict would be silently lost.
	if bab, bbs := os.Getenv("BABYSIT_TICKET"), os.Getenv("BBS_TICKET"); bab != "" && bbs != "" && bab != bbs {
		return identity.Env{}, &slug.EnvConflictError{BabysitTicket: bab, BBSTicket: bbs}
	}
	env, err := identity.ResolveStrict()
	// ErrNoRepo is soft — env can still resolve a ticket outside a repo, and
	// ResolveStrict already populated env.Ticket from the env vars. Every
	// other error is a loud abort.
	if err != nil && !errors.Is(err, slug.ErrNoRepo) {
		return env, err
	}
	// Rung 1: env. ResolveStrict folded BABYSIT_TICKET/BBS_TICKET into
	// env.Ticket; when either is set it wins outright.
	if os.Getenv("BABYSIT_TICKET") != "" || os.Getenv("BBS_TICKET") != "" {
		return env, nil
	}
	// Rung 2: manifest.yaml cwd match — beats the branch regex.
	if matches := ManifestCwdMatches(env.ProjectHome); len(matches) == 1 {
		env.Ticket = matches[0]
		return env, nil
	} else if len(matches) > 1 {
		return env, &AmbiguousTicketError{Candidates: matches}
	}
	// Rung 3: branch regex — already in env.Ticket via DerivedTicket.
	return env, nil
}

// ResolveProject resolves project scope only — slug, branch, project home —
// plus the env/branch ticket, without the manifest cwd rung. It is for
// commands whose target ticket is an explicit argument (get-manifest
// <ticket>, set-branch <ticket> …, switch <ticket>…, reconcile --ticket):
// an unrelated manifest ambiguity in the cwd must not reject a fully
// explicit command. The env-conflict abort still applies — a caller that
// set disagreeing vars gets a loud failure, not a silent pick.
func ResolveProject() (identity.Env, error) {
	if bab, bbs := os.Getenv("BABYSIT_TICKET"), os.Getenv("BBS_TICKET"); bab != "" && bbs != "" && bab != bbs {
		return identity.Env{}, &slug.EnvConflictError{BabysitTicket: bab, BBSTicket: bbs}
	}
	env, err := identity.ResolveStrict()
	if err != nil && !errors.Is(err, slug.ErrNoRepo) {
		return env, err
	}
	return env, nil
}

// ManifestCwdMatches returns the ticket ids whose manifest.yaml declares a
// worktree containing $PWD, deduped and in directory order.
func ManifestCwdMatches(projectHome string) []string {
	names, err := TicketIDs(projectHome)
	if err != nil {
		return nil
	}
	pwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	pwdReal, err := filepath.EvalSymlinks(pwd)
	if err != nil {
		pwdReal = pwd
	}

	tdir := filepath.Join(projectHome, "tickets")

	var matches []string
	seen := map[string]bool{}
	for _, tid := range names {
		m, err := ReadManifest(filepath.Join(tdir, tid, "manifest.yaml"))
		if err != nil {
			continue
		}
		mid := m.Ticket
		if mid == "" {
			mid = tid
		}
		for _, repo := range m.Repos {
			w := repo.Worktree
			// Single-repo manifests record "." — the repo root is unknown from
			// here, so cwd matching is skipped and the branch rung handles it.
			// (An empty value is skipped too; bash normpath("")="." would match
			// every cwd and resolve this ticket everywhere — a bug not worth
			// reproducing.)
			if w == "" || w == "." || w == "./" {
				continue
			}
			// bash realpath()s a relative worktree against CWD. Go's
			// EvalSymlinks leaves a relative path relative, so it could never
			// match the absolute pwdReal — absolutize first.
			wabs := w
			if !filepath.IsAbs(w) {
				wabs = filepath.Join(pwd, w)
			}
			wreal, err := filepath.EvalSymlinks(wabs)
			if err != nil {
				wreal = filepath.Clean(wabs)
			}
			if wreal == pwdReal || strings.HasPrefix(pwdReal, wreal+string(os.PathSeparator)) {
				if !seen[mid] {
					seen[mid] = true
					matches = append(matches, mid)
				}
				break
			}
		}
	}
	return matches
}
