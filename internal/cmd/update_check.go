package cmd

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/spf13/cobra"
)

const defaultRemoteURL = "https://raw.githubusercontent.com/reallongnguyen/babysit/main/VERSION"

// versionRe rejects non-version remote responses (HTML error pages, empty
// bodies) — bin/bbs-update-check:118. The body is space-stripped first, so it
// is always a single line and ^/$ need no multiline flag.
var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9.]+$`)

// newUpdateCheckCmd ports bin/bbs-update-check as `bbs update-check`, matching
// its stdout, stderr and exit codes exactly. Every path exits 0.
//
// DisableFlagParsing: the bash inspects only "$1" and ignores every other
// argument. Cobra's parser would reject unknown flags and would honor --force
// in any position, so it is switched off (same rationale as `bbs env`).
func newUpdateCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "update-check",
		Short:              "periodic version check",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			runUpdateCheck(args)
			return nil
		},
	}
}

func runUpdateCheck(args []string) {
	stateDir := config.Dir()
	cacheFile := filepath.Join(stateDir, "last-update-check")
	markerFile := filepath.Join(stateDir, "just-upgraded-from")
	snoozeFile := filepath.Join(stateDir, "update-snoozed")
	versionFile := filepath.Join(babysitDir(), "VERSION")

	remoteURL := os.Getenv("BABYSIT_REMOTE_URL")
	if remoteURL == "" {
		remoteURL = defaultRemoteURL
	}

	// ─── Force flag (busts cache + snooze) ───────────────────────
	if len(args) > 0 && args[0] == "--force" {
		os.Remove(cacheFile)
		os.Remove(snoozeFile)
	}

	// ─── Step 0: Check if updates are disabled ────────────────────
	// The bash execs "$BABYSIT_DIR/bin/bbs-config get update_check"; reading
	// the config natively is the documented divergence (see the header of
	// tests/test_bbs_update_check.sh).
	if v, _ := config.Get("update_check"); v == "false" {
		return
	}

	// ─── Step 1: Read local version ──────────────────────────────
	local := ""
	if isRegularFile(versionFile) {
		local = stripSpace(readFile(versionFile))
	}
	if local == "" {
		return
	}

	// ─── Step 2: Check "just upgraded" marker ────────────────────
	// Deliberately falls through: a JUST_UPGRADED line and a cached
	// UPGRADE_AVAILABLE line can both print in one run (bin/bbs-update-check:70).
	if isRegularFile(markerFile) {
		old := stripSpace(readFile(markerFile))
		os.Remove(markerFile)
		os.Remove(snoozeFile)
		if old != "" {
			fmt.Printf("JUST_UPGRADED %s %s\n", old, local)
		}
	}

	// ─── Step 3: Check cache freshness ───────────────────────────
	if isRegularFile(cacheFile) {
		// $(cat …) strips trailing newlines, nothing else.
		cached := strings.TrimRight(readFile(cacheFile), "\n")
		ttl := 0
		switch {
		case strings.HasPrefix(cached, "UP_TO_DATE"):
			ttl = 60
		case strings.HasPrefix(cached, "UPGRADE_AVAILABLE"):
			ttl = 720
		case strings.HasPrefix(cached, "CHECK_FAILED"):
			ttl = 15
		}

		if !staleByMmin(cacheFile, ttl) && ttl > 0 {
			switch {
			case strings.HasPrefix(cached, "UP_TO_DATE"):
				if awkField(cached, 2) == local {
					return
				}
			case strings.HasPrefix(cached, "UPGRADE_AVAILABLE"):
				if awkField(cached, 2) == local {
					if checkSnooze(snoozeFile, awkField(cached, 3)) {
						return
					}
					fmt.Println(cached)
					return
				}
			}
		}
	}

	// ─── Step 4: Slow path — fetch remote version ────────────────
	os.MkdirAll(stateDir, 0o755)

	body, err := fetchRemote(remoteURL)
	if err != nil {
		// Network failure: short TTL so we retry soon but don't hammer.
		writeCache(cacheFile, "CHECK_FAILED "+local)
		return
	}
	remote := stripSpace(body)
	if !versionRe.MatchString(remote) {
		writeCache(cacheFile, "CHECK_FAILED "+local)
		return
	}
	if local == remote {
		writeCache(cacheFile, "UP_TO_DATE "+local)
		return
	}

	line := "UPGRADE_AVAILABLE " + local + " " + remote
	writeCache(cacheFile, line)
	if checkSnooze(snoozeFile, remote) {
		return
	}
	fmt.Println(line)
}

// babysitDir ports bin/bbs-update-check:15 — `cd "$(dirname "$0")/.." && pwd`.
//
// Note it uses $0 *without* readlink -f, unlike bin/bbs-env (whose Go port
// resolves the symlink chain in internal/env.projectRoot). Reached through the
// ~/.claude/bbs-update-check shim that makes BABYSIT_DIR resolve to $HOME, so
// $HOME/VERSION is missing and the check silently exits 0. That is a latent bug
// in the bash, tracked as a follow-up; this port reproduces it exactly, so it
// must use os.Args[0] and must NOT call os.Executable/EvalSymlinks.
func babysitDir() string {
	if d := os.Getenv("BABYSIT_DIR"); d != "" {
		return d
	}
	// bash's `cd X/.. && pwd` is a lexical (logical) walk, so Join+Abs match it.
	parent := filepath.Join(filepath.Dir(os.Args[0]), "..")
	abs, err := filepath.Abs(parent)
	if err != nil {
		return parent
	}
	return abs
}

// checkSnooze ports check_snooze (bin/bbs-update-check:36-62).
// Snooze file format: "<version> <level> <epoch>"; level 1=24h, 2=48h, 3+=7d.
// A missing field, a non-numeric level/epoch, or a version mismatch (i.e. a new
// remote version) means "not snoozed".
func checkSnooze(snoozeFile, remoteVer string) bool {
	if !isRegularFile(snoozeFile) {
		return false
	}
	// awk 'NR==1 {print $N; exit}' — fields of the first line only.
	first := readFile(snoozeFile)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	f := splitFields(first)
	if len(f) < 3 {
		return false
	}
	sv, sl, se := f[0], f[1], f[2]
	if !allDigits(sl) || !allDigits(se) {
		return false
	}
	if sv != remoteVer {
		return false
	}

	var duration int64
	switch sl {
	case "1":
		duration = 86400
	case "2":
		duration = 172800
	default:
		duration = 604800
	}

	epoch, err := strconv.ParseInt(se, 10, 64)
	if err != nil {
		// Only reachable on an epoch too large for int64, where the bash's
		// arithmetic would overflow into an equally arbitrary answer.
		return false
	}
	return time.Now().Unix() < epoch+duration
}

// staleByMmin ports `find "$CACHE_FILE" -mmin +N` (bin/bbs-update-check:87).
// BSD find rounds the age up to the next full minute before comparing, so a
// 61-second-old file is 2 minutes to `-mmin`.
func staleByMmin(path string, ttlMin int) bool {
	fi, err := os.Stat(path)
	if err != nil {
		// find printed nothing → STALE empty → treated as fresh.
		return false
	}
	mins := int(math.Ceil(time.Since(fi.ModTime()).Seconds() / 60))
	return mins > ttlMin
}

func fetchRemote(url string) (string, error) {
	// curl -sf --max-time 5: a total deadline, HTTP >= 400 is a failure, and
	// redirects are NOT followed (no -L), so mirror all three.
	c := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeCache(path, line string) {
	os.WriteFile(path, []byte(line+"\n"), 0o644)
}

// readFile returns the file's contents, or "" on any error — mirroring the
// bash's `cat … 2>/dev/null || true`.
func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// isRegularFile mirrors `[ -f "$path" ]`: follows symlinks, false for a dir.
func isRegularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// stripSpace mirrors `tr -d '[:space:]'`, which deletes the six ASCII
// whitespace bytes *anywhere* in the input, not just at the ends. Notably it
// is not unicode-aware, so a NBSP survives.
func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r':
			return -1
		}
		return r
	}, s)
}

// splitFields mirrors awk's default field splitting on a single record: runs of
// spaces and tabs only. strings.Fields would also split on \r and unicode
// spaces, which would make a CRLF snooze file parse as valid where awk sees a
// trailing \r and rejects it.
func splitFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' })
}

// awkField mirrors `echo "$s" | awk '{print $n}'` inside a command
// substitution: one output line per input line holding that line's n-th field
// (empty when absent), with trailing newlines stripped.
func awkField(s string, n int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, len(lines))
	for i, ln := range lines {
		if f := splitFields(ln); n <= len(f) {
			out[i] = f[n-1]
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// allDigits mirrors `case "$x" in *[!0-9]*) …` — an empty string has no
// non-digit and therefore passes.
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
