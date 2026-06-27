// recall_bm25_leak_measure_test.go — REGRESSION GATE for the lexical (BM25)
// recall leak, end-to-end through the chat recall pipeline. This is the
// SYMMETRIC counterpart to recall_semantic_leak_measure_test.go.
//
// The leak being guarded is STRUCTURAL and was confirmed by measurement: the
// recall broadening penalty (recall_preflight.go) only DEMOTES a term-only
// straggler (×0.7) and is a complete no-op for a single-/common-only query
// (primary==""), so a query whose only matchable token is a corpus-common noun
// ("보고", "일정") injected off-topic wiki pages at confidence high/medium (e.g.
// 6 rows at score 1.55 in a 251-page corpus). The wiki lexical rarity floor
// (search.go bm25RarityFloor) closes it: a common-only query's unconfirmed
// lexical hits are dropped, while a rare-anchor query (거래처명/고유명사) is
// preserved. This runs WITHOUT an embedder — exactly the pure-BM25 path the
// recall_bench corpus exercises, and the path with no semantic confirmation to
// fall back on.
//
// Run: go test ./internal/pipeline/chat/ -run RecallBM25Leak -v
package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// buildBM25LeakStore builds a realistic-N (> wiki gate min corpus) pure-BM25
// wiki: many pages share a common business noun (high df → corpus-common), and
// rare proper-noun pages exist as legitimate single-term recall targets.
func buildBM25LeakStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	n := 0
	for i := 0; i < 60; i++ {
		n++
		body := fmt.Sprintf("일반 업무 문서 본문 채우기 텍스트 페이지 %d", n)
		if i%3 == 0 {
			body += " 월간 보고 정리 보고 라인 점검 일정 공유"
		}
		if err := store.WritePage(fmt.Sprintf("업무/f%03d.md", n), &wiki.Page{
			Meta: wiki.Frontmatter{
				ID: fmt.Sprintf("f%03d", n), Title: fmt.Sprintf("업무 문서 %d", n),
				Summary: "업무 문서 요약",
			}, Body: body,
		}); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
	}
	// Rare proper-noun page (df=1) — a legitimate one-term recall target.
	if err := store.WritePage("거래/mabasolar.md", &wiki.Page{
		Meta: wiki.Frontmatter{
			ID: "mabasolar", Title: "마바솔라 거래 메모", Category: "거래",
			Summary: "마바솔라 루프탑 RE100 거래",
		}, Body: "마바솔라 루프탑 RE100 거래 진행. 결제 6월 말.",
	}); err != nil {
		t.Fatalf("WritePage mabasolar: %v", err)
	}
	return store
}

// TestRecallBM25Leak_CommonNounExcluded is the core e2e gate: a common-only
// recall query (single corpus-common noun, no rare anchor) must inject NO wiki
// row — the off-topic pages it lexically matched are floored. An explicit cue
// gets the honest no-evidence notice instead. With the floor disabled the SAME
// query injects the off-topic pages (the leak), proving the gate is the cause.
func TestRecallBM25Leak_CommonNounExcluded(t *testing.T) {
	store := buildBM25LeakStore(t)

	// Two shapes of the leak, both bypassing the broadening penalty:
	//   primary=="" (single signal term)         → "그때 일정 정리해줘"
	//   degenerate primary (filler terms df==0)   → "전에 보고 좀 봐줘"
	for _, msg := range []string{"그때 일정 정리해줘", "전에 보고 어떻게 했지?"} {
		t.Run(strings.NewReplacer(" ", "_", "?", "").Replace(msg), func(t *testing.T) {
			// Floor ON (default): no wiki rows leak.
			out, _ := buildRecallPreflight(context.Background(),
				RunParams{SessionKey: "client:main", Message: msg},
				runDeps{wikiStore: store}, nil)
			if strings.Contains(out, "source=wiki") {
				var rows []string
				for _, ln := range strings.Split(out, "\n") {
					if strings.Contains(ln, "source=wiki") {
						rows = append(rows, strings.TrimSpace(ln))
					}
				}
				t.Errorf("LEAK: common-only recall %q injected wiki rows:\n%s", msg, strings.Join(rows, "\n"))
			}

			// Floor OFF: the SAME query leaks — proves the transition.
			t.Setenv("DENEB_WIKI_BM25_RARITY_FLOOR", "0.0001")
			leakOut, _ := buildRecallPreflight(context.Background(),
				RunParams{SessionKey: "client:main", Message: msg},
				runDeps{wikiStore: store}, nil)
			if !strings.Contains(leakOut, "source=wiki") {
				t.Errorf("with the floor disabled %q must leak wiki rows (proves the gate, not the corpus, excludes them)", msg)
			} else {
				n := strings.Count(leakOut, "source=wiki")
				t.Logf("floor-off leak reproduced for %q: %d wiki rows", msg, n)
			}
		})
	}
}

// TestRecallBM25Leak_RareNounSurvives is the over-block guard: a legitimate
// single rare-proper-noun recall must still surface its wiki page through the
// full pipeline. The rarity-keyed floor keeps it; a blunt single-term drop would
// not.
func TestRecallBM25Leak_RareNounSurvives(t *testing.T) {
	store := buildBM25LeakStore(t)
	const msg = "전에 마바솔라 건 어떻게 됐지?"
	out, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "client:main", Message: msg},
		runDeps{wikiStore: store}, nil)
	if !strings.Contains(out, "거래/mabasolar.md") {
		t.Errorf("over-block: legitimate rare-noun recall %q dropped its page:\n%s", msg, out)
	}
}
