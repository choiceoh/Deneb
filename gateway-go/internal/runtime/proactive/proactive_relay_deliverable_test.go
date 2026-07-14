package proactive

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// TestPublishDeliverableCreatesCardOrSuppressesThinContent covers the
// auto-publish safety net's card-side logic: a substantial deliverable files
// a doc_analysis card, while thin or contentless responses are suppressed by
// the substance floor (the false-positive control).
func TestPublishDeliverableCreatesCardOrSuppressesThinContent(t *testing.T) {
	structured := "## EPC 계약서 리스크 검토\n\n" +
		"| 조항 | 리스크 |\n|---|---|\n| O&M 단가 | 1,800→1,950만원 |\n\n" +
		"- 면책 조항 삭제 요구 (차남두 부장 회람)\n- 발전량 보증 연장 1년으로 협상"

	t.Run("substantial structured deliverable files a doc_analysis card", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{workFeed: feed}
		ok, err := d.publishDeliverable(structured)
		if err != nil || !ok {
			t.Fatalf("publishDeliverable = (%v, %v), want (true, nil)", ok, err)
		}
		if n := len(feed.items); n != 1 {
			t.Fatalf("want 1 card, got %d", n)
		}
		card := feed.items[0]
		if card.Source != workfeed.SourceDocAnalysis {
			t.Errorf("source = %q, want %q", card.Source, workfeed.SourceDocAnalysis)
		}
		if strings.TrimSpace(card.Title) == "" {
			t.Error("title should be derived from the body, got empty")
		}
		if !strings.Contains(card.Body, "면책 조항") {
			t.Errorf("body not preserved: %q", card.Body)
		}
	})

	t.Run("thin answer to a document is suppressed by the substance floor", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{workFeed: feed}
		ok, _ := d.publishDeliverable("이 문서는 3페이지입니다.")
		if ok || len(feed.items) != 0 {
			t.Errorf("thin answer must not card: ok=%v items=%d", ok, len(feed.items))
		}
	})

	t.Run("contentless response is suppressed", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{workFeed: feed}
		ok, _ := d.publishDeliverable("분석할 새 메일이 없습니다")
		if ok || len(feed.items) != 0 {
			t.Errorf("contentless must not card: ok=%v items=%d", ok, len(feed.items))
		}
	})

	t.Run("nil feed is a safe no-op", func(t *testing.T) {
		d := proactiveRelayDeps{}
		if ok, err := d.publishDeliverable(structured); ok || err != nil {
			t.Errorf("nil feed = (%v, %v), want (false, nil)", ok, err)
		}
	})
}
