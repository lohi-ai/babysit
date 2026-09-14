package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// dagUsage mirrors the `ticket` family's hand-parsed contract: subcommands
// parse their own argv because the bash original did, and the quirks are part
// of the interface callers script against.
const dagUsage = `usage: bbs-ticket dag [<ticket>] [--mermaid] [--json]

Print a project DAG — the decomposition a parent ticket declares through
` + "`children`" + `, layered into waves by its children's dependency edges
(` + "`relations.blocked_by`/`blocks`" + `).

Bare: the ticket this session is on, when one resolves — its own DAG, which
prints as a leaf when it declares no children — otherwise every DAG in the
project. <ticket>: just that one.
  --mermaid   emit a mermaid block (waves as columns; renders in an OMP
              session, readable as source anywhere else) instead of the
              text listing
  --json      emit the model the text and mermaid forms are both rendered from

Read-only: nothing here writes ticket state.
`

// runDAG is `bbs ticket dag`. It resolves the project from the current identity
// exactly as `board` does, so it works from the primary checkout, a ticket
// worktree, or an agent session that only exported BABYSIT_TICKET.
func runDAG(args []string) {
	var roots []string
	format := "text"
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--mermaid":
			format = "mermaid"
		case "--json":
			format = "json"
		case "-h", "--help", "help":
			fmt.Print(retarget(dagUsage))
			return
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "dag: unknown flag '%s'\n%s", a, retarget(dagUsage))
				os.Exit(2)
			}
			if len(roots) > 0 {
				// One root or none: the batch form is the bare invocation, and a
				// second positional is a typo (a split value) far more often than
				// it is an intent to compare two graphs.
				fmt.Fprintf(os.Stderr, "dag: only one <ticket> may be named; bare lists every DAG in this project\n%s", retarget(dagUsage))
				os.Exit(2)
			}
			roots = append(roots, a)
		}
	}

	env := identity.Resolve()
	if len(roots) == 0 {
		if env.Ticket != "" {
			roots = []string{env.Ticket}
		} else {
			roots = ticket.DAGRoots(env.ProjectHome)
		}
	}
	if len(roots) == 0 {
		fmt.Fprintln(os.Stderr, "dag: no decomposed tickets in this project (no ticket declares `children`)")
		os.Exit(0)
	}

	graphs := make([]ticket.Graph, 0, len(roots))
	for _, root := range roots {
		g, err := ticket.BuildGraph(env.ProjectHome, root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "STATUS: BLOCKED")
			if errors.Is(err, fs.ErrNotExist) {
				// The common case: a typo, or an id from another project.
				fmt.Fprintf(os.Stderr, "REASON: %s has no ticket record at %s\n", root,
					filepath.Join(env.ProjectHome, "tickets", root, "index.json"))
				fmt.Fprintln(os.Stderr, retarget("RECOMMENDATION: check the id (bbs-ticket board), or run `bbs-ticket dag` bare to list this project's DAGs."))
			} else {
				// The record is there and did not read — saying "no record"
				// here would send the human looking for a ticket that exists.
				fmt.Fprintf(os.Stderr, "REASON: %s\n", err)
				fmt.Fprintln(os.Stderr, retarget("RECOMMENDATION: read the record itself (bbs-ticket path index --read), fix what it reports, then re-run."))
			}
			os.Exit(2)
		}
		graphs = append(graphs, g)
	}

	project := filepath.Base(env.ProjectHome)
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(graphs); err != nil {
			fmt.Fprintf(os.Stderr, "dag: %v\n", err)
			os.Exit(1)
		}
	case "mermaid":
		for i, g := range graphs {
			if i > 0 {
				fmt.Println()
			}
			fmt.Print(dagMermaid(g))
		}
	default:
		for i, g := range graphs {
			if i > 0 {
				fmt.Println()
			}
			fmt.Print(dagText(g, project))
		}
	}
}

