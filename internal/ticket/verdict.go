package ticket

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
)

// statusRe matches the first STATUS: line of a verdict body — the small fixed
// alphabet callers branch on instead of parsing prose.
var statusRe = regexp.MustCompile(`^STATUS:[[:space:]]*(DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT)\b`)

// VerdictStatus is the canonical read behind `bbs ticket verdict-status`: one
// of {none|DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT} for the named skill.
func VerdictStatus(st *Store, skill string) string {
	return VerdictStatusAt(st.VerdictPath(skill))
}

// VerdictStatusAt is VerdictStatus for a caller that already holds the file
// path — the dashboard composes one row per verdict file it enumerated, so
// re-deriving the path through a Store would only re-split what it just joined.
func VerdictStatusAt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "none"
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		// First STATUS: line wins. Verdict files are append-once-overwrite, so a
		// later duplicate would be a bug; taking the first is stable regardless.
		if m := statusRe.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
	}
	return "none"
}

// BodyHasStatus reports whether a verdict body carries a status line, scanned
// with the same matcher VerdictStatus reads by — so the set-verdict write
// guard can never disagree with the gate it exists to protect.
func BodyHasStatus(body []byte) bool {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if statusRe.MatchString(sc.Text()) {
			return true
		}
	}
	return false
}
