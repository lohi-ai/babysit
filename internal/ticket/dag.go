package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The project graph is a read of ticket state, not a second store: `children`
// says where a ticket sits, `relations.blocked_by`/`blocks` say when it can
// run. Nothing here writes — a DAG view that reconciled would move the thing it
// is describing.
//
// Two shapes in real data drive the rules below:
//
//   - `parent` and `children` disagree. Tickets sit in a `children` array with
//     `parent: null`, and `children` names ids with no ticket directory at all
//     (`bs-11a4z8n8` in ~/.babysit/projects/lohi-ai-agentray). So the walk
//     descends `children` only, and `parent` is display-only: a second traversal
//     source would invent members the parent never declared.
//   - Edges leave the subtree. A child's `blocked_by` can name a standalone
//     root in another part of the project, and a child can even block its own
//     parent (`bs-eibysqho.blocked_by = [bs-j8jh2rt1]`). Those are resolved one
//     hop and marked `External`; expanding them reached a large fraction of the
//     project on the real 60-ticket graph, which is why the bound is one hop.

// GraphNode is one ticket in a project graph. Every field is serialized as-is:
// the dashboard renders this verbatim rather than re-deriving an edge, a wave or
// a state from it.
type GraphNode struct {
	ID       string `json:"id"`
	Parent   string `json:"parent"`
	Position string `json:"position"`
	Status   string `json:"status"`
	QA       string `json:"qa"`
	ReviewPR string `json:"review_pr"`
	// Children is the ticket's own fan-out. A non-empty list on a node inside
	// the tree is a nested parent — its children are nodes here too, so the
	// panel can point at it without descoping what it shows.
	Children  []string `json:"children"`
	BlockedBy []string `json:"blocked_by"`
	Blocks    []string `json:"blocks"`
	// Wave is the node's dependency layer. External nodes carry -1: they are
	// never layered (they are not in the tree), and a real wave number would
	// put them back inside a lane the panel deliberately keeps separate.
	Wave int `json:"wave"`
	// State is the graph-admission state — done, running, ready, waiting,
	// not_found — which is a different question from `status`: a ticket can be
	// `planned` and still be waiting on a blocker. The raw status rides
	// alongside so nothing is lost by the distinction.
	State string `json:"state"`
	// External marks a node reached by an edge but not a member of the subtree;
	// Dangling marks an id with no ticket record at all.
	External bool `json:"external"`
	Dangling bool `json:"dangling"`
}

// GraphCounts is the panel's header tally, precomputed because "is anything
// dispatchable" is the first question asked of the graph and counting 40 nodes
// in the renderer would be the second place those rules live.
type GraphCounts struct {
	Nodes    int `json:"nodes"`
	Waves    int `json:"waves"`
	Ready    int `json:"ready"`
	Waiting  int `json:"waiting"`
	Done     int `json:"done"`
	Running  int `json:"running"`
	External int `json:"external"`
	Dangling int `json:"dangling"`
}

// Graph is one project DAG rooted at a parent ticket. The root is context, not
// a node (rendering it as a wave-0 card duplicates the page it is on).
type Graph struct {
	Root   string      `json:"root"`
	Nodes  []GraphNode `json:"nodes"`
	Waves  [][]string  `json:"waves"`
	Cycles [][]string  `json:"cycles"`
	Counts GraphCounts `json:"counts"`
}

// Node states. Terminal statuses settle a dependency even when the ticket did
// not succeed: cancelled and duplicate unblock their dependents in every other
// part of babysit, and a graph that kept them open would show a dead project as
// stuck forever.
const (
	StateDone     = "done"
	StateRunning  = "running"
	StateReady    = "ready"
	StateWaiting  = "waiting"
	StateNotFound = "not_found"
)

// settledStatuses are the statuses that count as "this dependency is over", and
// runningStatuses the ones that mean a worker is on it. Both are read through
// the predicates below rather than as maps, so the board, the serving batch and
// the DAG ask one question with one answer.
var settledStatuses = map[string]bool{"done": true, "cancelled": true, "duplicate": true}