// dagText renders the wave listing. It is the form for a terminal with no
// mermaid renderer, and it carries the same tally the dashboard panel shows —
// the first question asked of a project graph is "can anything start now".
//
// Everything here stays inside printable ASCII: this is the default output, and
// it is read by terminals, log scrapers and byte-oriented scripts that are not
// promised UTF-8. The mermaid and json forms carry the same information without
// that constraint.
func dagText(g ticket.Graph, project string) string {
	var b strings.Builder
	c := g.Counts
	fmt.Fprintf(&b, "DAG %s (%s) - %d ticket%s, %d wave%s: %d ready, %d waiting, %d running, %d done",
		g.Root, project, c.Nodes, plural(c.Nodes), c.Waves, plural(c.Waves),
		c.Ready, c.Waiting, c.Running, c.Done)
	if c.External > 0 {
		fmt.Fprintf(&b, ", %d outside", c.External)
	}
	if c.Dangling > 0 {
		fmt.Fprintf(&b, ", %d not found", c.Dangling)
	}
	b.WriteString("\n")

	if len(g.Nodes) == 0 {
		b.WriteString("  (no children declared)\n")
		return b.String()
	}

	byID := map[string]ticket.GraphNode{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}

	for i, wave := range g.Waves {
		fmt.Fprintf(&b, "\nWAVE %d  (%d)\n", i, len(wave))
		// The model already ordered this wave (plan position, then id); re-sorting
		// here by id would throw away the planner's intended order.
		for _, id := range wave {
			n, ok := byID[id]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "  %s\n", dagRow(n, byID))
		}
	}

	var outside []ticket.GraphNode
	for _, n := range g.Nodes {
		if n.External {
			outside = append(outside, n)
		}
	}
	if len(outside) > 0 {
		b.WriteString("\nOUTSIDE THIS SUBTREE  (one hop - not expanded)\n")
		for _, n := range outside {
			where := ""
			if n.Parent != "" {
				where = " parent " + n.Parent
			}
			if n.Status != "" {
				where += " status " + n.Status
			}
			fmt.Fprintf(&b, "  %-11s %-14s %s\n", stateLabel(n.State), n.ID, strings.TrimSpace(where))
		}
	}

	if len(g.Cycles) > 0 {
		b.WriteString("\nCYCLE\n")
		for _, cyc := range g.Cycles {
			// The first id closes the loop so the reading is unambiguous.
			fmt.Fprintf(&b, "  %s - these tickets block each other, so no wave can go first.\n",
				strings.Join(append(append([]string{}, cyc...), cyc[0]), " -> "))
		}
	}
	return b.String()
}

// dagRow renders one node. Settled and dangling nodes stay on one line: a done
// ticket has no "why isn't this moving" to answer, and a wall of five-line done
// rows buries the one ticket that can be dispatched.
func dagRow(n ticket.GraphNode, byID map[string]ticket.GraphNode) string {
	pos := "-"
	if n.Position != "" {
		pos = "#" + n.Position
	}
	line := fmt.Sprintf("%-9s %-14s %-5s %-12s", stateLabel(n.State), n.ID, pos, orDefault(n.Status, "-"))
	if n.State == ticket.StateWaiting {
		var waiting []string
		for _, id := range n.BlockedBy {
			dep, ok := byID[id]
			if !ok {
				waiting = append(waiting, id+" (not found)")
				continue
			}
			waiting = append(waiting, fmt.Sprintf("%s (%s)", id, orDefault(dep.Status, "-")))
		}
		if len(waiting) > 0 {
			line += "<- blocked by " + strings.Join(waiting, ", ")
		}
	}
	// Verdicts are the gate a human reads next, so they ride on the rows that
	// can still move; a done ticket's verdicts are history.
	if n.State == ticket.StateReady || n.State == ticket.StateRunning || n.State == ticket.StateWaiting {
		line += fmt.Sprintf("  qa:%s review-pr:%s", orDefault(n.QA, "-"), orDefault(n.ReviewPR, "-"))
	}
	if len(n.Children) > 0 {
		line += fmt.Sprintf("  (%d nested)", len(n.Children))
	}
	return line
}

// mermaidID keeps a mermaid node id to [A-Za-z0-9_] and makes that mapping
// injective: a ticket id carries `-`, which would otherwise parse as edge
// syntax, and a lossy sanitizer would merge two distinct tickets into one node.
// Every character outside the safe set becomes `_x<hex>`, so nothing is
// ambiguous — `bs-a-b` and `bs-a_b` stay two nodes.
func mermaidID(id string) string {
	var b strings.Builder
	b.WriteString("n_")
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "_x%02x", r)
		}
	}
	return b.String()
}

