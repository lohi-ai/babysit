// Package qaconfig reads named-environment QA config from project files
// (.babysit/qa.yaml and .babysit/qa.local.yaml), porting bin/bbs-qa-config's
// hand-rolled awk parsers bug-for-bug.
//
// This is deliberately NOT a YAML parser and does not use yaml.v3: the awk
// semantics it must preserve differ from YAML — inline ` #` comments are
// stripped even inside quoted values, exactly one leading and one trailing
// quote character are removed, duplicate keys are last-write-wins, an
// optional `qa:` parent header is accepted, and only single-line scalars are
// read. Every parser below is a rule-order transliteration of its awk
// counterpart.
package qaconfig

import (
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RepoToplevel mirrors _repo_toplevel: `git rev-parse --show-toplevel
// 2>/dev/null || true`. It execs git rather than walking up for .git because
// the bash's behavior on symlinked repo paths (git prints the physical path,
// which appears verbatim in `check` diagnostics) and under GIT_DIR /
// GIT_WORK_TREE / GIT_CEILING_DIRECTORIES overrides is not reproducible
// natively. Temporary shim: swap for internal/git once bs-u1tzig4t lands.
func RepoToplevel() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	// $(...) strips all trailing newlines.
	return strings.TrimRight(string(out), "\n")
}

// POSIX-compiled to match awk's leftmost-longest matching.
var (
	reFullComment   = regexp.MustCompilePOSIX(`^[[:space:]]*#`)
	reInlineComment = regexp.MustCompilePOSIX(`[[:space:]]+#.*$`)
	reQAHeader      = regexp.MustCompilePOSIX(`^qa:[[:space:]]*$`)
	reEnvsTop       = regexp.MustCompilePOSIX(`^environments:[[:space:]]*$`)
	reEnvsIndent    = regexp.MustCompilePOSIX(`^[[:space:]]+environments:[[:space:]]*$`)
	reDashName      = regexp.MustCompilePOSIX(`^[[:space:]]*-[[:space:]]+name:[[:space:]]*`)
	reCredsIndent   = regexp.MustCompilePOSIX(`^[[:space:]]+credentials:[[:space:]]*$`)
	reCredsTop      = regexp.MustCompilePOSIX(`^credentials:[[:space:]]*$`)
	reLeak          = regexp.MustCompilePOSIX(`^[[:space:]]*(password|token|secret|api_key)[[:space:]]*:`)

	reFieldIndent = map[string]*regexp.Regexp{}
	reFieldTop    = map[string]*regexp.Regexp{}
)

func init() {
	for _, f := range []string{"url", "runtime", "guideline", "username_env", "password_env"} {
		reFieldIndent[f] = regexp.MustCompilePOSIX(`^[[:space:]]+` + f + `:[[:space:]]*`)
	}
	for _, f := range []string{"url", "runtime", "guideline", "start", "check", "flows"} {
		reFieldTop[f] = regexp.MustCompilePOSIX(`^` + f + `:[[:space:]]*`)
	}
}

// splitLines yields awk-style records: \n-separated, no phantom record for a
// trailing newline, none at all for an empty file.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// stripQuotes replicates gsub(/^["']|["']$/, ""): at most one leading and one
// trailing quote character, removed independently.
func stripQuotes(s string) string {
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		s = s[1:]
	}
	if len(s) > 0 && (s[len(s)-1] == '"' || s[len(s)-1] == '\'') {
		s = s[:len(s)-1]
	}
	return s
}

func stripInlineComment(line string) string {
	if loc := reInlineComment.FindStringIndex(line); loc != nil {
		return line[:loc[0]]
	}
	return line
}

// isTopKey replicates /^[a-zA-Z]/ (the !/^[[:space:]]/ half is redundant).
func isTopKey(line string) bool {
	return len(line) > 0 && (line[0] >= 'a' && line[0] <= 'z' || line[0] >= 'A' && line[0] <= 'Z')
}

// readLines returns the file's records, or nil on any read error — the awk
// calls run under `2>/dev/null || true`, so unreadable means empty.
func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return splitLines(string(b))
}

