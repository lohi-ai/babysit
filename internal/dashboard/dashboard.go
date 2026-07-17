// Package dashboard ports the snapshot core of bin/bbs-dashboard: it walks
// ~/.babysit state (projects → tickets, sessions, analytics) and composes the
// nested JSON object the web SPA loads as `window.__BBS_DATA__`. The bash
// shelled out to jq/python for every parse and accumulation; this builds the
// same structure natively. Serialization + JS-escaping live in the caller.
package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func timeNow() int64 { return time.Now().Unix() }

// Options configures one snapshot composition.
type Options struct {
	StateDir       string
	Version        string
	SnapshotAt     string
	SlugOverride   string // deprecated single-project filter; "" = all projects
	DecisionsCap   int
	SkillEventsCap int
	Warn           func(msg string) // stderr sink (caller adds the bbs-dashboard: prefix)
}

func (o Options) warn(msg string) {
	if o.Warn != nil {
		o.Warn(msg)
	}
}

// obj/arr aliases keep the composition readable and marshal deterministically
// (Go sorts map keys), so re-runs are byte-identical.
type obj = map[string]interface{}
type arr = []interface{}

var slugBad = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

func badSlug(s string) bool {
	return s == "" || s == "." || s == ".." || strings.Contains(s, "/") || slugBad.MatchString(s)
}

// Compose builds the full v2 snapshot object.
func Compose(o Options) obj {
	projects := obj{}
	var projectDirs []string
	if o.SlugOverride != "" {
		projectDirs = []string{filepath.Join(o.StateDir, "projects", o.SlugOverride)}
	} else {
		projectDirs = sortedDirs(filepath.Join(o.StateDir, "projects"))
	}

	foundAny := false
	for _, dir := range projectDirs {
		if dir == "" {
			continue
		}
		slug := filepath.Base(dir)
		if badSlug(slug) {
			o.warn("skipping '" + slug + "' (rejected by path-traversal guard)")
			continue
		}
		projects[slug] = projectBlock(o, dir)
		foundAny = true
	}
	if !foundAny && o.SlugOverride == "" {
		o.warn("no projects found; run autopilot first.")
	}

	decisions, decisionsTotal := tailRows(filepath.Join(o.StateDir, "analytics", "decisions.jsonl"), o.DecisionsCap)
	skillEvents, skillEventsTotal := tailRows(filepath.Join(o.StateDir, "analytics", "skill-usage.jsonl"), o.SkillEventsCap)

	truncations := arr{}
	if decisionsTotal > o.DecisionsCap {
		truncations = append(truncations, obj{"kind": "decisions", "kept": len(decisions), "total": decisionsTotal})
	}
	if skillEventsTotal > o.SkillEventsCap {
		truncations = append(truncations, obj{"kind": "skillEvents", "kept": len(skillEvents), "total": skillEventsTotal})
	}

	var activeProject interface{}
	if keys := sortedKeys(projects); len(keys) > 0 {
		activeProject = keys[0]
	}

	return obj{
		"meta": obj{
			"schema_version":  2,
			"generated_at":    o.SnapshotAt,
			"babysit_version": o.Version,
			"active_project":  activeProject,
			"truncations":     truncations,
		},
		"projects":       projects,
		"decisions":      decisions,
		"skillEvents":    skillEvents,
		"builderProfile": jsonlArray(filepath.Join(o.StateDir, "builder-profile.jsonl")),
		"journalTail":    journalTail(filepath.Join(o.StateDir, "journal.log")),
		"sessions":       activeSessions(o.StateDir),
	}
}

// ─── project block ───────────────────────────────────────────────────────────

func projectBlock(o Options, projectDir string) obj {
	ticketsDir := filepath.Join(projectDir, "tickets")
	summaries := arr{}
	details := obj{}
	timeline := arr{}

	for _, tdir := range sortedDirs(ticketsDir) {
		if _, err := os.Stat(filepath.Join(tdir, "index.json")); err != nil {
			continue
		}
		detail, ok := ticketDetail(o, tdir)
		if !ok {
			continue
		}
		id, _ := detail["id"].(string)
		details[id] = detail
		summaries = append(summaries, obj{
			"id": detail["id"], "title": detail["title"], "status": detail["status"],
			"phase": detail["phase"], "branch": detail["branch"], "parent": detail["parent"],
			"size": detail["size"], "updated_at": detail["updated_at"], "created_at": detail["created_at"],
		})
		// timeline: each history row + {ticket: id}
		if rows, ok := parseJSONL(filepath.Join(tdir, "history.jsonl")); ok {
			for _, r := range rows {
				if m, ok := r.(map[string]interface{}); ok {
					m["ticket"] = id
					timeline = append(timeline, m)
				} else {
					timeline = append(timeline, r)
				}
			}
		}
	}

	sortByStringDesc(summaries, "updated_at")
	sortByStringDesc(timeline, "ts")

	return obj{
		"tickets":      summaries,
		"ticketDetail": details,
		"timeline":     timeline,
		"analytics":    analytics(filepath.Join(projectDir, "analytics", "skill-usage.jsonl")),
	}
}

