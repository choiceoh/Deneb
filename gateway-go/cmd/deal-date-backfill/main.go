// deal-date-backfill normalizes the 문서 일자 already sitting in the typed deal
// ledger (~/.deneb/wiki/.deals.jsonl) to ISO, so 월별 매출/발주 추이 can bucket
// every row instead of the 42% that happened to be filed as YYYY-MM-DD.
//
// Filing normalizes new rows on its own (domain/wiki/deal_date.go); this is the
// one-off pass over the rows written before it existed. It is idempotent — a
// second run finds the dates already ISO and changes nothing.
//
// Usage:
//
//	deal-date-backfill                # dry run: report every rewrite, touch nothing
//	deal-date-backfill --apply        # rewrite, after copying the ledger aside
//	deal-date-backfill --apply --verbose
//
// Rollback: the run writes .deals.jsonl.bak-<timestamp> next to the ledger, and
// ~/.deneb/wiki is a git repo — `git -C ~/.deneb/wiki checkout .deals.jsonl`
// restores the pre-run state either way.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "deal-date-backfill:", err)
		os.Exit(1)
	}
}

func run() error {
	state := config.ResolveStateDir()
	wikiDir := flag.String("wiki", filepath.Join(state, "wiki"), "wiki directory holding .deals.jsonl")
	diaryDir := flag.String("diary", filepath.Join(state, "diary"), "diary directory (store dependency)")
	apply := flag.Bool("apply", false, "실제로 원장을 다시 씀 (기본: dry run)")
	verbose := flag.Bool("verbose", false, "변환된 행을 전부 출력 (기본: 요약 + 연도 추정분)")
	flag.Parse()

	store, err := wiki.NewStore(*wikiDir, *diaryDir)
	if err != nil {
		return fmt.Errorf("open wiki store: %w", err)
	}
	rep, err := store.BackfillDealDates(*apply)
	if err != nil {
		return err
	}

	mode := "DRY RUN — 아무것도 바꾸지 않음 (--apply 로 실행)"
	if *apply {
		mode = "APPLIED"
	}
	fmt.Printf("== deal-date-backfill (%s) ==\n", mode)
	fmt.Printf("원장: %s\n", filepath.Join(*wikiDir, ".deals.jsonl"))
	fmt.Printf("행 %d개 · 이미 ISO %d개 · 변환 %d건(%d행) · 미해결 %d건\n",
		rep.Rows, rep.AlreadyISO, len(rep.Changed), rep.RowsWritten, len(rep.Unresolved))

	inferred := 0
	for _, c := range rep.Changed {
		if c.YearInferred {
			inferred++
		}
	}
	if inferred > 0 {
		// The only conversions that are a judgement rather than a re-spelling:
		// print them always, even without --verbose, so they get eyeballed.
		fmt.Printf("\n-- 연도 추정 %d건 (원문에 연도 없음 → 파일링 시각 기준) --\n", inferred)
		for _, c := range rep.Changed {
			if c.YearInferred {
				fmt.Printf("  L%-4d %-14s %s → %s  (%s)\n", c.Line, c.Field, c.From, c.To, c.Counterparty)
			}
		}
	}
	if *verbose && len(rep.Changed) > 0 {
		fmt.Printf("\n-- 변환 %d건 --\n", len(rep.Changed))
		for _, c := range rep.Changed {
			fmt.Printf("  L%-4d %-14s %-24s → %s  (%s)\n", c.Line, c.Field, c.From, c.To, c.Counterparty)
		}
	}
	if len(rep.Unresolved) > 0 {
		fmt.Printf("\n-- 미해결 %d건 (원문 유지 — 단일 날짜가 없는 표기) --\n", len(rep.Unresolved))
		for _, c := range rep.Unresolved {
			fmt.Printf("  L%-4d %-14s %-24s (%s)\n", c.Line, c.Field, c.From, c.Counterparty)
		}
	}
	if rep.BackupPath != "" {
		fmt.Printf("\n백업: %s\n", rep.BackupPath)
		fmt.Printf("되돌리기: cp %s %s\n", rep.BackupPath, filepath.Join(*wikiDir, ".deals.jsonl"))
	}
	return nil
}
