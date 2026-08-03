// Package webui carries the built dashboard SPA inside the binary.
//
// It exists because the two halves of babysit ship separately: `brew install
// bbs` delivers this binary and nothing else, while web/ lives in the git
// checkout. Without an embedded copy, `bbs dashboard` on a brew-only install
// has no SPA to serve and no way to build one — the dist/ tree is gitignored
// and the release archives never carried it.
//
// The directory is a placeholder in a normal `go build`: only .keep is in it,
// so FS reports false and callers fall back to web/dist on disk. Release builds
// populate it — see the before-hooks in .goreleaser.yaml.
package webui

import (
	"embed"
	"io/fs"
)

// all: so the .keep placeholder counts as an embeddable file. Without it a
// fresh checkout — where .keep is the only thing in dist/ — fails to compile
// with "contains no embeddable files", which would break every `go build` that
// hasn't run the web build first.
//
//go:embed all:dist
var embedded embed.FS

// FS returns the embedded SPA rooted at dist/, and whether a real build is in
// there. index.html is the marker: the placeholder alone doesn't make a
// servable site, and a caller that treated it as one would serve 404s instead
// of falling back to the copy on disk.
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