func ticketDetail(o Options, tdir string) (obj, bool) {
	id := filepath.Base(tdir)
	idx, err := parseJSONObject(filepath.Join(tdir, "index.json"))
	if err != nil {
		o.warn("skipping " + id + " -- corrupt index at " + filepath.Join(tdir, "index.json") + ": invalid JSON")
		return nil, false
	}

	title := firstHeading(filepath.Join(tdir, "requirement.md"))
	if title == "" {
		title = id
	}

	var checkpoint interface{}
	if v, err := parseJSONValue(filepath.Join(tdir, "checkpoint.json")); err == nil {
		checkpoint = v
	}

	history := arr{}
	if rows, ok := parseJSONL(filepath.Join(tdir, "history.jsonl")); ok {
		history = rows
	}

	repos := manifestRepos(filepath.Join(tdir, "manifest.yaml"))

	return obj{
		"id":               id,
		"title":            title,
		"status":           dig(idx, "status", "unknown"),
		"phase":            digRaw(idx, "phase"),
		"branch":           digPath(idx, "pointers", "branch"),
		"parent":           digRaw(idx, "parent"),
		"size":             digPath(idx, "pointers", "ticket_size"),
		"updated_at":       digRaw(idx, "updated_at"),
		"created_at":       digRaw(idx, "created_at"),
		"requirement":      fileCappedOrNull(filepath.Join(tdir, "requirement.md"), 51200),
		"plan":             fileCappedOrNull(filepath.Join(tdir, "plan.md"), 51200),
		"manifest":         fileCappedOrNull(filepath.Join(tdir, "manifest.md"), 51200),
		"repos":            repos,
		"checkpoint":       checkpoint,
		"history":          history,
		"handoffs":         namedFiles(filepath.Join(tdir, "handoffs"), ".md"),
		"verdicts":         namedFiles(filepath.Join(tdir, "verdicts"), ".md"),
		"verdict_statuses": verdictStatuses(filepath.Join(tdir, "verdicts")),
		"reviews":          namedFiles(filepath.Join(tdir, "reviews"), ".md"),
		"evidence":         evidenceFiles(filepath.Join(tdir, "evidence")),
	}, true
}

// ─── analytics ───────────────────────────────────────────────────────────────

func analytics(path string) obj {
	rows := jsonlArray(path)

	// per_skill: group by skill (default "unknown"), aggregate, sort by runs desc.
	skillKeys := map[string]*obj{}
	var skillOrder []string
	for _, r := range rows {
		m, _ := r.(map[string]interface{})
		sk := strOr(m["skill"], "unknown")
		e := skillKeys[sk]
		if e == nil {
			ne := obj{"skill": sk, "runs": 0, "total_s": 0.0, "success": 0, "error": 0}
			skillKeys[sk] = &ne
			e = &ne
			skillOrder = append(skillOrder, sk)
		}
		(*e)["runs"] = (*e)["runs"].(int) + 1
		(*e)["total_s"] = (*e)["total_s"].(float64) + numOr(m["duration_s"], 0)
		switch strOr(m["outcome"], "") {
		case "success":
			(*e)["success"] = (*e)["success"].(int) + 1
		case "error":
			(*e)["error"] = (*e)["error"].(int) + 1
		}
	}
	sort.Strings(skillOrder)
	perSkill := arr{}
	for _, k := range skillOrder {
		perSkill = append(perSkill, *skillKeys[k])
	}
	sort.SliceStable(perSkill, func(i, j int) bool {
		return perSkill[i].(obj)["runs"].(int) > perSkill[j].(obj)["runs"].(int)
	})

	// per_day: group by ts[0:10], count, sort by day asc.
	dayCount := map[string]int{}
	for _, r := range rows {
		m, _ := r.(map[string]interface{})
		ts := strOr(m["ts"], "")
		if len(ts) > 10 {
			ts = ts[:10]
		}
		dayCount[ts]++
	}
	perDay := arr{}
	for _, d := range sortedStringKeys(dayCount) {
		perDay = append(perDay, obj{"day": d, "runs": dayCount[d]})
	}

	// outcome: group by outcome (default "unknown"), count, sort by count desc.
	outCount := map[string]int{}
	for _, r := range rows {
		m, _ := r.(map[string]interface{})
		outCount[strOr(m["outcome"], "unknown")]++
	}
	outcome := arr{}
	for _, oc := range sortedStringKeys(outCount) {
		outcome = append(outcome, obj{"outcome": oc, "count": outCount[oc]})
	}
	sort.SliceStable(outcome, func(i, j int) bool {
		return outcome[i].(obj)["count"].(int) > outcome[j].(obj)["count"].(int)
	})

	return obj{"rows": rows, "per_skill": perSkill, "per_day": perDay, "outcome": outcome}
}

