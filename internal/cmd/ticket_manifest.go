package cmd

import (
	"fmt"
	"os"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This file ports the manifest.yaml family of bin/bbs-ticket.bash: get-manifest,
// set-branch, and init (index.json seed + first manifest.yaml). The parser and
// writer live in internal/ticket/manifest.go. `ensure` stays delegated — it cuts
// git branches (safe-cut gate) and belongs with the base-ops slice.

// runGetManifest ports get-manifest: read the canonical manifest.yaml and print
// it as JSON, validating version 1. Mirrors the bash quirk where a version
// mismatch prints the BLOCK line to stderr but still exits 0 (the trailing
// `echo` resets the status).
func runGetManifest(args []string) {
	env := identity.Resolve()
	if len(args) > 0 && args[0] != "" {
		env.Ticket = args[0] // EXPLICIT_TICKET override
	}
	needTicket(env)
	st := ticket.New(env)
	m := st.ManifestPath()
	if !fileExists(m) {
		fmt.Fprintf(os.Stderr, "get-manifest: no manifest at %s\n", m)
		os.Exit(1)
	}
	man, err := ticket.ReadManifest(m)
	if err != nil || man == nil || man.Version != "1" {
		fmt.Fprintf(os.Stderr, "manifest version '%s' != 1 (BLOCK)\n", manifestVersion(man))
		fmt.Println() // the subcommand's trailing `echo`
		os.Exit(0)
	}
	fmt.Println(string(ticket.MarshalManifest(man)))
	os.Exit(0)
}

// runSetBranch ports set-branch: update repos[name==repo].branch and re-emit the
// manifest. Unlike get-manifest, a version mismatch or a missing repo is fatal
// (exit 2), matching the bash `|| exit 2` guards.
func runSetBranch(args []string) {
	sticket, srepo, sbranch := argAt(args, 0), argAt(args, 1), argAt(args, 2)
	if sticket == "" || srepo == "" || sbranch == "" {
		fmt.Fprintln(os.Stderr, "set-branch: usage: bbs-ticket set-branch <ticket> <repo> <branch>")
		os.Exit(2)
	}
	env := identity.Resolve()
	env.Ticket = sticket
	st := ticket.New(env)
	m := st.ManifestPath()
	if !fileExists(m) {
		fmt.Fprintf(os.Stderr, "set-branch: no manifest at %s (ticket may not exist; run 'bbs-ticket ensure' first)\n", m)
		os.Exit(1)
	}
	acquireOrDie(st)
	man, err := ticket.ReadManifest(m)
	if err != nil || man == nil || man.Version != "1" {
		st.ReleaseLock()
		fmt.Fprintf(os.Stderr, "manifest version '%s' != 1 (BLOCK)\n", manifestVersion(man))
		fmt.Fprintln(os.Stderr, "set-branch: manifest_read failed")
		os.Exit(2)
	}
	hit := false
	for i := range man.Repos {
		if man.Repos[i].Name == srepo {
			man.Repos[i].Branch = sbranch
			hit = true
			break
		}
	}
	if !hit {
		st.ReleaseLock()
		fmt.Fprintf(os.Stderr, "repo '%s' not in manifest\n", srepo)
		os.Exit(2)
	}
	if err := ticket.WriteManifest(m, man.Ticket, man.Title, man.Repos); err != nil {
		st.ReleaseLock()
		fmt.Fprintln(os.Stderr, "set-branch: manifest_write failed")
		os.Exit(2)
	}
	st.ReleaseLock()
	os.Exit(0)
}

// runInit ports init: seed index.json (ensure_defaults + id + pointers.branch +
// optional origin/parent/pointer fields), append a ticket_initialized history
// row on a fresh ticket, and seed manifest.yaml when one doesn't exist. No git:
// branch-cutting is `ensure`'s job.
func runInit(args []string) {
	env := identity.Resolve()
	needTicket(env)

	var parent, otype, seed, pplan, ddoc, pos, repo, worktree string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--parent":
			parent, i = valueOf(args, i, "--parent"), i+1
		case "--origin-type":
			otype, i = valueOf(args, i, "--origin-type"), i+1
		case "--seed":
			seed, i = valueOf(args, i, "--seed"), i+1
		case "--plan":
			pplan, i = valueOf(args, i, "--plan"), i+1
		case "--design-doc":
			ddoc, i = valueOf(args, i, "--design-doc"), i+1
		case "--position":
			pos, i = valueOf(args, i, "--position"), i+1
		case "--repo":
			repo, i = valueOf(args, i, "--repo"), i+1
		case "--worktree":
			worktree, i = valueOf(args, i, "--worktree"), i+1
		}
	}

	st := ticket.New(env)
	st.EnsureDirs()
	acquireOrDie(st)

	f := st.IndexPath()
	fresh := !fileExists(f)
	doc := ticket.ReadDoc(f)
	doc.EnsureDefaults(env.Ticket) // unconditional in bash, idempotent here
	doc.Set("id", env.Ticket)
	doc.Set("pointers.branch", env.Branch)
	if parent != "" {
		doc.Set("parent", parent)
		doc.Set("origin.parent", parent)
	}
	if otype != "" {
		doc.Set("origin.type", otype)
	}
	if seed != "" {
		doc.Set("origin.seed", seed)
	}
	if pplan != "" {
		doc.Set("origin.plan", pplan)
	}
	if ddoc != "" {
		doc.Set("origin.design_doc", ddoc)
	}
	if pos != "" {
		doc.Set("origin.position", pos)
	}
	if repo != "" {
		doc.Set("pointers.repo", repo)
	}
	if worktree != "" {
		doc.Set("pointers.worktree", worktree)
	}
	if err := ticket.WriteDoc(f, doc); err != nil {
		st.ReleaseLock()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if fresh {
		st.HistoryAppendExtra("ticket_initialized", actorRole(), "")
		m := st.ManifestPath()
		if !fileExists(m) {
			repos := []ticket.Repo{initRepo(repo, worktree, env)}
			if err := ticket.WriteManifest(m, env.Ticket, "", repos); err != nil {
				fmt.Fprintln(os.Stderr, "init: warning — manifest.yaml seed failed (continuing)")
			}
		}
	}

	st.ReleaseLock()
	fmt.Println(f)
	os.Exit(0)
}

// initRepo builds the single manifest repo entry init seeds: the --repo path
// (with --worktree) when given, else single_repo_json with the resolved slug.
func initRepo(repo, worktree string, env identity.Env) ticket.Repo {
	if repo != "" {
		wt := worktree
		if wt == "" {
			wt = "."
		}
		return ticket.Repo{Name: repo, Branch: env.Branch, Canonical: ".", Worktree: wt, Base: "main", Pushed: "false"}
	}
	slug := env.Slug // bash `${SLUG:-repo}`; identity already defaults empty→unknown
	if slug == "" {
		slug = "repo"
	}
	return ticket.Repo{Name: slug, Branch: env.Branch, Canonical: ".", Worktree: ".", Base: "main", Pushed: "false"}
}

// manifestVersion returns the parsed version string for the BLOCK message, "" if
// the manifest is nil.
func manifestVersion(m *ticket.Manifest) string {
	if m == nil {
		return ""
	}
	return m.Version
}

// argAt returns the positional arg at i, or "" — mirrors bash `${N:-}`.
func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
