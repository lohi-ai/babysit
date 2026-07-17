// Package env ports bin/bbs-env's environment resolution for babysit skills:
// the .env.base auto-load plus the resolve / is-set / list-prefix / prompt
// lookups. Everything is native — no jq/yq/bash is ever invoked.
//
// The two invariants carried over from the bash script: shell env always wins
// over .env file values, and a variable set to the empty string counts as
// unset (the bash tests are `[ -n "$(printenv VAR)" ]`).
package env

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// prefixes is the resolution priority applied by the --prefix flag: a LOCAL_
// override beats a STG_ override beats the bare name.
var prefixes = []string{"LOCAL_", "STG_", ""}

// envLineRe mirrors the `^([A-Za-z_][A-Za-z0-9_]*)=(.*)` match in
// lib/load-env-file.sh. Lines that don't match are skipped.
var envLineRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// LoadFile parses a .env file and sets only the variables that are not already
// present in the process environment. It is a port of _load_env_file in
// lib/load-env-file.sh and must keep its semantics: comments, blank lines, and
// any line containing `${` are skipped; one layer of matching single or double
// quotes is stripped, and only on an unquoted value is an inline ` #` comment
// trimmed; a trailing CR (CRLF files) is dropped. A missing or unreadable file
// is a no-op.
func LoadFile(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t\r\v\f"), "#") {
			continue
		}
		if line == "" || strings.Contains(line, "${") {
			continue
		}
		m := envLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := m[1], m[2]
		switch {
		case len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"',
			len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'':
			val = val[1 : len(val)-1]
		default:
			// `${val%% #*}` — greedy, so it cuts from the first " #".
			if i := strings.Index(val, " #"); i >= 0 {
				val = val[:i]
			}
		}
		val = strings.TrimSuffix(val, "\r")
		// Shell env takes priority; LookupEnv (not Getenv) so a var that is
		// set-but-empty still shadows the file value, as printenv's exit code
		// does in bash.
		if _, ok := os.LookupEnv(key); !ok {
			os.Setenv(key, val)
		}
	}
}

// LoadProject auto-loads .env.base before resolution, mirroring
// _load_project_env. app and envFile are the already-resolved overrides
// (flag beating BABYSIT_APP / BABYSIT_ENV_FILE). An explicit env file wins
// outright; otherwise the app's config/<app>/.env.base is loaded, and when no
// app can be detected every config/*/.env.base is.
func LoadProject(app, envFile string) {
	if envFile != "" {
		LoadFile(envFile)
		return
	}
	root := projectRoot()
	if root == "" {
		return
	}
	configDir := filepath.Join(root, "config")
	if app == "" {
		app = detectApp(configDir)
	}
	if app != "" {
		LoadFile(filepath.Join(configDir, app, ".env.base"))
		return
	}
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			LoadFile(filepath.Join(configDir, e.Name(), ".env.base"))
		}
	}
}

// projectRoot resolves the repo root from the running binary, matching the
// bash `readlink -f "$0"` + `dirname`/`..` dance. The binary lives at
// <root>/bin/bbs and is reached through bbs-* compat symlinks, so the link
// chain has to be followed before walking up.
func projectRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(filepath.Dir(exe))
}

// detectApp ports _detect_app's CWD detection: the first config/<app>/ whose
// name — or its rig spelling, with dashes as underscores — appears as a path
// component of the working directory.
//
// The bash also has a GT_RIG branch that sources config/rig-map.sh and calls
// its resolve_rig function. That is deliberately not ported: rig-map.sh is a
// bash file absent from this repo, and running it would mean shelling out.
// With no rig-map.sh present the bash falls through to CWD detection, which is
// exactly what this does.
func detectApp(configDir string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		app := e.Name()
		if isPathComponent(cwd, strings.ReplaceAll(app, "-", "_")) || isPathComponent(cwd, app) {
			return app
		}
	}
	return ""
}

// isPathComponent reports whether name appears as a directory component of
// path — the `*/name/*` and `*/name` globs in _detect_app.
func isPathComponent(path, name string) bool {
	return strings.Contains(path, "/"+name+"/") || strings.HasSuffix(path, "/"+name)
}

// Resolve returns the value of the first variable that is set, and whether any
// was. With prefix, each name is tried as LOCAL_<name>, STG_<name>, <name>
// before moving to the next name.
func Resolve(names []string, prefix bool) (string, bool) {
	for _, name := range names {
		for _, p := range lookupPrefixes(prefix) {
			if val := os.Getenv(p + name); val != "" {
				return val, true
			}
		}
	}
	return "", false
}

// IsSet reports whether name is set, honoring the same prefix priority.
func IsSet(name string, prefix bool) bool {
	for _, p := range lookupPrefixes(prefix) {
		if os.Getenv(p+name) != "" {
			return true
		}
	}
	return false
}

func lookupPrefixes(prefix bool) []string {
	if prefix {
		return prefixes
	}
	return []string{""}
}

// ListPrefix returns the sorted KEY=VALUE environment entries whose line starts
// with prefix — the `env | awk 'index($0, prefix) == 1' | sort` pipeline.
//
// Ordering is byte order, which is `sort` under LC_ALL=C. The bash inherits the
// caller's locale, so under e.g. en_US.UTF-8 it collates punctuation-insensitively
// and can order LOCAL_A_B before LOCAL_AB where byte order does the reverse.
// That is deliberately not reproduced: system collation is not reachable
// natively (x/text is not libc strcoll), and the bash's own order already varies
// by platform and locale — so there is no single order to match. Byte order is
// the deterministic reading, and nothing consumes this ordering programmatically.
func ListPrefix(prefix string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	sort.Strings(out)
	return out
}

// Missing returns the names that are not set, in the order given.
func Missing(names []string) []string {
	var out []string
	for _, name := range names {
		if os.Getenv(name) == "" {
			out = append(out, name)
		}
	}
	return out
}