// EnvironmentRows ports _yaml_environments: one `<env>|<field>|<value>` row
// per non-empty field per env, in flush order.
func EnvironmentRows(path string) []string {
	var rows []string
	inQA, inEnvs, inCreds := false, false, false
	name := ""
	var url, runtime, guideline, userEnv, passEnv string
	flush := func() {
		if url != "" {
			rows = append(rows, name+"|url|"+url)
		}
		if runtime != "" {
			rows = append(rows, name+"|runtime|"+runtime)
		}
		if guideline != "" {
			rows = append(rows, name+"|guideline|"+guideline)
		}
		if userEnv != "" {
			rows = append(rows, name+"|username_env|"+userEnv)
		}
		if passEnv != "" {
			rows = append(rows, name+"|password_env|"+passEnv)
		}
	}
	for _, line := range readLines(path) {
		if reFullComment.MatchString(line) {
			continue
		}
		line = stripInlineComment(line)
		if reQAHeader.MatchString(line) {
			inQA = true
			continue
		}
		if isTopKey(line) {
			// No continue — the line still reaches the rules below. This also
			// makes the awk's dedicated "end of envs block" flush rule dead
			// code (in_envs is already 0 when it runs), so it is omitted:
			// a pending env is flushed only by the next `- name:` or at EOF.
			inQA, inEnvs, inCreds = false, false, false
		}
		if reEnvsTop.MatchString(line) || (inQA && reEnvsIndent.MatchString(line)) {
			// A second `environments:` header discards any pending env
			// without flushing, like the awk.
			inEnvs, inCreds, name = true, false, ""
			continue
		}
		if inEnvs && reDashName.MatchString(line) {
			if name != "" {
				flush()
			}
			name = stripQuotes(reDashName.ReplaceAllString(line, ""))
			url, runtime, guideline, userEnv, passEnv = "", "", "", "", ""
			inCreds = false
			continue
		}
		if inEnvs && name != "" {
			if m := reFieldIndent["url"]; m.MatchString(line) {
				url, inCreds = stripQuotes(m.ReplaceAllString(line, "")), false
				continue
			}
			if m := reFieldIndent["runtime"]; m.MatchString(line) {
				runtime, inCreds = stripQuotes(m.ReplaceAllString(line, "")), false
				continue
			}
			if m := reFieldIndent["guideline"]; m.MatchString(line) {
				guideline, inCreds = stripQuotes(m.ReplaceAllString(line, "")), false
				continue
			}
			if reCredsIndent.MatchString(line) {
				inCreds = true
				continue
			}
			if m := reFieldIndent["username_env"]; inCreds && m.MatchString(line) {
				userEnv = stripQuotes(m.ReplaceAllString(line, ""))
				continue
			}
			if m := reFieldIndent["password_env"]; inCreds && m.MatchString(line) {
				passEnv = stripQuotes(m.ReplaceAllString(line, ""))
				continue
			}
		}
	}
	if name != "" {
		flush()
	}
	return rows
}

// SimpleLocalRows ports _yaml_simple_local: the standalone simple shape,
// exposed as a single env named "local". Everything after an `environments:`
// line is skipped for the rest of the file (in_envs is never reset — a bash
// bug kept for parity).
func SimpleLocalRows(path string) []string {
	inQA, inEnvs, inCreds := false, false, false
	var url, runtime, guideline, start, check, flows, userEnv, passEnv string
	for _, line := range readLines(path) {
		if reFullComment.MatchString(line) {
			continue
		}
		line = stripInlineComment(line)
		if reQAHeader.MatchString(line) {
			inQA = true
			continue
		}
		if isTopKey(line) {
			inQA, inCreds = false, false // in_envs deliberately not reset
		}
		if reEnvsTop.MatchString(line) || (inQA && reEnvsIndent.MatchString(line)) {
			inEnvs = true // no continue: falls into the skip rule below
		}
		if inEnvs {
			continue
		}
		if reCredsTop.MatchString(line) {
			inCreds = true
			continue
		}
		if m := reFieldIndent["username_env"]; inCreds && m.MatchString(line) {
			userEnv = stripQuotes(m.ReplaceAllString(line, ""))
			continue
		}
		if m := reFieldIndent["password_env"]; inCreds && m.MatchString(line) {
			passEnv = stripQuotes(m.ReplaceAllString(line, ""))
			continue
		}
		matched := false
		for key, dst := range map[string]*string{
			"url": &url, "runtime": &runtime, "guideline": &guideline,
			"start": &start, "check": &check, "flows": &flows,
		} {
			if m := reFieldTop[key]; m.MatchString(line) {
				*dst = stripQuotes(m.ReplaceAllString(line, ""))
				matched = true
				break
			}
		}
		if matched {
			continue
		}
	}
	if url == "" {
		return nil
	}
	appendNote := func(base, note string) string {
		if base == "" {
			return note
		}
		return base + "; " + note
	}
	if start != "" {
		guideline = appendNote(guideline, "Start: "+start)
	}
	if check != "" {
		guideline = appendNote(guideline, "Check: "+check)
	}
	if flows != "" {
		guideline = appendNote(guideline, "Flows: "+flows)
	}
	rows := []string{"local|url|" + url}
	if runtime != "" {
		rows = append(rows, "local|runtime|"+runtime)
	}
	if guideline != "" {
		rows = append(rows, "local|guideline|"+guideline)
	}
	if userEnv != "" {
		rows = append(rows, "local|username_env|"+userEnv)
	}
	if passEnv != "" {
		rows = append(rows, "local|password_env|"+passEnv)
	}
	return rows
}

