package textsearch

import (
	"fmt"
	"testing"
)

func TestBasicSearch(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "The quick brown fox")
	idx.Upsert("doc2", "The lazy brown dog")
	idx.Upsert("doc3", "A completely unrelated document")

	hits := idx.Search("brown", 10)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
}

func TestANDSearch(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "brown fox jumps")
	idx.Upsert("doc2", "brown dog sleeps")
	idx.Upsert("doc3", "white fox runs")

	// "brown fox" should only match doc1 (AND mode).
	hits := idx.Search("brown fox", 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 AND hit, got %d", len(hits))
	}
	if hits[0].ID != "doc1" {
		t.Fatalf("expected doc1, got %s", hits[0].ID)
	}
}

func TestORFallback(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "apples and oranges")
	idx.Upsert("doc2", "bananas and grapes")

	// "apples bananas" has no AND match, should fall back to OR.
	hits := idx.Search("apples bananas", 10)
	if len(hits) != 2 {
		t.Fatalf("expected 2 OR hits, got %d", len(hits))
	}
}

func TestHangulSearch(t *testing.T) {
	idx := New()
	idx.Upsert("page1", "DGX Spark 설정 가이드", "GPU 설정 방법을 설명합니다")
	idx.Upsert("page2", "네트워크 설정", "네트워크 인터페이스 구성")

	// Hangul prefix matching: "설정" should match both.
	hits := idx.Search("설정", 10)
	if len(hits) != 2 {
		t.Fatalf("expected 2 Hangul hits, got %d", len(hits))
	}
}

func TestHangulPrefixScoresCandidate(t *testing.T) {
	idx := New()
	idx.Upsert("page1", "라면 레시피", "계란을 넣고 3분 끓입니다")

	hits := idx.Search("레시", 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 Hangul prefix hit, got %d", len(hits))
	}
	if hits[0].ID != "page1" {
		t.Fatalf("expected page1, got %s", hits[0].ID)
	}
	if hits[0].Score <= 0 {
		t.Fatalf("expected positive score, got %f", hits[0].Score)
	}
}

func TestDocFreqAndRarity(t *testing.T) {
	idx := New()
	// 10 docs: "common" in all 10, "rare" in 1. Hangul "보고" in 4 (+ prefix
	// "보고서" counts via Hangul-prefix matching, same as scoring).
	for i := 0; i < 10; i++ {
		fields := []string{fmt.Sprintf("doc %d common word", i)}
		if i == 0 {
			fields = append(fields, "rare unique")
		}
		if i < 3 {
			fields = append(fields, "보고 내용")
		}
		if i == 3 {
			fields = append(fields, "보고서 작성") // prefix of 보고
		}
		idx.Upsert(fmt.Sprintf("d%d", i), fields...)
	}

	if df := idx.DocFreq("common"); df != 10 {
		t.Errorf("DocFreq(common) = %d, want 10", df)
	}
	if df := idx.DocFreq("rare"); df != 1 {
		t.Errorf("DocFreq(rare) = %d, want 1", df)
	}
	// Hangul prefix: "보고" matches 보고(3) + 보고서(1) == 4, mirroring scoring.
	if df := idx.DocFreq("보고"); df != 4 {
		t.Errorf("DocFreq(보고) = %d, want 4 (Hangul prefix)", df)
	}
	if df := idx.DocFreq("absent"); df != 0 {
		t.Errorf("DocFreq(absent) = %d, want 0", df)
	}

	// Rarity: rare (df=1) == 1.0; common (df=N) → 0; absent → 0; order holds.
	if r := idx.NormalizedRarity("rare"); r != 1.0 {
		t.Errorf("rarity(rare df=1) = %.3f, want 1.0", r)
	}
	if r := idx.NormalizedRarity("common"); r > 0.1 {
		t.Errorf("rarity(common df=N) = %.3f, want ~0", r)
	}
	if r := idx.NormalizedRarity("absent"); r != 0 {
		t.Errorf("rarity(absent) = %.3f, want 0", r)
	}
	if idx.NormalizedRarity("보고") <= idx.NormalizedRarity("common") {
		t.Errorf("a mid-frequency term must be rarer than an all-docs term")
	}

	// QueryMaxRarity: max over tokens; a query with a rare anchor reads rare,
	// a common-only query reads ~0, absent tokens contribute nothing.
	if r := idx.QueryMaxRarity("common rare"); r != 1.0 {
		t.Errorf("QueryMaxRarity(common rare) = %.3f, want 1.0 (rare anchor dominates)", r)
	}
	if r := idx.QueryMaxRarity("common word"); r > 0.1 {
		t.Errorf("QueryMaxRarity(common-only) = %.3f, want ~0", r)
	}
	if r := idx.QueryMaxRarity("absent missing"); r != 0 {
		t.Errorf("QueryMaxRarity(all-absent) = %.3f, want 0", r)
	}
	if r := idx.QueryMaxRarity(""); r != 0 {
		t.Errorf("QueryMaxRarity(empty) = %.3f, want 0", r)
	}
}

