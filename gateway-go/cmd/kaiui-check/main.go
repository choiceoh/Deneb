// Command kaiui-check validates kai-ui interactive UI blocks for tests and the
// live-test harness. It reads text from stdin, extracts every ```kai-ui fenced
// block (or treats the whole input as one block when no fence is present), and
// validates each against the schema in internal/pipeline/chat/kaiui.
//
// Exit code: 0 = all blocks valid, 1 = issues found, 3 = read error.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/kaiui"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read stdin:", err)
		os.Exit(3)
	}
	text := string(data)

	blocks := kaiui.ExtractFences(text)
	fenced := len(blocks) > 0
	if !fenced {
		// No fence — treat the whole input as a candidate block so callers can
		// validate raw JSON that lost its fence in transit (e.g. Telegram).
		blocks = []string{text}
	}

	bad := 0
	for i, b := range blocks {
		issues, err := kaiui.Validate(b)
		if err != nil {
			bad++
			fmt.Printf("block %d: NOT JSON: %v\n", i, err)
			continue
		}
		if len(issues) == 0 {
			fmt.Printf("block %d: VALID\n", i)
			continue
		}
		bad++
		fmt.Printf("block %d: %d issue(s)\n", i, len(issues))
		for _, is := range issues {
			fmt.Printf("  - %s\n", is)
		}
	}
	if !fenced {
		fmt.Fprintln(os.Stderr, "note: no ```kai-ui fence found; validated raw input as a single block")
	}
	if bad > 0 {
		os.Exit(1)
	}
}
