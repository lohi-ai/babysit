// Package telemetry ports bin/bbs-telemetry-log: it appends one JSONL event
// to the local analytics log.
//
// The port is deliberately bug-for-bug faithful to the bash. Several
// behaviors below are defects rather than intent; they are reproduced (and
// labelled BUG) because skills and the dashboard already consume the current
// output, and fixing them is a separate ticket. Do not "clean up" a BUG
// comment without one.
package telemetry

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/config"
)

// maxFieldBytes mirrors `head -c 200` in the bash json_safe: a byte budget,
// not a rune budget, so a multi-byte rune can be split mid-sequence.
const maxFieldBytes = 200

// staleSessionWindow mirrors `find -mmin -120`.
const staleSessionWindow = 120 * time.Minute

// Event is one parsed invocation. Field values are raw as parsed; jsonSafe is
// applied at render time, matching where the bash applies it.
type Event struct {
	Skill        string
	Duration     string
	Outcome      string
	UsedBrowse   string
	SessionID    string
	ErrorClass   string
	ErrorMessage string
	FailedStep   string
	EventType    string
	Invoker      string
}

// Dirs resolves the paths the run works against.
type Dirs struct {
	Babysit   string // BABYSIT_DIR — repo root, per the bash's dirname "$0"/..
	State     string // BABYSIT_STATE_DIR — default $HOME/.babysit
	Analytics string
	JSONL     string
}

// ResolveDirs mirrors the bash's header assignments.
//
// BABYSIT_DIR is `$(cd "$(dirname "$0")/.." && pwd)` — note the bash resolves
// no symlinks here (unlike bin/bbs-env, which used `readlink -f`). The
// installed entrypoint is a symlink at ~/.claude/bbs-telemetry-log, so in a
// real run this lands on $HOME, not the repo. That is load-bearing for BUG 1
// and for babysit_version being "unknown" in production rows; resolving the
// symlink here would silently change both.
//
// `cd`+`pwd` is logical (-L), so ".." is applied lexically — which is exactly
// what filepath.Abs's Clean does.
func ResolveDirs(argv0 string) Dirs {
	babysit := os.Getenv("BABYSIT_DIR")
	if babysit == "" {
		abs, err := filepath.Abs(argv0)
		if err == nil {
			babysit = filepath.Dir(filepath.Dir(abs))
		}
	}

	state := os.Getenv("BABYSIT_STATE_DIR")
	if state == "" {
		// The bash uses $HOME directly; read it directly too, rather than
		// os.UserHomeDir, so a pinned HOME behaves identically.
		state = filepath.Join(os.Getenv("HOME"), ".babysit")
	}

	analytics := filepath.Join(state, "analytics")
	return Dirs{
		Babysit:   babysit,
		State:     state,
		Analytics: analytics,
		JSONL:     filepath.Join(analytics, "skill-usage.jsonl"),
	}
}

// Tier reports the configured telemetry tier: "off" or "local".
//
// BUG 1 (replicated): the bash reads this by shelling out to
// $BABYSIT_DIR/bin/bbs-config. When that path is missing or not executable
// the command simply fails, and `|| true` collapses the tier to "local" — so
// a user's `telemetry: off` is silently ignored. Because BABYSIT_DIR resolves
// to $HOME in a real install (see ResolveDirs), this is the *production*
// path: `off` has never actually taken effect there.
//
// The config file itself is BABYSIT_STATE_DIR-based, so internal/config reads
// the same file bbs-config would — but only behind the same gate, or the port
// would honor `off` where the bash does not.
func Tier(d Dirs) string {
	const fallback = "local"
	if !isExecutable(filepath.Join(d.Babysit, "bin", "bbs-config")) {
		return fallback
	}
	v, ok := config.Get("telemetry")
	if !ok {
		return fallback
	}
	switch v {
	case "off", "local":
		return v
	default:
		return fallback
	}
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path) // follows symlinks: a broken link fails, as exec would
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode().Perm()&0o111 != 0
}

// jsonSafe ports the bash helper:
//
//	printf '%s' "$1" | tr -d '"\\' | tr -d '[:cntrl:]' | head -c 200
//
// Byte-oriented throughout. Note it *deletes* quotes and backslashes rather
// than escaping them, which is how the bash gets away with printf-assembled
// JSON.
func jsonSafe(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
		case c < 0x20 || c == 0x7f: // [:cntrl:]
		default:
			b = append(b, c)
		}
	}
	if len(b) > maxFieldBytes {
		b = b[:maxFieldBytes]
	}
	return string(b)
}

// Version reads $BABYSIT_DIR/VERSION with all whitespace stripped.
//
// The bash is `$(cat "$VERSION_FILE" 2>/dev/null | tr -d '[:space:]' || echo "unknown")`.
// The `||` fires on the *pipeline* status, which only reports cat's failure
// because `set -o pipefail` is on — so an unreadable file yields "unknown",
// while a readable-but-empty file yields "" (not "unknown").
func Version(d Dirs) string {
	b, err := os.ReadFile(filepath.Join(d.Babysit, "VERSION"))
	if err != nil {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r':
			return -1
		}
		return r
	}, string(b))
}