// ─── sessions ────────────────────────────────────────────────────────────────

func activeSessions(stateDir string) obj {
	d := filepath.Join(stateDir, "sessions")
	entries, err := os.ReadDir(d)
	if err != nil {
		return obj{"count": 0, "slugs": arr{}, "sessions": arr{}}
	}
	now := timeNow()
	count := 0
	slugs := arr{}
	sessions := arr{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime().Unix()
		if mt <= 0 {
			continue
		}
		ageMin := int((now - mt) / 60)
		if ageMin > 120 {
			continue
		}
		count++
		slugs = append(slugs, e.Name())
		sessions = append(sessions, sessionRow(filepath.Join(d, e.Name()), ageMin))
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].(obj)["age_min"].(int) < sessions[j].(obj)["age_min"].(int)
	})
	return obj{"count": count, "slugs": slugs, "sessions": sessions}
}

func sessionRow(path string, ageMin int) obj {
	f := parseFlatYAML(path)
	sid := strings.TrimSuffix(filepath.Base(path), ".yaml")
	return obj{
		"id":         sid,
		"ticket":     nilIfEmpty(f["ticket"]),
		"product":    nilIfEmpty(f["product"]),
		"cwd":        nilIfEmpty(f["cwd"]),
		"started_at": nilIfEmpty(f["started_at"]),
		"age_min":    ageMin,
	}
}

// parseFlatYAML reads the preamble session-writer's flat key:value schema.
func parseFlatYAML(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" || strings.HasPrefix(ln, "#") || !strings.Contains(ln, ":") {
			continue
		}
		k, v, _ := strings.Cut(ln, ":")
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "'\"")
	}
	return out
}

// ─── manifest.yaml → repos[] ─────────────────────────────────────────────────

func manifestRepos(path string) arr {
	b, err := os.ReadFile(path)
	if err != nil {
		return arr{}
	}
	var raw []map[string]interface{}
	var cur map[string]interface{}
	for _, ln := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimLeft(ln, " ")
		if ln == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(ln, "  - ") {
			cur = map[string]interface{}{}
			raw = append(raw, cur)
			kv := strings.TrimSpace(ln[4:])
			if k, v, ok := strings.Cut(kv, ":"); ok {
				cur[strings.TrimSpace(k)] = coerceYAML(strings.TrimSpace(v))
			}
		} else if strings.HasPrefix(ln, "    ") && cur != nil {
			kv := strings.TrimSpace(ln)
			if k, v, ok := strings.Cut(kv, ":"); ok {
				cur[strings.TrimSpace(k)] = coerceYAML(strings.TrimSpace(v))
			}
		}
	}
	out := arr{}
	for _, r := range raw {
		pushed := false
		if b, ok := r["pushed"].(bool); ok {
			pushed = b
		}
		out = append(out, obj{
			"name":      nilIfEmptyVal(r["name"]),
			"branch":    nilIfEmptyVal(r["branch"]),
			"canonical": nilIfEmptyVal(r["canonical"]),
			"worktree":  nilIfEmptyVal(r["worktree"]),
			"base":      nilIfEmptyVal(r["base"]),
			"pushed":    pushed,
		})
	}
	return out
}

func coerceYAML(v string) interface{} {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		return v[1 : len(v)-1]
	}
	return v
}

