package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/dashboard"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/spf13/cobra"
)

// newDashboardCmd ports bin/bbs-dashboard: snapshot ~/.babysit state into
// web/dist/data.js (as `window.__BBS_DATA__ = {...};`) and open the dashboard.
// The snapshot composition is native Go (internal/dashboard); npm build, vite
// dev, and browser-open still shell out.
func newDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "dashboard",
		Short:              "snapshot babysit state into the web dashboard",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runDashboard(args)
		},
	}
}

const dashUsage = `bbs-dashboard — snapshot babysit state into web/dist/data.js and open the dashboard.

Usage:
  bbs-dashboard                  Walk every project under ~/.babysit/projects/ (v2 default).
  bbs-dashboard build            Run ` + "`npm install && npm run build`" + ` in web/.
  bbs-dashboard --slug <name>    DEPRECATED: kept for one release; cross-project view is now default.
                                 Filter the snapshot to a single project by slug.
  bbs-dashboard --no-open        Snapshot only -- don't open browser (for CI/tests).
  bbs-dashboard --dev            Dev mode: snapshot to web/public/data.js, run vite dev on :5173
                                 with HMR, open browser. Sibling of the default file:// path.
  bbs-dashboard --help           Print usage.

Env overrides:
  BABYSIT_STATE_DIR              Default: ~/.babysit
  BABYSIT_DASHBOARD_REPO         Default: dirname of this script's repo root
  BBS_DASHBOARD_MAX_BYTES        Default: 5000000. If data.js exceeds this, caps are
                                 tightened to 1000/100 and one retry is attempted.
`

func dashErr(msg string) { fmt.Fprintf(os.Stderr, retarget("bbs-dashboard: %s\n"), msg) }

func runDashboard(args []string) error {
	// ── flag parse ──
	subcmd, slugOverride := "", ""
	open, dev := true, false
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Print(retarget(dashUsage))
			return nil
		case a == "--no-open":
			open = false
		case a == "--dev":
			dev = true
		case a == "--slug":
			if i+1 >= len(args) {
				dashErr("--slug requires a name")
				os.Exit(1)
			}
			slugOverride = args[i+1]
			i++
		case strings.HasPrefix(a, "--slug="):
			slugOverride = strings.TrimPrefix(a, "--slug=")
		case a == "build":
			subcmd = "build"
		case a == "--":
			i = len(args)
			continue
		case strings.HasPrefix(a, "-"):
			dashErr("unknown flag: " + a)
			os.Exit(1)
		default:
			dashErr("unknown argument: " + a)
			os.Exit(1)
		}
		i++
	}

	// ── resolve paths ──
	exe, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	scriptDir := filepath.Dir(exe)
	repoRoot := os.Getenv("BABYSIT_DASHBOARD_REPO")
	if repoRoot == "" {
		repoRoot = filepath.Dir(scriptDir)
	}
	stateDir := os.Getenv("BABYSIT_STATE_DIR")
	if stateDir == "" {
		stateDir = filepath.Join(os.Getenv("HOME"), ".babysit")
	}
	webDir := filepath.Join(repoRoot, "web")
	distDir := filepath.Join(webDir, "dist")
	maxBytes := 5000000
	if v := os.Getenv("BBS_DASHBOARD_MAX_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxBytes = n
		}
	}

	// ── subcommand: build ──
	if subcmd == "build" {
		if _, err := os.Stat(webDir); err != nil {
			dashErr("web/ not found at " + webDir)
			os.Exit(2)
		}
		if _, err := exec.LookPath("npm"); err != nil {
			dashErr("npm not found; install Node.js and retry")
			os.Exit(2)
		}
		if err := runIn(webDir, "npm", "install", "--silent"); err != nil {
			return errSilent
		}
		if err := runIn(webDir, "npm", "run", "build"); err != nil {
			return errSilent
		}
		return nil
	}

	// ── validate --slug override (deprecated) ──
	if slugOverride != "" {
		dashErr("--slug is DEPRECATED: kept for one release; cross-project view is now default")
		if badDashSlug(slugOverride) {
			dashErr("invalid slug '" + slugOverride + "' (allowed: a-z A-Z 0-9 _ . -, no slashes)")
			os.Exit(1)
		}
		if fi, err := os.Stat(filepath.Join(stateDir, "projects", slugOverride)); err != nil || !fi.IsDir() {
			dashErr("no babysit state for slug '" + slugOverride + "' at " + filepath.Join(stateDir, "projects", slugOverride))
			os.Exit(1)
		}
	}

	// ── reconcile statuses before snapshotting ──
	if os.Getenv("BBS_DASHBOARD_NO_RECONCILE") == "" {
		reconcileProjects(stateDir)
	}

	version := "unknown"
	if b, err := os.ReadFile(filepath.Join(repoRoot, "VERSION")); err == nil {
		version = strings.TrimSpace(string(b))
	}
	snapshotAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	compose := func(decisionsCap, skillEventsCap int) dashboard.Options {
		return dashboard.Options{
			StateDir:       stateDir,
			Version:        version,
			SnapshotAt:     snapshotAt,
			SlugOverride:   slugOverride,
			DecisionsCap:   decisionsCap,
			SkillEventsCap: skillEventsCap,
			Warn:           dashErr,
		}
	}

	// ── choose destination ──
	var dataJS string
	if dev {
		if err := os.MkdirAll(filepath.Join(webDir, "public"), 0o755); err != nil {
			dashErr("cannot create " + filepath.Join(webDir, "public"))
			os.Exit(2)
		}
		dataJS = filepath.Join(webDir, "public", "data.js")
	} else {
		if err := os.MkdirAll(distDir, 0o755); err != nil {
			dashErr("cannot create " + distDir)
			os.Exit(2)
		}
		dataJS = filepath.Join(distDir, "data.js")
	}
	tmp := fmt.Sprintf("%s.tmp.%d", dataJS, os.Getpid())

	snapshot := dashboard.Compose(compose(2000, 2000))
	size, err := writeEscapedJSON(snapshot, tmp)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if size > maxBytes {
		snapshot = dashboard.Compose(compose(1000, 100))
		markForced(snapshot)
		size, err = writeEscapedJSON(snapshot, tmp)
		if err != nil {
			os.Remove(tmp)
			return err
		}
		if size > maxBytes {
			dashErr(fmt.Sprintf("WARNING: data.js is %d bytes (over BBS_DASHBOARD_MAX_BYTES=%d); writing anyway (forced)", size, maxBytes))
		}
	}
	if err := os.Rename(tmp, dataJS); err != nil {
		os.Remove(tmp)
		return err
	}

	if dev {
		fmt.Printf(retarget("bbs-dashboard: wrote %s\n"), dataJS)
	} else {
		fmt.Printf(retarget("bbs-dashboard: wrote %s; open file://%s/index.html\n"), dataJS, distDir)
	}

	// ── dev mode: vite ──
	if dev {
		return runDevServer(webDir, size, open)
	}

	// ── production / file:// open ──
	if open {
		if _, err := os.Stat(filepath.Join(distDir, "index.html")); err != nil {
			dashErr("web/dist/ missing; run: bbs-dashboard build")
			os.Exit(1)
		}
		openBrowser("file://" + filepath.Join(distDir, "index.html"))
	}
	return nil
}