// Sessions ports:
//
//	find "$STATE_DIR/sessions" -mmin -120 -type f | wc -l
//	[ -n "$_SC" ] && [ "$_SC" -gt 0 ] && SESSIONS="$_SC"
//
// Regular files only, recursive, symlinks not followed, and the count is used
// only when positive — otherwise the default "1" stands.
func Sessions(d Dirs) string {
	const fallback = "1"
	dir := filepath.Join(d.State, "sessions")
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fallback
	}

	cutoff := time.Now().Add(-staleSessionWindow)
	count := 0
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil //nolint:nilerr // find skips what it cannot read
		}
		// find -type f tests the link itself, so a symlink is not a match.
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().After(cutoff) {
			count++
		}
		return nil
	})

	if count > 0 {
		return strconv.Itoa(count)
	}
	return fallback
}

// ── Temporary parity shim ────────────────────────────────────────────────
// uname/git are exec'd rather than reimplemented, matching the bash exactly.
//
// This is deliberate, not laziness:
//   - `git remote get-url origin` honors insteadOf/url.* rewrites, so reading
//     .git/config would report a different URL than the bash does.
//   - `git rev-parse --abbrev-ref HEAD` prints the literal "HEAD" when
//     detached — a shape no config read reproduces.
//   - `uname -m` and runtime.GOARCH disagree off darwin/arm64 (x86_64 vs
//     amd64, aarch64 vs arm64), so GOARCH would silently corrupt the arch
//     field on Linux — and a differential harness on darwin/arm64 cannot
//     catch it, because both agree there.
//
// Swap for internal/git (and x/sys/unix.Uname) once bs-u1tzig4t lands, as
// bs-taoqnrjk did.

// run executes a command and returns its trimmed stdout, or "" on any failure
// — the bash's `$(cmd 2>/dev/null || true)`.
func run(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// OSName ports `uname -s | tr '[:upper:]' '[:lower:]'`.
func OSName() string { return strings.ToLower(run("uname", "-s")) }

// Arch ports `uname -m`.
func Arch() string { return run("uname", "-m") }

func gitAvailable() bool {
	_, err := exec.LookPath("git") // `command -v git`
	return err == nil
}

// These port, in order, the two expressions of:
//
//	sed 's|.*[:/]\([^/]*/[^/]*\)\.git$|\1|;s|.*[:/]\([^/]*/[^/]*\)$|\1|'
//
// Both are anchored at $ and lead with a greedy .*, so a match always spans
// the whole line and the capture is the last "<owner>/<repo>" segment pair.
var (
	slugDotGit = regexp.MustCompile(`.*[:/]([^/]*/[^/]*)\.git$`)
	slugPlain  = regexp.MustCompile(`.*[:/]([^/]*/[^/]*)$`)
)

// RepoSlug ports the `git remote get-url origin | sed … | tr '/' '-'` pipeline.
// Verified against the bash for ssh, https, bare-path and multi-segment URLs.
func RepoSlug() string {
	if !gitAvailable() {
		return ""
	}
	out := run("git", "remote", "get-url", "origin")
	if out == "" {
		return ""
	}
	// sed is line-oriented; apply per line, then tr across the whole stream.
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		ln = slugDotGit.ReplaceAllString(ln, "$1")
		ln = slugPlain.ReplaceAllString(ln, "$1")
		lines[i] = ln
	}
	return strings.ReplaceAll(strings.Join(lines, "\n"), "/", "-")
}

// Branch ports `git rev-parse --abbrev-ref HEAD`, which prints "HEAD" when
// detached.
func Branch() string {
	if !gitAvailable() {
		return ""
	}
	return run("git", "rev-parse", "--abbrev-ref", "HEAD")
}

// ── Rendering ────────────────────────────────────────────────────────────

// durationField ports:
//
//	case "$DURATION" in
//	  ''|*[!0-9]*) DURATION="" ;;
//	  *) [ "$DURATION" -gt 86400 ] && DURATION="" ;;
//	esac
//
// It returns the rendered field plus any stderr the bash would have emitted.
//
// BUG 3 (replicated): for an all-digit value too large for the shell's
// integer type, `[` fails with "integer expected" instead of comparing. The
// `&&` therefore never clears DURATION, and the oversized value is emitted
// raw into duration_s. A negative value hits the *[!0-9]* branch (the '-' is
// not a digit) and correctly becomes null.
//
// Also note a leading-zero value like "007" passes through verbatim, which is
// not valid JSON. That too is the bash's behavior.
func durationField(raw string) (field, stderr string) {
	if raw == "" || strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return "null", ""
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		// BUG 3: `[` errored, so the guard never ran and raw survives.
		return raw, fmt.Sprintf("bbs-telemetry-log: [: %s: integer expected\n", raw)
	}
	if v > 86400 {
		return "null", ""
	}
	return raw, ""
}