// dagMermaid emits the graph as a mermaid diagram. This is the form a foreman
// session shows: OMP renders a fenced mermaid block in-TUI, and on a harness
// without a renderer the source is still a readable listing.
//
// Waves are laid out left-to-right, each one a column with its members stacked
// inside it (`direction TB`). The obvious `graph TD` puts a 20-child wave in one
// horizontal row — measured at 8156px on the real bs-tbjyth79 graph, which no
// terminal pane can show; columns keep the same graph under 700px wide.
//
// Styling is stroke-only — `fill:none` with a coloured stroke — so the diagram
// reads on both a light and a dark terminal without knowing which one it is on.
func dagMermaid(g ticket.Graph) string {
	var b strings.Builder
	b.WriteString("```mermaid\nflowchart LR\n")

	byID := map[string]ticket.GraphNode{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}

	for i, wave := range g.Waves {
		fmt.Fprintf(&b, "  subgraph w%d[\"WAVE %d\"]\n    direction TB\n", i, i)
		for _, id := range wave {
			fmt.Fprintf(&b, "    %s\n", mermaidNode(id, byID[id]))
		}
		b.WriteString("  end\n")
	}

	// Edges come from the members' normalized `blocked_by`, and every endpoint of
	// one is a node this graph holds: the walk collects a member's blockers as
	// members or as externals, and an outside node carries no edges of its own
	// (the model clears them, so the one-hop bound is not re-derived here).
	seen := map[string]bool{}
	var edges []string
	for _, n := range g.Nodes {
		if n.Dangling {
			continue
		}
		for _, dep := range n.BlockedBy {
			key := dep + "->" + n.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, fmt.Sprintf("  %s --> %s", mermaidID(dep), mermaidID(n.ID)))
		}
	}
	sort.Strings(edges)
	for _, e := range edges {
		b.WriteString(e + "\n")
	}

	// Every edge endpoint is a declared wave member or an outside node, so each
	// id above already carries a declaration and a class.
	for _, n := range g.Nodes {
		if n.External {
			fmt.Fprintf(&b, "  %s\n", mermaidNode(n.ID, n))
		}
	}

	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  class %s %s;\n", mermaidID(n.ID), mermaidClass(n.State))
	}

	b.WriteString("  classDef done stroke:#2f9e44,fill:none,stroke-width:2px;\n")
	b.WriteString("  classDef running stroke:#1971c2,fill:none,stroke-width:2px;\n")
	b.WriteString("  classDef ready stroke:#e8590c,fill:none,stroke-width:2px;\n")
	b.WriteString("  classDef waiting stroke:#868e96,fill:none,stroke-width:1px;\n")
	b.WriteString("  classDef not_found stroke:#c92a2a,fill:none,stroke-width:2px,stroke-dasharray:4 3;\n")
	b.WriteString("```\n")
	return b.String()
}

// mermaidText escapes text that goes inside a mermaid label. Labels are
// delimited by `"`, and `origin.position` is free text a planner writes
// (`bbs ticket init --position`), so an unescaped quote would close the label
// early — turning the rest of the line into diagram syntax — and a newline would
// end the statement outright. `#quot;` is mermaid's own entity escape.
func mermaidText(s string) string {
	s = strings.ReplaceAll(s, `"`, "#quot;")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// mermaidNode declares one node. The label carries the id, its raw status and —
// only when that differs — its admission state, so the diagram answers "what is
// this and can it run" without a second read. A settled ticket would otherwise
// print `done · done`.
func mermaidNode(id string, n ticket.GraphNode) string {
	var bits []string
	if n.Status != "" {
		bits = append(bits, n.Status)
	}
	if n.State != "" && !strings.EqualFold(n.Status, n.State) {
		bits = append(bits, stateLabel(n.State))
	}
	if n.Position != "" {
		bits = append(bits, "#"+n.Position)
	}
	label := id
	if len(bits) > 0 {
		label += "<br/>" + strings.Join(bits, " · ")
	}
	return fmt.Sprintf(`%s["%s"]`, mermaidID(id), mermaidText(label))
}

// mermaidClass maps a state onto the classDef alphabet. `waiting` shares the
// muted stroke with anything unrecognised, so a state added later degrades to
// "not highlighted" rather than to a mermaid parse error.
func mermaidClass(state string) string {
	switch state {
	case ticket.StateDone:
		return "done"
	case ticket.StateRunning:
		return "running"
	case ticket.StateReady:
		return "ready"
	case ticket.StateNotFound:
		return "not_found"
	default:
		return "waiting"
	}
}

// stateLabel is the state as a human reads it: upper-cased, with the underscore
// in `not_found` turned into the space the panel and the cycle callout use.
func stateLabel(state string) string {
	return strings.ToUpper(strings.ReplaceAll(state, "_", " "))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
