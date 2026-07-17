// Package identity resolves babysit ticket identity the way bin/bbs-ticket.bash
// does: by shelling out to bbs-slug — the canonical resolver bin, deliberately
// not reimplemented here — and applying the env-first ladder on top of its
// output. Keeping bbs-slug as the single source means the Go and bash bins
// cannot disagree about a slug (they share its ~/.babysit/slug-cache).
package identity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Env is the identity context every ticket subcommand runs against.
type Env struct {
	Slug          string
	Branch        string
	Ticket        string // BABYSIT_TICKET > BBS_TICKET > DerivedTicket
	DerivedTicket string // bbs-slug's branch-regex derivation
	ProjectHome   string
}

// SlugBin mirrors bin/bbs-ticket.bash:243-248 — prefer the bbs-slug sitting next
// to this binary, then PATH, then the installed copy under ~/.claude.
func SlugBin() string {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		if cand := filepath.Join(filepath.Dir(exe), "bbs-slug"); isExec(cand) {
			return cand
		}
	}
	if p, err := exec.LookPath("bbs-slug"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), ".claude", "bbs-slug")
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// Resolve runs `bbs-slug env` and applies the ladder. Like the bash
// `eval "$(bbs-slug env 2>/dev/null || true)"`, a failing bbs-slug (its own
// env-conflict exit, or a missing bin) is not fatal — the fields stay unset and
// the documented "unknown" defaults take over.
func Resolve() Env {
	kv := map[string]string{}
	if out, err := exec.Command(SlugBin(), "env").Output(); err == nil {
		for _, ln := range strings.Split(string(out), "\n") {
			if k, v, ok := strings.Cut(ln, "="); ok {
				kv[k] = v
			}
		}
	}

	e := Env{
		Slug:          orElse(kv["SLUG"], "unknown"),
		Branch:        orElse(kv["BRANCH"], "unknown"),
		DerivedTicket: kv["TICKET"],
	}
	e.Ticket = firstNonEmpty(os.Getenv("BABYSIT_TICKET"), os.Getenv("BBS_TICKET"), e.DerivedTicket)

	// bbs-slug always echoes BABYSIT_PROJECT_HOME, and the bash `eval` imports it
	// into the shell before the fallback expansion runs — so its value wins
	// whenever bbs-slug ran at all. The BABYSIT_HOME fallback only applies when
	// it didn't.
	e.ProjectHome = kv["BABYSIT_PROJECT_HOME"]
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
