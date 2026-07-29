package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The subcommands that exist as `bbs <sub>`. The `bbs-<sub>` spellings are
// legacy argv0 aliases, and a Homebrew install ships only two of them
// (bbs-config, bbs-env) — so help text that spells a command `bbs-ticket`
// hands a brew-only user something that is not on their PATH.
//
// Longest alternatives first: Go's regexp is leftmost-first, so `qa-config`
// must be tried before `config`. Names deliberately absent are the ones that
// only look like aliases — `bbs-serving`, `bbs-qa-lease`, `bbs-merge-base` are
// lock and state identifiers, not commands, and must survive untouched.
var aliasRe = regexp.MustCompile(
	`(^|[^/[:alnum:]_-])bbs-(analytics-cron|codex-competitive|learnings-search|learnings-log|telemetry-log|update-check|qa-config|autopilot|dashboard|secrets|upgrade|config|design|ticket|slug|env)([^[:alnum:]_-]|$)`)

// retarget rewrites `bbs-<sub>` to `bbs <sub>` in user-facing help text, but
// only when the binary was invoked as plain `bbs`. Called through a `bbs-<sub>`
// compat symlink it returns the text byte-for-byte unchanged.
//
// That conditional is what makes this safe: the differential harnesses in
// tests/ drive the Go binary through the hyphenated symlinks (bin/bbs-ticket,
// bin/bbs-env) and diff its output against the frozen bash oracles in
// tests/fixtures/*.reference, so those comparisons stay byte-identical. Only
// the space-form invocation — the one every skill and doc now uses — gets the
// rewritten text, so help always echoes back the spelling the caller used.
func retarget(s string) string {
	if strings.HasPrefix(filepath.Base(os.Args[0]), "bbs-") {
		return s
	}
	// Adjacent matches share a boundary character ("bbs-ticket bbs-slug"), and
	// a single pass consumes it — so replace until it reaches a fixed point.
	for {
		out := aliasRe.ReplaceAllString(s, "${1}bbs ${2}${3}")
		if out == s {
			return s
		}
		s = out
	}
}