// StatusSettled reports whether a lifecycle rung means the ticket is over: its
// dependency is discharged and nothing downstream is waiting on its work. It is
// the model layer's one definition of that set — a status added to the ladder
// cannot leave one reader behind.
func StatusSettled(status string) bool { return settledStatuses[status] }

// runningStatuses are the statuses that mean a worker is on it.
var runningStatuses = map[string]bool{"in_progress": true, "in_review": true}

// The human override axis, which lives beside `status` and never rewrites it
// (see internal/cmd/ticket_control.go): a cancelled ticket keeps the rung it
// was parked at. Admission has to read both axes, or twenty real records —
// control-cancelled, still parked on `triage`/`planned` — would be offered as
// dispatchable work the moment they are rendered.
const (
	ControlPaused    = "paused"
	ControlCancelled = "cancelled"
)

// plainID reports whether an id can name a ticket inside the project. An id
// carrying a path separator, or one of the traversal components, is a path and
// not a ticket: joining it would read a record outside <projectHome>/tickets and
// render another project's graph under this project's name. The dashboard route
// guard (`badDashSlug`, `idRe`) applies the same rule to the ids it accepts.
func plainID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

// BuildGraph reads the project graph rooted at root. A missing root record is an
// error — the caller asked about a specific ticket and silence would read as
// "no children"; every other gap (a dangling child, a missing blocker) is data,
// not an error.
//
// A root whose record exists but cannot be parsed is the same error to the
// caller, carrying the read failure rather than a claim that nothing is there.
func BuildGraph(projectHome, root string) (Graph, error) {
	g := Graph{Root: root, Nodes: []GraphNode{}, Waves: [][]string{}, Cycles: [][]string{}}

	home := func(id string) string { return filepath.Join(projectHome, "tickets", id) }
	docs := map[string]Doc{}
	readErrs := map[string]error{}
	readDoc := func(id string) (Doc, bool) {
		if d, ok := docs[id]; ok {
			return d, d != nil
		}
		if !plainID(id) {
			docs[id] = nil
			readErrs[id] = fmt.Errorf("%q is not a ticket id", id)
			return nil, false
		}
		d, err := ReadDocStrict(filepath.Join(home(id), "index.json"))
		if err != nil {
			docs[id] = nil
			readErrs[id] = err
			return nil, false
		}
		docs[id] = d
		return d, true
	}

	rootDoc, ok := readDoc(root)
	if !ok {
		// The strict error rides out with the root: a record that exists but
		// does not parse is not the same fact as a ticket that is not there, and
		// the caller's message ("no record at <path>") would be a lie.
		if err := readErrs[root]; err != nil {
			return g, err
		}
		return g, os.ErrNotExist
	}

	// ─── the subtree: descend `children`, breadth-first, cycle-safe ──────────
	var members []string
	seen := map[string]bool{root: true}
	frontier := rootDoc.Children()
	for len(frontier) > 0 {
		var next []string
		for _, id := range frontier {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			members = append(members, id)
			if d, ok := readDoc(id); ok {
				next = append(next, d.Children()...)
			}
		}
		frontier = next
	}

	// ─── nodes, plus the one-hop outside reach of their edges ────────────────
	inTree := map[string]bool{}
	for _, id := range members {
		inTree[id] = true
	}

	node := func(id string, external bool) GraphNode {
		d, ok := readDoc(id)
		n := GraphNode{
			ID: id, Wave: -1, External: external, Dangling: !ok,
			Children: []string{}, BlockedBy: []string{}, Blocks: []string{},
		}
		if !ok {
			n.State = StateNotFound
			return n
		}
		n.Parent = d.Get("parent")
		n.Position = d.Get("origin.position")
		n.Status = d.Get("status")
		n.Children = d.Children()
		n.BlockedBy = strList(d.Value("relations.blocked_by"))
		n.Blocks = strList(d.Value("relations.blocks"))
		n.QA = VerdictStatusAt(filepath.Join(home(id), "verdicts", "qa.md"))
		n.ReviewPR = VerdictStatusAt(filepath.Join(home(id), "verdicts", "review-pr.md"))
		return n
	}

	byID := map[string]*GraphNode{}
	for _, id := range members {
		n := node(id, false)
		byID[id] = &n
	}

	// Dependencies are recorded on both sides — the dependent's blocked_by and
	// the blocker's blocks — and a record may write only one of them. Reading
	// both and deduping is what keeps a half-linked relation from silently
	// removing an edge from the picture. A self-edge is kept: a ticket that
	// blocks itself can never start, and the residue pass reports it as the
	// cycle it is.
	deps := map[string]map[string]bool{} // dependent -> blockers
	addDep := func(dependent, blocker string) {
		if dependent == "" || blocker == "" {
			return
		}
		if deps[dependent] == nil {
			deps[dependent] = map[string]bool{}
		}
		deps[dependent][blocker] = true
	}

	var externals []string
	extSeen := map[string]bool{}
	noteExternal := func(id string) {
		if id == "" || inTree[id] || extSeen[id] {
			return
		}
		extSeen[id] = true
		externals = append(externals, id)
	}

	for _, id := range members {
		n := byID[id]
		for _, dep := range n.BlockedBy {
			addDep(id, dep)
			if !inTree[dep] {
				noteExternal(dep)
			}
		}
		// `blocks` is read for layering only: a relation whose outside end is the
		// dependent leaves no blocker to discover — only the members' blocked_by
		// can name a blocker outside the subtree, and a project-wide scan for the
		// reverse side is the unbounded expansion this walk deliberately refuses.
		for _, down := range n.Blocks {
			addDep(down, id)
		}
	}

	// A member's serialized edges are the ones the model just used — both ways.
	// A relation written only on the blocker's `blocks` side still moves the
	// dependent's wave and state, so leaving `blocked_by` as the raw field would
	// print a WAITING node with no visible reason for waiting; and the reverse
	// field is filled from the same set, so a relation written on only one side
	// does not leave the other card's `blocks` silently half-populated.
	blocks := map[string][]string{}
	for id := range deps {
		if !inTree[id] {
			continue // an outside dependent is not part of this graph
		}
		for blocker := range deps[id] {
			blocks[blocker] = append(blocks[blocker], id)
		}
	}
	for _, id := range members {
		list := make([]string, 0, len(deps[id]))
		for blocker := range deps[id] {
			list = append(list, blocker)
		}
		sort.Strings(list)
		byID[id].BlockedBy = list

		down := blocks[id]
		sort.Strings(down)
		if down == nil {
			down = []string{}
		}
		byID[id].Blocks = down
	}

	// ─── waves: longest path over dependency edges inside the tree ───────────
	//
	// Kahn by layer rather than DFS: it hands back the layering and the residue
	// in one pass, and the residue is exactly the cycle set. Both orders are
	// sorted so a re-run of the same project prints the same graph.
	//
	// Sort key for a fan-out: the planner's `origin.position` when the record
	// carries one, then the id. A numbered position orders numerically, so
	// `origin.position` 10 does not sort before 9; a position that is not a
	// number orders lexically, and the two kinds never mix inside one comparison.
	// Switching between numeric and lexical comparison per pair would be
	// non-transitive — `sort.Slice` requires a strict weak order and would be
	// free to return any arrangement at all.
	type posKey struct {
		numbered bool
		number   int
		raw      string
		id       string
	}
	key := func(id string) posKey {
		// A ticket with no position keys on its own id, the way the planner's
		// fallback order reads: the id is the last tiebreak anyway, and an empty
		// key would sort it ahead of a sibling that carries a non-numeric
		// position — a real value, since `origin.position` is free text.
		k := posKey{id: id, raw: id}
		if n := byID[id]; n != nil && n.Position != "" {
			k.raw = n.Position
			if v, err := strconv.Atoi(n.Position); err == nil {
				k.numbered, k.number = true, v
			}
		}
		return k
	}
	less := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			a, b := key(ids[i]), key(ids[j])
			switch {
			case a.numbered != b.numbered:
				return a.numbered // a real position leads a missing one
			case a.numbered && a.number != b.number:
				return a.number < b.number
			case !a.numbered && a.raw != b.raw:
				return a.raw < b.raw
			}
			return a.id < b.id
		})
	}

	// In-degree over the deduped edge set. `deps` is already a set-of-sets, so a
	// relation recorded on both sides — or twice on one — counts once.
	inDegree := map[string]int{}
	for dependent, blockers := range deps {
		if !inTree[dependent] {
			continue // an outside dependent cannot be layered
		}
		for blocker := range blockers {
			if inTree[blocker] {
				inDegree[dependent]++
			}
		}
	}

	layer := 0
	unlayered := map[string]bool{}
	for _, id := range members {
		unlayered[id] = true
	}
	for len(unlayered) > 0 {
		var ready []string
		for id := range unlayered {
			if inDegree[id] == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			break // the rest are in cycles — handled below
		}
		less(ready)
		for _, id := range ready {
			byID[id].Wave = layer
			delete(unlayered, id)
		}
		for _, id := range ready {
			for _, other := range members {
				if unlayered[other] && deps[other][id] {
					inDegree[other]--
				}
			}
		}
		g.Waves = append(g.Waves, ready)
		layer++
	}
	if len(unlayered) > 0 {
		// The residue is unorderable, not hidden: it becomes one final lane and
		// its edges are reported as cycles below. Assigning it a lane keeps the
		// model total, so the renderer never has to special-case a node.
		var rest []string
		for id := range unlayered {
			rest = append(rest, id)
		}
		less(rest)
		for _, id := range rest {
			byID[id].Wave = layer
		}
		g.Waves = append(g.Waves, rest)
		g.Cycles = findCycles(rest, deps, inTree)
	}

	// ─── states ──────────────────────────────────────────────────────────────
	//
	// A blocker is judged by its *record*, never by the state already written to
	// a node: the two are computed in one pass, so consulting a neighbour's
	// State would make the answer depend on traversal order. Readmission is
	// deliberately non-transitive — a dependent is ready only once its blockers
	// are settled, so a chain of planned tickets resolves bottom-up on its own.
	//
	// A blocker outside the tree is still a blocker, so its record is read here
	// (one hop): "waiting on bs-x" should mean "bs-x is done" or "bs-x is gone",
	// not "unknown".
	//
	// Both axes are read. `status` is the lifecycle rung; `control` is the human
	// override, which never rewrites it — a control-cancelled ticket is still
	// parked on whatever rung it was dropped at. Reading status alone would
	// offer that ticket as ready work, and would keep its dependents waiting on
	// a dependency a human has already settled.
	admission := func(status, control string) string {
		switch {
		case settledStatuses[status], control == ControlCancelled:
			return StateDone
		case control == ControlPaused:
			// Pause deliberately leaves the rung alone, and a controlled ticket
			// refuses to dispatch a new attempt at all
			// (internal/cmd/ticket_control.go). So a paused ticket is never
			// running — whatever rung it was parked at, including
			// `in_progress` — and never ready.
			return StateWaiting
		case runningStatuses[status]:
			return StateRunning
		case status == "blocked":
			// An explicit block is the ticket saying so; a graph that read it
			// as ready would contradict the record it is displaying.
			return StateWaiting
		default:
			return StateReady
		}
	}
	stateOf := func(id string) string {
		d, ok := readDoc(id)
		if !ok {
			return StateNotFound
		}
		return admission(d.Get("status"), d.Get("control.state"))
	}

	for _, id := range members {
		n := byID[id]
		if n.Dangling {
			// A child declared by `children` with no record is not "ready" — it
			// is a broken edge, and the whole point of showing it is to say so.
			n.State = StateNotFound
			continue
		}
		if state := stateOf(id); state != StateReady {
			n.State = state
			continue
		}
		// READY is the only state that depends on the neighbours, and it is the
		// question the graph exists to answer: can this be dispatched now.
		n.State = StateReady
		for blocker := range deps[id] {
			if stateOf(blocker) != StateDone {
				n.State = StateWaiting
				break
			}
		}
	}

	// ─── assemble ────────────────────────────────────────────────────────────
	for _, id := range members {
		g.Nodes = append(g.Nodes, *byID[id])
	}
	less(externals)
	for _, id := range externals {
		n := node(id, true)
		n.State = stateOf(id)
		// An outside node is one hop of context, not a member: its own record's
		// relations belong to a graph this one deliberately does not expand.
		// Carrying them would hand every renderer a second hop to leak — the SPA
		// did — plus edges whose far end the model never counted.
		n.BlockedBy, n.Blocks = []string{}, []string{}
		g.Nodes = append(g.Nodes, n)
	}

	c := &g.Counts
	c.Nodes = len(members)
	c.Waves = len(g.Waves)
	c.External = len(externals)
	for _, n := range g.Nodes {
		if n.Dangling {
			c.Dangling++
		}
		if n.External {
			continue
		}
		switch n.State {
		case StateReady:
			c.Ready++
		case StateWaiting:
			c.Waiting++
		case StateDone:
			c.Done++
		case StateRunning:
			c.Running++
		}
	}
	return g, nil
}

