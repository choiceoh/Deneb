package wiki

import (
	"fmt"
	"path/filepath"
	"testing"
)

// The conjunction escape: a query whose tokens are EACH corpus-common must
// still search when the tokens co-occur in few pages — the max-rarity gate
// alone un-searches exactly the best-documented subjects (measured: 4 of the
// Korean probe's 7 residual misses were multi-token all-common queries with
// zero rendered rows). The leak stays closed: commons that co-occur
// everywhere remain gated.
func TestBM25RarityFloor_ConjunctionEscape(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	n := 0
	write := func(body string) {
		n++
		if err := store.WritePage(fmt.Sprintf("업무/c%03d.md", n), &Page{
			Meta: Frontmatter{ID: fmt.Sprintf("c%03d", n), Title: fmt.Sprintf("문서 %d", n), Summary: "요약"},
			Body: body,
		}); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
	}
	// 30 pages carrying "정산" alone, 30 carrying "회의" alone — both tokens
	// individually common (df≈31/61) — plus ONE page where they co-occur.
	for i := 0; i < 30; i++ {
		write("월말 정산 자료 채우기 본문")
	}
	for i := 0; i < 30; i++ {
		write("주간 회의 자료 채우기 본문")
	}
	write("특별 정산 회의 결과: 해남 프로젝트 정산 기준 확정")
	target := fmt.Sprintf("업무/c%03d.md", n)

	if n+1 < bm25GateMinCorpus {
		t.Fatalf("corpus too small for the gate: %d", n)
	}

	rMax := store.fts.queryMaxRarity("정산 회의")
	rConj := store.fts.queryConjunctionRarity("정산 회의")
	t.Logf("rarity(max)=%.3f rarity(conj)=%.3f floor=%.2f", rMax, rConj, bm25RarityFloor)
	if rMax >= bm25RarityFloor {
		t.Fatalf("fixture broke: both tokens must be individually common (max %.3f)", rMax)
	}
	if rConj < bm25RarityFloor {
		t.Fatalf("conjunction of a near-unique pair must read rare, got %.3f", rConj)
	}

	res := searchPaths(t, store, "정산 회의", 5)
	found := false
	for _, r := range res {
		if r.Path == target {
			found = true
		}
	}
	if !found {
		t.Fatalf("conjunction escape must surface the co-occurrence page, got %+v", res)
	}

	// Commons that co-occur everywhere stay gated: every page carries
	// "자료 본문" together, so the conjunction is as common as the tokens.
	if r := store.fts.queryConjunctionRarity("자료 본문"); r >= bm25RarityFloor {
		t.Fatalf("everywhere-co-occurring commons must stay below the floor, got %.3f", r)
	}
	if res := searchPaths(t, store, "자료 본문", 5); len(res) != 0 {
		t.Fatalf("LEAK: everywhere-common conjunction admitted %d pages", len(res))
	}
}
