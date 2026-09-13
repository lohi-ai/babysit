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

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

func timeNow() int64 { return time.Now().Unix() }

// Options configures one snapshot composition.
type Options struct {
	StateDir     string
	Version      string
	SnapshotAt   string
	SlugOverride string // deprecated single-project filter; "" = all projects
	// CurrentSlug is the project of the directory the dashboard was launched
	// from. It seeds meta.active_project — the filter the SPA opens on — so
	// `bbs dashboard` in a repo shows that repo. Ignored when the slug has no
	// state on disk: an active_project naming a project the snapshot does not
	// carry would leave the SPA filtered to nothing.
	CurrentSlug string
	// CurrentDir is the repo folder the dashboard was launched from (the repo's
	// primary worktree, not a ticket worktree). It seeds meta.current_dir, which
	// the SPA prefills into the spawn form — a foreman is bound to a folder, and
	// the folder the human started the server in is the one they mean. Empty
	// when the launch cwd was not a git repo.
	CurrentDir     string
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

	// The cwd's project wins; alphabetical-first is only the fallback for a
	// launch outside any known repo. Without this the SPA opened on whichever
	// slug sorted first — usually a leftover test fixture, never the project
	// the human was standing in.
	var activeProject interface{}
	if _, ok := projects[o.CurrentSlug]; ok {
		activeProject = o.CurrentSlug
	} else if keys := sortedKeys(projects); len(keys) > 0 {
		activeProject = keys[0]
	}

	return obj{
		"meta": obj{
			"schema_version":  2,
			"generated_at":    o.SnapshotAt,
			"babysit_version": o.Version,
			"active_project":  activeProject,
			"current_dir":     o.CurrentDir,
			"truncations":     truncations,
		},
		"projects":       projects,
		"decisions":      decisions,
		"skillEvents":    skillEvents,
		"builderProfile": jsonlArray(filepath.Join(o.StateDir, "builder-profile.jsonl")),
		"journalTail":    journalTail(filepath.Join(o.StateDir, "journal.log")),
		"sessions":       activeSessions(o.StateDir),
		"foremen":        foremen(o.StateDir, projects),
	}
}

// foremen lists ~/.babysit/foremen/<id>.yaml with the count of tickets assigned
// to each. The count is derived here rather than stored on the record because
// the assignment lives on the ticket — a counter on the foreman would be a
// second copy of the same fact, free to drift the moment a ticket is assigned
// by a writer that doesn't know the record exists.
//
// Liveness is deliberately NOT derived here: heartbeat age changes every
// second, and a snapshot that bakes it in reads as live long after it stopped
// being true. The SPA grades the raw stamps at render time.
func foremen(stateDir string, projects obj) arr {
	assigned := map[string]int{}
	for _, p := range projects {
		block, _ := p.(obj)
		list, _ := block["tickets"].(arr)
		for _, t := range list {
			row, _ := t.(obj)
			if id, ok := row["assignee"].(string); ok && id != "" {
				assigned[id]++
			}
		}
	}
	out := arr{}
	for _, r := range foreman.ListIn(filepath.Join(stateDir, "foremen")) {
		out = append(out, obj{
			"id":              r.ID,
			"owner":           r.Owner,
			"project_dir":     r.ProjectDir,
			"workspace_dir":   r.WorkspaceDir,
			"workspace_ref":   r.WorkspaceRef,
			"workspace_title": r.WorkspaceTitle,
			"session":         r.Session,
			"status":          r.Status,
			"heartbeat":       r.Heartbeat,
			"unreachable":     r.Unreachable,
			"assigned":        assigned[r.ID],
		})
	}
	return out
}

// ─── project block ───────────────────────────────────────────────────────────

