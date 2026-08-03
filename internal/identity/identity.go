// Package identity resolves babysit ticket identity the way bin/bbs-ticket.bash
// did: derive the project slug, then apply the env-first ladder on top of it.
//
// The bash shelled out to bbs-slug to keep one resolver; this calls
// internal/slug in-process for the same reason. The distinction matters: as a
// subprocess it depended on a bbs-slug argv0 alias existing on PATH, and a brew
// install ships only bbs-config and bbs-env. A missing alias is not an error
// here — Resolve() swallows it — so the whole ladder silently degraded to
// SLUG=unknown, pointing every ticket path at ~/.babysit/projects/unknown.
// In-process, the resolver is always reachable.
package identity

import (
	"os"
	"path/filepath"

	"github.com/reallongnguyen/babysit/internal/slug"
)

// Env is the identity context every ticket subcommand runs against.
type Env struct {
	Slug          string
	Branch        string
	Ticket        string // BABYSIT_TICKET > BBS_TICKET > DerivedTicket
	DerivedTicket string // slug's branch-regex derivation
	ProjectHome   string
}

// Resolve derives the slug and applies the ladder. Like the bash
// `eval "$(bbs-slug env 2>/dev/null || true)"`, a failed derivation (the
// env-conflict exit, or running outside a git repo) is not fatal — the fields
// stay unset and the documented "unknown" defaults take over.
func Resolve() Env {
	info, err := slug.Resolve()
	if err != nil {
		info = &slug.Info{}
	}

	e := Env{
		Slug:          orElse(info.Slug, "unknown"),
		Branch:        orElse(info.Branch, "unknown"),
		DerivedTicket: info.Ticket,
	}
	e.Ticket = firstNonEmpty(os.Getenv("BABYSIT_TICKET"), os.Getenv("BBS_TICKET"), e.DerivedTicket)

	// slug.Resolve always computes a project home, and the bash `eval` imported
	// it into the shell before the fallback expansion ran — so its value wins
	// whenever the derivation succeeded. The BABYSIT_HOME fallback only applies
	// when it didn't.
	e.ProjectHome = info.ProjectHome
	if e.ProjectHome == "" {
		e.ProjectHome = filepath.Join(BabysitHome(), "projects", e.Slug)
	}
	return e
}

// BabysitHome is ${BABYSIT_HOME:-$HOME/.babysit}.
func BabysitHome() string {
	if h := os.Getenv("BABYSIT_HOME"); h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".babysit")
}

func orElse(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
