package recall

// Env-gated diagnostic (DENEB_MISS_DEBUG=1 + a wiki COPY in DENEB_WIKI_DIR):
// prints cue verdict, derived queries, per-source row counts, and direct wiki
// hits for the probe's residual-miss questions. This is the instrument that
// isolated the rarity-gate conjunction blindness and the bare-이어 cue eater;
// keep the question list synced with the current misses when triaging.

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestKoreanMissDebug(t *testing.T) {
	if os.Getenv("DENEB_MISS_DEBUG") == "" {
		t.Skip("diagnostic only — set DENEB_MISS_DEBUG=1")
	}
	store, err := wiki.NewStore(os.Getenv("DENEB_WIKI_DIR"), os.Getenv("DENEB_DIARY_DIR"))
	if err != nil {
		t.Fatalf("wiki: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	questions := []string{
		"올해 하반기 착공 파이프라인 총 용량이 얼마지?",
		"비금도 케이블 관련 마지막 메일이 온 게 언제였지?",
		"모듈 조달 갭 메우려고 새로 뚫은 공급 채널이 어디였지?",
		"해남 신재생단지 삽 언제 떠?",
		"금호타이어",
		"임형철은 어느 회사 담당자야?", // control: also cue-less but renders 7 rows
	}
	for _, q := range questions {
		cue := hasCue(q)
		queries := searchQueries(q)
		block, _ := Build(context.Background(), Params{
			SessionKey: "client:korean-probe", Message: q, FilesToolReachable: true,
		}, Deps{Wiki: store}, logger)
		rows := strings.Count(block, "- source=")
		t.Logf("MISSDBG q=%q cue=%v queries=%v rows=%d blocklen=%d", q, cue, queries, rows, len(block))
		for _, query := range queries {
			report, serr := store.SearchWithOptions(context.Background(), query, 3, wiki.QueryOptions{})
			if serr != nil {
				t.Logf("  WIKIDBG query=%q err=%v", query, serr)
				continue
			}
			for _, h := range report.Results {
				t.Logf("  WIKIDBG query=%q hit=%s score=%.3f", query, h.Path, h.Score)
			}
			if len(report.Results) == 0 {
				t.Logf("  WIKIDBG query=%q ZERO hits", query)
			}
		}
	}
}
