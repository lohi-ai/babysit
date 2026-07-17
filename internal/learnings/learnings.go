// Package learnings is the shared store behind `bbs learnings-log` (write)
// and `bbs learnings-search` (read): ~/.babysit/analytics/decisions.jsonl,
// the Auto-Decision Framework's audit trail. Both commands reproduce their
// bash originals byte-for-byte, so everything here mirrors shell semantics
// (tr/head sanitizing, $()-style newline stripping, grep/tail filtering).
package learnings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/config"
)

// AnalyticsDir mirrors the bins' env ladder:
// BABYSIT_ANALYTICS_DIR > BABYSIT_STATE_DIR/analytics > $HOME/.babysit/analytics.
// The state-dir fallback is config.Dir() — the one place that resolves it.
func AnalyticsDir() string {
	if d := os.Getenv("BABYSIT_ANALYTICS_DIR"); d != "" {
		return d
	}
	return filepath.Join(config.Dir(), "analytics")
}

// JSONSafe is bash json_safe: tr -d '"\\' | tr -d '[:cntrl:]' | head -c 500.
// Byte-level on purpose — head -c may split a multibyte rune, and the oracle
// does too.
func JSONSafe(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 || c == 0x7f {
			continue
		}
		b = append(b, c)
	}
	if len(b) > 500 {
		b = b[:500]
	}
	return string(b)
}

// Timestamp is `date -u +%Y-%m-%dT%H:%M:%SZ`.
func Timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// Append writes one pre-formatted JSONL line to dir/decisions.jsonl.
// Logging must never fail the caller (the bash runs set -uo without -e and
// suffixes every step with `|| true`), so all errors are swallowed.
func Append(dir, line string) {
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "decisions.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// ProjectSlug derives the project scope for learnings-search:
// git remote get-url origin | sed 's|.*[:/]||;s|\.git$||;s|/|-|g'
// ("" when not in a repo / no origin — the caller then skips slug filtering).
// Note this is the repo *basename*, NOT bbs-slug's owner-repo slug; reusing
// bbs-slug semantics here would change which rows match.
//
// TEMPORARY PARITY SHIM: this execs the git binary because internal/git does
// not exist on this branch's base (it lands with bs-u1tzig4t). Once that
// ticket lands, swap the exec below for the internal/git remote-URL lookup —
// this function is the only call site.
func ProjectSlug() string {
	out, _ := exec.Command("git", "remote", "get-url", "origin").Output()
	url := strings.TrimRight(string(out), "\n")
	// sed 's|.*[:/]||' — greedy: strip through the LAST ':' or '/'.
	if i := strings.LastIndexAny(url, ":/"); i >= 0 {
		url = url[i+1:]
	}
	// sed 's|\.git$||'
	url = strings.TrimSuffix(url, ".git")
	// sed 's|/|-|g' — no-op after the strip above, kept for fidelity.
	return strings.ReplaceAll(url, "/", "-")
}

// ReadStore returns dir/decisions.jsonl as $(cat file) would: trailing
// newlines stripped. ok=false when the file is missing or not a regular file
// (bash `[ ! -f ]` → exit 0). An existing-but-unreadable file reproduces
// cat's stderr line and yields empty content, like the shell pipeline.
func ReadStore(dir string) (content string, ok bool) {
	path := filepath.Join(dir, "decisions.jsonl")
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		// Exists but unreadable (e.g. permission): bash's RESULT=$(cat "$FILE")
		// has no stderr suppression and no set -e, so cat's error prints and
		// the script carries on with an empty RESULT, still exiting 0.
		msg := err.Error()
		var pe *fs.PathError
		if errors.As(err, &pe) {
			msg = pe.Err.Error()
		}
		if msg != "" {
			msg = strings.ToUpper(msg[:1]) + msg[1:] // strerror capitalizes: "Permission denied"
		}
		fmt.Fprintf(os.Stderr, "cat: %s: %s\n", path, msg)
		return "", true
	}
	return strings.TrimRight(string(b), "\n"), true
}
