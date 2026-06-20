package filestore

import (
	"context"
	"path/filepath"
	"testing"
)

// pathSet returns the set of result paths for easy membership assertions.
func pathSet(hits []ScoredEntry) map[string]bool {
	s := make(map[string]bool, len(hits))
	for _, h := range hits {
		s[h.Entry.PathDisplay] = true
	}
	return s
}

// TestHybridSearch_ExactNameSurvivesBelowFloor is the core hybrid gain: a file
// whose NAME literally contains the query but whose chunk cosine lands in the
// BGE-M3 Korean noise band (~0.6, below the 0.73 floor) is DROPPED by the
// cosine-only Search, but HybridSearch keeps it on the exact-name signal.
func TestHybridSearch_ExactNameSurvivesBelowFloor(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// The file is named after "탑솔라" but its body is generic boilerplate, so the
	// query "탑솔라 계약" has only a name overlap and a sub-floor body cosine.
	const body = "본 문서는 일반적인 안내 사항을 담고 있는 내용입니다"
	mustPut(t, store, "/계약/탑솔라 계약서.txt", body)
	// A second, unrelated file so the corpus has >1 doc for BM25 IDF.
	mustPut(t, store, "/회의/점심 메뉴.txt", "점심 메뉴 커피 음료 목록입니다")

	const query = "탑솔라 계약 관련 문서" // >= minChunkRunes
	// Hand-place the body's chunk at cosine ~0.6 to the query (noise band), and the
	// other file far away. fixedEmbedder maps each exact text to a vector.
	embed := &fixedEmbedder{vecs: map[string][]float32{
		body: {1, 0, 0},
		"점심 메뉴 커피 음료 목록입니다": {0, 1, 0},
		query: {0.6, 0, 0.8}, // cos to body = 0.6 < floor 0.73
	}}

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// Cosine-only Search drops the file (body cosine 0.6 < 0.73 floor).
	semOnly, err := idx.Search(ctx, query, 5, embed)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if pathSet(semOnly)["/계약/탑솔라 계약서.txt"] {
		t.Fatalf("precondition failed: cosine-only Search unexpectedly kept the sub-floor file: %+v", semOnly)
	}

	// HybridSearch keeps it on the exact NAME match ("탑솔라"/"계약" are in the name).
	hits, err := idx.HybridSearch(ctx, query, 5, embed, plainText)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if !pathSet(hits)["/계약/탑솔라 계약서.txt"] {
		t.Fatalf("hybrid dropped the exact-name file (should survive below floor): %+v", hits)
	}
}

// TestHybridSearch_RejectsSemanticNoise pins the floor's noise rejection: a file
// with a 0.6-band cosine AND no lexical overlap with the query is still cut, so
// hybrid doesn't reopen the Korean-noise hole the floor closed.
func TestHybridSearch_RejectsSemanticNoise(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	const fileA = "계약 납기 지연 위약금 조항입니다"
	const fileB = "점심 메뉴 커피 음료 목록입니다"
	mustPut(t, store, "/계약/납기.txt", fileA)
	mustPut(t, store, "/회의/메뉴.txt", fileB)

	// A query that shares NO tokens with either file's name or body, placed at the
	// ~0.6 noise band to fileA. No lexical overlap + sub-floor cosine ⇒ must be cut.
	const noiseQuery = "전혀 무관한 별개 주제 질문" // disjoint vocabulary
	embed := &fixedEmbedder{vecs: map[string][]float32{
		fileA:      {1, 0, 0},
		fileB:      {0, 1, 0},
		noiseQuery: {0.6, 0, 0.8}, // 0.6 to fileA, in the noise band
	}}

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	hits, err := idx.HybridSearch(ctx, noiseQuery, 5, embed, plainText)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("semantic-noise query returned %d hits, want 0 (floor must hold): %+v", len(hits), hits)
	}
}

// TestHybridSearch_AgreementRanksTop verifies RRF: when one file matches BOTH
// lexically (name+body tokens) and semantically (high cosine) while another
// matches only semantically, the doubly-supported file ranks first.
func TestHybridSearch_AgreementRanksTop(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// fileHit shares vocabulary with the query (lexical) AND we place it at high
	// cosine. fileSem only gets a high cosine (no shared tokens with the query).
	const fileHit = "delivery delay 납기 지연 위약금 계약"
	const fileSem = "logistics schedule risk 물류 일정 위험"
	mustPut(t, store, "/계약/납기지연.txt", fileHit)
	mustPut(t, store, "/분석/물류위험.txt", fileSem)

	const query = "납기 지연 delivery delay 계약"
	embed := &fixedEmbedder{vecs: map[string][]float32{
		fileHit: {1, 0, 0},
		fileSem: {0.95, 0.31, 0}, // ~0.95 cosine to query, above floor, no shared tokens
		query:   {1, 0, 0},       // identical to fileHit (cosine 1.0) and ~0.95 to fileSem
	}}

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	hits, err := idx.HybridSearch(ctx, query, 5, embed, plainText)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("want both files, got %d: %+v", len(hits), hits)
	}
	if hits[0].Entry.PathDisplay != "/계약/납기지연.txt" {
		t.Fatalf("top hit = %q, want the lexically+semantically agreeing file /계약/납기지연.txt: %+v",
			hits[0].Entry.PathDisplay, hits)
	}
}

