// Package design ports the parsing core of bin/bbs-design: the limited-YAML
// frontmatter reader, the line-based CSV→record parser, and the deep-merge —
// all faithful to the awk/jq the bash shelled out to, so `bbs design` produces
// the same structures. Serialization is left to the caller (encoding/json);
// the differential harness compares JSON semantically, so only the parsed
// shape has to match, not jq's exact bytes.
package design

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// ─── CSV (line-based, matching the awk parse_row) ────────────────────────────
//
// The awk reads one physical line per record: quoted fields may contain commas,
// a doubled `""` is a literal quote. A field with an embedded newline is NOT
// handled (the bash breaks there too) — so we deliberately do NOT use
// encoding/csv, which would join such lines and diverge.

// KV is one header→value cell, kept in header order.
type KV struct{ Key, Val string }

// Row is one CSV record: ordered cells plus an index for lookups.
type Row struct {
	Cols []KV
	idx  map[string]string
}

// Get returns the cell for a header, or "" when absent — matching jq's
// `.["Header"] // ""`.
func (r Row) Get(k string) string { return r.idx[k] }

// Map renders the row as a plain map for JSON emission. Under the harness's
// `jq -S` canonicalization key order is irrelevant, so a map is sufficient.
func (r Row) Map() map[string]string {
	m := make(map[string]string, len(r.Cols))
	for _, c := range r.Cols {
		m[c.Key] = c.Val
	}
	return m
}

// stripCR removes carriage returns from each field, matching the awk jesc
// (`gsub(/\r/, "", v)`) so CRLF files normalize identically.
func stripCR(fields []string) []string {
	for i := range fields {
		if strings.IndexByte(fields[i], '\r') >= 0 {
			fields[i] = strings.ReplaceAll(fields[i], "\r", "")
		}
	}
	return fields
}

// parseCSVLine replicates the awk parse_row char scanner.
func parseCSVLine(line string) []string {
	var fields []string
	var field strings.Builder
	q := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if q && i+1 < len(line) && line[i+1] == '"' {
				field.WriteByte('"')
				i++
				continue
			}
			q = !q
		case c == ',' && !q:
			fields = append(fields, field.String())
			field.Reset()
		default:
			field.WriteByte(c)
		}
	}
	fields = append(fields, field.String())
	return fields
}

// ReadCSV parses a CSV file into header-keyed rows (first line = header).
func ReadCSV(path string) ([]Row, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) == 0 {
		return nil, nil
	}
	hdr := stripCR(parseCSVLine(lines[0]))
	var rows []Row
	for _, ln := range lines[1:] {
		cells := stripCR(parseCSVLine(ln))
		r := Row{idx: map[string]string{}}
		for i, h := range hdr {
			v := ""
			if i < len(cells) {
				v = cells[i]
			}
			r.Cols = append(r.Cols, KV{h, v})
			if _, seen := r.idx[h]; !seen {
				r.idx[h] = v
			}
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// ─── YAML frontmatter (faithful port of _yaml_frontmatter_to_json) ───────────

var (
	fmDelim   = regexp.MustCompile(`^---[ \t]*$`)
	fmComment = regexp.MustCompile(`^[ \t]*#`)
	fmBlank   = regexp.MustCompile(`^[ \t]*$`)
	fmKey     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*[ \t]*:`)
	reInt     = regexp.MustCompile(`^-?[0-9]+$`)
	reFloat   = regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)
	reDblQ    = regexp.MustCompile(`^".*"$`)
	reSglQ    = regexp.MustCompile(`^'.*'$`)
)

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

func trimSpaces(s string) string { return strings.Trim(s, " ") }

func stripQuotes(s string) string {
	if reDblQ.MatchString(s) || reSglQ.MatchString(s) {
		return s[1 : len(s)-1]
	}
	return s
}

// quoteScalar converts a scalar YAML token to a JSON fragment, mirroring the
// awk quote_scalar exactly (null/bool passthrough, inline {}/[] recursion,
// int/float passthrough, else a quoted+escaped string).
func quoteScalar(v string) string {
	v = trimSpaces(v)
	switch v {
	case "", "null", "~":
		return "null"
	case "true", "false":
		return v
	}
	if strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") ||
		strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		return yamlishInline(v)
	}
	if reInt.MatchString(v) || reFloat.MatchString(v) {
		return v
	}
	v = stripQuotes(v)
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

// yamlishInline converts an inline array/object to strict JSON, matching the
// awk's naive comma/colon splitting (including its quirks on nested commas).
func yamlishInline(s string) string {
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := s[1 : len(s)-1]
		parts := strings.Split(inner, ",")
		var b strings.Builder
		b.WriteByte('[')
		for i, p := range parts {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quoteScalar(p))
		}
		b.WriteByte(']')
		return b.String()
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		inner := s[1 : len(s)-1]
		parts := strings.Split(inner, ",")
		var b strings.Builder
		b.WriteByte('{')
		first := true
		for _, p := range parts {
			kv := strings.Split(p, ":")
			if len(kv) < 2 {
				continue
			}
			k := trimSpaces(kv[0])
			vv := strings.Join(kv[1:], ":")
			k = strings.TrimPrefix(k, `"`)
			k = strings.TrimSuffix(k, `"`)
			k = strings.TrimPrefix(k, `'`)
			k = strings.TrimSuffix(k, `'`)
			if !first {
				b.WriteString(", ")
			}
			first = false
			b.WriteString(`"` + k + `": ` + quoteScalar(vv))
		}
		b.WriteByte('}')
		return b.String()
	}
	return quoteScalar(s)
}

