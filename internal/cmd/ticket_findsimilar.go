package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// runFindSimilar ports find-similar (bbs-ticket.bash:2684-2782): score open
// tickets by containment of the input token bag in each ticket's (slug ∪
// requirement) bag, emitting the top matches as TSV.

// stopWords mirrors the embedded python STOP set.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"to": true, "for": true, "in": true, "on": true, "with": true, "that": true,
	"this": true, "is": true, "are": true, "be": true, "it": true, "its": true,
	"by": true, "as": true, "at": true, "from": true, "into": true, "but": true,
	"not": true, "no": true, "so": true, "if": true, "then": true, "than": true,
	"i": true, "we": true, "you": true, "they": true, "he": true, "she": true,
	"my": true, "your": true, "build": true, "create": true, "add": true,
	"fix": true, "make": true, "do": true, "run": true, "new": true,
	"use": true, "using": true,
}

var (
	tokenRe        = regexp.MustCompile(`[a-z0-9]+`)
	slugFromBranch = regexp.MustCompile(`^[a-z]+/bs-[a-z0-9]+_(.+)$`)
)

func fsTok(text string) map[string]bool {
	out := map[string]bool{}
	for _, p := range tokenRe.FindAllString(strings.ToLower(text), -1) {
		if len(p) > 1 && !stopWords[p] {
			out[p] = true
		}
	}
	return out
}

func runFindSimilar(args []string) {
	fromInput := ""
	limitStr, minStr := "3", "0.6"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from-input":
			fromInput, i = valueAt(args, i), i+1
		case "--from-input-file":
			if b, err := os.ReadFile(valueAt(args, i)); err == nil {
				fromInput = string(b)
			}
			i++
		case "--limit":
			limitStr, i = valueAt(args, i), i+1
		case "--min-score":
			minStr, i = valueAt(args, i), i+1
		}
	}
	if fromInput == "" {
		fmt.Fprintln(os.Stderr, "find-similar: --from-input <text> required")
		os.Exit(2)
	}
	limit, err1 := strconv.Atoi(strings.TrimSpace(limitStr))
	minScore, err2 := strconv.ParseFloat(strings.TrimSpace(minStr), 64)
	if err1 != nil || err2 != nil {
		fmt.Fprintln(os.Stderr, "find-similar: --limit and --min-score must be numeric")
		os.Exit(2)
	}

	env := identity.Resolve()
	tdir := filepath.Join(env.ProjectHome, "tickets")
	if info, err := os.Stat(tdir); err != nil || !info.IsDir() {
		os.Exit(0)
	}

	inputTokens := fsTok(fromInput)
	if len(inputTokens) == 0 {
		os.Exit(0)
	}

	closed := map[string]bool{"done": true, "cancelled": true, "merged": true}
	entries, err := os.ReadDir(tdir)
	if err != nil {
		os.Exit(0)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	type result struct {
		score float64
		tid   string
		slug  string
	}
	var results []result
	for _, e := range entries {
		tid := e.Name()
		idx := filepath.Join(tdir, tid, "index.json")
		if !fileExists(idx) {
			continue
		}
		doc := ticket.ReadDoc(idx)
		if closed[doc.Get("status")] {
			continue
		}
		branch := doc.Get("pointers.branch")
		slug := ""
		if m := slugFromBranch.FindStringSubmatch(branch); m != nil {
			slug = m[1]
		}
		ticketTokens := fsTok(slug)
		if b, err := os.ReadFile(filepath.Join(tdir, tid, "requirement.md")); err == nil {
			for t := range fsTok(string(b)) {
				ticketTokens[t] = true
			}
		}
		if len(ticketTokens) == 0 {
			continue
		}
		inter := 0
		for t := range inputTokens {
			if ticketTokens[t] {
				inter++
			}
		}
		score := float64(inter) / float64(len(inputTokens))
		if score >= minScore {
			display := slug
			if display == "" {
				display = tid
			}
			results = append(results, result{score, tid, display})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].tid < results[j].tid
	})
	for i, r := range results {
		if i >= limit {
			break
		}
		fmt.Printf("%s\t%.3f\t%s\n", r.tid, r.score, r.slug)
	}
	os.Exit(0)
}
