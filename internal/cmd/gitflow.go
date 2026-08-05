package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Git-flow policy resolution — the one codepath that reads
// `.babysit/git-flow.yaml`. Everything a repo's speed↔quality tradeoff implies
// derives from a single `profile:` key; `bbs autopilot git-flow` prints the
// derived set so skills consume it instead of re-parsing the yaml with their
// own fallbacks.
//
//	profile        pet            startup        enterprise
//	priority       ship now       speed > qual   quality > speed
//	mode           trunk          worktree       worktree
//	land           none           local          local
//	rigor          smoke          standard       strict
//	review effort  low            medium         high
//
// `startup` and `enterprise` default to `worktree` because parallel tickets are
// the normal shape and the two halves are one decision: worktrees keep the
// primary checkout pinned to `base_branch`, which is the only state in which
// `serve` can compose a finished batch for review — `land: local`'s
// human-QA-before-PR checkpoint. Under `mode: branch` the first ticket cuts in
// place, the primary leaves base, and every compose BLOCKs. The cost is real
// (a commit + `merge-base` per test iteration instead of edit-and-refresh); a
// repo that runs one ticket at a time buys the 0-step loop back with an
// explicit `mode: branch` + `land: pr`.
//
// `pet` stays on `trunk`: it answers "mistakes are cheap, no ceremony", and
// separable parallel tickets is the request that means a repo has outgrown it.
// It is also what an unconfigured repo resolves to — no config should read as
// no ceremony, the same as running plain git in a repo nobody set up.

type gitFlowPolicy struct {
	Profile      string // pet | startup | enterprise
	BaseBranch   string
	Mode         string // trunk | branch | worktree
	Land         string // none | local | pr
	Push         string // true | false
	Rigor        string // smoke | standard | strict
	ReviewEffort string // low | medium | high
}

var gitFlowProfiles = map[string]gitFlowPolicy{
	"pet":        {Mode: "trunk", Land: "none", Push: "true", Rigor: "smoke", ReviewEffort: "low"},
	"startup":    {Mode: "worktree", Land: "local", Push: "true", Rigor: "standard", ReviewEffort: "medium"},
	"enterprise": {Mode: "worktree", Land: "local", Push: "true", Rigor: "strict", ReviewEffort: "high"},
}

// gitFlowAliases keeps repos configured with the four pre-profile names
// working. Each pins the mode/land its name literally promises rather than
// inheriting the profile's preset — `branch-pr` means branch + PR whatever
// `startup` derives today.
var gitFlowAliases = map[string]struct{ profile, mode, land string }{
	"trunk":           {"pet", "trunk", "none"},
	"branch-pr":       {"startup", "branch", "pr"},
	"worktree-pr":     {"enterprise", "worktree", "pr"},
	"worktree-review": {"enterprise", "worktree", "local"},
}

// resolveGitFlow reads the git-flow.yaml of the repo at dir ("" = process cwd)
// and derives the full policy. A missing file is not an error — it resolves to
// the default profile, same as a repo that never ran setup-project.
func resolveGitFlow(dir string) (gitFlowPolicy, error) {
	content := ""
	if top := gitOutIn(dir, "rev-parse", "--show-toplevel"); top != "" {
		if b, err := os.ReadFile(filepath.Join(top, ".babysit", "git-flow.yaml")); err == nil {
			content = string(b)
		}
	}
	return gitFlowFrom(content, baseBranchIn(dir))
}

