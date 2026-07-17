// Command bbs is the babysit CLI. It is a multicall binary: when invoked
// through a `bbs-<name>` compat symlink it dispatches to the `<name>`
// subcommand, so the legacy `bbs-config` name runs `bbs config`.
package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/reallongnguyen/babysit/internal/cmd"
)

func main() {
	root := cmd.NewRootCmd()
	if base := filepath.Base(os.Args[0]); strings.HasPrefix(base, "bbs-") {
		sub := strings.TrimPrefix(base, "bbs-")
		root.SetArgs(append([]string{sub}, os.Args[1:]...))
	}
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