// TestNormalizedRarityNStable confirms the property the floor relies on: a df==1
// term reads exactly 1.0 at EVERY corpus size, and an all-docs (df==N) term
// reads toward 0 as N grows. At realistic N (≥ the wiki gate's 30-doc minimum)
// the all-docs term is firmly sub-floor; the test also pins the small-N
// coarseness (N=2 → ~0.26) that is exactly why the wiki gate stays OFF below 30.
func TestNormalizedRarityNStable(t *testing.T) {
	build := func(n int) *Index {
		idx := New()
		for i := 0; i < n; i++ {
			f := []string{fmt.Sprintf("everywhere doc%d", i)}
			if i == 0 {
				f = append(f, "onlyhere")
			}
			idx.Upsert(fmt.Sprintf("d%d", i), f...)
		}
		return idx
	}
	// df==1 is exactly 1.0 at any N — the rarest-anchor invariant.
	for _, n := range []int{2, 10, 30, 50, 263} {
		if r := build(n).NormalizedRarity("onlyhere"); r != 1.0 {
			t.Errorf("N=%d: rarity(df=1) = %.3f, want 1.0", n, r)
		}
	}
	// An all-docs term: coarse at tiny N (why the gate is off below 30), firmly
	// sub-floor at realistic N. Monotonically decreasing toward 0 as N grows.
	small := build(2).NormalizedRarity("everywhere")
	if small < 0.20 || small > 0.35 {
		t.Errorf("N=2 df=N rarity = %.3f, expected the coarse ~0.26 (gate-off rationale)", small)
	}
	for _, n := range []int{30, 263} {
		if r := build(n).NormalizedRarity("everywhere"); r > 0.05 {
			t.Errorf("N=%d: df=N rarity = %.3f, want ~0 (firmly sub-floor at realistic N)", n, r)
		}
	}
}

func TestUpsertReplace(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "old content here")

	hits := idx.Search("old", 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for old, got %d", len(hits))
	}

	idx.Upsert("doc1", "new content here")

	hits = idx.Search("old", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits for old after update, got %d", len(hits))
	}

	hits = idx.Search("new", 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for new, got %d", len(hits))
	}
}

func TestRemove(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "hello world")
	idx.Remove("doc1")

	if idx.Len() != 0 {
		t.Fatalf("expected len 0 after remove, got %d", idx.Len())
	}

	hits := idx.Search("hello", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits after remove, got %d", len(hits))
	}
}

func TestLimit(t *testing.T) {
	idx := New()
	for i := 0; i < 20; i++ {
		idx.Upsert(string(rune('a'+i)), "common word")
	}

	hits := idx.Search("common", 5)
	if len(hits) != 5 {
		t.Fatalf("expected 5 hits with limit, got %d", len(hits))
	}
}

func TestSnippet(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "This is a long document about Go programming and testing frameworks")

	hits := idx.Search("testing", 10)
	if len(hits) == 0 {
		t.Fatal("expected at least 1 hit")
	}
	if hits[0].Snippet == "" {
		t.Fatal("expected non-empty snippet")
	}
}

func TestScoreOrdering(t *testing.T) {
	idx := New()
	idx.Upsert("high", "fox fox fox fox fox") // high TF for "fox"
	idx.Upsert("low", "the fox and the dog")  // low TF for "fox"

	hits := idx.Search("fox", 10)
	if len(hits) < 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ID != "high" {
		t.Fatalf("expected 'high' first (higher TF), got %s", hits[0].ID)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatal("expected higher score for document with more term frequency")
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! 안녕하세요")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "hello" || tokens[1] != "world" || tokens[2] != "안녕하세요" {
		t.Fatalf("unexpected tokens: %v", tokens)
	}
}
