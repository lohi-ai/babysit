package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// babysitDir is what `update-check` reads VERSION from and what `upgrade`
// git-pulls, so getting it wrong disables both silently — update-check exits 0
// with no output, and upgrade blames a healthy clone for "not installed via git
// clone".
//
// The regression this guards went unnoticed because every case in
// tests/test_bbs_update_check.sh and tests/test_bbs_upgrade.sh pins BABYSIT_DIR
// to a throwaway temp dir for safety, which short-circuits the function before
// it ever looks at argv[0]. The argv[0] path is the one every real install
// takes, so it needs coverage that deliberately leaves BABYSIT_DIR unset.
func TestBabysitDir(t *testing.T) {
	orig := os.Args[0]
	t.Cleanup(func() { os.Args[0] = orig })
	t.Setenv("BABYSIT_DIR", "")

	// Stage a checkout plus the two symlinks setup-skills creates into it.
	root := t.TempDir()
	checkout := filepath.Join(root, "skills", "babysit")
	for _, d := range []string{
		filepath.Join(checkout, "bin"),
		filepath.Join(root, ".local", "bin"),
		filepath.Join(root, ".claude"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	realBin := filepath.Join(checkout, "bin", "bbs")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "VERSION"), []byte("1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	localBin := filepath.Join(root, ".local", "bin", "bbs") // ~/.local/bin/bbs
	claudeBin := filepath.Join(root, ".claude", "bbs")      // ~/.claude/bbs
	for _, link := range []string{localBin, claudeBin} {
		if err := os.Symlink(realBin, link); err != nil {
			t.Fatal(err)
		}
	}

	// t.TempDir() sits under a symlinked prefix on macOS (/var → /private/var),
	// which EvalSymlinks resolves too — so compare against the resolved form.
	want, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("resolves through install symlinks", func(t *testing.T) {
		// Both of these are what the preamble's PATH heal actually selects;
		// before EvalSymlinks they yielded ~/.local and $HOME respectively.
		for _, argv0 := range []string{localBin, claudeBin} {
			os.Args[0] = argv0
			if got := babysitDir(); got != want {
				t.Errorf("argv0=%s\n got: %s\nwant: %s", argv0, got, want)
			}
		}
	})

	t.Run("direct invocation still works", func(t *testing.T) {
		os.Args[0] = realBin
		if got := babysitDir(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("BABYSIT_DIR wins", func(t *testing.T) {
		t.Setenv("BABYSIT_DIR", "/explicit/override")
		os.Args[0] = localBin
		if got := babysitDir(); got != "/explicit/override" {
			t.Errorf("got %s, want /explicit/override", got)
		}
	})

	t.Run("no checkout behind it stays outside one", func(t *testing.T) {
		// A brew install has no clone to pull; landing outside a checkout is
		// what makes upgrade's "not installed via git clone" the honest answer.
		brew := filepath.Join(root, "brew", "bin")
		if err := os.MkdirAll(brew, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(brew, "bbs")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		os.Args[0] = bin
		if got := babysitDir(); got == want {
			t.Errorf("brew-shaped install resolved to the checkout %s", got)
		}
	})
}
