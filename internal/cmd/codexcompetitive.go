package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// codexCompetitiveUsage must stay byte-identical to the usage() heredoc in
// bin/bbs-codex-competitive.
const codexCompetitiveUsage = `Usage: bbs-codex-competitive [--root DIR] [--check] [--dry-run] [--project-only|--global-only]

Project links:
  AGENTS.md      -> CLAUDE.md
  .agents/skills -> ../.claude/skills

Global skill links:
  ~/.codex/skills/bbs:<name> -> <root>/.claude/skills/<name>
  ~/.Codex/skills/bbs:<name> -> <root>/.claude/skills/<name>  (if present)

The source of truth stays Claude-facing; this command gives Codex the global
skill entries and repo-local filenames it expects without copying content.

Options:
  --root DIR  Repository root. Defaults to git top-level or current directory.
  --check     Fail if the Codex-facing symlinks are missing or incorrect.
  --dry-run   Print what would change without writing links.
  --project-only  Only manage AGENTS.md and .agents/skills.
  --global-only   Only manage global Codex skills.
`

// Link targets, verbatim from the bash. Both are relative: AGENTS.md and
// .agents/skills point within the repo, so a clone stays self-contained.
const (
	codexDocTarget    = "CLAUDE.md"
	codexSkillsTarget = "../.claude/skills"
)

// newCodexCompetitiveCmd ports bin/bbs-codex-competitive as
// `bbs codex-competitive`, matching its output bytes and exit codes exactly.
//
// Flag parsing is disabled and done by hand: the bash rolls its own arg loop
// whose error text, ordering and exit codes (2 for misuse, 1 for a failed
// precondition) are the contract skills depend on. Letting cobra/pflag near
// the arguments would replace all of it with cobra's own.
func newCodexCompetitiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "codex-competitive",
		Short:              "link Codex-facing Babysit artifacts to the Claude-facing source",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runCodexCompetitive(args)
		},
	}
}

func runCodexCompetitive(args []string) error {
	var (
		root                                   string
		check, dryRun, projectOnly, globalOnly bool
	)

	for len(args) > 0 {
		switch args[0] {
		case "--root":
			// `ROOT="${2:-}"; shift 2` — with no value left, shift 2 fails and
			// set -e kills the script silently on both channels.
			if len(args) < 2 {
				return errSilent
			}
			root, args = args[1], args[2:]
		case "--check":
			check, args = true, args[1:]
		case "--dry-run":
			dryRun, args = true, args[1:]
		case "--project-only":
			projectOnly, args = true, args[1:]
		case "--global-only":
			globalOnly, args = true, args[1:]
		case "--help", "-h":
			// Exits straight away, so it outruns the mutual-exclusion check.
			fmt.Print(codexCompetitiveUsage)
			return nil
		default:
			fmt.Fprintf(os.Stderr, "unknown option: %s\n", args[0])
			fmt.Fprint(os.Stderr, codexCompetitiveUsage)
			os.Exit(2)
		}
	}

	if projectOnly && globalOnly {
		fmt.Fprintln(os.Stderr, "--project-only and --global-only are mutually exclusive")
		os.Exit(2)
	}

	if root == "" {
		root = codexGitTopLevel()
	}
	// `ROOT="$(cd "$ROOT" && pwd)"`. A failed cd aborts under set -e; bash prints
	// its own `<script>: line 56: cd: …: No such file or directory` diagnostic,
	// which names the script path and a bash line number and so has no native
	// equivalent. Exit 1 is the contract; the diagnostic is not reproduced.
	abs, err := codexLogicalAbs(root)
	if err != nil {
		return errSilent
	}

	// Built by concatenation, not filepath.Join: these strings are printed, and
	// Join would clean away doubled slashes the bash happily emits.
	var (
		srcDoc       = abs + "/CLAUDE.md"
		srcSkills    = abs + "/.claude/skills"
		dstDoc       = abs + "/AGENTS.md"
		dstAgentsDir = abs + "/.agents"
		dstSkills    = abs + "/.agents/skills"
	)

	if st, err := os.Stat(srcDoc); err != nil || !st.Mode().IsRegular() {
		fmt.Fprintf(os.Stderr, "missing source: %s\n", srcDoc)
		return errSilent
	}
	if st, err := os.Stat(srcSkills); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "missing source: %s\n", srcSkills)
		return errSilent
	}

	changed := false

	if !globalOnly {
		if !codexLinkPointsTo(dstDoc, codexDocTarget) {
			changed = true
			fmt.Printf("would link AGENTS.md -> %s\n", codexDocTarget)
		}
		if !codexLinkPointsTo(dstSkills, codexSkillsTarget) {
			changed = true
			fmt.Printf("would link .agents/skills -> %s\n", codexSkillsTarget)
		}
	}

	codexHomes, ok := codexHomeDirs()
	if !ok {
		return errSilent
	}

	// The bash also counts skills into `skill_count` here; nothing ever reads it,
	// so a dead counter is the one thing not carried over.
	if !projectOnly {
		for _, skillDir := range codexSkillDirs(srcSkills) {
			name := filepath.Base(skillDir)
			for _, codexHome := range codexHomes {
				dst := codexHome + "/skills/bbs:" + name
				if !codexLinkPointsTo(dst, skillDir) {
					changed = true
					fmt.Printf("would link %s -> %s\n", dst, skillDir)
				}
			}
		}
	}

	if check {
		if !changed {
			fmt.Println("Codex symlinks are current")
			return nil
		}
		fmt.Fprintln(os.Stderr, "Codex symlinks are stale; run bin/bbs-codex-competitive")
		return errSilent
	}

	if dryRun {
		if !changed {
			fmt.Println("Codex symlinks are current")
		}
		return nil
	}

	// Past this point every failure is a bare `set -e` abort in the bash: the
	// diagnostic comes from mkdir/ln, not from the script, so only exit 1 is
	// reproduced.
	if !globalOnly {
		if err := os.MkdirAll(dstAgentsDir, 0o777); err != nil {
			return errSilent
		}
		// Bug-for-bug: `rm -rf "$DST_DOC" "$DST_SKILLS"` is unconditional, so a
		// real AGENTS.md file or a populated .agents/skills directory is deleted
		// without warning. Replicated deliberately; see the ticket's QA verdict.
		if err := os.RemoveAll(dstDoc); err != nil {
			return errSilent
		}
		if err := os.RemoveAll(dstSkills); err != nil {
			return errSilent
		}
		if err := os.Symlink(codexDocTarget, dstDoc); err != nil {
			return errSilent
		}
		if err := os.Symlink(codexSkillsTarget, dstSkills); err != nil {
			return errSilent
		}
	}

	if !projectOnly {
		for _, codexHome := range codexHomes {
			if err := os.MkdirAll(codexHome+"/skills", 0o777); err != nil {
				return errSilent
			}
		}

		for _, skillDir := range codexSkillDirs(srcSkills) {
			name := filepath.Base(skillDir)
			for _, codexHome := range codexHomes {
				dst := codexHome + "/skills/bbs:" + name
				fi, lerr := os.Lstat(dst)
				switch {
				case lerr == nil && fi.Mode()&os.ModeSymlink != 0:
					// ln -sfn: replace the link itself, never follow it into a dir.
					if err := codexForceSymlink(skillDir, dst); err != nil {
						return errSilent
					}
				case lerr == nil:
					fmt.Fprintf(os.Stderr, "exists and not symlink: %s\n", dst)
					return errSilent
				default:
					if err := os.Symlink(skillDir, dst); err != nil {
						return errSilent
					}
				}
			}
		}
	}

	fmt.Println("Codex symlinks updated")
	return nil
}

