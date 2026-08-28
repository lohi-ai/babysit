package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reallongnguyen/babysit/internal/git"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// This file ports the ticket filesystem broker of bin/bbs-ticket.bash: path (the
// canonical→legacy resolver with dual-presence + sunset telemetry), list,
// reconcile (the filesystem→status ladder), find-similar, and assert-cwd. All
// are read/derive over the Layout C home; reconcile is the only mutator and it
// only advances index.json.status forward.

var pathKinds = map[string]bool{
	"home": true, "index": true, "requirement": true, "design": true,
	"plan": true, "manifest": true, "history": true, "checkpoint": true,
	"handoff": true, "verdict": true, "review": true, "evidence": true,
	"sub-ticket": true, "worktree": true,
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func legacySunset() string   { return envOr("BBS_LEGACY_SUNSET", "2026-07-01") }
func legacyHardfail() string { return envOr("BBS_LEGACY_HARDFAIL", "2026-09-29") }

// legacyWarnFrom mirrors _date_minus_days(hardfail, 14).
func legacyWarnFrom() string {
	if t, err := time.Parse("2006-01-02", legacyHardfail()); err == nil {
		return t.AddDate(0, 0, -14).Format("2006-01-02")
	}
	return legacyHardfail()
}

// runPath ports the `path` subcommand (bbs-ticket.bash:2784-3087).
func runPath(args []string) {
	kind := ""
	if len(args) > 0 {
		kind = args[0]
		args = args[1:]
	}
	if kind == "" {
		printPathHelp()
		os.Exit(2)
	}
	if !pathKinds[kind] {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: unknown kind (try: bbs-ticket path)\n"), kind)
		os.Exit(2)
	}

	mode, pskill, pname, pseq, pslug := "", "", "", "", ""
	platest := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--read":
			mode = "read"
		case "--write":
			mode = "write"
		case "--skill":
			pskill, i = valueAt(args, i), i+1
		case "--name":
			pname, i = valueAt(args, i), i+1
		case "--seq":
			pseq, i = valueAt(args, i), i+1
		case "--slug":
			pslug, i = valueAt(args, i), i+1
		case "--latest":
			platest = true
		default:
			fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: unknown selector '%s'\n"), kind, args[i])
			printPathKindHelp(kind)
			os.Exit(2)
		}
	}

	if mode == "" {
		printPathKindHelp(kind)
		os.Exit(2)
	}

	if mode == "write" {
		switch kind {
		case "handoff":
			fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: handoff: --write rejected, use `bbs-ticket add-handoff --skill S --status STATUS` instead"))
			os.Exit(2)
		case "verdict":
			fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: verdict: --write rejected, use `bbs-ticket set-verdict --skill S` instead"))
			os.Exit(2)
		case "review":
			fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: review: --write rejected, use `bbs-ticket set-review --skill S` instead"))
			os.Exit(2)
		case "worktree":
			fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: worktree: --write rejected; worktree path is derived from manifest.yaml"))
			os.Exit(2)
		}
	}

	if pskill != "" {
		pskill = safePathComponent(kind, "skill", pskill)
	}
	if pname != "" {
		pname = safePathComponent(kind, "name", pname)
	}
	if pslug != "" {
		pslug = safePathComponent(kind, "slug", pslug)
	}
	if pseq != "" {
		pseq = safeSeq(kind, pseq)
	}

	switch kind {
	case "verdict", "review":
		if pskill == "" {
			fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: --skill is required (try: bbs-ticket path %s --skill <name> --read)\n"), kind, kind)
			os.Exit(2)
		}
	case "evidence":
		if pskill == "" {
			fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: evidence: --skill is required (try: bbs-ticket path evidence --skill <name> --name <file> --write)"))
			os.Exit(2)
		}
		if mode == "write" && pname == "" {
			fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: evidence: --name is required for --write"))
			os.Exit(2)
		}
	case "sub-ticket":
		if mode == "write" {
			if pseq == "" {
				fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: sub-ticket: --seq is required for --write"))
				os.Exit(2)
			}
			if pslug == "" {
				fmt.Fprintln(os.Stderr, retarget("bbs-ticket path: sub-ticket: --slug is required for --write"))
				os.Exit(2)
			}
		}
	}

	env := identity.Resolve()
	needTicket(env)
	st := ticket.New(env)
	th := st.Home()

	canonical := ""
	switch kind {
	case "home":
		canonical = th
	case "index":
		canonical = filepath.Join(th, "index.json")
	case "requirement":
		canonical = filepath.Join(th, "requirement.md")
	case "design":
		canonical = filepath.Join(th, "design.md")
	case "plan":
		canonical = filepath.Join(th, "plan.md")
	case "manifest":
		canonical = filepath.Join(th, "manifest.md")
	case "history":
		canonical = filepath.Join(th, "history.jsonl")
	case "checkpoint":
		canonical = filepath.Join(th, "checkpoint.json")
	case "verdict":
		canonical = filepath.Join(th, "verdicts", pskill+".md")
	case "review":
		canonical = filepath.Join(th, "reviews", pskill+".md")
	case "evidence":
		if pname != "" {
			canonical = filepath.Join(th, "evidence", pskill, pname)
		} else {
			canonical = filepath.Join(th, "evidence", pskill)
		}
	case "sub-ticket":
		canonical = subTicketCanonical(th, pseq, pslug)
	case "handoff":
		canonical = handoffCanonical(th, pskill, pseq, platest)
	case "worktree":
		emitWorktree(st)
		os.Exit(0)
	}

	if mode == "write" {
		if kind == "home" {
			_ = os.MkdirAll(canonical, 0o755)
		} else {
			_ = os.MkdirAll(filepath.Dir(canonical), 0o755)
		}
		fmt.Println(canonical)
		os.Exit(0)
	}

	// --read: canonical → legacy fallbacks.
	canonicalHit := canonical != "" && pathExists(canonical)
	legacyHit := firstLegacyHit(th, kind, pskill, platest, pseq, pslug)

	if canonicalHit && legacyHit != "" {
		fmt.Fprintf(os.Stderr, "BBS_PATH_DUAL %s %s %s\n", kind, canonical, legacyHit)
		pathTelemetryAppend(env, kind, "dual", canonical, legacyHit)
	}
	if canonicalHit {
		fmt.Println(canonical)
		os.Exit(0)
	}
	if legacyHit != "" {
		today := time.Now().Format("2006-01-02")
		if today > legacySunset() {
			fmt.Fprintf(os.Stderr, "BBS_PATH_LEGACY_SUNSET %s %s (sunset %s, hard-fail %s)\n", kind, legacyHit, legacySunset(), legacyHardfail())
		}
		if today > legacyWarnFrom() {
			daysLeft := "?"
			if hf, err := time.Parse("2006-01-02", legacyHardfail()); err == nil {
				if td, err := time.Parse("2006-01-02", today); err == nil {
					daysLeft = strconv.Itoa(int(hf.Sub(td).Hours()) / 24)
				}
			}
			fmt.Fprintf(os.Stderr, "BBS_PATH_HARDFAIL_IN %sd %s %s\n", daysLeft, kind, legacyHit)
		}
		if os.Getenv("BBS_PATH_TELEMETRY") != "0" {
			fmt.Fprintf(os.Stderr, "BBS_PATH_FALLBACK %s %s\n", kind, legacyHit)
			pathTelemetryAppend(env, kind, "fallback", canonical, legacyHit)
		}
		fmt.Println(legacyHit)
		os.Exit(0)
	}

	if canonical != "" {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: not found at %s (legacy fallbacks also empty)\n"), kind, canonical)
	} else {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: not found (no matching files)\n"), kind)
	}
	os.Exit(1)
}

