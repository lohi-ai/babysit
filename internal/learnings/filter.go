package learnings

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var errBadRegex = errors.New("bad regex")

// Grep is `grep -i <pattern>` over content's lines: BRE, case-insensitive.
// ok=false ⇔ grep would exit non-zero (no match, or invalid pattern — the
// bash suppresses grep's stderr and `|| exit 0`s both). Known divergences
// from BSD grep, per the bbs-env port's precedent (unreachable from real
// queries, excluded from the harness): backreferences (\1–\9) and non-UTF-8
// pattern bytes report as invalid instead of matching.
func Grep(content, pattern string) (string, bool) {
	translated, err := breToRE2(pattern)
	if err != nil {
		return "", false
	}
	re, err := regexp.Compile(translated)
	if err != nil {
		return "", false
	}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if re.MatchString(line) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "\n"), true
}

// breToRE2 rewrites a POSIX BRE into RE2 syntax: +?|(){} are literal in BRE,
// \(\)\{\} are the operators, * is literal with no preceding atom, ^/$ anchor
// only at the pattern edges.
func breToRE2(pat string) (string, error) {
	var b strings.Builder
	b.WriteString("(?i)")
	atom := false // does a quantifiable atom precede? (BRE: bare '*' without one is literal)
	for i := 0; i < len(pat); {
		c := pat[i]
		switch c {
		case '\\':
			if i+1 >= len(pat) {
				return "", errBadRegex // trailing backslash: grep errors out
			}
			d := pat[i+1]
			switch {
			case d == '(', d == '{':
				b.WriteByte(d)
				atom = false
			case d == ')', d == '}':
				b.WriteByte(d)
				atom = true
			case d >= '1' && d <= '9':
				return "", errBadRegex // backreference — unsupported divergence
			case d == '<', d == '>':
				b.WriteString(`\b`)
				atom = false
			default:
				b.WriteString(regexp.QuoteMeta(string(d)))
				atom = true
			}
			i += 2
			continue
		case '*':
			if atom {
				b.WriteByte('*')
			} else {
				b.WriteString(`\*`)
				atom = true
			}
		case '.':
			b.WriteByte('.')
			atom = true
		case '[':
			j, err := bracketEnd(pat, i)
			if err != nil {
				return "", err
			}
			// POSIX brackets treat '\' as a literal; RE2 treats it as an escape.
			b.WriteString(strings.ReplaceAll(pat[i:j], `\`, `\\`))
			i = j
			atom = true
			continue
		case '^':
			if i == 0 {
				b.WriteByte('^')
				atom = false
			} else {
				b.WriteString(`\^`)
				atom = true
			}
		case '$':
			if i == len(pat)-1 {
				b.WriteByte('$')
			} else {
				b.WriteString(`\$`)
				atom = true
			}
		case '+', '?', '|', '(', ')', '{', '}':
			b.WriteByte('\\')
			b.WriteByte(c)
			atom = true
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
			atom = true
		}
		i++
	}
	return b.String(), nil
}

// bracketEnd returns the index just past the ']' closing the bracket
// expression opened at pat[i], honoring the POSIX rules: a leading ^ and a
// ']' right after it are content, and [:class:] / [.sym.] / [=eq=] blocks may
// contain ']'.
func bracketEnd(pat string, i int) (int, error) {
	j := i + 1
	if j < len(pat) && pat[j] == '^' {
		j++
	}
	if j < len(pat) && pat[j] == ']' {
		j++
	}
	for j < len(pat) {
		if pat[j] == '[' && j+1 < len(pat) && (pat[j+1] == ':' || pat[j+1] == '.' || pat[j+1] == '=') {
			end := strings.Index(pat[j+2:], string(pat[j+1])+"]")
			if end < 0 {
				return 0, errBadRegex
			}
			j += 2 + end + 2
			continue
		}
		if pat[j] == ']' {
			return j + 1, nil
		}
		j++
	}
	return 0, errBadRegex // unmatched '[' — grep errors out
}

// TailN is `tail -n <arg>` over content's lines (pipe mode, as the bash
// pipeline uses it). errMsg is BSD tail's stderr line when arg is rejected —
// the caller prints it and still exits 0, matching the script's trailing
// `exit 0`. Semantics probed on macOS: strtoll parse (leading spaces ok,
// whole token must consume), +N = print from line N (1-based, +0 ≡ +1),
// N / -N = last N lines, ±0 = nothing; overflow gives "failed to allocate
// memory" except in +N mode, which just seeks past EOF silently.
func TailN(content, arg string) (out, errMsg string) {
	t := strings.TrimLeft(arg, " \t\n\v\f\r")
	fromStart := strings.HasPrefix(t, "+")
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) {
			if fromStart {
				return "", ""
			}
			return "", "tail: failed to allocate memory: Cannot allocate memory"
		}
		return "", fmt.Sprintf("tail: illegal offset -- %s: Invalid argument", arg)
	}
	lines := strings.Split(content, "\n")
	if fromStart {
		start := n
		if start < 1 {
			start = 1
		}
		if start > int64(len(lines)) {
			return "", ""
		}
		return strings.Join(lines[start-1:], "\n"), ""
	}
	if n < 0 {
		n = -n
	}
	if n == 0 {
		return "", ""
	}
	if n < int64(len(lines)) {
		lines = lines[int64(len(lines))-n:]
	}
	return strings.Join(lines, "\n"), ""
}