// TopScalar ports _yaml_top_scalar: first match in the file wins (even an
// empty value — indistinguishable from no match through the shell's $()).
func TopScalar(path, key string) string {
	inQA := false
	reKeyTop := regexp.MustCompilePOSIX(`^` + key + `:[[:space:]]*`)
	reKeyIndent := regexp.MustCompilePOSIX(`^[[:space:]]+` + key + `:[[:space:]]*`)
	reKeySub := regexp.MustCompilePOSIX(`^[[:space:]]*` + key + `:[[:space:]]*`)
	for _, line := range readLines(path) {
		if reFullComment.MatchString(line) {
			continue
		}
		line = stripInlineComment(line)
		if reQAHeader.MatchString(line) {
			inQA = true
			continue
		}
		if isTopKey(line) {
			inQA = false
		}
		if reKeyTop.MatchString(line) || (inQA && reKeyIndent.MatchString(line)) {
			return stripQuotes(reKeySub.ReplaceAllString(line, ""))
		}
	}
	return ""
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// CollectRows ports _collect_rows: every `<env>|<field>|<value>|<source>` row
// in lowest-to-highest precedence order (consumers take the LAST match).
func CollectRows() []string {
	repo := RepoToplevel()
	var rows []string
	add := func(file, source string, parse func(string) []string) {
		if repo == "" || !fileExists(file) {
			return
		}
		for _, r := range parse(file) {
			rows = append(rows, r+"|"+source)
		}
	}
	qa := repo + "/.babysit/qa.yaml"
	local := repo + "/.babysit/qa.local.yaml"
	add(qa, "qa.yaml", EnvironmentRows)
	add(qa, "qa.yaml(simple)", SimpleLocalRows)
	add(local, "qa.local.yaml", EnvironmentRows)
	add(local, "qa.local.yaml(simple)", SimpleLocalRows)
	return rows
}

// CollectDefaultEnv ports _collect_default_env. Note the bash checks
// [ -n "$f" ] (always true), not [ -n "$repo" ] — so with no repo it probes
// the literal paths /.babysit/qa.{local.,}yaml, kept for parity.
func CollectDefaultEnv() string {
	repo := RepoToplevel()
	for _, f := range []string{repo + "/.babysit/qa.local.yaml", repo + "/.babysit/qa.yaml"} {
		if !fileExists(f) {
			continue
		}
		if v := TopScalar(f, "default_env"); v != "" {
			return v
		}
		for _, r := range SimpleLocalRows(f) {
			if strings.HasPrefix(r, "local|url|") {
				return "local"
			}
		}
	}
	return ""
}

// CollectTopScalar ports _collect_top_scalar: highest-precedence non-empty
// value of a top-level scalar. Unlike CollectDefaultEnv this one does guard
// on the repo being resolvable.
func CollectTopScalar(key string) string {
	repo := RepoToplevel()
	if repo == "" {
		return ""
	}
	for _, f := range []string{repo + "/.babysit/qa.local.yaml", repo + "/.babysit/qa.yaml"} {
		if !fileExists(f) {
			continue
		}
		if v := TopScalar(f, key); v != "" {
			return v
		}
	}
	return ""
}

// ShQuote ports _shquote: single-quote wrapping safe for eval.
func ShQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Field splits a row like awk -F'|' and returns $n (1-based), "" when absent.
func Field(row string, n int) string {
	parts := strings.Split(row, "|")
	if n-1 < len(parts) {
		return parts[n-1]
	}
	return ""
}

// SortU replicates `sort -u` under the C locale: byte order, deduplicated.
// Locale-collated ordering (LANG-dependent) is not reachable natively; like
// the bbs-env port, determinism is chosen and the divergence documented.
func SortU(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	sort.Strings(out)
	return out
}

// LeakCheckHits returns grep -nE style "N:line" hits for inline credential
// literals, or nil (read errors read as clean, like `grep ... || true`).
func LeakCheckHits(path string) []string {
	var hits []string
	for i, line := range readLines(path) {
		if reLeak.MatchString(line) {
			hits = append(hits, strconv.Itoa(i+1)+":"+line)
		}
	}
	return hits
}
