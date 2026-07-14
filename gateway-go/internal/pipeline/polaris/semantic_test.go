package polaris

import (
	"context"
	"strings"
	"testing"
)

// semFakeEmbedder maps text to a 2-dim concept vector: dim0 = "payment/billing"
// cluster {대금, 기성, 청구, 지급}, dim1 = "deploy" cluster {배포, 재기동, 서버}.
// A query about payment finds a summary about billing even with no shared word.
type semFakeEmbedder struct{ healthy bool }

func (f semFakeEmbedder) IsHealthy() bool { return f.healthy }
func (f semFakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		var pay, dep float32
		for _, w := range []string{"대금", "기성", "청구", "지급"} {
			if strings.Contains(t, w) {
				pay = 1
			}
		}
		for _, w := range []string{"배포", "재기동", "서버"} {
			if strings.Contains(t, w) {
				dep = 1
			}
		}
		out[i] = []float32{pay, dep}
	}
	return out, nil
}

// TestSearchSummariesSemanticReturnsParaphrasedMatch: a summary about "기성
// 청구" (billing) is found by a payment-concept query that shares no keyword,
// and only for OTHER sessions (the current one is excluded — its summaries
// are already in context).
func TestSearchSummariesSemanticReturnsParaphrasedMatch(t *testing.T) {
	s := testStore(t)
	defer s.Close()

	// Session A (a past conversation): billing summary, no "대금" keyword.
	if _, err := s.InsertSummary(SummaryNode{
		SessionKey: "sessA", Level: 1, Content: "금호 곡성 기성 청구 처리 완료", CreatedAt: 1000, MsgStart: 0, MsgEnd: 9,
	}); err != nil {
		t.Fatalf("InsertSummary A: %v", err)
	}
	// Session B: unrelated deploy summary.
	if _, err := s.InsertSummary(SummaryNode{
		SessionKey: "sessB", Level: 1, Content: "추론 서버 재기동 및 배포", CreatedAt: 2000, MsgStart: 0, MsgEnd: 9,
	}); err != nil {
		t.Fatalf("InsertSummary B: %v", err)
	}

	s.SetSummaryEmbedder(semFakeEmbedder{healthy: true})
	if err := s.warmSummarySem(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}

	// Query in payment terms, excluding a THIRD (current) session.
	batch := s.SearchSummariesSemantic(context.Background(), "current", []string{"곡성 대금 지급 어떻게 했지"}, 5)
	if len(batch) != 1 {
		t.Fatalf("batch len %d, want 1", len(batch))
	}
	if len(batch[0]) == 0 {
		t.Fatal("semantic should surface the billing summary")
	}
	top := batch[0][0]
	if top.SessionKey != "sessA" {
		t.Errorf("top hit session = %q, want sessA (billing)", top.SessionKey)
	}
	if !strings.Contains(top.Content, "기성 청구") {
		t.Errorf("top content = %q, want the billing summary", top.Content)
	}

	// Excluding sessA drops it from results.
	excl := s.SearchSummariesSemantic(context.Background(), "sessA", []string{"곡성 대금 지급 어떻게 했지"}, 5)
	for _, h := range excl[0] {
		if h.SessionKey == "sessA" {
			t.Error("excluded session should not appear")
		}
	}
}

// TestSearchSummariesSemanticReturnsNilWhenDisabled: no embedder → nil (keyword-only recall).
func TestSearchSummariesSemanticReturnsNilWhenDisabled(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	if _, err := s.InsertSummary(SummaryNode{SessionKey: "sessA", Level: 1, Content: "아무 요약 내용", CreatedAt: 1000}); err != nil {
		t.Fatalf("InsertSummary: %v", err)
	}
	if batch := s.SearchSummariesSemantic(context.Background(), "cur", []string{"아무 요약"}, 5); batch != nil {
		t.Errorf("expected nil without embedder, got %v", batch)
	}
}