func projectBlock(o Options, projectDir string) obj {
	ticketsDir := filepath.Join(projectDir, "tickets")
	summaries := arr{}
	details := obj{}
	timeline := arr{}

	ids, _ := ticket.TicketIDs(projectDir)
	for _, id := range ids {
		tdir := filepath.Join(ticketsDir, id)
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
			"assignee": detail["assignee"], "control": detail["control"],
			// The summary carries the approval record so a list can pin
			// "waiting on you" without loading every ticket's detail.
			"approval": detail["approval"],
			// children + run are projections of the same index.json/checkpoint
			// the detail already read — the list needs them to grade parent
			// progress and current step without opening every detail.
			"children": detail["children"],
			"run":      detail["checkpoint"],
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
	idx, err := ticket.ReadDocStrict(filepath.Join(tdir, "index.json"))
	if err != nil {
		o.warn("skipping " + id + " -- corrupt index at " + filepath.Join(tdir, "index.json") + ": " + err.Error())
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
		"id":         id,
		"title":      title,
		"status":     dig(idx, "status", "unknown"),
		"phase":      digRaw(idx, "phase"),
		"branch":     digPath(idx, "pointers", "branch"),
		"parent":     digRaw(idx, "parent"),
		"size":       digPath(idx, "pointers", "ticket_size"),
		"updated_at": digRaw(idx, "updated_at"),
		"created_at": digRaw(idx, "created_at"),
		// assignee and control ride alongside status, never merged into it:
		// status is the reconciled lifecycle rung, control is the human's
		// override on top of it. Folding "paused" into status would destroy
		// the rung it interrupted and make resume a guess.
		"assignee": digRaw(idx, "assignee"),
		"control":  digRaw(idx, "control"),
		// The DAG edges live on the record: children is the parent's fan-out,
		// origin/relations are the child's place in it, siblings the cross-repo
		// peers. The SPA renders them verbatim rather than re-deriving them.
		"children":  digRaw(idx, "children"),
		"origin":    digRaw(idx, "origin"),
		"relations": digRaw(idx, "relations"),
		"siblings":  digRaw(idx, "siblings"),
		// The approval record and the artifacts it points at travel together:
		// the record is the question, these are what the human reads to answer
		// it, and a screen that had one without the other could not decide.
		"approval":         digRaw(idx, "approval"),
		"requirement":      fileCappedOrNull(filepath.Join(tdir, "requirement.md"), 51200),
		"plan":             fileCappedOrNull(filepath.Join(tdir, "plan.md"), 51200),
		"design":           fileCappedOrNull(filepath.Join(tdir, "design.md"), 51200),
		"prototype":        prototype(tdir),
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

// prototypeCap bounds the mock embedded in the snapshot. A design prototype is
// one self-contained HTML file; past a megabyte it is carrying assets the
// approval screen has no business inlining into every poll.
const prototypeCap = 1 << 20

// prototype reports the ticket's mock and, when it fits, its full source.
//
// The HTML rides *inside* the snapshot rather than being fetched, because the
// file:// path has no server to fetch from — an approval screen that can only
// show the mock while a server happens to be running is not a design
// checkpoint. Oversized mocks embed nothing rather than a prefix: half an HTML
// document renders as garbage, and "open it in a tab" is the better answer.
func prototype(tdir string) interface{} {
	path := filepath.Join(tdir, "prototype.html")
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	out := obj{"path": path, "bytes": st.Size(), "html": nil}
	if st.Size() <= prototypeCap {
		if b, err := os.ReadFile(path); err == nil {
			out["html"] = string(b)
		}
	}
	return out
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

// manifestRepos renders a manifest's repos[] for the SPA through the canonical
// parser. A missing or malformed manifest yields an empty array — the same
// shape the hand parser produced for a file it could not read.
func manifestRepos(path string) arr {
	m, err := ticket.ReadManifest(path)
	if err != nil {
		return arr{}
	}
	out := arr{}
	for _, r := range m.Repos {
		out = append(out, obj{
			"name":      nilIfEmptyVal(r.Name),
			"branch":    nilIfEmptyVal(r.Branch),
			"canonical": nilIfEmptyVal(r.Canonical),
			"worktree":  nilIfEmptyVal(r.Worktree),
			"base":      nilIfEmptyVal(r.Base),
			"pushed":    r.Pushed == "true",
		})
	}
	return out
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
		out[skill] = ticket.VerdictStatusAt(filepath.Join(dir, n))
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
