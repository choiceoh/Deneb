// Command denebui-check validates deneb-ui interactive UI blocks for tests and the
// live-test harness. It reads text from stdin, extracts every ```deneb-ui fenced
// block (or treats the whole input as one block when no fence is present), and
// validates each against the schema in internal/pipeline/chat/denebui.
//
// Exit code: 0 = all blocks valid, 1 = issues found, 3 = read error.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read stdin:", err)
		os.Exit(3)
	}
	if code := check(string(data), os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

func check(text string, stdout, stderr io.Writer) int {
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
			continue
		}
		bad++
		fmt.Fprintf(stdout, "block %d: %d issue(s)\n", i, len(issues))
		for _, is := range issues {
			fmt.Fprintf(stdout, "  - %s\n", is)
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