// FrontmatterJSON parses the YAML frontmatter of a file into a compact JSON
// string, byte-for-byte as the awk emitted it (before jq pretty-printed). A
// file with `---`/`---` and no keys yields "{}", the documented empty signal.
func FrontmatterJSON(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "}"
	}
	var out strings.Builder
	indentStack := []int{-1}
	first := []int{1}
	depth := 0
	inFM := false
	started := false

	push := func(ind int) {
		depth++
		if depth < len(indentStack) {
			indentStack[depth] = ind
			first[depth] = 1
		} else {
			indentStack = append(indentStack, ind)
			first = append(first, 1)
		}
	}

	for _, line := range strings.Split(string(b), "\n") {
		if fmDelim.MatchString(line) {
			if !inFM {
				inFM = true
				started = true
				out.WriteByte('{')
				first[0] = 1
				continue
			}
			break // second delimiter — stop (awk `exit` → END)
		}
		if !inFM {
			continue
		}
		if fmComment.MatchString(line) || fmBlank.MatchString(line) {
			continue
		}
		ind := leadingSpaces(line)
		content := line[ind:]
		loc := fmKey.FindString(content)
		if loc == "" {
			continue
		}
		key := content[:len(loc)-1] // everything up to the colon
		key = strings.TrimRight(key, " \t")
		rest := content[len(loc):]
		rest = strings.TrimLeft(rest, " \t")

		for depth > 0 && indentStack[depth] >= ind {
			out.WriteByte('}')
			depth--
		}
		if first[depth] == 0 {
			out.WriteString(", ")
		}
		first[depth] = 0
		out.WriteString(`"` + key + `": `)
		if rest == "" {
			out.WriteByte('{')
			push(ind)
		} else {
			out.WriteString(quoteScalar(rest))
		}
	}

	for depth > 0 {
		out.WriteByte('}')
		depth--
	}
	out.WriteByte('}')
	_ = started
	return out.String()
}

// ─── deep merge (faithful to the jq deep_merge; right wins per leaf) ─────────

// DeepMerge merges b into a: recursively for two objects, otherwise b wins.
func DeepMerge(a, b interface{}) interface{} {
	am, aok := a.(map[string]interface{})
	bm, bok := b.(map[string]interface{})
	if !aok || !bok {
		return b
	}
	out := make(map[string]interface{}, len(am)+len(bm))
	for k, v := range am {
		out[k] = v
	}
	for k, bv := range bm {
		if av, ok := out[k]; ok {
			if _, aIsObj := av.(map[string]interface{}); aIsObj {
				if _, bIsObj := bv.(map[string]interface{}); bIsObj {
					out[k] = DeepMerge(av, bv)
					continue
				}
			}
		}
		out[k] = bv
	}
	return out
}

// ParseFrontmatterValue parses a file's frontmatter into a Go value ({} on
// empty). An error means the produced JSON was not parseable (matching a jq
// failure in the bash).
func ParseFrontmatterValue(path string) (interface{}, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(FrontmatterJSON(path)), &v); err != nil {
		return nil, err
	}
	return v, nil
}