// codexGitTopLevel ports `$(git rev-parse --show-toplevel 2>/dev/null || pwd)`.
//
// Shelling out to git is deliberate: `rev-parse` honors GIT_DIR, GIT_WORK_TREE,
// GIT_CEILING_DIRECTORIES and worktree layouts, and reimplementing that on top
// of a Go library would diverge from the bash in exactly the cases that matter.
// The sibling ports (internal/git, qaconfig, upgrade) shell out the same way.
func codexGitTopLevel() string {
	// Output() discards stderr like the bash's 2>/dev/null.
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		// Command substitution strips trailing newlines.
		return strings.TrimRight(string(out), "\n")
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// codexLogicalAbs ports `cd "$path" && pwd`, which yields the *logical* path:
// bash resolves `..` lexically and leaves symlinked components spelled as the
// caller wrote them. filepath.Abs does the same; EvalSymlinks must not be used
// here or a --root under /var would come back as /private/var on macOS and no
// longer match what the caller passed.
func codexLogicalAbs(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// cd fails unless the target exists and is a directory; it follows symlinks.
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	return abs, nil
}

// codexHomeDirs ports the codex_homes array. Reports false when the bash would
// have died first.
func codexHomeDirs() ([]string, bool) {
	if ch := os.Getenv("CODEX_HOME"); ch != "" {
		return []string{ch}, true
	}
	home, ok := os.LookupEnv("HOME")
	if !ok {
		// Under set -u, "$HOME/.codex" on an unset HOME aborts the script before
		// it can print anything of its own.
		return nil, false
	}
	// Bug-for-bug: the bash guards the second entry with
	// [ "$HOME/.Codex" = "$HOME/.codex" ], a *string* compare that is never equal,
	// so .Codex is always appended — despite the usage text's "(if present)", and
	// even though on a case-insensitive filesystem it is the very same directory,
	// which makes every global link get emitted and written twice.
	return []string{home + "/.codex", home + "/.Codex"}, true
}

// codexSkillDirs yields what `for skill_dir in "$SRC_SKILLS"/*` iterates over,
// after the bash's `[ -d ]` test and references|shared|.* filter.
//
// Ordering is byte order (os.ReadDir sorts by name), whereas the bash's glob
// collates per LC_COLLATE. The two agree under the C locale; nothing consumes
// this ordering programmatically, and libc collation isn't reachable natively.
func codexSkillDirs(srcSkills string) []string {
	entries, err := os.ReadDir(srcSkills)
	if err != nil {
		// An unreadable dir leaves the glob unmatched, i.e. no iterations.
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		// The glob never yields dotfiles without dotglob, so the bash's `.*)`
		// case is unreachable; ReadDir does yield them, hence the explicit skip.
		if strings.HasPrefix(name, ".") || name == "references" || name == "shared" {
			continue
		}
		path := srcSkills + "/" + name
		// [ -d ] follows symlinks, so a link to a directory counts.
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, path)
	}
	return out
}

// codexLinkPointsTo ports link_points_to: a symlink whose literal target text
// matches. The target is compared as written, never resolved.
func codexLinkPointsTo(path, target string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	got, err := os.Readlink(path)
	return err == nil && got == target
}

// codexForceSymlink ports `ln -sfn target link`.
func codexForceSymlink(target, link string) error {
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, link)
}
