package ticket

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is tickets/<id>/manifest.yaml (schema: docs/identity.md).
//
// Every field is a string on purpose: bash manifest_read is a hand-rolled line
// parser whose values are always strings, so `pushed: false` reads back as
// "false" and an absent key as "". yaml.v3 decodes a scalar into a string field
// using its raw text, which reproduces that exactly.
type Manifest struct {
	Version   string `yaml:"version"`
	Ticket    string `yaml:"ticket"`
	Title     string `yaml:"title"`
	Repos     []Repo `yaml:"repos"`
	CreatedAt string `yaml:"created_at"`
	UpdatedAt string `yaml:"updated_at"`
}

type Repo struct {
	Name      string `yaml:"name"`
	Branch    string `yaml:"branch"`
	Canonical string `yaml:"canonical"`
	Worktree  string `yaml:"worktree"`
	Base      string `yaml:"base"`
	Pushed    string `yaml:"pushed"`
}

// ReadManifest parses a manifest.yaml. Version is not validated here: the
// resolve ladder reads manifests of any version, and get-manifest (which does
// enforce version 1) still runs in bash.
func ReadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ManifestPath is <ticket home>/manifest.yaml — mirrors bash manifest_path.
func (s *Store) ManifestPath() string { return filepath.Join(s.Home(), "manifest.yaml") }

// WriteManifest re-emits manifest.yaml with the fixed schema and atomic rename,
// mirroring bash manifest_write — including preserving an existing file's
// created_at line and the yval quoting rule. Repos carry Pushed as the string
// "true"/"false" (how ReadManifest and init produce them), matching bash where
// manifest_write coerces to a yaml bool.
func WriteManifest(path, ticket, title string, repos []Repo) error {
	now := isoNow()
	var b strings.Builder
	b.WriteString("version: 1\n")
	b.WriteString("ticket: " + yval(ticket) + "\n")
	b.WriteString("title: " + yval(title) + "\n")
	created := yval(now)
	if prev, ok := prevCreated(path); ok {
		created = prev // preserve the already-stored created_at value verbatim
	}
	b.WriteString("created_at: " + created + "\n")
	b.WriteString("updated_at: " + yval(now) + "\n")
	b.WriteString("repos:\n")
	for _, r := range repos {
		b.WriteString("  - name: " + yval(r.Name) + "\n")
		b.WriteString("    branch: " + yval(r.Branch) + "\n")
		b.WriteString("    canonical: " + yval(orDefault(r.Canonical, ".")) + "\n")
		b.WriteString("    worktree: " + yval(orDefault(r.Worktree, ".")) + "\n")
		b.WriteString("    base: " + yval(orDefault(r.Base, "main")) + "\n")
		b.WriteString("    pushed: " + yamlBool(r.Pushed == "true") + "\n")
	}
	return atomicWriteBytes(path, []byte(b.String()))
}

// prevCreated returns the raw created_at line value from an existing manifest,
// matching `awk -F': ' '$1=="created_at"{print $2; exit}'`.
func prevCreated(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if i := strings.Index(ln, ": "); i >= 0 && ln[:i] == "created_at" {
			return ln[i+2:], true
		}
	}
	return "", false
}

// yval mirrors manifest_write's python yval(): single-quote (doubling inner
// quotes) when the value carries anything yaml-significant, else emit raw.
func yval(v string) string {
	if strings.ContainsAny(v, ":#'\"\n") || strings.HasPrefix(v, " ") || strings.HasSuffix(v, " ") {
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return v
}

func yamlBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// MarshalManifest renders a manifest byte-identically to manifest_read's
// `json.dump(out, sys.stdout)`: fixed key order (the dict insertion / file
// order, not sorted), Python default ", " / ": " separators, version as the int
// 1, and pushed as a real bool. Only reached after version 1 is confirmed.
func MarshalManifest(m *Manifest) []byte {
	var b strings.Builder
	b.WriteString(`{"version": 1`)
	for _, kv := range [][2]string{
		{"ticket", m.Ticket}, {"title", m.Title},
		{"created_at", m.CreatedAt}, {"updated_at", m.UpdatedAt},
	} {
		b.WriteString(", " + pyScalar(kv[0]) + ": " + pyScalar(kv[1]))
	}
	b.WriteString(`, "repos": [`)
	for i, r := range m.Repos {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("{" + pyScalar("name") + ": " + pyScalar(r.Name))
		for _, kv := range [][2]string{
			{"branch", r.Branch}, {"canonical", r.Canonical},
			{"worktree", r.Worktree}, {"base", r.Base},
		} {
			b.WriteString(", " + pyScalar(kv[0]) + ": " + pyScalar(kv[1]))
		}
		b.WriteString(", " + pyScalar("pushed") + ": " + yamlBool(r.Pushed == "true"))
		b.WriteString("}")
	}
	b.WriteString("]}")
	return []byte(b.String())
}
