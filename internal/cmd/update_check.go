package cmd

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
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
// its stdout and exit codes exactly. Every path exits 0 except an fs write
// failure, which the bash's `set -e` turns into an exit 1 (see runUpdateCheck).
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
			if err := runUpdateCheck(args); err != nil {
				// The bash has no error path of its own here: set -e aborts on
				// the failing mkdir/rm/redirect and the message on stderr is
				// whatever mkdir/rm/bash emitted. Only the exit code and the
				// (absence of) stdout are contractual, so the wording differs.
				fmt.Fprintln(os.Stderr, "bbs update-check: "+err.Error())
				return errSilent
			}
			return nil
		},
	}
}

// runUpdateCheck returns a non-nil error only where the bash would have died on
// `set -e` — a failing `rm -f`, `mkdir -p`, or `> "$CACHE_FILE"` redirect.
// Everything else is silent-and-exit-0, as in the original.
func runUpdateCheck(args []string) error {
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
		if err := rmF(cacheFile); err != nil {
			return err
		}
		if err := rmF(snoozeFile); err != nil {
			return err
		}
	}

	// ─── Step 0: Check if updates are disabled ────────────────────
	// The bash execs "$BABYSIT_DIR/bin/bbs-config get update_check"; reading
	// the config natively is the documented divergence (see the header of
	// tests/test_bbs_update_check.sh).
	if v, _ := config.Get("update_check"); v == "false" {
		return nil
	}

	// ─── Step 1: Read local version ──────────────────────────────
	local := ""
	if isRegularFile(versionFile) {
		local = stripSpace(readFile(versionFile))
	}
	if local == "" {
		return nil
	}

	// ─── Step 2: Check "just upgraded" marker ────────────────────
	// Deliberately falls through: a JUST_UPGRADED line and a cached
	// UPGRADE_AVAILABLE line can both print in one run (bin/bbs-update-check:70).
	if isRegularFile(markerFile) {
		old := stripSpace(readFile(markerFile))
		if err := rmF(markerFile); err != nil {
			return err
		}
		if err := rmF(snoozeFile); err != nil {
			return err
		}
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
					return nil
				}
			case strings.HasPrefix(cached, "UPGRADE_AVAILABLE"):
				if awkField(cached, 2) == local {
					if checkSnooze(snoozeFile, awkField(cached, 3)) {
						return nil
					}
					fmt.Println(cached)
					return nil
				}
			}
		}
	}

	// ─── Step 4: Slow path — fetch remote version ────────────────
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}

	body, err := fetchRemote(remoteURL)
	if err != nil {
		// Network failure: short TTL so we retry soon but don't hammer.
		return writeCache(cacheFile, "CHECK_FAILED "+local)
	}
	remote := stripSpace(body)
	if !versionRe.MatchString(remote) {
		return writeCache(cacheFile, "CHECK_FAILED "+local)
	}
	if local == remote {
		return writeCache(cacheFile, "UP_TO_DATE "+local)
	}

	line := "UPGRADE_AVAILABLE " + local + " " + remote
	if err := writeCache(cacheFile, line); err != nil {
		return err
	}
	if checkSnooze(snoozeFile, remote) {
		return nil
	}
	fmt.Println(line)
	return nil
}

// rmF mirrors `rm -f`: an absent file is success, any other failure is fatal
// under set -e.
func rmF(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// babysitDir ports bin/bbs-update-check:15 — `cd "$(dirname "$0")/.." && pwd`.
//
// Note it uses $0 *without* readlink -f, unlike bin/bbs-env (whose Go port
// resolves the symlink chain in internal/env.projectRoot). Reached through the
// ~/.claude/bbs-update-check shim that makes BABYSIT_DIR resolve to $HOME, so
// $HOME/VERSION is missing and the check silently exits 0. That is a latent bug
// in the bash, tracked as a follow-up; this port reproduces it exactly, so it
// must NOT call os.Executable/EvalSymlinks (either would resolve the shim and
// "fix" the bug).
//
// Reproducing it does take one step the bash gets for free. A script's $0 is
// never bare: the kernel hands the interpreter the path execve resolved, so a
// PATH-found script sees $0=/path/to/bbs-update-check. A *binary* gets argv[0]
// verbatim, so the same PATH invocation leaves os.Args[0]="bbs-update-check" —
// and Dir()+".." would then resolve against the caller's cwd, deriving a
// BABYSIT_DIR that changes with wherever the user happened to cd. LookPath
// redoes the shell's PATH search to recover the path bash would have seen. It
// does not resolve symlinks, so the shim bug survives intact.
func babysitDir() string {
	if d := os.Getenv("BABYSIT_DIR"); d != "" {
		return d
	}
	argv0 := os.Args[0]
	if !strings.ContainsRune(argv0, filepath.Separator) {
		if p, err := exec.LookPath(argv0); err == nil {
			argv0 = p
		}
	}
	// bash's `cd X/.. && pwd` is a lexical (logical) walk, so Join+Abs match it.
	parent := filepath.Join(filepath.Dir(argv0), "..")
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
		// Divergence on an epoch too large for int64 (deliberate): bash's
		// $(( se + duration )) silently wraps, so it may or may not suppress the
		// notice depending on where the wrap lands. That is C overflow, not a
		// contract; "not snoozed" is the useful answer. Only reachable by
		// hand-editing the snooze file to a >19-digit epoch.
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
	//
	// BABYSIT_REMOTE_URL is a documented override, and curl serves file:// URLs
	// while net/http alone does not — without this a file:// override would read
	// as a network failure where the bash succeeds. A missing path becomes a 404
	// and so fails like curl -f does.
	t := &http.Transport{}
	t.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))
	c := &http.Client{
		Transport: t,
		Timeout:   5 * time.Second,
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

// writeCache mirrors `echo "$line" > "$CACHE_FILE"`, whose redirect failure is
// fatal under set -e.
func writeCache(path, line string) error {
	return os.WriteFile(path, []byte(line+"\n"), 0o644)
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
// whitespace bytes *anywhere* in the input, not just at the ends.
//
// Byte-wise on purpose: tr is not unicode-aware (a NBSP survives), and
// strings.Map would decode, so it rewrites every invalid-UTF-8 byte to U+FFFD
// and would corrupt a VERSION or marker file the bash passes through untouched.
//
// Divergence on invalid UTF-8 (deliberate): BSD tr under a UTF-8 locale exits
// "Illegal byte sequence", which pipefail+set -e turn into a silent exit 1;
// under LC_ALL=C the same tr passes the bytes through. This matches the C
// locale — reproducing an exit code that depends on the caller's $LANG is not a
// contract worth having. Only reachable via a corrupt VERSION/marker/remote.
func stripSpace(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\v', '\f', '\r':
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
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
