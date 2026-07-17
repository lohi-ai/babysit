package ticket

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
)

// SessionWindow is the 120-minute liveness window shared by `session list` and
// `board` (bash: SS_CUTOFF / BD_SESS_DIR).
const SessionWindow = 7200 // seconds

// SessionsDir is ${BABYSIT_HOME:-$HOME/.babysit}/sessions.
func SessionsDir() string { return filepath.Join(identity.BabysitHome(), "sessions") }

// SessionField reads one top-level scalar from a flat session yaml, mirroring
// bash ss_field(): skip comments, first `key:` line wins, strip surrounding
// quotes. Missing file or key yields "".
func SessionField(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		ln := sc.Text()
		if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "#") {
			continue
		}
		if !strings.HasPrefix(ln, key+":") {
			continue
		}
		v := strings.TrimLeft(strings.TrimPrefix(ln, key+":"), " \t")
		return strings.Trim(v, `"'`)
	}
	return ""
}

// ValidSessionID guards path construction: ids are [A-Za-z0-9_-]+ only.
func ValidSessionID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
