package textsearch

import (
	"fmt"
	"testing"
	"time"
)

// TestHangulMultiTermSearchStaysBounded is the causal guard for the mailstore
// latency incident: a few-thousand-doc Korean corpus with a multi-term OR
// fallback used to take ~100s because matchingDocs scanned the vocabulary
// inside the per-candidate BM25 loop. After index-time Hangul expansion +
// hoisted IDF + OR candidate cap, the same shape must stay well under a second.
func TestHangulMultiTermSearchStaysBounded(t *testing.T) {
	idx := New()
	for i := 0; i < 2500; i++ {
		idx.Upsert(fmt.Sprintf("d%d", i),
			fmt.Sprintf("일반 메일 본문 내용 %d 계약 검토 일정 선택 회신", i),
			"첨부 없이 본문만 있는 메시지입니다")
	}
	idx.Upsert("target", "리파워링 인버터 남원 대광 소이 선택 발주 건")

	start := time.Now()
	hits := idx.Search("리파워링 인버터 남원 대광 소이 선택", 10)
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Hangul multi-term search took %s, want ≤500ms", elapsed)
	}
	if len(hits) == 0 || hits[0].ID != "target" {
		t.Fatalf("hits=%+v, want target first", hits)
	}
}

// TestORCandidateCapPrefersRareTerms: when OR would flood from a common term,
// rarest-first filling must still surface the rare-term document inside the cap.
func TestORCandidateCapPrefersRareTerms(t *testing.T) {
	idx := New()
	// Flood the common Hangul posting well past maxORCandidates.
	for i := 0; i < maxORCandidates+200; i++ {
		idx.Upsert(fmt.Sprintf("common-%d", i), "선택 관련 일반 안내 메일")
	}
	idx.Upsert("rare-hit", "희귀고유명사 선택")

	hits := idx.SearchOR("희귀고유명사 선택", maxORCandidates)
	found := false
	for _, h := range hits {
		if h.ID == "rare-hit" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("rare-hit missing from capped OR results (%d hits); rarest-first fill failed", len(hits))
	}
}

func TestHangulExpansionRemovedOnUpsertAndRemove(t *testing.T) {
	idx := New()
	idx.Upsert("permit", "개발행위허가 신청")
	if df := idx.DocFreq("행위허가"); df != 1 {
		t.Fatalf("DocFreq(행위허가) = %d after index, want 1", df)
	}
	idx.Upsert("permit", "다른 내용으로 교체")
	if df := idx.DocFreq("행위허가"); df != 0 {
		t.Fatalf("DocFreq(행위허가) = %d after replace, want 0 (expansion keys leaked)", df)
	}
	idx.Upsert("deal", "대한전선 프로젝트")
	idx.Remove("deal")
	if df := idx.DocFreq("대한전선"); df != 0 {
		t.Fatalf("DocFreq(대한전선) = %d after remove, want 0", df)
	}
}
