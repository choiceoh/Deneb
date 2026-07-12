// Command denebui-check validates deneb-ui interactive UI blocks for tests and the
// live-test harness. It reads text from stdin, extracts every ```deneb-ui fenced
// block (or treats the whole input as one block when no fence is present), and
// validates each against the schema in internal/pipeline/chat/denebui.
//
// --stats additionally prints, per block, the composition advisories and a
// node-type histogram — corpus audits over real transcripts aggregate these to
// see which nodes production actually uses and where composition drifts.
//
// Exit code: 0 = all blocks valid, 1 = issues found, 3 = read error.
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
)

func main() {
	stats := false
	for _, a := range os.Args[1:] {
		if a == "--stats" {
			stats = true
		}
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read stdin:", err)
		os.Exit(3)
	}
	if code := check(string(data), stats, os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

func check(text string, stats bool, stdout, stderr io.Writer) int {
	blocks := denebui.ExtractFences(text)
	fenced := len(blocks) > 0
	if !fenced {
		// No fence — treat the whole input as a candidate block so callers can
		// validate raw HTML/legacy-JSON that lost its fence in transit.
		blocks = []string{text}
	}

	bad := 0
	for i, b := range blocks {
		issues, err := denebui.Validate(b)
		if err != nil {
			bad++
			fmt.Fprintf(stdout, "block %d: NOT PARSEABLE: %v\n", i, err)
			continue
		}
		if len(issues) == 0 {
			fmt.Fprintf(stdout, "block %d: VALID\n", i)
		} else {
			bad++
			fmt.Fprintf(stdout, "block %d: %d issue(s)\n", i, len(issues))
			for _, is := range issues {
				fmt.Fprintf(stdout, "  - %s\n", is)
			}
		}
		if stats {
			printBlockStats(b, stdout)
		}
	}
	if !fenced {
		fmt.Fprintln(stderr, "note: no ```deneb-ui fence found; validated raw input as a single block")
	}
	if bad > 0 {
		return 1
	}
	return 0
}

// printBlockStats prints the composition advisories and node-type histogram of
// one HTML-format block ("stats: -" for legacy JSON, which has no projection).
func printBlockStats(body string, w io.Writer) {
	if adv := denebui.CompositionAdvisories(body); len(adv) > 0 {
		fmt.Fprintf(w, "  advisories: %s\n", strings.Join(adv, ","))
	}
	hist := nodeHistogram(body)
	if len(hist) == 0 {
		fmt.Fprintln(w, "  stats: -")
		return
	}
	keys := make([]string, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, hist[k]))
	}
	fmt.Fprintf(w, "  nodes: %s\n", strings.Join(parts, " "))
}

func nodeHistogram(body string) map[string]int {
	body = strings.TrimSpace(body)
	if !denebui.IsHTMLBody(body) {
		return nil
	}
	root, _ := denebui.ParseHTML(body)
	hist := map[string]int{}
	countNodes(root, hist)
	return hist
}

func countNodes(v any, hist map[string]int) {
	switch n := v.(type) {
	case []any:
		for _, e := range n {
			countNodes(e, hist)
		}
	case map[string]any:
		if t, ok := n["type"].(string); ok && t != "" {
			hist[t]++
		}
		countNodes(n["children"], hist)
		countNodes(n["items"], hist)
		if tabs, ok := n["tabs"].([]any); ok {
			for _, t := range tabs {
				if tm, ok := t.(map[string]any); ok {
					countNodes(tm["children"], hist)
				}
			}
		}
	}
}