var dashSlugBad = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

func badDashSlug(s string) bool {
	return s == "" || s == "." || s == ".." || strings.Contains(s, "/") || dashSlugBad.MatchString(s)
}

// reconcileProjects advances every project's ticket statuses before composing a
// snapshot, so the dashboard never shows a rung the filesystem has already left
// behind. It calls reconcileOne directly — both live in package cmd, so the old
// fork/exec of a sibling `bbs-ticket` bought nothing but a missing-binary failure
// mode and one process per project. Logs go to stderr; stdout is the report.
func reconcileProjects(stateDir string) {
	projects := filepath.Join(stateDir, "projects")
	entries, err := os.ReadDir(projects)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projDir := filepath.Join(projects, e.Name())
		ticketsDir := filepath.Join(projDir, "tickets")
		tickets, err := os.ReadDir(ticketsDir)
		if err != nil {
			continue
		}
		env := identity.Env{ProjectHome: projDir}
		for _, t := range tickets {
			if t.IsDir() {
				_ = reconcileOne(os.Stderr, env, t.Name(), false, true)
			}
		}
	}
}

// writeEscapedJSON marshals the snapshot compactly (no HTML entity escaping),
// applies the JS-embedding escapes (</ → <\/, U+2028/U+2029 → \uXXXX), and
// writes `window.__BBS_DATA__ = <json>;\n`. Returns the byte count.
func writeEscapedJSON(snapshot interface{}, dest string) (int, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(snapshot); err != nil {
		return 0, err
	}
	// Go's json encoder already escapes U+2028/U+2029 to \uXXXX (always, even
	// with SetEscapeHTML(false)). Only the </ → <\/ script-break guard remains.
	compact := strings.TrimRight(buf.String(), "\n")
	compact = strings.ReplaceAll(compact, "</", `<\/`)
	out := "window.__BBS_DATA__ = " + compact + ";\n"
	if err := os.WriteFile(dest, []byte(out), 0o644); err != nil {
		return 0, err
	}
	return len(out), nil
}

// markForced adds {forced: true} to each meta.truncations entry.
func markForced(snapshot map[string]interface{}) {
	meta, _ := snapshot["meta"].(map[string]interface{})
	if meta == nil {
		return
	}
	tr, _ := meta["truncations"].([]interface{})
	for _, t := range tr {
		if m, ok := t.(map[string]interface{}); ok {
			m["forced"] = true
		}
	}
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runDevServer(webDir string, size int, open bool) error {
	if _, err := exec.LookPath("npm"); err != nil {
		dashErr("npm not found; install Node.js and retry")
		os.Exit(2)
	}
	if size > 2000000 {
		dashErr(fmt.Sprintf("WARN: web/public/data.js is %d bytes (>2MB); first paint may be slow", size))
	}
	if _, err := os.Stat(filepath.Join(webDir, "node_modules")); err != nil {
		fmt.Println(retarget("bbs-dashboard: installing web/ dependencies (first run)..."))
		_ = runIn(webDir, "npm", "install")
	}
	// vite reuse/port detection is a convenience the bash did with lsof; the
	// simple faithful behavior is to hand off to `npm run dev`, which itself
	// picks the next free port when :5173 is taken.
	cmd := exec.Command("npm", "run", "dev")
	if open {
		cmd = exec.Command("npm", "run", "dev", "--", "--open")
	}
	cmd.Dir = webDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return errSilent
	}
	return nil
}

func openBrowser(target string) {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("open"); err == nil {
		cmd = exec.Command("open", target)
	} else if _, err := exec.LookPath("xdg-open"); err == nil {
		cmd = exec.Command("xdg-open", target)
	} else {
		fmt.Printf("no `open` or `xdg-open`; visit: %s\n", target)
		return
	}
	if err := cmd.Run(); err != nil {
		dashErr("open failed; visit " + target)
	}
}