// TestHybridSearch_RejectsSingleCommonToken pins lexMinMatchTokens: a file that
// shares exactly ONE common, non-name query token (and is semantically unrelated)
// must NOT be admitted on that lone lexical hit — the guard against a stopword-ish
// term dragging in an off-topic file.
func TestHybridSearch_RejectsSingleCommonToken(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// fileShare's BODY contains the common word "문서" but nothing else from the
	// query, and its name has no query token. fileOther is fully unrelated.
	const fileShare = "이것은 회사 소개 문서 입니다 제품 안내"
	const fileOther = "점심 메뉴 커피 음료 목록"
	mustPut(t, store, "/소개/회사소개.txt", fileShare)
	mustPut(t, store, "/회의/메뉴.txt", fileOther)

	// Query shares only "문서" with fileShare; semantically both are far (low cos).
	const query = "탑솔라 계약 문서 검토 요청"
	embed := &fixedEmbedder{vecs: map[string][]float32{
		fileShare: {1, 0, 0},
		fileOther: {0, 1, 0},
		query:     {0.2, 0.2, 0.96}, // low cosine to both — well below floor
	}}

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	hits, err := idx.HybridSearch(ctx, query, 5, embed, plainText)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if pathSet(hits)["/소개/회사소개.txt"] {
		t.Fatalf("single-common-token file was admitted (want rejected by lexMinMatchTokens): %+v", hits)
	}
}

// TestHybridSearch_SemanticOnlyAboveFloor is a no-regression check: a file with
// no lexical overlap but a genuine above-floor cosine (a real meaning match) is
// still returned, exactly as cosine-only Search would.
func TestHybridSearch_SemanticOnlyAboveFloor(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// The body shares no tokens with the (differently-phrased) query, but sits at
	// a high cosine — the meaning-match case semantic search exists for.
	const body = "납기 지연 위약금 조항 계약"
	mustPut(t, store, "/계약/납기.txt", body)
	mustPut(t, store, "/회의/메뉴.txt", "점심 메뉴 커피")

	const query = "delivery delay penalty contract risk" // disjoint tokens from body
	embed := &fixedEmbedder{vecs: map[string][]float32{
		body:       {1, 0, 0},
		"점심 메뉴 커피": {0, 1, 0},
		query:      {0.99, 0.141, 0}, // ~0.99 cosine to body, above floor
	}}

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	hits, err := idx.HybridSearch(ctx, query, 5, embed, plainText)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if !pathSet(hits)["/계약/납기.txt"] {
		t.Fatalf("semantic-only above-floor match was dropped: %+v", hits)
	}
	// The displayed Score is the cosine (a familiar 0–1 number), not the RRF value.
	for _, h := range hits {
		if h.Entry.PathDisplay == "/계약/납기.txt" && h.Score < minSemanticScore {
			t.Fatalf("displayed score %.3f < floor for an above-floor hit (should be the cosine)", h.Score)
		}
	}
}

// TestHybridSearch_Degrades mirrors Search's degradation contract: a
// nil/unhealthy embedder, a too-short query, or an empty index returns an empty
// slice and no error, so the caller falls back to name/content search.
func TestHybridSearch_Degrades(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	mustPut(t, store, "/a.txt", "delivery 납기 지연 위약금")
	embed := newFakeEmbedder("delivery", "납기", "지연")
	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// Unhealthy embedder → empty, no error.
	down := &fakeEmbedder{healthy: false, vocab: []string{"delivery"}}
	if hits, err := idx.HybridSearch(ctx, "delivery 납기 지연 검색", 5, down, plainText); err != nil || len(hits) != 0 {
		t.Fatalf("down embedder = (%v, %v), want (empty, nil)", hits, err)
	}
	// nil embedder → empty, no error.
	if hits, err := idx.HybridSearch(ctx, "delivery 납기 지연 검색", 5, nil, plainText); err != nil || len(hits) != 0 {
		t.Fatalf("nil embedder = (%v, %v), want (empty, nil)", hits, err)
	}
	// Too-short query (< minChunkRunes) → empty, no error.
	if hits, err := idx.HybridSearch(ctx, "납기", 5, embed, plainText); err != nil || len(hits) != 0 {
		t.Fatalf("short query = (%v, %v), want (empty, nil)", hits, err)
	}
	// Nil receiver is safe.
	var nilIdx *SemanticIndex
	if hits, err := nilIdx.HybridSearch(ctx, "delivery 납기 지연 검색", 5, embed, plainText); err != nil || len(hits) != 0 {
		t.Fatalf("nil index = (%v, %v), want (empty, nil)", hits, err)
	}
}

// TestHybridSearch_TopMatchRanksOverPartial sanity-checks ordering with the
// vocab-count fakeEmbedder (orthogonal vocab → exact-0 cosine for the unrelated
// file): the file matching the query both ways outranks the one matching neither.
func TestHybridSearch_TopMatchRanksOverPartial(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	mustPut(t, store, "/계약/납기.txt", "delivery delay penalty 납기 지연 위약금 계약")
	mustPut(t, store, "/회의/메뉴.txt", "lunch menu coffee 점심 메뉴 커피")

	embed := newFakeEmbedder("delivery", "delay", "납기", "지연", "lunch", "menu", "점심", "커피")
	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	hits, err := idx.HybridSearch(ctx, "납기 지연 delivery delay", 5, embed, plainText)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("HybridSearch returned no hits")
	}
	if hits[0].Entry.PathDisplay != "/계약/납기.txt" {
		t.Errorf("top hit = %q, want /계약/납기.txt: %+v", hits[0].Entry.PathDisplay, hits)
	}
}
