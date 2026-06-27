// bm25_rarity_floor_test.go — REGRESSION GATE for the lexical (BM25) recall
// leak, the SYMMETRIC counterpart to the semantic-only floor
// (TestMergeSearchResults_SemanticOnlyFloorOverride in semantic_test.go).
//
// The leak: the broadening penalty in the recall preflight only DEMOTES a
// term-only straggler (×0.7) and never fires for a single-/common-only query at
// all, so a query whose only matchable token is a corpus-common noun ("보고",
// "일정") returned off-topic pages at full BM25 score. Unlike the semantic side,
// the gate here keys on the query term's corpus RARITY (NormalizedRarity), not a
// raw score threshold — because a single LEXICAL match can be a strong signal
// when the term is rare (a 거래처명/고유명사 in one page is a legitimate one-term
// recall), whereas a single weak SEMANTIC match is always noise. So the gate is
// deliberately more conservative: it floors common-only queries while preserving
// every rare-anchor query, and never touches a semantically-confirmed hit.
//
// Run: go test ./internal/domain/wiki/ -run BM25Rarity -v
package wiki

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// buildRarityCorpus builds an N≥bm25GateMinCorpus pure-BM25 corpus (no embedder)
// where a common noun appears in a fraction of pages (high df → low rarity) and
// each rare proper noun in exactly one page (df=1 → rarity 1.0).
func buildRarityCorpus(t *testing.T) (*Store, int) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	n := 0
	// 60 filler pages, ~third carrying the common noun "보고" → df≈20/63 (~0.32 of
	// N): comfortably above bm25GateMinCorpus, common enough to read sub-floor.
	for i := 0; i < 60; i++ {
		n++
		body := fmt.Sprintf("일반 업무 문서 본문 채우기 텍스트 페이지 %d", n)
		if i%3 == 0 {
			body += " 월간 보고 정리 보고 라인 점검"
		}
		if err := store.WritePage(fmt.Sprintf("업무/f%03d.md", n), &Page{
			Meta: Frontmatter{
				ID: fmt.Sprintf("f%03d", n), Title: fmt.Sprintf("문서 %d", n),
				Summary: "업무 문서 요약",
			}, Body: body,
		}); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
	}
	// Rare proper-noun pages (df=1 each) — legitimate single-term recall targets.
	for path, pg := range map[string]*Page{
		"거래/mabasolar.md": {Meta: Frontmatter{
			ID: "mabasolar", Title: "마바솔라 거래 메모", Category: "거래",
			Summary: "마바솔라 루프탑 RE100 거래",
		}, Body: "마바솔라 루프탑 RE100 거래 진행."},
		"거래/ganae.md": {Meta: Frontmatter{
			ID: "ganae", Title: "가나에너지 협업", Category: "거래",
			Summary: "가나에너지 케이블 협업",
		}, Body: "가나에너지 케이블 협업 진행."},
	} {
		n++
		if err := store.WritePage(path, pg); err != nil {
			t.Fatalf("WritePage %s: %v", path, err)
		}
	}
	return store, n
}

func searchPaths(t *testing.T, store *Store, q string, limit int) []SearchResult {
	t.Helper()
	res, err := store.Search(context.Background(), q, limit)
	if err != nil {
		t.Fatalf("Search(%q): %v", q, err)
	}
	return res
}

// TestBM25RarityFloor_CommonNounLeakExcluded is the core gate: at realistic N, a
// common-only query (single corpus-common noun) returns NOTHING — the off-topic
// pages it lexically matched are floored. With the floor disabled (env override
// to 0) the SAME query returns those pages: the leak→no-leak transition.
func TestBM25RarityFloor_CommonNounLeakExcluded(t *testing.T) {
	store, n := buildRarityCorpus(t)
	if n < bm25GateMinCorpus {
		t.Fatalf("corpus N=%d below gate min %d — gate would be off", n, bm25GateMinCorpus)
	}

	// Measure the discriminator the gate keys on.
	rCommon := store.fts.queryMaxRarity("보고")
	rRare := store.fts.queryMaxRarity("마바솔라")
	t.Logf("N=%d  rarity(보고)=%.3f  rarity(마바솔라)=%.3f  floor=%.2f", n, rCommon, rRare, bm25RarityFloor)
	if rCommon >= bm25RarityFloor {
		t.Fatalf("common noun 보고 rarity %.3f should be below floor %.2f", rCommon, bm25RarityFloor)
	}
	if rRare < bm25RarityFloor {
		t.Fatalf("rare noun 마바솔라 rarity %.3f should be above floor %.2f", rRare, bm25RarityFloor)
	}

	// Floor ON (default): the common-only query is floored → no off-topic pages.
	if res := searchPaths(t, store, "보고", 5); len(res) != 0 {
		t.Errorf("LEAK: common-only query '보고' admitted %d pages (want 0): %+v", len(res), res)
	}

	// Floor OFF (override 0): the SAME query leaks the off-topic pages — proves
	// the gate, not the corpus, is what excludes them.
	t.Setenv("DENEB_WIKI_BM25_RARITY_FLOOR", "0.0001")
	leaked := searchPaths(t, store, "보고", 5)
	if len(leaked) == 0 {
		t.Errorf("with the floor disabled the common noun must leak its lexical matches (proves transition)")
	} else {
		t.Logf("floor-off leak reproduced: %d pages, top score=%.3f", len(leaked), leaked[0].Score)
	}
}

