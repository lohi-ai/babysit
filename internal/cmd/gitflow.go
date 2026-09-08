package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Git-flow policy resolution — the one codepath that reads
// `.babysit/git-flow.yaml`. A single `profile:` key says what a mistake costs
// in this repo; `bbs autopilot git-flow` prints the derived set so skills
// consume it instead of re-parsing the yaml with their own fallbacks.
//
//	profile        pet            startup        enterprise
//	priority       ship now       speed > qual   quality > speed
//	land           none           pr             pr
//	rigor          smoke          standard       strict
//	review effort  low            medium         high
//
// No profile derives a mode: every repo works on the branch the user is
// already standing on (`trunk`). Cutting branches and diverting to worktrees
// is something a run *asks for* — `--mode` on `ensure`/autopilot, which is
// what `foreman` passes for a parallel batch — never something a repo gets by
// having (or not having) a config file. A tool that silently moves the user
// off their branch is a tool they cannot manage.
//
// `finish` is likewise opt-in per repo. It is the one key that lets a run close
// a ticket out by itself — merging into the local base, or opening the PR —
// and it stays unset by default because both are decisions somebody should
// have written down rather than inherited from a profile.

type gitFlowPolicy struct {
	Profile      string // pet | startup | enterprise
	BaseBranch   string
	Mode         string // trunk | branch | worktree
	Land         string // none | local | pr
	Finish       string // review | land | pr — who closes a verified ticket
	Push         string // true | false
	Rigor        string // smoke | standard | strict
	ReviewEffort string // low | medium | high
}

// A profile only sets the cost-of-a-mistake dials. The git shape is not one of
// them — see gitFlowFrom, which presets it identically for all three.
var gitFlowProfiles = map[string]gitFlowPolicy{
	"pet":        {Land: "none", Rigor: "smoke", ReviewEffort: "low"},
	"startup":    {Land: "pr", Rigor: "standard", ReviewEffort: "medium"},
	"enterprise": {Land: "pr", Rigor: "strict", ReviewEffort: "high"},
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
	p, err := gitFlowFrom(content, baseBranchIn(dir))
	return verifierClamp(p), err
}

// verifierClamp is the mechanical half of "the verifier does not close out".
// A spawned verifier grades a diff it did not write; the session that spawned
// it owns git. Asking for that in the prompt only would be prose enforcement —
// 44cf669 is the record of what that is worth — so the policy the run actually
// reads answers for it: nothing to finish, nothing to push.
func verifierClamp(p gitFlowPolicy) gitFlowPolicy {
	if os.Getenv("BABYSIT_VERIFIER") != "" {
		p.Finish, p.Push = "review", "false"
	}
	return p
}

// gitFlowFrom is resolveGitFlow's pure core: yaml text + the already-resolved
// base branch in, derived policy out.
func gitFlowFrom(content, base string) (gitFlowPolicy, error) {
	name := gfScalar(content, "profile")
	aliasMode, aliasLand := "", ""
	if name == "" {
		// Unconfigured means "behave like a plain git repo": no PR venue, smoke
		// QA, work on the current branch. Explicit keys still win below, so a
		// pre-`profile:` config keeps the shape it wrote down.
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
	// The git shape every profile shares, stated once: stay on the branch the
	// user is standing on, push it, never move the local base unattended. Only
	// a legacy alias or an explicit key below changes it.
	p.Mode, p.Push, p.Finish = "trunk", "true", "review"
	if aliasMode != "" {
		p.Mode = aliasMode
	}
	if aliasLand != "" {
		p.Land = aliasLand
	}
	// `auto_land` was the boolean half of what `finish` now says in one word.
	// Keeping it as a silent alias would leave two keys for one decision — and
	// dropping it silently would turn a repo that lands into one that stops. So
	// it fails loudly with the one-line rewrite, once, per repo.
	if v := gfScalar(content, "auto_land"); v != "" {
		want := "review"
		if v == "true" {
			want = "land"
		}
		return p, fmt.Errorf("auto_land is gone from .babysit/git-flow.yaml — replace 'auto_land: %s' with 'finish: %s' (review|land|pr)", v, want)
	}
	if aliasMode == "" && gfScalar(content, "mode") == "" {
		switch gfScalar(content, "ticket_branch") { // legacy alias for mode
		case "optional":
			p.Mode = "trunk"
		case "required":
			p.Mode = "branch"
		}
	}

	// Explicit keys always win over the profile preset, and each is validated
	// against its enum as it is read — one rule per key, in one place.
	for _, k := range []struct {
		key, allowed string
		dst          *string
	}{
		{"mode", "trunk|branch|worktree", &p.Mode},
		{"land", "none|local|pr", &p.Land},
		{"push", "true|false", &p.Push},
		{"finish", "review|land|pr", &p.Finish},
	} {
		if v := gfScalar(content, k.key); v != "" {
			*k.dst = v
		}
		if !slices.Contains(strings.Split(k.allowed, "|"), *k.dst) {
			return p, fmt.Errorf("invalid %s '%s' in .babysit/git-flow.yaml (%s)", k.key, *k.dst, k.allowed)
		}
	}
	// The one combination that cannot be honoured. `create-pr` BLOCKs under
	// `land: none` by design — the push is the release there — so a repo asking
	// foreman to finish by PR would discover the contradiction only after a
	// batch had already run. Say it at config-read instead.
	if p.Finish == "pr" && p.Land == "none" {
		return p, fmt.Errorf("finish 'pr' needs a PR venue, but land is 'none' in .babysit/git-flow.yaml — set land: pr (or profile: startup) to open PRs, or finish: land to merge into %s locally", p.BaseBranch)
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
	fmt.Printf("BBS_FINISH=%s\n", shq(p.Finish))
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
