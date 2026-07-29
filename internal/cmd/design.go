package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/reallongnguyen/babysit/internal/design"
	"github.com/spf13/cobra"
)

// newDesignCmd ports bin/bbs-design as `bbs design` — the design-intelligence
// broker (tokens / suggest / components / ux-check). It parses DESIGN.md
// frontmatter and the design-ui CSV tables natively (no awk/jq/find).
func newDesignCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "design",
		Short:              "design-intelligence broker",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runDesign(args)
		},
	}
}

const designHelp = `bbs-design — design intelligence broker.

Subcommands:
  tokens [--field <path>] [--design <file>]
      Read DESIGN.md YAML frontmatter as JSON. With --field, emit the leaf
      value (e.g. --field colors.primary). Merges repo-root MASTER with
      ticket-scoped override (tickets/<ticket>/design.md) when both exist:
      deep-merge, ticket overrides MASTER per-leaf.

  suggest --product <type> [--data <dir>]
      Emit a JSON block joining products.csv + ui-reasoning.csv +
      colors.csv for the given Product Type. Anti-slop filter is applied.

  components [--root <dir>] [--ext <pattern>]
      Walk a frontend root and list component-shaped files as JSONL
      {name, path, kind}.

  ux-check --category <cat> [--data <dir>]
      Emit ux-guidelines.csv rows for the given category as JSONL.
`

func designDie(format string, a ...interface{}) error {
	fmt.Fprintf(os.Stderr, retarget("bbs-design: ")+format+"\n", a...)
	os.Exit(2)
	return nil
}

func runDesign(args []string) error {
	if len(args) == 0 {
		return designDie("usage: bbs-design <tokens|suggest|components|ux-check> [args]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "tokens":
		return designTokens(rest)
	case "suggest":
		return designSuggest(rest)
	case "components":
		return designComponents(rest)
	case "ux-check":
		return designUXCheck(rest)
	case "-h", "--help":
		fmt.Print(designHelp)
		return nil
	default:
		return designDie("unknown subcommand: %s", sub)
	}
}

// designDataDir mirrors _default_data_dir: prefer the data dir next to the
// binary's repo, then the known plugin/skill install locations.
func designDataDir() string {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		repoRoot := filepath.Dir(filepath.Dir(exe))
		data := filepath.Join(repoRoot, ".claude", "skills", "design-ui", "data")
		if fi, err := os.Stat(data); err == nil && fi.IsDir() {
			return data
		}
	}
	home, _ := os.UserHomeDir()
	for _, plug := range []string{
		filepath.Join(home, ".claude/plugins/marketplaces/babysit/.claude/skills/design-ui/data"),
		filepath.Join(home, ".claude/skills/bbs:design-ui/data"),
		filepath.Join(home, ".claude/skills/design-ui/data"),
	} {
		if fi, err := os.Stat(plug); err == nil && fi.IsDir() {
			return plug
		}
	}
	// Last resort — caller sees the missing-file error.
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(filepath.Dir(exe)), ".claude", "skills", "design-ui", "data")
	}
	return ".claude/skills/design-ui/data"
}

// ─── tokens ──────────────────────────────────────────────────────────────────

func designTokens(args []string) error {
	field, designArg := "", ""
	for len(args) > 0 {
		switch args[0] {
		case "--field":
			if len(args) < 2 {
				return designDie("tokens: --field needs a value")
			}
			field, args = args[1], args[2:]
		case "--design":
			if len(args) < 2 {
				return designDie("tokens: --design needs a value")
			}
			designArg, args = args[1], args[2:]
		default:
			return designDie("tokens: unknown arg %s", args[0])
		}
	}

	master, override := resolveDesignPaths(designArg)
	if master == "" && override == "" {
		return designDie("tokens: no DESIGN.md found (repo root or ticket-scoped)")
	}

	var m, o interface{} = map[string]interface{}{}, map[string]interface{}{}
	if master != "" {
		v, err := design.ParseFrontmatterValue(master)
		if err != nil {
			return designDie("tokens: could not parse %s", master)
		}
		m = v
	}
	if override != "" {
		v, err := design.ParseFrontmatterValue(override)
		if err != nil {
			return designDie("tokens: could not parse %s", override)
		}
		o = v
	}
	merged := design.DeepMerge(m, o)

	if field != "" {
		fmt.Println(designFieldString(navigate(merged, strings.Split(field, "."))))
		return nil
	}
	b, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return designDie("tokens: could not render JSON")
	}
	fmt.Println(string(b))
	return nil
}

// resolveDesignPaths mirrors _resolve_design_paths: master from --design or a
// repo-root DESIGN.md; override from bbs-ticket's ticket-scoped design doc.
func resolveDesignPaths(designArg string) (master, override string) {
	if designArg != "" {
		master = designArg
	} else if fi, err := os.Stat("DESIGN.md"); err == nil && !fi.IsDir() {
		master = "DESIGN.md"
	}
	if _, err := exec.LookPath("bbs-ticket"); err == nil {
		if p := strings.TrimSpace(ticketOut("get", "pointers.design")); p != "" && isFile(p) {
			override = p
		} else if p := strings.TrimSpace(ticketOut("path", "design", "--read")); p != "" && isFile(p) {
			override = p
		}
	}
	return master, override
}

