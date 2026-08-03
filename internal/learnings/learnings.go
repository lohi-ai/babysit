// Package learnings appends to ~/.babysit/analytics/decisions.jsonl, the
// Auto-Decision Framework's audit trail. Skills write it with an inline
// printf; this is the in-binary path, used by the approval self-resolve.
package learnings

import (
	"os"
	"path/filepath"
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
