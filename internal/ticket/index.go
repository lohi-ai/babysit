package ticket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Doc is the generic index.json document. It is a map (not a typed struct) so a
// Go write preserves every field a bash write left behind — the strangler runs
// Go and bash mutators against the same file, and a typed round-trip would drop
// fields the ported subcommands don't model. Mirrors bash json_mutate, which
// loads the whole object, mutates one path, and rewrites it.
type Doc map[string]interface{}

// ReadDoc loads index.json into a generic map. Missing or malformed → empty map,
// matching bash json_mutate, which seeds '{}' when the file is absent, and
// json_read, which prints empty on any failure. UseNumber keeps integers exact
// (e.g. origin.position) so a round-trip never rewrites 1 as 1.0.
func ReadDoc(path string) Doc {
	d := Doc{}
	b, err := os.ReadFile(path)
	if err != nil {
		return d
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&d); err != nil || d == nil {
		return Doc{}
	}
	return d
}

// WriteDoc touches updated_at and atomically rewrites index.json. Output is
// 2-space-indented with sorted keys (Go sorts map keys) — JSON-equivalent to
// bash's `json.dump(d, fh, indent=2, sort_keys=True)` plus trailing newline.
func WriteDoc(path string, d Doc) error {
	d["updated_at"] = isoNow()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil { // Encode already appends "\n"
		return err
	}
	return atomicWriteBytes(path, buf.Bytes())
}

// atomicWriteBytes mirrors bash mktemp+mv: write a sibling temp, then rename.
func atomicWriteBytes(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// EnsureDefaults mirrors the json_mutate ensure_defaults op: seed the schema's
// fields only where absent, and stamp created_at once.
func (d Doc) EnsureDefaults(ticketID string) {
	setDefault(d, "id", ticketID)
	setDefault(d, "status", "triage")
	setDefault(d, "phase", nil)
	setDefault(d, "parent", nil)
	setDefault(d, "children", []interface{}{})
	setDefault(d, "relations", map[string]interface{}{
		"blocks": []interface{}{}, "blocked_by": []interface{}{},
		"duplicate_of": nil, "related": []interface{}{},
	})
	setDefault(d, "siblings", []interface{}{})
	setDefault(d, "labels", []interface{}{})
	setDefault(d, "assignee", nil)
	setDefault(d, "origin", map[string]interface{}{
		"type": "standalone", "parent": nil, "seed": nil,
		"plan": nil, "position": nil, "design_doc": nil,
	})
	setDefault(d, "pointers", map[string]interface{}{})
	if _, ok := d["created_at"]; !ok {
		d["created_at"] = isoNow()
	}
}

func setDefault(d Doc, k string, v interface{}) {
	if _, ok := d[k]; !ok {
		d[k] = v
	}
}

// walk descends the dotted-path parents, creating intermediate maps where the
// current value is missing or not an object — matching bash walk(create=True).
func walk(d Doc, parts []string) (map[string]interface{}, string) {
	cur := map[string]interface{}(d)
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			cur[p] = next
		}
		cur = next
	}
	return cur, parts[len(parts)-1]
}

// Set mirrors the json_mutate set op: only the null/true/false sentinels are
// coerced; every other value stays a string (auto-coercing "42" to int once
// broke ticket ids).
func (d Doc) Set(dotted, value string) {
	var v interface{}
	switch value {
	case "null":
		v = nil
	case "true":
		v = true
	case "false":
		v = false
	default:
		v = value
	}
	parent, key := walk(d, strings.Split(dotted, "."))
	parent[key] = v
}

// Append mirrors the json_mutate append op: setdefault a list, then append the
// string only if not already present (bash `if value not in lst`).
func (d Doc) Append(dotted, value string) error {
	parent, key := walk(d, strings.Split(dotted, "."))
	lst, err := asList(parent, key, dotted)
	if err != nil {
		return err
	}
	for _, e := range lst {
		if s, ok := e.(string); ok && s == value {
			return nil
		}
	}
	parent[key] = append(lst, value)
	return nil
}

// AppendObj mirrors the json_mutate append_obj op: dedupe by full equality.
func (d Doc) AppendObj(dotted, objJSON string) error {
	dec := json.NewDecoder(strings.NewReader(objJSON))
	dec.UseNumber()
	var obj interface{}
	if err := dec.Decode(&obj); err != nil {
		return err
	}
	parent, key := walk(d, strings.Split(dotted, "."))
	lst, err := asList(parent, key, dotted)
	if err != nil {
		return err
	}
	for _, e := range lst {
		if reflect.DeepEqual(e, obj) {
			return nil
		}
	}
	parent[key] = append(lst, obj)
	return nil
}

func asList(parent map[string]interface{}, key, dotted string) ([]interface{}, error) {
	existing, present := parent[key]
	if !present {
		return []interface{}{}, nil
	}
	lst, ok := existing.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s is not a list", dotted)
	}
	return lst, nil
}

