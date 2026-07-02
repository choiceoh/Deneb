// wiki-restructure migrates a wiki directory onto the standardized per-project
// layout (프로젝트/<이름>/{대표.md, 로그.md, 기자재/, 메일분석/} — see
// internal/domain/wiki/project_layout.go).
//
// Dry-run by default: prints the ordered action list and every decision the
// rules refused to make. --apply executes. Operator judgment (topic merges,
// junk deletion, event pages folded into 로그.md) goes in a --plan JSON file of
// wiki.RestructureOp entries, applied before the rule passes.
//
// ⚠️ Stop the gateway before --apply: the Store's locking is in-process only,
// and a live gateway holds in-memory search/index state that direct disk
// migration would desynchronize. Restart it afterwards (startup rebuilds the
// FTS index; the tool itself rebuilds index.md).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wiki-restructure:", err)
		os.Exit(1)
	}
}

func run() error {
	home, _ := os.UserHomeDir()
	wikiDir := flag.String("wiki-dir", filepath.Join(home, ".deneb", "wiki"), "wiki root directory")
	planPath := flag.String("plan", "", "optional JSON plan file ([]{op,source,target,note})")
	apply := flag.Bool("apply", false, "execute the migration (default: dry-run report only)")
	flag.Parse()

	var plan []wiki.RestructureOp
	if *planPath != "" {
		data, err := os.ReadFile(*planPath)
		if err != nil {
			return fmt.Errorf("read plan: %w", err)
		}
		if err := json.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("parse plan: %w", err)
		}
	}

	store, err := wiki.NewStore(*wikiDir, "")
	if err != nil {
		return fmt.Errorf("open wiki store: %w", err)
	}
	defer store.Close()

	rep, err := wiki.RestructureProjectLayout(store, plan, *apply)
	if err != nil {
		return err
	}

	mode := "DRY-RUN"
	if rep.Applied {
		mode = "APPLIED"
	}
	fmt.Printf("== wiki-restructure %s — %d actions, %d skipped ==\n", mode, len(rep.Actions), len(rep.Skipped))
	for _, a := range rep.Actions {
		fmt.Println("  " + a)
	}
	if len(rep.Skipped) > 0 {
		fmt.Println("-- needs a plan decision / skipped --")
		for _, s := range rep.Skipped {
			fmt.Println("  " + s)
		}
	}
	if rep.Applied {
		fmt.Printf("-- result: merged=%d moved=%d deleted=%d errors=%d --\n",
			rep.Merged, rep.Moved, rep.Deleted, len(rep.Errors))
		for _, e := range rep.Errors {
			fmt.Println("  ERROR " + e)
		}
		if len(rep.Errors) > 0 {
			return fmt.Errorf("%d actions failed", len(rep.Errors))
		}
	} else {
		fmt.Println("(dry-run — nothing written; re-run with --apply after stopping the gateway)")
	}
	return nil
}
