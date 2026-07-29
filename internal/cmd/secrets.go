package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reallongnguyen/babysit/internal/env"
	"github.com/spf13/cobra"
)

// newSecretsCmd ports bin/bbs-secrets as `bbs secrets` — the project-local
// dotenv auto-loader (load / seed / ensure-gitignore).
//
// DisableFlagParsing: seed / ensure-gitignore parse their own --repo-root / --
// and reject unknown -flags with exit 2, so cobra's parser stays out of the way.
func newSecretsCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "secrets",
		Short:              "project-local env-var auto-loader",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runSecrets(args)
		},
	}
}

// secretsHelp is a clean rendering of the bin's doc block. The bash printed the
// comment header through a sed pipeline whose `\?` behaves differently on BSD
// vs GNU sed (macOS keeps the leading `# `, Linux strips it), so its bytes are
// platform-dependent; help is not load-bearing (skills call load/seed/
// ensure-gitignore, never parse this), so the port emits a stable version.
const secretsHelp = `bbs-secrets — project-local env-var auto-loader for babysit skills.

Subcommands:
  load
      Walk up from $PWD to find the nearest dir containing .babysit/ and
      load <repo>/.babysit/.env.
      Emits ` + "`export KEY='ESCAPED_VAL'`" + ` lines for keys NOT already in shell env.
      Consumer: ` + "`eval \"$(bbs-secrets load)\"`" + `.

      Precedence (highest wins): shell-exported value > repo file.

  seed --repo-root <path> <var>...
      Idempotently create <path>/.babysit/.env with ` + "`# <var>=`" + ` commented placeholders.
      Calls ensure-gitignore internally first (defense-in-depth: file is
      gitignored before any value can land in it). Prints ` + "`created: <path>`" + `
      or ` + "`exists: <path>`" + `.

  ensure-gitignore --repo-root <path>
      Idempotently append ` + "`.babysit/.env`" + ` to <path>/.gitignore. Prints
      ` + "`added` or `present`" + `.
`

func runSecrets(args []string) error {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
	}
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}

	switch sub {
	case "load":
		return secretsLoad()
	case "seed":
		return secretsSeed(rest)
	case "ensure-gitignore":
		return secretsEnsureGitignore(rest)
	case "help", "-h", "--help", "":
		fmt.Print(secretsHelp)
		return nil
	default:
		fmt.Fprintf(os.Stderr, retarget("bbs-secrets: unknown subcommand: %s\n"), sub)
		os.Exit(2)
		return nil
	}
}

// secretsFindRepoRoot walks up from cwd (resolved physically, like `cd && pwd -P`)
// for the nearest dir containing .babysit/. Empty when none is found.
func secretsFindRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	d, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return ""
	}
	for d != "" && d != "/" {
		if fi, err := os.Stat(filepath.Join(d, ".babysit")); err == nil && fi.IsDir() {
			return d
		}
		d = filepath.Dir(d)
	}
	return ""
}

func secretsLoad() error {
	root := secretsFindRepoRoot()
	if root == "" {
		return nil // nothing to load → silent no-op
	}

	// First-occurrence value per key, only for keys not already in the
	// environment (shell exports win). Emitted sorted, matching `compgen -v | sort -u`.
	vals := map[string]string{}
	for _, kv := range env.ParseFile(filepath.Join(root, ".babysit", ".env")) {
		if _, ok := vals[kv.Key]; ok {
			continue // first occurrence wins
		}
		if _, set := os.LookupEnv(kv.Key); set {
			continue // shell env wins
		}
		vals[kv.Key] = kv.Val
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		esc := strings.ReplaceAll(vals[k], "'", `'\''`)
		fmt.Printf("export %s='%s'\n", k, esc)
	}
	return nil
}

// parseRepoRoot mirrors the shared flag loop of seed / ensure-gitignore: pull
// --repo-root, stop at -- or the first positional, reject unknown -flags (exit
// 2). Returns the repo root and the positionals that follow.
func parseRepoRoot(name string, args []string) (string, []string) {
	repoRoot := ""
loop:
	for len(args) > 0 {
		switch {
		case args[0] == "--repo-root":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, retarget("bbs-secrets %s: --repo-root requires a path\n"), name)
				os.Exit(2)
			}
			repoRoot, args = args[1], args[2:]
		case args[0] == "--":
			args = args[1:]
			break loop
		case strings.HasPrefix(args[0], "-"):
			fmt.Fprintf(os.Stderr, retarget("bbs-secrets %s: unknown flag: %s\n"), name, args[0])
			os.Exit(2)
		default:
			break loop
		}
	}
	if repoRoot == "" {
		fmt.Fprintf(os.Stderr, retarget("bbs-secrets %s: --repo-root is required\n"), name)
		os.Exit(2)
	}
	if fi, err := os.Stat(repoRoot); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, retarget("bbs-secrets %s: not a directory: %s\n"), name, repoRoot)
		os.Exit(2)
	}
	return repoRoot, args
}

func secretsSeed(args []string) error {
	repoRoot, vars := parseRepoRoot("seed", args)

	// Defense-in-depth: gitignore FIRST, even if the caller forgot.
	if _, err := ensureGitignore(repoRoot); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(repoRoot, ".babysit"), 0o755); err != nil {
		return errSilent
	}
	target := filepath.Join(repoRoot, ".babysit", ".env")
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		fmt.Printf("exists: %s\n", target)
		return nil
	}

	var b strings.Builder
	b.WriteString("# Project-local env vars for babysit qa credential resolution.\n")
	b.WriteString("# Uncomment a line and fill in the value to populate the matching\n")
	b.WriteString("# $NAME shell variable for /bbs:qa.\n")
	b.WriteString("# This file is gitignored — never commit values.\n")
	b.WriteString("\n")
	for _, v := range vars {
		if v != "" {
			fmt.Fprintf(&b, "# %s=\n", v)
		}
	}
	if err := os.WriteFile(target, []byte(b.String()), 0o644); err != nil {
		return errSilent
	}
	fmt.Printf("created: %s\n", target)
	return nil
}

func secretsEnsureGitignore(args []string) error {
	repoRoot, _ := parseRepoRoot("ensure-gitignore", args)
	msg, err := ensureGitignore(repoRoot)
	if err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}

// ensureGitignore idempotently adds `.babysit/.env` to <repoRoot>/.gitignore,
// returning "present" or "added". Shared by seed (silently) and the subcommand.
func ensureGitignore(repoRoot string) (string, error) {
	gi := filepath.Join(repoRoot, ".gitignore")
	existing, err := os.ReadFile(gi)
	if err != nil && !os.IsNotExist(err) {
		return "", errSilent
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if line == ".babysit/.env" {
			return "present", nil
		}
	}
	f, err := os.OpenFile(gi, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", errSilent
	}
	defer f.Close()
	if _, err := f.WriteString(".babysit/.env\n"); err != nil {
		return "", errSilent
	}
	return "added", nil
}