// Get mirrors bash json_read: walk the dotted path; a missing key or a JSON null
// yields the empty string; a dict/list renders as compact JSON; every scalar
// renders like Python str() (True/False for bools).
func (d Doc) Get(dotted string) string {
	var cur interface{} = map[string]interface{}(d)
	if dotted != "" {
		for _, p := range strings.Split(dotted, ".") {
			m, ok := cur.(map[string]interface{})
			if !ok {
				return ""
			}
			v, present := m[p]
			if !present {
				return ""
			}
			cur = v
		}
	}
	if cur == nil {
		return ""
	}
	switch v := cur.(type) {
	case map[string]interface{}, []interface{}:
		// Match Python json.dump default: sorted keys (the on-disk order, since
		// WriteDoc sorts) with ", " / ": " separators, so `get <container>` is
		// byte-identical to the bash oracle, not merely JSON-equivalent.
		var b strings.Builder
		writePy(&b, cur)
		return b.String()
	case bool:
		if v {
			return "True" // bash renders scalars via Python str(): capitalized bools
		}
		return "False"
	case string:
		return v
	default:
		return fmt.Sprintf("%v", cur) // json.Number etc. render as their literal
	}
}

// writePy emits a value the way Python's json.dumps does by default: ", " and
// ": " separators, keys sorted (WriteDoc already sorts them on disk).
func writePy(b *strings.Builder, v interface{}) {
	switch x := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pyScalar(k))
			b.WriteString(": ")
			writePy(b, x[k])
		}
		b.WriteByte('}')
	case []interface{}:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			writePy(b, e)
		}
		b.WriteByte(']')
	default:
		b.WriteString(pyScalar(v))
	}
}

// pyScalar marshals one scalar with HTML escaping off (matching Python).
func pyScalar(v interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}

// AcquireLock takes the .index.lock via mkdir (atomic on POSIX) — the same lock
// path and semantics as lib/lock.sh, so a Go mutator and a bash mutator on the
// same ticket mutually exclude during the strangler. ~50 retries at 100ms ≈ 5s.
func (s *Store) AcquireLock() error {
	th := s.Home()
	_ = os.MkdirAll(th, 0o755)
	lockPath := filepath.Join(th, ".index.lock")
	for tries := 0; ; tries++ {
		if err := os.Mkdir(lockPath, 0o755); err == nil {
			return nil
		}
		if tries >= 50 {
			return fmt.Errorf("failed to acquire lock")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ReleaseLock removes the lock directory (bash rmdir via EXIT trap).
func (s *Store) ReleaseLock() {
	_ = os.RemoveAll(filepath.Join(s.Home(), ".index.lock"))
}

// LockPath is the .index.lock directory, surfaced for the stale-lock hint.
func (s *Store) LockPath() string { return filepath.Join(s.Home(), ".index.lock") }

// HistoryAppendExtra appends one event to history.jsonl. It mirrors the bash
// history_append python: core fields first (ts, ticket, branch, event, optional
// actor), then any keys from a JSON-object extra merged in parse order with the
// core fields protected; a non-object extra is stored verbatim under "extra".
// Best-effort, like bash.
func (s *Store) HistoryAppendExtra(event, actor, extra string) {
	h := s.Home()
	_ = os.MkdirAll(h, 0o755)
	line := buildHistoryLine(isoNow(), s.Env.Ticket, s.Env.Branch, event, actor, extra)
	f, err := os.OpenFile(filepath.Join(h, "history.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

func buildHistoryLine(ts, ticket, branch, event, actor, extra string) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	writeJSONField(&b, "ts", ts, true)
	writeJSONField(&b, "ticket", ticket, false)
	writeJSONField(&b, "branch", branch, false)
	writeJSONField(&b, "event", event, false)
	if actor != "" {
		writeJSONField(&b, "actor", actor, false)
	}
	if extra != "" {
		if pairs, ok := orderedObjectPairs(extra); ok {
			for _, p := range pairs {
				switch p.key {
				case "ts", "ticket", "branch", "event", "actor":
					continue // core fields are protected from caller override
				}
				b.WriteByte(',')
				kb, _ := json.Marshal(p.key)
				b.Write(kb)
				b.WriteByte(':')
				b.Write(p.raw)
			}
		} else {
			writeJSONField(&b, "extra", extra, false)
		}
	}
	b.WriteByte('}')
	return b.Bytes()
}

func writeJSONField(b *bytes.Buffer, key, val string, first bool) {
	if !first {
		b.WriteByte(',')
	}
	kb, _ := json.Marshal(key)
	b.Write(kb)
	b.WriteByte(':')
	vb, _ := json.Marshal(val)
	b.Write(vb)
}

type objPair struct {
	key string
	raw json.RawMessage
}

// orderedObjectPairs decodes a top-level JSON object preserving key order and
// raw values. It returns ok=false for anything that is not a strict, single JSON
// object (leading non-'{', or trailing content) — matching bash, which only
// merges when json.loads yields a dict and otherwise stores the raw string.
func orderedObjectPairs(s string) ([]objPair, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, false
	}
	var out []objPair
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := kt.(string)
		if !ok {
			return nil, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		out = append(out, objPair{key, raw})
	}
	if _, err := dec.Token(); err != nil { // consume closing '}'
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF { // reject trailing content
		return nil, false
	}
	return out, true
}