// findCycles walks the residue of the layering — the nodes Kahn could not order
// — and returns each cycle it closes, normalized to start at its smallest id so
// the same cycle found from two entry points prints once.
//
// Only the residue is searched: a node the layering placed has no path back to
// itself, so searching the whole graph would cost more and find nothing.
func findCycles(residue []string, deps map[string]map[string]bool, inTree map[string]bool) [][]string {
	rest := map[string]bool{}
	for _, id := range residue {
		rest[id] = true
	}

	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var stack []string
	var out [][]string
	var seen = map[string]bool{}

	var visit func(id string)
	visit = func(id string) {
		color[id] = grey
		stack = append(stack, id)
		var next []string
		for blocker := range deps[id] {
			if inTree[blocker] && rest[blocker] {
				next = append(next, blocker)
			}
		}
		sort.Strings(next)
		for _, b := range next {
			switch color[b] {
			case grey:
				// Found a cycle: the slice of the stack from b back to the top.
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == b {
						cyc := append([]string{}, stack[i:]...)
						out = appendUniqueCycle(out, seen, cyc)
						break
					}
				}
			case white:
				visit(b)
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}

	sorted := append([]string{}, residue...)
	sort.Strings(sorted)
	for _, id := range sorted {
		if color[id] == white {
			visit(id)
		}
	}
	return out
}

// appendUniqueCycle normalizes a cycle to start at its smallest id and appends
// it when that shape has not been recorded yet.
func appendUniqueCycle(out [][]string, seen map[string]bool, cyc []string) [][]string {
	if len(cyc) == 0 {
		return out
	}
	min := 0
	for i, id := range cyc {
		if id < cyc[min] {
			min = i
		}
	}
	rot := append(append([]string{}, cyc[min:]...), cyc[:min]...)
	// A cycle of two nodes reached from either side normalizes to the same
	// rotation; a longer one can be found in either direction only if the edges
	// run both ways, which is a different cycle and is kept.
	key := strings.Join(rot, "\x00")
	if seen[key] {
		return out
	}
	seen[key] = true
	return append(out, rot)
}

// Children reads the record's `children` as a list of ids, tolerating a bare
// string for the one-child case. Exported because the dashboard asks the same
// question when it decides whether a ticket gets a DAG tab: two parsers of one
// field would let the CLI show a graph the tab hides.
func (d Doc) Children() []string { return strList(d.Value("children")) }

// strList reads a Doc value that should be a list of ids. A bare string reads as
// a one-element list — a record may legitimately carry `blocked_by: "bs-x"` —
// and anything else (a missing key, a number, an object) reads as empty rather
// than failing the whole graph: the graph degrades one edge, it does not
// disappear.
func strList(v interface{}) []string {
	out := []string{}
	switch x := v.(type) {
	case []interface{}:
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range x {
			if s != "" {
				out = append(out, s)
			}
		}
	case string:
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// DAGRoots lists the project's graph roots — tickets that declare children —
// sorted by id. It is the default subject set for `bbs ticket dag` invoked
// without a ticket, where printing every DAG is more useful than refusing.
func DAGRoots(projectHome string) []string {
	ids, err := TicketIDs(projectHome)
	if err != nil {
		return nil
	}
	var out []string
	for _, id := range ids {
		d := ReadDoc(filepath.Join(projectHome, "tickets", id, "index.json"))
		if len(d.Children()) > 0 {
			out = append(out, id)
		}
	}
	return out
}
