package ticket

import (
	"os"

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