// valueAt returns args[i+1] or "" (bash ${2:-}); the caller advances i.
func valueAt(args []string, i int) string {
	if i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// safeSeq mirrors bash _safe_seq: numeric-only, else exit 2.
func safeSeq(kind, value string) string {
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(value) {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: --seq '%s' rejected (must be numeric)\n"), kind, value)
		os.Exit(2)
	}
	return value
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func firstGlob(pattern string) string {
	m, _ := filepath.Glob(pattern)
	sort.Strings(m)
	for _, f := range m {
		if pathExists(f) {
			return f
		}
	}
	return ""
}

func subTicketCanonical(th, pseq, pslug string) string {
	switch {
	case pseq != "" && pslug != "":
		return filepath.Join(th, "sub-tickets", fmt.Sprintf("%03d-%s.md", atoiSeq(pseq), pslug))
	case pseq != "":
		return firstGlob(filepath.Join(th, "sub-tickets", fmt.Sprintf("%03d-*.md", atoiSeq(pseq))))
	case pslug != "":
		return firstGlob(filepath.Join(th, "sub-tickets", "[0-9][0-9][0-9]-"+pslug+".md"))
	}
	return ""
}

func handoffCanonical(th, pskill, pseq string, platest bool) string {
	if platest {
		if pskill != "" {
			// highest-NNN match (bash keeps the last glob hit, which is sorted)
			return lastGlob(filepath.Join(th, "handoffs", "[0-9][0-9][0-9]-"+pskill+"-*.md"))
		}
		l := filepath.Join(th, "handoffs", "LATEST")
		if b, err := os.ReadFile(l); err == nil {
			name := strings.TrimRight(string(b), "\n")
			if invalidLatestName(name) {
				fmt.Fprintf(os.Stderr, retarget("bbs-ticket path handoff: LATEST contains invalid name '%s'\n"), name)
				os.Exit(3)
			}
			return filepath.Join(th, "handoffs", name)
		}
		return ""
	}
	if pseq != "" {
		padded := fmt.Sprintf("%03d", atoiSeq(pseq))
		if pskill != "" {
			return firstGlob(filepath.Join(th, "handoffs", padded+"-"+pskill+"-*.md"))
		}
		return firstGlob(filepath.Join(th, "handoffs", padded+"-*.md"))
	}
	// No selector: directory ref (no legacy fall-back here).
	return filepath.Join(th, "handoffs")
}

func lastGlob(pattern string) string {
	m, _ := filepath.Glob(pattern)
	sort.Strings(m)
	last := ""
	for _, f := range m {
		if pathExists(f) {
			last = f
		}
	}
	return last
}

func atoiSeq(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// emitWorktree ports the worktree kind: read the worktree path from manifest.yaml
// for the repo matching the current repo basename (fallback first), printing only
// an absolute path. Empty manifest / relative / "." → prints nothing (exit 0).
func emitWorktree(st *ticket.Store) {
	mpath := st.ManifestPath()
	m, err := ticket.ReadManifest(mpath)
	if err != nil {
		return
	}
	current := filepath.Base(gitOut("rev-parse", "--show-toplevel"))
	var matched *ticket.Repo
	for i := range m.Repos {
		if m.Repos[i].Name == current {
			matched = &m.Repos[i]
			break
		}
	}
	if matched == nil && len(m.Repos) > 0 {
		matched = &m.Repos[0]
	}
	if matched == nil {
		return
	}
	wt := matched.Worktree
	if wt == "" || wt == "." {
		return
	}
	// product_root is always empty in single-repo mode, so a relative worktree
	// path cannot be resolved and prints nothing.
	if !filepath.IsAbs(wt) {
		return
	}
	fmt.Println(wt)
}

// firstLegacyHit mirrors _legacy_paths + the first-existing scan. Returns "" when
// no legacy entry exists or after the hard-fail date.
func firstLegacyHit(th, kind, skill string, latest bool, seq, slug string) string {
	if time.Now().Format("2006-01-02") > legacyHardfail() {
		return ""
	}
	var cands []string
	switch kind {
	case "plan":
		cands = append(cands, filepath.Join(th, "plan-feature", "plan.md"))
	case "verdict":
		if skill == "autoplan" {
			cands = append(cands, filepath.Join(th, "autoplan", "verdict.md"))
		}
	case "handoff":
		if latest {
			switch skill {
			case "build":
				cands = append(cands, filepath.Join(th, "build", "change-brief.md"))
			case "implement":
				cands = append(cands, filepath.Join(th, "implement", "change-brief.md"))
			}
		}
	case "sub-ticket":
		base := filepath.Join(th, "decompose", "sub-tickets")
		switch {
		case seq != "" && slug != "":
			cands = append(cands,
				filepath.Join(base, fmt.Sprintf("%03d-%s.md", atoiSeq(seq), slug)),
				filepath.Join(base, seq+"-"+slug+".md"))
		case seq != "":
			cands = append(cands, globList(filepath.Join(base, fmt.Sprintf("%03d-*.md", atoiSeq(seq))))...)
			cands = append(cands, globList(filepath.Join(base, seq+"-*.md"))...)
		case slug != "":
			cands = append(cands, globList(filepath.Join(base, "[0-9]*-"+slug+".md"))...)
		}
	}
	for _, p := range cands {
		if p != "" && pathExists(p) {
			return p
		}
	}
	return ""
}

func globList(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	sort.Strings(m)
	return m
}

func pathTelemetryAppend(env identity.Env, kind, event, canonical, legacy string) {
	dir := filepath.Join(babysitHome(), "analytics")
	caller := os.Getenv("SKILL_NAME")
	if caller == "" {
		caller = actorRole()
	}
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	line := fmt.Sprintf(`{"ts":"%s","event":"%s","kind":"%s","canonical":"%s","legacy":"%s","caller":"%s","ticket":"%s"}`+"\n",
		time.Now().UTC().Format("2006-01-02T15:04:05Z"), event, kind, canonical, legacy, caller, env.Ticket)
	f, err := os.OpenFile(filepath.Join(dir, "path-fallbacks.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// runList ports `list` (bbs-ticket.bash:3089-3142).
func runList(args []string) {
	kind := ""
	if len(args) > 0 {
		kind = args[0]
		args = args[1:]
	}
	if kind == "" {
		fmt.Fprintln(os.Stderr, retarget("bbs-ticket list: <kind> required (try: bbs-ticket path)"))
		os.Exit(2)
	}
	switch kind {
	case "handoff", "verdict", "review", "evidence", "sub-ticket":
	default:
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket list: %s: kind is not listable\n"), kind)
		os.Exit(2)
	}
	pskill := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--skill":
			pskill, i = valueAt(args, i), i+1
		default:
			fmt.Fprintf(os.Stderr, retarget("bbs-ticket list: %s: unknown selector '%s'\n"), kind, args[i])
			os.Exit(2)
		}
	}
	if pskill != "" {
		pskill = safePathComponent(kind, "skill", pskill)
	}

	env := identity.Resolve()
	needTicket(env)
	th := ticket.New(env).Home()

	var out []string
	switch kind {
	case "handoff":
		if pskill != "" {
			out = globList(filepath.Join(th, "handoffs", "[0-9][0-9][0-9]-"+pskill+"-*.md"))
		} else {
			out = globList(filepath.Join(th, "handoffs", "[0-9][0-9][0-9]-*.md"))
		}
	case "verdict":
		out = globList(filepath.Join(th, "verdicts", "*.md"))
	case "review":
		out = globList(filepath.Join(th, "reviews", "*.md"))
	case "evidence":
		root := filepath.Join(th, "evidence")
		if pskill != "" {
			root = filepath.Join(th, "evidence", pskill)
		}
		out = findFiles(root)
	case "sub-ticket":
		out = globList(filepath.Join(th, "sub-tickets", "[0-9][0-9][0-9]-*.md"))
	}
	for _, f := range out {
		if pathExists(f) {
			fmt.Println(f)
		}
	}
	os.Exit(0)
}

// findFiles mirrors `find <dir> -type f | sort` (regular files only).
func findFiles(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// The ladder, in order. Membership means two things: a rung reconcile can
// derive *into*, and a rung it will move a ticket *out of*.
//
// `in_progress` is never derived — nothing on disk distinguishes it — but it
// has to be listed, or a ticket somebody set to in_progress is read as
// terminal and freezes there, unreachable by the landing it later gets.
// `done` is here for the same reason in reverse: it is a target a landed
// ticket must be able to reach, and as a current status it simply never
// advances. Genuinely terminal states (`cancelled`, `duplicate`, `blocked`)
// stay out — those are a human's word about the ticket, not a rung.
var reconcileRank = map[string]int{
	"triage": 0, "backlog": 1, "planned": 2, "decomposed": 3,
	"in_progress": 4, "in_review": 5, "done": 6,
}

// runReconcile ports `reconcile` (bbs-ticket.bash:3144-3267): advance
// index.json.status forward along the filesystem-derived ladder, never
// downgrading and never touching terminal/explicit states.
func runReconcile(args []string) {
	all, dry, quiet, one := false, false, false, ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			all = true
		case "--dry-run":
			dry = true
		case "--quiet":
			quiet = true
		case "--ticket":
			one, i = valueAt(args, i), i+1
		default:
			fmt.Fprintf(os.Stderr, "reconcile: unknown arg '%s'\n", args[i])
			os.Exit(2)
		}
	}
	if all && one != "" {
		fmt.Fprintln(os.Stderr, "reconcile: --all and --ticket are mutually exclusive")
		os.Exit(2)
	}

	env := identity.Resolve()
	if all {
		tdir := filepath.Join(env.ProjectHome, "tickets")
		entries, err := os.ReadDir(tdir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reconcile: no tickets dir at %s\n", tdir)
			os.Exit(0)
		}
		rc := 0
		for _, e := range entries {
			if e.IsDir() {
				if err := reconcileOne(os.Stdout, env, e.Name(), dry, quiet); err != nil {
					rc = 1
				}
			}
		}
		os.Exit(rc)
	}
	if one != "" {
		if err := reconcileOne(os.Stdout, env, one, dry, quiet); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	needTicket(env)
	if err := reconcileOne(os.Stdout, env, env.Ticket, dry, quiet); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// reconcileOne advances one ticket's status. out receives the per-ticket log —
// `bbs ticket reconcile` sends it to stdout; the dashboard, whose stdout is its
// own report, sends it to stderr.
func reconcileOne(out io.Writer, env identity.Env, tid string, dry, quiet bool) error {
	stEnv := env
	stEnv.Ticket = tid
	st := ticket.New(stEnv)
	th := st.Home()
	idx := filepath.Join(th, "index.json")
	if !fileExists(idx) {
		if !quiet {
			fmt.Fprintf(out, "%s: skip (no index.json)\n", tid)
		}
		return nil
	}
	doc := ticket.ReadDoc(idx)
	cur := doc.Get("status")
	if cur == "" {
		cur = "triage"
	}
	// A paused or cancelled ticket is frozen where the human left it. Advancing
	// its status here would make the resume land on a rung the human never saw.
	if ctl := doc.Get("control.state"); ctl != "" {
		if !quiet {
			fmt.Fprintf(out, "%s: %s (skip — %s)\n", tid, cur, ctl)
		}
		return nil
	}
	target := reconcileTarget(th)

	curR, curKnown := reconcileRank[cur]
	if !curKnown {
		if !quiet {
			fmt.Fprintf(out, "%s: %s (skip — terminal/explicit)\n", tid, cur)
		}
		return nil
	}
	tgtR := reconcileRank[target]
	if tgtR <= curR {
		if !quiet {
			fmt.Fprintf(out, "%s: %s (no advancement; derived=%s)\n", tid, cur, target)
		}
		return nil
	}
	if dry {
		fmt.Fprintf(out, "%s: %s -> %s (dry-run)\n", tid, cur, target)
		return nil
	}
	if err := applyStatus(st, target); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s -> %s FAILED\n", tid, cur, target)
		return err
	}
	fmt.Fprintf(out, "%s: %s -> %s\n", tid, cur, target)
	return nil
}

// reconcileTarget derives the ladder rung from filesystem signals: branch
// merged into base → done, pushed manifest or a PR pointer → in_review, else
// manifest.md → decomposed, plan.md → planned, requirement.md → backlog, else
// triage.
//
// Every rung stays a *fact* — a file on disk, a pushed flag, a merge in git.
// The qa and review-pr verdicts are deliberately not read here (ticket-layout.md
// § Status enum): a verified ticket nobody closed out is not finished, and
// deriving from verdicts would hide exactly that gap.
func reconcileTarget(th string) string {
	has := func(name string) bool {
		info, err := os.Stat(filepath.Join(th, name))
		return err == nil && info.Size() > 0
	}
	if landed(th) {
		return "done"
	}
	if my, err := os.ReadFile(filepath.Join(th, "manifest.yaml")); err == nil {
		for _, ln := range strings.Split(string(my), "\n") {
			if strings.HasPrefix(ln, "    pushed:") {
				if strings.TrimSpace(strings.SplitN(ln, ":", 2)[1]) == "true" {
					return "in_review"
				}
			}
		}
	}
	// A PR pointer is the same rung as pushed: the change is out for review.
	// Whether that PR merged is a network question, and reconcile runs over
	// every ticket on every inbox tick — the merge shows up locally as
	// `landed` above, once the human pulls it.
	if idx := ticket.ReadIndex(filepath.Join(th, "index.json")); idx.Pointers.PR != "" {
		return "in_review"
	}
	switch {
	case has("manifest.md"):
		return "decomposed"
	case has("plan.md"):
		return "planned"
	case has("requirement.md"):
		return "backlog"
	default:
		return "triage"
	}
}

// applyStatus performs set-status's locked mutate + history append without
// exiting — used by reconcile in place of bash's `"$0" set-status` re-exec.
func applyStatus(st *ticket.Store, target string) error {
	if err := st.AcquireLock(); err != nil {
		return err
	}
	defer st.ReleaseLock()
	doc := loadForMutate(st)
	old := doc.Get("status")
	doc.Set("status", target)
	if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
		return err
	}
	st.HistoryAppendExtra("status_changed", actorRole(),
		fmt.Sprintf(`{"from":"%s","to":"%s"}`, old, target))
	return nil
}

// runAssertCwd ports assert-cwd (bbs-ticket.bash:3401-3407): a compatibility
// no-op since product mode was removed.
func runAssertCwd() { os.Exit(0) }

func printPathHelp() {
	fmt.Fprint(os.Stderr, retarget(`usage: bbs-ticket path <kind> [selectors] --read|--write
       bbs-ticket list <kind> [selectors]

Kinds (canonical Layout C path under <TH> = $BABYSIT_PROJECT_HOME/tickets/$TICKET):

  KIND          CANONICAL PATH                          SELECTORS              LIST
  home          <TH>/                                   —                      no
  index         <TH>/index.json                         —                      no
  requirement   <TH>/requirement.md                     —                      no
  design        <TH>/design.md                          —                      no
  plan          <TH>/plan.md                            —                      no
  manifest      <TH>/manifest.md                        —                      no
  handoff       <TH>/handoffs/<NNN>-<skill>-<status>.md --skill --latest --seq yes (--skill)
  verdict       <TH>/verdicts/<skill>.md                --skill (required)     yes
  review        <TH>/reviews/<skill>.md                 --skill (required)     yes
  history       <TH>/history.jsonl                      —                      no
  evidence      <TH>/evidence/<skill>/<name>            --skill (req) --name   yes (--skill)
  sub-ticket    <TH>/sub-tickets/<NNN>-<slug>.md        --seq --slug           yes
  checkpoint    <TH>/checkpoint.json                    —                      no
  worktree      <manifest.yaml repos[].worktree>        —                      no (--read only)

Mode:
  --read   walk canonical → legacy fallbacks; print first hit; exit 1 if none
  --write  print canonical (mkdir -p parent); rejected for handoff/verdict/review

Examples:
  bbs-ticket path plan --read
  PLAN_OUT="$(bbs-ticket path plan --write)" && cp draft.md "$PLAN_OUT"
  bbs-ticket path handoff --skill build --latest --read
  bbs-ticket list handoff --skill build
`))
}

func printPathKindHelp(kind string) {
	canonical := map[string]string{
		"home": "<TH>/", "index": "<TH>/index.json", "requirement": "<TH>/requirement.md",
		"design": "<TH>/design.md", "plan": "<TH>/plan.md", "manifest": "<TH>/manifest.md",
		"history": "<TH>/history.jsonl", "checkpoint": "<TH>/checkpoint.json",
		"worktree": "<manifest.yaml repos[].worktree>",
	}[kind]
	switch kind {
	case "home", "index", "requirement", "design", "plan", "manifest", "history", "checkpoint":
		fmt.Fprintf(os.Stderr, retarget(`usage: bbs-ticket path %s --read|--write

Canonical: %s
Selectors: (none)

Examples:
  bbs-ticket path %s --read
  bbs-ticket path %s --write
`), kind, canonical, kind, kind)
	case "worktree":
		fmt.Fprint(os.Stderr, retarget(`usage: bbs-ticket path worktree --read

Prints the ticket worktree path from manifest.yaml for the current repo.
(Matches the repo entry whose name == current repo basename; falls back to first.)
Empty if manifest.yaml does not exist — not an error.
Selectors: (none). --write is rejected.

Examples:
  WT="$(bbs-ticket path worktree --read)"
`))
	case "handoff":
		fmt.Fprint(os.Stderr, retarget(`usage: bbs-ticket path handoff [--skill S] [--latest | --seq N] --read

Canonical: <TH>/handoffs/<NNN>-<skill>-<status>.md
Selectors: --skill S, --latest, --seq N
Note: handoff --write is rejected — use `)+"`bbs-ticket add-handoff --skill S --status X`"+` instead.

Examples:
  bbs-ticket path handoff --skill build --latest --read
  bbs-ticket path handoff --seq 3 --read
`)
	case "verdict", "review":
		fmt.Fprintf(os.Stderr, retarget(`usage: bbs-ticket path %s --skill S --read

Canonical: <TH>/%ss/<skill>.md
Selectors: --skill S (required)
Note: %s --write is rejected — use `)+"`bbs-ticket set-%s --skill S`"+` instead.

Examples:
  bbs-ticket path %s --skill autoplan --read
  bbs-ticket list %s
`, kind, kind, kind, kind, kind, kind)
	case "evidence":
		fmt.Fprint(os.Stderr, retarget(`usage: bbs-ticket path evidence --skill S [--name N] --read|--write

Canonical: <TH>/evidence/<skill>/<name>
Selectors: --skill S (required), --name N (required for --write)

Examples:
  EVD="$(bbs-ticket path evidence --skill quality --name summary.md --write)"
  bbs-ticket path evidence --skill quality --name summary.md --read
  bbs-ticket list evidence --skill quality
`))
	case "sub-ticket":
		fmt.Fprint(os.Stderr, retarget(`usage: bbs-ticket path sub-ticket --seq N --slug S --read|--write

Canonical: <TH>/sub-tickets/<NNN>-<slug>.md
Selectors: --seq N (numeric), --slug S

Examples:
  ST="$(bbs-ticket path sub-ticket --seq 1 --slug user-flow --write)"
  bbs-ticket path sub-ticket --seq 1 --slug user-flow --read
  bbs-ticket list sub-ticket
`))
	default:
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: unknown kind (try: bbs-ticket path)\n"), kind)
	}
}

// landed reports whether the ticket's work is already merged into its base in
// every repo the manifest names — the top rung of the ladder, and the signal
// that a run actually crossed its finish handler (`bbs ticket land`, or a merge
// commit the human pulled) rather than stopping at QA-ready.
//
// Every unknown answers false. No manifest, a repo whose checkout is gone, a
// git that failed: reconcile only advances, so a missed landing costs one tick
// and is picked up by the next, while a wrong one strands a live ticket at a
// terminal rung that reconcile will never move again. That asymmetry is why the
// test is "base merged this exact commit" and not "base can reach it" — see
// git.MergedTips for the vacuous cases the loose form reports as finished.
//
// A branch equal to its base (trunk mode) is skipped outright: the work went
// straight onto base, so there is no merge to find and never will be.
func landed(th string) bool {
	m, err := ticket.ReadManifest(filepath.Join(th, "manifest.yaml"))
	if err != nil || m.Version != "1" || len(m.Repos) == 0 {
		return false
	}
	for _, r := range m.Repos {
		if r.Branch == "" || r.Base == "" || r.Branch == r.Base {
			return false
		}
		// The worktree is the only absolute path a manifest row carries
		// (`canonical` is the marker "."), and every worktree of a repo shares
		// its refs. Once it is cleaned up, fall back to the process cwd — which
		// is the repo whenever this runs from a board or inbox tick, and simply
		// fails to resolve the branch when it isn't.
		dir := r.Worktree
		if dir != "" {
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				dir = ""
			}
		}
		tip := git.RevParseIn(dir, r.Branch)
		if tip == "" || !mergedTips(dir, r.Base)[tip] {
			return false
		}
	}
	return true
}

// mergedTips memoizes one `git rev-list --merges` per (dir, base). reconcile
// runs over every ticket in a project, so without this a 135-ticket board would
// fork git 135 times for the same answer. A nil result is cached too: a repo
// that failed once fails the same way for the rest of the sweep.
//
// Locked, and expiring: reconcileProjects runs inside the dashboard's snapshot
// handler, so two concurrent polls touch this map on different goroutines. The
// TTL is what keeps that server honest — a CLI process is gone long before it
// matters, but a dashboard left running for a day would otherwise answer every
// snapshot from the git it read at boot, and never notice a branch landing.
const mergedTTL = 5 * time.Second

var (
	mergedMu    sync.Mutex
	mergedCache = map[string]mergedEntry{}
)

type mergedEntry struct {
	tips map[string]bool
	at   time.Time
}

func mergedTips(dir, base string) map[string]bool {
	key := dir + "\x00" + base
	mergedMu.Lock()
	defer mergedMu.Unlock()
	if e, ok := mergedCache[key]; ok && time.Since(e.at) < mergedTTL {
		return e.tips
	}
	tips := git.MergedTips(dir, base)
	mergedCache[key] = mergedEntry{tips: tips, at: time.Now()}
	return tips
}