func ticketOut(args ...string) string {
	out, err := exec.Command("bbs-ticket", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// navigate walks a dotted path through nested objects, returning nil when the
// path cannot be followed (matching jq's null-on-missing).
func navigate(v interface{}, keys []string) interface{} {
	for _, k := range keys {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// designFieldString renders a leaf as `jq -r` would: raw string, bare
// number/bool, "null" for null, compact JSON for a composite.
func designFieldString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// ─── suggest ─────────────────────────────────────────────────────────────────

var antiSlop = regexp.MustCompile(`purple|violet|gradient|three-column|3-column|chromatic|glassmorphism.*everywhere|bubbly|bouncy`)

func designSuggest(args []string) error {
	product, dataDir := "", designDataDir()
	for len(args) > 0 {
		switch args[0] {
		case "--product":
			if len(args) < 2 {
				return designDie("suggest: --product needs a value")
			}
			product, args = args[1], args[2:]
		case "--data":
			if len(args) < 2 {
				return designDie("suggest: --data needs a value")
			}
			dataDir, args = args[1], args[2:]
		default:
			return designDie("suggest: unknown arg %s", args[0])
		}
	}
	if product == "" {
		return designDie("suggest: --product <type> required")
	}
	if fi, err := os.Stat(dataDir); err != nil || !fi.IsDir() {
		return designDie("suggest: data dir not found: %s", dataDir)
	}
	pf := filepath.Join(dataDir, "products.csv")
	rf := filepath.Join(dataDir, "ui-reasoning.csv")
	cf := filepath.Join(dataDir, "colors.csv")
	for _, f := range []string{pf, rf, cf} {
		if !isFile(f) {
			return designDie("suggest: missing %s", f)
		}
	}

	products, err := design.ReadCSV(pf)
	if err != nil {
		return designDie("suggest: %v", err)
	}
	var productRow *design.Row
	for i := range products {
		if strings.EqualFold(products[i].Get("Product Type"), product) {
			productRow = &products[i]
			break
		}
	}
	if productRow == nil {
		fmt.Printf("{\"error\":\"product not found: %s\"}\n", product)
		return nil
	}
	pno := productRow.Get("No")

	reasoning, _ := design.ReadCSV(rf)
	var reasoningVal interface{}
	for i := range reasoning {
		if reasoning[i].Get("No") == pno {
			anti := reasoning[i].Get("Anti_Patterns")
			if antiSlop.MatchString(strings.ToLower(anti)) {
				reasoningVal = map[string]interface{}{"reasoning_filtered": true, "anti_pattern": anti}
			} else {
				reasoningVal = reasoning[i].Map()
			}
			break
		}
	}

	colors, _ := design.ReadCSV(cf)
	var colorsVal interface{}
	for i := range colors {
		if colors[i].Get("No") == pno {
			colorsVal = colors[i].Map()
			break
		}
	}

	out := map[string]interface{}{
		"product_no":   productRow.Get("No"),
		"product_type": productRow.Get("Product Type"),
		"product":      productRow.Map(),
		"reasoning":    reasoningVal,
		"colors":       colorsVal,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return nil
}

// ─── components ──────────────────────────────────────────────────────────────

func designComponents(args []string) error {
	root, extPattern := "", ""
	for len(args) > 0 {
		switch args[0] {
		case "--root":
			if len(args) < 2 {
				return designDie("components: --root needs a value")
			}
			root, args = args[1], args[2:]
		case "--ext":
			if len(args) < 2 {
				return designDie("components: --ext needs a value")
			}
			extPattern, args = args[1], args[2:]
		default:
			return designDie("components: unknown arg %s", args[0])
		}
	}

	if root == "" {
		for _, cand := range []string{"components", "src/components", "app/components", "packages/ui/src"} {
			if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
				root = cand
				break
			}
		}
	}
	if root == "" {
		return nil // greenfield — empty inventory
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil
	}

	exts := []string{".tsx", ".jsx", ".vue", ".svelte"}
	if extPattern != "" {
		exts = strings.Split(extPattern, ",")
	}

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		matched := false
		for _, e := range exts {
			if strings.HasSuffix(path, e) {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
		base := filepath.Base(path)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		if name == "" || name[0] < 'A' || name[0] > 'Z' {
			return nil // components lead uppercase by convention
		}
		kind := strings.TrimPrefix(filepath.Ext(base), ".")
		fmt.Printf("{\"name\":\"%s\",\"path\":\"%s\",\"kind\":\"%s\"}\n", name, path, kind)
		return nil
	})
	return nil
}

// ─── ux-check ────────────────────────────────────────────────────────────────

func designUXCheck(args []string) error {
	category, dataDir := "", designDataDir()
	for len(args) > 0 {
		switch args[0] {
		case "--category":
			if len(args) < 2 {
				return designDie("ux-check: --category needs a value")
			}
			category, args = args[1], args[2:]
		case "--data":
			if len(args) < 2 {
				return designDie("ux-check: --data needs a value")
			}
			dataDir, args = args[1], args[2:]
		default:
			return designDie("ux-check: unknown arg %s", args[0])
		}
	}
	if category == "" {
		return designDie("ux-check: --category <name> required")
	}
	f := filepath.Join(dataDir, "ux-guidelines.csv")
	if !isFile(f) {
		return designDie("ux-check: missing %s", f)
	}
	rows, err := design.ReadCSV(f)
	if err != nil {
		return designDie("ux-check: %v", err)
	}
	for i := range rows {
		if strings.EqualFold(rows[i].Get("Category"), category) {
			b, _ := json.Marshal(rows[i].Map())
			fmt.Println(string(b))
		}
	}
	return nil
}