// TestBM25RarityFloor_RareNounSurvives is the over-block guard: a LEGITIMATE
// single rare-proper-noun query must still surface its page. A blunt "drop all
// single-term hits" rule would kill this; the rarity-keyed gate keeps it.
func TestBM25RarityFloor_RareNounSurvives(t *testing.T) {
	store, _ := buildRarityCorpus(t)
	for _, tc := range []struct{ q, want string }{
		{"마바솔라", "거래/mabasolar.md"},
		{"가나에너지", "거래/ganae.md"},
	} {
		res := searchPaths(t, store, tc.q, 5)
		found := false
		for _, r := range res {
			if r.Path == tc.want {
				found = true
			}
		}
		if !found {
			t.Errorf("over-block: legitimate rare-noun query %q dropped its page %q (got %+v)", tc.q, tc.want, res)
		}
	}
}

// TestBM25RarityFloor_SmallCorpusGateOff proves the conservative small-corpus
// guard: below bm25GateMinCorpus the gate never engages, so a tiny wiki whose
// every term is technically "common" (large df fraction) still returns hits.
func TestBM25RarityFloor_SmallCorpusGateOff(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// 2 pages both about the same common terms — rarity is ~0.26 (sub-floor), but
	// N=2 < gate min so the gate is OFF and both still surface.
	for p, pg := range map[string]*Page{
		"운영시스템/p1.md": {
			Meta: Frontmatter{ID: "p1", Title: "게이트웨이 포트 정책", Summary: "게이트웨이 포트는 18789"},
			Body: "게이트웨이 포트는 18789를 사용한다.",
		},
		"운영시스템/p2.md": {
			Meta: Frontmatter{ID: "p2", Title: "게이트웨이 포트 변경", Summary: "게이트웨이 포트는 19000"},
			Body: "게이트웨이 포트는 19000으로 변경되었다.",
		},
	} {
		if err := store.WritePage(p, pg); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
	}
	if got := store.fts.docCount(); got >= bm25GateMinCorpus {
		t.Fatalf("test premise broken: N=%d not below gate min %d", got, bm25GateMinCorpus)
	}
	if res := searchPaths(t, store, "게이트웨이 포트", 5); len(res) < 2 {
		t.Errorf("small-corpus gate-off: legitimate query dropped (got %d, want >=2): %+v", len(res), res)
	}
}

// TestBM25RarityFloor_Override confirms the DENEB_WIKI_BM25_RARITY_FLOOR override
// moves the gate and a malformed value falls back to the default.
func TestBM25RarityFloor_Override(t *testing.T) {
	if got := bm25RarityFloorValue(); got != bm25RarityFloor {
		t.Fatalf("default = %.3f, want %.3f", got, bm25RarityFloor)
	}
	t.Setenv("DENEB_WIKI_BM25_RARITY_FLOOR", "0.9")
	if got := bm25RarityFloorValue(); got != 0.9 {
		t.Errorf("override 0.9 = %.3f", got)
	}
	t.Setenv("DENEB_WIKI_BM25_RARITY_FLOOR", "not-a-number")
	if got := bm25RarityFloorValue(); got != bm25RarityFloor {
		t.Errorf("malformed override should fall back to default, got %.3f", got)
	}
	t.Setenv("DENEB_WIKI_BM25_RARITY_FLOOR", "1.5") // out of (0,1]
	if got := bm25RarityFloorValue(); got != bm25RarityFloor {
		t.Errorf("out-of-range override should fall back to default, got %.3f", got)
	}
}

// TestBM25RarityFloor_SemanticConfirmedSurvives proves the gate never drops a
// lexical hit that semantic similarity confirms: even for a common-only query,
// a page whose cosine >= semSupportThreshold is kept (mergeSearchResults path).
func TestBM25RarityFloor_SemanticConfirmedSurvives(t *testing.T) {
	bm25 := []SearchResult{{Path: "confirmed.md", Score: 0.4}, {Path: "weak.md", Score: 0.4}}
	sem := []SearchResult{
		{Path: "confirmed.md", Score: 0.65}, // >= semSupportThreshold: confirmed → kept
		{Path: "weak.md", Score: 0.20},      // < threshold + common-only query → dropped
	}
	out := mergeSearchResults(bm25, sem, 10, true /* commonOnlyQuery */)
	has := func(p string) bool {
		for _, r := range out {
			if r.Path == p {
				return true
			}
		}
		return false
	}
	if !has("confirmed.md") {
		t.Errorf("semantically-confirmed lexical hit must survive a common-only query")
	}
	if has("weak.md") {
		t.Errorf("unconfirmed lexical hit on a common-only query must be dropped")
	}
}