// quotedOrNull renders an optional string field: a json_safe'd quoted value,
// or a bare null when unset.
func quotedOrNull(s string) string {
	if s == "" {
		return "null"
	}
	return `"` + jsonSafe(s) + `"`
}

// Render assembles the event line.
//
// The bytes are assembled by hand rather than via encoding/json on purpose:
// the bash printf emits every field raw, so json.Marshal would quote the
// oversized duration of BUG 3 and escape what jsonSafe merely deletes —
// "fixing" the output and breaking parity.
func Render(d Dirs, e Event, now time.Time) (line, stderr string) {
	durField, durErr := durationField(e.Duration)

	browse := "false"
	if e.UsedBrowse == "true" {
		browse = "true"
	}

	sessions := Sessions(d)
	if sessions == "" { // "${SESSIONS:-1}"
		sessions = "1"
	}

	line = fmt.Sprintf(
		`{"v":1,"ts":"%s","event_type":"%s","skill":"%s","session_id":"%s",`+
			`"babysit_version":"%s","os":"%s","arch":"%s","duration_s":%s,`+
			`"outcome":"%s","error_class":%s,"error_message":%s,"failed_step":%s,`+
			`"used_browse":%s,"sessions":%s,"invoker":"%s","_repo_slug":"%s","_branch":"%s"}`+"\n",
		now.UTC().Format("2006-01-02T15:04:05Z"),
		jsonSafe(e.EventType),
		jsonSafe(e.Skill),
		jsonSafe(e.SessionID),
		Version(d),
		OSName(),
		Arch(),
		durField,
		jsonSafe(e.Outcome),
		quotedOrNull(e.ErrorClass),
		quotedOrNull(e.ErrorMessage),
		quotedOrNull(e.FailedStep),
		browse,
		sessions,
		jsonSafe(e.Invoker),
		jsonSafe(RepoSlug()),
		jsonSafe(Branch()),
	)
	return line, durErr
}

// ── Pending markers ──────────────────────────────────────────────────────

// pendingField ports `grep -o '"key":"[^"]*"' | head -1 | awk -F'"' '{print $4}'`.
// grep is line-oriented, so the value class excludes newline as well as quote.
func pendingField(data, key string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `":"([^"\n]*)"`)
	if m := re.FindStringSubmatch(data); m != nil {
		return m[1]
	}
	return ""
}

// FinalizePending drains stale .pending-<session> markers, emitting one
// close-out row each, and then removes our own marker.
//
// Dead code in practice: nothing in the repo writes .pending-* any more (the
// preamble's start hook that did was removed). Ported as-is for parity rather
// than dropped — deleting it is a scope call for a separate ticket.
//
// The row it writes is a *different, narrower* shape than Render's: no
// error_message/failed_step/_repo_slug/_branch, and sessions/invoker are
// hardcoded. Values are interpolated raw, without json_safe.
func FinalizePending(d Dirs, sessionID string) {
	matches, _ := filepath.Glob(filepath.Join(d.Analytics, ".pending-*"))
	for _, pfile := range matches {
		fi, err := os.Lstat(pfile)
		if err != nil || !fi.Mode().IsRegular() { // `[ -f "$PFILE" ] || continue`
			continue
		}
		if strings.TrimPrefix(filepath.Base(pfile), ".pending-") == sessionID {
			continue
		}

		b, _ := os.ReadFile(pfile)
		_ = os.Remove(pfile)
		data := string(b)
		if data == "" {
			continue
		}

		_ = os.MkdirAll(d.Analytics, 0o755)
		line := fmt.Sprintf(
			`{"v":1,"ts":"%s","event_type":"skill_run","skill":"%s","session_id":"%s",`+
				`"babysit_version":"%s","os":"%s","arch":"%s","duration_s":null,`+
				`"outcome":"unknown","error_class":null,"used_browse":false,`+
				`"sessions":1,"invoker":"unknown"}`+"\n",
			pendingField(data, "ts"),
			pendingField(data, "skill"),
			pendingField(data, "session_id"),
			pendingField(data, "babysit_version"),
			OSName(),
			Arch(),
		)
		appendLine(d.JSONL, line)
	}

	RemovePending(d, sessionID)
}

// RemovePending drops our own marker. The bash guards on a non-empty session
// id, so an empty one leaves ".pending-" itself untouched.
func RemovePending(d Dirs, sessionID string) {
	if sessionID == "" {
		return
	}
	_ = os.Remove(filepath.Join(d.Analytics, ".pending-"+sessionID))
}

// appendLine appends to the log, swallowing every error — the bash ends each
// write with `2>/dev/null || true`, because telemetry must never break a skill
// run.
func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck // best-effort, as in the bash
	_, _ = f.WriteString(line)
}

// Append writes one event row.
func Append(d Dirs, e Event, now time.Time) (stderr string) {
	_ = os.MkdirAll(d.Analytics, 0o755)
	line, stderr := Render(d, e, now)
	appendLine(d.JSONL, line)
	return stderr
}
