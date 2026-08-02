package workspace

import (
	"fmt"
	"path/filepath"
)

// Resolver answers topology questions for one repo checkout. It loads the
// repo's config.yaml and its workspace once, at construction: `bbs ticket
// serve` asks per sibling inside a loop over a constant toplevel, and
// resolving through two yaml files per question would re-read them N times.
// Constructing one per loop keeps the caching visible and scoped instead of
// hiding a package-level cache that tests would have to reset.
type Resolver struct {
	toplevel string
	gitURL   string
	cfg      RepoConfig
	present  bool
	ws       Workspace
	wsOK     bool
	err      error
}

// NewResolver loads the repo's config and workspace. gitURL is the repo's
// origin url and may be empty — it is one of the two ways a registry entry is
// matched to this checkout, the other being the local path.
//
// A repo with no config.yaml yields a resolver that answers "not a member" to
// everything and no error. That is today's every repo.
func NewResolver(toplevel, gitURL string) *Resolver {
	r := &Resolver{toplevel: toplevel, gitURL: gitURL}
	cfg, present, err := LoadRepoConfig(toplevel)
	r.cfg, r.present, r.err = cfg, present, err
	if err != nil || !present {
		return r
	}
	ws, wsErr := Load(cfg.Workspace)
	if wsErr != nil {
		r.err = fmt.Errorf("repo declares workspace %q, which is not registered on this machine.\n"+
			"  Fix: bbs workspace add-repo %s --git-url <url> --path %s",
			cfg.Workspace, cfg.Workspace, toplevel)
		return r
	}
	r.ws, r.wsOK = ws, true
	if !r.member() {
		r.err = fmt.Errorf("repo declares workspace %q, but that workspace does not list this repo.\n"+
			"  Fix: bbs workspace add-repo %s --git-url <url> --path %s",
			cfg.Workspace, cfg.Workspace, toplevel)
	}
	return r
}

// member reports whether the workspace lists this checkout, by local path or
// by git url.
func (r *Resolver) member() bool {
	for _, e := range r.ws.Repos {
		if e.Path != "" && samePath(e.Path, r.toplevel) {
			return true
		}
		if r.gitURL != "" && e.GitURL == r.gitURL {
			return true
		}
	}
	return false
}

// Err is the membership conflict, if any. A conflict is a genuine
// disagreement between two things that both claim to describe this repo, so
// callers BLOCK on it rather than picking a side — the same call the ticket
// identity ladder makes.
func (r *Resolver) Err() error { return r.err }

// Registered reports whether this repo is a usable workspace member.
func (r *Resolver) Registered() bool { return r.present && r.wsOK && r.err == nil }

// Name is the workspace this repo belongs to, or "".
func (r *Resolver) Name() string {
	if !r.Registered() {
		return ""
	}
	return r.cfg.Workspace
}

// Config is the repo's config, and whether a file was present.
func (r *Resolver) Config() (RepoConfig, bool) { return r.cfg, r.present }

// FanOut reports whether sibling repos should be resolved at all.
//
// This is what repo_type buys. Under monorepo, a ticket's siblings live in
// this same checkout: there is no sibling path to resolve, and treating an
// unresolvable one as missing config would report a problem that isn't there.
// Anything else — polyrepo, unset, or no config.yaml — keeps today's behavior.
func (r *Resolver) FanOut() bool {
	return !(r.Registered() && r.cfg.RepoType == TypeMonorepo)
}

// RolePath resolves a sibling role to its local path through the registry.
// Only entries that carry a path on this machine can answer.
func (r *Resolver) RolePath(role string) (string, bool) {
	if !r.Registered() || role == "" {
		return "", false
	}
	for _, e := range r.ws.Repos {
		if e.Role == role && e.Path != "" {
			return e.Path, true
		}
	}
	return "", false
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
