// Package config reads and writes the babysit config file
// (~/.babysit/config.yaml) natively, without shelling out to jq/yq.
//
// Reads parse the file via yaml.v3. Writes are format-preserving native edits
// that mirror the legacy bin/bbs-config bash script byte-for-byte: the file is
// a documented comment header plus flat `key: value` lines that users may hand
// edit, so a full yaml round-trip (which would drop comments and reflow the
// header) is deliberately avoided.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// configHeader is written verbatim on the first `set` into a fresh file. It
// must stay byte-identical to CONFIG_HEADER in bin/bbs-config.
const configHeader = `# babysit configuration — edit freely, changes take effect on next skill run.
# Docs: https://github.com/reallongnguyen/babysit
#
# ─── Behavior ────────────────────────────────────────────────────────
# proactive: true           # Auto-invoke skills when the request matches one.
#                           # Set to false to only run skills explicitly typed.
#
# ─── Telemetry ───────────────────────────────────────────────────────
# telemetry: local          # off | local
#                           #   off   — no data recorded
#                           #   local — JSONL to ~/.babysit/analytics/ (never leaves machine)
#
# ─── Updates ─────────────────────────────────────────────────────────
# auto_upgrade: false       # true = silently run bbs-upgrade on session start
# update_check: true        # false = suppress upgrade-available notifications
#
`

// Dir returns the babysit state directory, honoring BABYSIT_STATE_DIR
// (default ~/.babysit) — matching bin/bbs-config.
func Dir() string {
	if d := os.Getenv("BABYSIT_STATE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".babysit")
}

// Path returns the config file path.
func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

// Get returns the value for a top-level key and whether it was present.
// A missing file, unparseable file, or absent key yields ("", false) — never
// an error — mirroring the bash `grep ... 2>/dev/null || true` behavior. When
// a key appears more than once, the last occurrence wins (like `tail -1`).
func Get(key string) (string, bool) {
	b, err := os.ReadFile(Path())
	if err != nil {
		return "", false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil || len(doc.Content) == 0 {
		return "", false
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return "", false
	}
	val, found := "", false
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			val = m.Content[i+1].Value
			found = true
		}
	}
	return val, found
}

// Set writes key/value into the config file, preserving all existing content.
// The value is truncated to its first line (matching `head -1`). On a fresh
// file the documented header is seeded first. An existing `key:` line is
// replaced in place; otherwise `key: value` is appended.
func Set(key, value string) error {
	if i := strings.IndexByte(value, '\n'); i >= 0 {
		value = value[:i]
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	path := Path()
	b, err := os.ReadFile(path)
	var content string
	switch {
	case err == nil:
		content = string(b)
	case os.IsNotExist(err):
		content = configHeader
	default:
		return err
	}

	prefix := key + ":"
	lines := strings.Split(content, "\n")
	matched := false
	for i, ln := range lines {
		if strings.HasPrefix(ln, prefix) {
			lines[i] = key + ": " + value
			matched = true
		}
	}
	if matched {
		content = strings.Join(lines, "\n")
	} else {
		content += key + ": " + value + "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// List returns the raw file bytes (or nil if the file is missing), matching
// `cat 2>/dev/null || true`.
func List() []byte {
	b, err := os.ReadFile(Path())
	if err != nil {
		return nil
	}
	return b
}