// gitFlowFrom is resolveGitFlow's pure core: yaml text + the already-resolved
// base branch in, derived policy out.
func gitFlowFrom(content, base string) (gitFlowPolicy, error) {
	name := gfScalar(content, "profile")
	aliasMode, aliasLand := "", ""
	if name == "" {
		// Unconfigured means "behave like a plain git repo": work lands on the
		// branch the user is already standing on, nothing is cut, nothing is
		// composed. That is `pet`. A repo earns branches, worktrees and review
		// venues by asking for them in setup-project — never by default.
		// Explicit keys still win below, so a pre-`profile:` config that spells
		// out `mode:`/`land:`/`ticket_branch:` keeps the shape it wrote down.
		name = "pet"
	} else if a, ok := gitFlowAliases[name]; ok {
		name, aliasMode, aliasLand = a.profile, a.mode, a.land
	}
	p, ok := gitFlowProfiles[name]
	if !ok {
		return p, fmt.Errorf("invalid profile '%s' in .babysit/git-flow.yaml (pet|startup|enterprise, or legacy trunk|branch-pr|worktree-pr|worktree-review)", name)
	}
	p.Profile = name
	p.BaseBranch = base
	if aliasMode != "" {
		p.Mode = aliasMode
	}
	if aliasLand != "" {
		p.Land = aliasLand
	}

	// Explicit keys always win over the profile preset.
	if m := gfScalar(content, "mode"); m != "" {
		p.Mode = m
	} else if aliasMode == "" {
		switch gfScalar(content, "ticket_branch") { // legacy alias for mode
		case "optional":
			p.Mode = "trunk"
		case "required":
			p.Mode = "branch"
		}
	}
	landSet := gfScalar(content, "land")
	if landSet != "" {
		p.Land = landSet
	}
	if v := gfScalar(content, "push"); v != "" {
		p.Push = v
	}

	switch p.Mode {
	case "trunk", "branch", "worktree":
	default:
		return p, fmt.Errorf("invalid mode '%s' in .babysit/git-flow.yaml (trunk|branch|worktree)", p.Mode)
	}
	switch p.Land {
	case "none", "local", "pr":
	default:
		return p, fmt.Errorf("invalid land '%s' in .babysit/git-flow.yaml (none|local|pr)", p.Land)
	}
	switch p.Push {
	case "true", "false":
	default:
		return p, fmt.Errorf("invalid push '%s' in .babysit/git-flow.yaml (true|false)", p.Push)
	}
	// `branch` + `local` reads as configured and can never run: the first
	// ticket cuts in place, the primary leaves base_branch, and every compose
	// BLOCKs with "primary checkout is on 'feat/…', not base".
	//
	// A lone `mode: branch` — the solo-loop opt-out, and every repo configured
	// before `land: local` became a preset — means "one ticket at a time", and
	// the landing that shape has always implied is a PR per ticket. Derive it
	// rather than making a file that used to resolve stop resolving. Only a
	// human who wrote *both* halves gets the error, because there the two keys
	// contradict each other and guessing which one was meant would be wrong.
	if p.Mode == "branch" && p.Land == "local" {
		if landSet == "" {
			p.Land = "pr"
		} else {
			return p, fmt.Errorf("incoherent .babysit/git-flow.yaml: mode 'branch' with land 'local' — a branch cut in place takes the primary checkout off '%s', so nothing can compose there. Use 'land: pr' for per-ticket PRs, or drop 'mode: branch' to keep the profile's worktree default", p.BaseBranch)
		}
	}
	return p, nil
}

// printGitFlow backs `bbs autopilot git-flow`: shell-eval-able BBS_* lines, so
// a skill reads policy with one `eval` instead of its own yaml fallbacks.
//
// Every value is single-quoted. The consumer contract is `eval "$(bbs autopilot
// git-flow)"`, so an unquoted value is arbitrary shell: `base_branch: main;
// rm -rf ~` is a committed file in a repo babysit clones and reads. The other
// keys are enum-validated, but quoting them too keeps one rule to remember.
func printGitFlow() {
	p, err := resolveGitFlow("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-flow: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("BBS_PROFILE=%s\n", shq(p.Profile))
	fmt.Printf("BBS_BASE_BRANCH=%s\n", shq(p.BaseBranch))
	fmt.Printf("BBS_MODE=%s\n", shq(p.Mode))
	fmt.Printf("BBS_LAND=%s\n", shq(p.Land))
	fmt.Printf("BBS_PUSH=%s\n", shq(p.Push))
	fmt.Printf("BBS_RIGOR=%s\n", shq(p.Rigor))
	fmt.Printf("BBS_REVIEW_EFFORT=%s\n", shq(p.ReviewEffort))
}

// shq single-quotes s for `eval`. Inside single quotes the shell expands
// nothing, so the only character needing care is the quote itself.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ─── yaml scalars ────────────────────────────────────────────────────────────

var gfCommentLine = regexp.MustCompile(`^\s*#`)
var gfCommentTail = regexp.MustCompile(`\s+#.*$`)

// gfScalar reads a top-level scalar key: first non-comment line beginning
// `key:`, prefix + trailing comment + surrounding quotes stripped. One parser
// for every git-flow key — `profile:` and `base_branch:` must not be read by
// different rules.
func gfScalar(content, key string) string {
	prefix := key + ":"
	for _, ln := range strings.Split(content, "\n") {
		if gfCommentLine.MatchString(ln) {
			continue
		}
		if strings.HasPrefix(ln, prefix) {
			return gfValue(ln[len(prefix):])
		}
	}
	return ""
}

func gfValue(v string) string {
	if j := gfCommentTail.FindStringIndex(v); j != nil {
		v = v[:j[0]]
	}
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, `"`)
	v = strings.TrimPrefix(v, `'`)
	v = strings.TrimSuffix(v, `"`)
	v = strings.TrimSuffix(v, `'`)
	return v
}

// gitFlowBase resolves the base branch out of git-flow.yaml: top-level
// `base_branch:`, then the `develop:` key nested under `branches:`.
func gitFlowBase(content string) string {
	if v := gfScalar(content, "base_branch"); v != "" {
		return v
	}
	topKey := regexp.MustCompile(`^[A-Za-z_]`)
	branchesHdr := regexp.MustCompile(`^branches:\s*$`)
	develop := regexp.MustCompile(`^\s+develop:\s*`)
	inBranches := false
	for _, ln := range strings.Split(content, "\n") {
		if gfCommentLine.MatchString(ln) {
			continue
		}
		if branchesHdr.MatchString(ln) {
			inBranches = true
			continue
		}
		if topKey.MatchString(ln) && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			inBranches = false
		}
		if inBranches && develop.MatchString(ln) {
			return gfValue(develop.ReplaceAllString(ln, ""))
		}
	}
	return ""
}
