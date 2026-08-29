package polaris

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestBridgeSearchSessionsSemanticCollapsesPerSession proves the sessions-tool
// facing wrapper, not just the store arm underneath it: a paraphrase reaches
// through the Bridge, several summary nodes of ONE conversation collapse to a
// single line, and the current session stays excluded.
func TestBridgeSearchSessionsSemanticCollapsesPerSession(t *testing.T) {
	s := testStore(t)
	defer s.Close()

	// Two billing summaries in the SAME past conversation — the collapse case.
	for _, c := range []struct {
		content string
		at      int64
	}{
		{"금호 곡성 기성 청구 처리 완료", 1000},
		{"곡성 기성 청구 잔액 확인", 1500},
	} {
		if _, err := s.InsertSummary(SummaryNode{
			SessionKey: "sessA", Level: 1, Content: c.content, CreatedAt: c.at, MsgStart: 0, MsgEnd: 9,
		}); err != nil {
			t.Fatalf("InsertSummary: %v", err)
		}
	}
	if _, err := s.InsertSummary(SummaryNode{
		SessionKey: "sessB", Level: 1, Content: "추론 서버 재기동 및 배포", CreatedAt: 2000, MsgStart: 0, MsgEnd: 9,
	}); err != nil {
		t.Fatalf("InsertSummary B: %v", err)
	}
	s.SetSummaryEmbedder(semFakeEmbedder{healthy: true})
	if err := s.warmSummarySem(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}

	b := NewBridge(nil, s, slog.Default())
	hits := b.SearchSessionsSemantic(context.Background(), "current", "곡성 대금 지급 어떻게 했지", 5)
	if len(hits) == 0 {
		t.Fatal("paraphrased query must reach the billing conversation through the Bridge")
	}
	seen := map[string]int{}
	for _, h := range hits {
		seen[h.SessionKey]++
	}
	if seen["sessA"] != 1 {
		t.Fatalf("sessA must appear exactly once (summary nodes collapse), got %d: %+v", seen["sessA"], hits)
	}
	if seen["current"] != 0 {
		t.Error("the current session must stay excluded")
	}
	top := hits[0]
	if top.SessionKey != "sessA" || !strings.Contains(top.Snippet, "기성 청구") {
		t.Errorf("top hit = %q/%q, want sessA billing", top.SessionKey, top.Snippet)
	}
	if top.At == 0 {
		t.Error("hit must carry the conversation timestamp — the aggregation side filters by date")
	}

	// Excluding sessA itself drops it.
	for _, h := range b.SearchSessionsSemantic(context.Background(), "sessA", "곡성 대금 지급 어떻게 했지", 5) {
		if h.SessionKey == "sessA" {
			t.Error("excluded session should not appear")
		}
	}
}

// TestBridgeSearchSessionsSemanticDegradesQuietly: no embedder (or a blank
// query) yields nil, so the sessions tool falls back to keyword-only search
// instead of erroring.
func TestBridgeSearchSessionsSemanticDegradesQuietly(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	if _, err := s.InsertSummary(SummaryNode{SessionKey: "sessA", Level: 1, Content: "아무 요약", CreatedAt: 1000}); err != nil {
		t.Fatalf("InsertSummary: %v", err)
	}
	b := NewBridge(nil, s, slog.Default())
	if got := b.SearchSessionsSemantic(context.Background(), "current", "대금 지급", 5); got != nil {
		t.Errorf("disabled semantic index must degrade to nil, got %+v", got)
	}
	if got := b.SearchSessionsSemantic(context.Background(), "current", "   ", 5); got != nil {
		t.Errorf("blank query must degrade to nil, got %+v", got)
	}
}