// ─── file helpers ────────────────────────────────────────────────────────────

func firstHeading(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "# ") {
			return strings.TrimPrefix(ln, "# ")
		}
	}
	return ""
}

func fileCappedOrNull(path string, cap int) interface{} {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if len(b) <= cap {
		return string(b)
	}
	return string(b[:cap]) + "\n[...truncated at 50KB]\n"
}

func namedFiles(dir, ext string) arr {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return arr{}
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := arr{}
	for _, n := range names {
		body, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		out = append(out, obj{"name": n, "body": string(body)})
	}
	return out
}

var statusLine = regexp.MustCompile(`^STATUS:[[:space:]]*(DONE_WITH_CONCERNS|DONE|BLOCKED|NEEDS_CONTEXT)\b`)

func verdictStatuses(dir string) obj {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return obj{}
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := obj{}
	for _, n := range names {
		skill := strings.TrimSuffix(n, ".md")
		st := "none"
		if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
			for _, ln := range strings.Split(string(b), "\n") {
				if m := statusLine.FindStringSubmatch(ln); m != nil {
					st = m[1]
					break
				}
			}
		}
		out[skill] = st
	}
	return out
}

func evidenceFiles(dir string) arr {
	out := arr{}
	if _, err := os.Stat(dir); err != nil {
		return out
	}
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, rel)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].(string) < out[j].(string) })
	return out
}

// ─── analytics/journal/builder-profile file readers ─────────────────────────

func jsonlArray(path string) arr {
	rows, _ := parseJSONL(path)
	if rows == nil {
		return arr{}
	}
	return rows
}

func journalTail(path string) arr {
	b, err := os.ReadFile(path)
	if err != nil {
		return arr{}
	}
	lines := splitLines(string(b))
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	out := arr{}
	for _, l := range lines {
		out = append(out, l)
	}
	return out
}

// tailRows returns the newest `cap` parsed JSONL rows and the total row count.
func tailRows(path string, cap int) (arr, int) {
	b, err := os.ReadFile(path)
	if err != nil {
		return arr{}, 0
	}
	lines := splitLines(string(b))
	total := len(lines)
	if len(lines) > cap {
		lines = lines[len(lines)-cap:]
	}
	out := arr{}
	for _, l := range lines {
		var v interface{}
		if json.Unmarshal([]byte(l), &v) == nil {
			out = append(out, v)
		}
	}
	return out, total
}

// ─── JSON parse helpers ──────────────────────────────────────────────────────

func parseJSONObject(path string) (map[string]interface{}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func parseJSONValue(path string) (interface{}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// parseJSONL parses every non-empty line; ok=false if any line is invalid
// (matching the bash all-or-nothing `jq ... || echo '[]'`).
func parseJSONL(path string) (arr, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	out := arr{}
	for _, l := range splitLines(string(b)) {
		var v interface{}
		if err := json.Unmarshal([]byte(l), &v); err != nil {
			return arr{}, false
		}
		out = append(out, v)
	}
	return out, true
}

// ─── small utilities ─────────────────────────────────────────────────────────

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func sortedDirs(parent string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(parent, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m obj) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedStringKeys(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// sortByStringDesc stable-sorts an array of objects by a string field, desc,
// matching jq `sort_by(.field // "") | reverse`.
func sortByStringDesc(a arr, field string) {
	sort.SliceStable(a, func(i, j int) bool {
		return fieldStr(a[i], field) > fieldStr(a[j], field)
	})
}

func fieldStr(v interface{}, field string) string {
	if m, ok := v.(map[string]interface{}); ok {
		if s, ok := m[field].(string); ok {
			return s
		}
	}
	return ""
}

func dig(m map[string]interface{}, key string, def interface{}) interface{} {
	if v, ok := m[key]; ok && v != nil {
		return v
	}
	return def
}

func digRaw(m map[string]interface{}, key string) interface{} {
	if v, ok := m[key]; ok {
		return v
	}
	return nil
}

func digPath(m map[string]interface{}, a, b string) interface{} {
	if inner, ok := m[a].(map[string]interface{}); ok {
		if v, ok := inner[b]; ok && v != nil {
			return v
		}
	}
	return nil
}

func strOr(v interface{}, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func numOr(v interface{}, def float64) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return def
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nilIfEmptyVal(v interface{}) interface{} {
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	if v == nil {
		return nil
	}
	return v
}
