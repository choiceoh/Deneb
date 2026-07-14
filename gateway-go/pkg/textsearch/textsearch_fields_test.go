package textsearch

import (
	"math"
	"strings"
	"testing"
)

// TestUpsertFieldsRanksCanonicalFirstWhenTitleBoosted is the causal proof for field boosts: the
// same two documents, the same query — flat indexing ranks the body-heavy
// distractor first (higher raw term frequency), while a title boost flips the
// order so the page whose *identity* carries the term wins. This is the
// BM25F-lite behavior the wiki identity-field boost relies on.
func TestUpsertFieldsRanksCanonicalFirstWhenTitleBoosted(t *testing.T) {
	const query = "정산"

	seedFlat := func(idx *Index) {
		idx.Upsert("canonical", "정산 프로세스", "거래처와 월별로 마감한다 상세 절차는 본문 참조")
		idx.Upsert("distractor", "회의록", "정산 논의함 정산 기한 확인함 정산 재논의 예정 다음 주제는 발주였다")
	}

	// Flat: the distractor's tf=3 body beats the canonical page's tf=1 title.
	flat := New()
	seedFlat(flat)
	hits := flat.Search(query, 2)
	if len(hits) != 2 {
		t.Fatalf("flat search hits = %d, want 2", len(hits))
	}
	if hits[0].ID != "distractor" {
		t.Fatalf("flat ranking: top = %s, want distractor (the crowding failure the boost exists to fix)", hits[0].ID)
	}

	// Boosted: identical texts, but the title field carries weight 3 — the
	// canonical page must now outrank the body-heavy distractor.
	boosted := New()
	boosted.UpsertFields(
		"canonical",
		Field{Text: "정산 프로세스", Weight: 3},
		Field{Text: "거래처와 월별로 마감한다 상세 절차는 본문 참조", Weight: 1},
	)
	boosted.UpsertFields(
		"distractor",
		Field{Text: "회의록", Weight: 3},
		Field{Text: "정산 논의함 정산 기한 확인함 정산 재논의 예정 다음 주제는 발주였다", Weight: 1},
	)
	hits = boosted.Search(query, 2)
	if len(hits) != 2 {
		t.Fatalf("boosted search hits = %d, want 2", len(hits))
	}
	if hits[0].ID != "canonical" {
		t.Fatalf("boosted ranking: top = %s, want canonical", hits[0].ID)
	}
}

// TestSearchReturnsSameTieOrderAcrossRuns pins the equal-score ordering: candidates
// come from map iteration, so without the ID tie-break identical documents
// would shuffle between runs (measured as test flakiness; the same shuffle
// would hit production ranking across restarts).
func TestSearchReturnsSameTieOrderAcrossRuns(t *testing.T) {
	idx := New()
	// Identical field content → identical BM25 scores for the query.
	idx.Upsert("b-doc", "정산 안내", "내용")
	idx.Upsert("a-doc", "정산 안내", "내용")
	for i := 0; i < 5; i++ {
		hits := idx.Search("정산", 2)
		if len(hits) != 2 || hits[0].ID != "a-doc" || hits[1].ID != "b-doc" {
			t.Fatalf("run %d: tie order not deterministic by ID: %+v", i, hits)
		}
	}
}

// TestUpsertFieldsRejectsNaNInfWeights — NaN/Inf weights must be normalized
// away: a NaN score breaks sort.Slice's strict ordering and Inf swamps ranks.
func TestUpsertFieldsRejectsNaNInfWeights(t *testing.T) {
	idx := New()
	idx.UpsertFields("nan", Field{Text: "정산 문서", Weight: math.NaN()})
	idx.UpsertFields("inf", Field{Text: "정산 기록", Weight: math.Inf(1)})
	idx.UpsertFields("neg", Field{Text: "정산 노트", Weight: -3})
	hits := idx.Search("정산", 3)
	if len(hits) != 3 {
		t.Fatalf("want 3 hits, got %d", len(hits))
	}
	for _, h := range hits {
		if math.IsNaN(h.Score) || math.IsInf(h.Score, 0) || h.Score <= 0 {
			t.Fatalf("weight sanitization failed: %+v", h)
		}
	}
}

// TestUpsertFieldsWithFlatWeightPreservesUpsertBehavior — weight 1.0 everywhere must behave
// byte-identically to plain Upsert (same candidates, same scores), so existing
// callers can migrate without a behavior change.
func TestUpsertFieldsWithFlatWeightPreservesUpsertBehavior(t *testing.T) {
	plain := New()
	plain.Upsert("a", "정산 프로세스", "본문 내용")
	plain.Upsert("b", "회의록", "정산 논의 내용")

	viaFields := New()
	viaFields.UpsertFields("a", Field{Text: "정산 프로세스", Weight: 1}, Field{Text: "본문 내용"})
	viaFields.UpsertFields("b", Field{Text: "회의록", Weight: 1}, Field{Text: "정산 논의 내용"})

	p, f := plain.Search("정산", 5), viaFields.Search("정산", 5)
	if len(p) != len(f) {
		t.Fatalf("hit counts differ: plain=%d fields=%d", len(p), len(f))
	}
	for i := range p {
		if p[i].ID != f[i].ID || p[i].Score != f[i].Score {
			t.Fatalf("hit %d differs: plain=%+v fields=%+v", i, p[i], f[i])
		}
	}
}

// TestUpsertFieldsHiddenFieldsExcludedFromSnippetDisplay: a Hidden field must still make
// the document matchable (membership/scoring) but its text must never appear
// as the result snippet — retrieval-only anchors (wiki cues) are index
// metadata, not content.
func TestUpsertFieldsHiddenFieldsExcludedFromSnippetDisplay(t *testing.T) {
	idx := New()
	idx.UpsertFields(
		"doc",
		Field{Text: "프로젝트 일정 정리 본문입니다", Weight: 1},
		Field{Text: "계약금 선수금 회수 조건", Weight: 2, Hidden: true},
	)
	hits := idx.Search("계약금", 5)
	if len(hits) != 1 || hits[0].ID != "doc" {
		t.Fatalf("hidden field must still match, hits=%+v", hits)
	}
	if strings.Contains(hits[0].Snippet, "계약금") || strings.Contains(hits[0].Snippet, "선수금") {
		t.Fatalf("hidden field text leaked into snippet: %q", hits[0].Snippet)
	}
	if !strings.Contains(hits[0].Snippet, "본문") {
		t.Fatalf("snippet should fall back to visible fields, got %q", hits[0].Snippet)
	}

	// No hidden fields → zero behavior change (snippet may quote any field).
	idx.UpsertFields(
		"plain",
		Field{Text: "제목", Weight: 2},
		Field{Text: "잔금 입금 확인", Weight: 1},
	)
	hits = idx.Search("잔금", 5)
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, "잔금") {
		t.Fatalf("visible-field snippet regressed: %+v", hits)
	}
}
