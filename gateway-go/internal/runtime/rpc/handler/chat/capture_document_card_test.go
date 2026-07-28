package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// fakeCaptureFeed is a minimal MiniappDeps.WorkFeed for the capture-card tests.
type fakeCaptureFeed struct{ items []workfeed.Item }

func (f *fakeCaptureFeed) Append(it workfeed.Item) (workfeed.Item, error) {
	f.items = append(f.items, it)
	return it, nil
}

func (f *fakeCaptureFeed) List(int, bool) ([]workfeed.Item, int, error) {
	return f.items, len(f.items), nil
}

func (f *fakeCaptureFeed) Correct(string, string) (workfeed.Item, error) { return workfeed.Item{}, nil }

func (f *fakeCaptureFeed) Rewrite(string, string) (workfeed.Item, error) { return workfeed.Item{}, nil }

func TestCardCapturedDocumentCreatesCardAndSkipsDuplicates(t *testing.T) {
	const substantial = "## 계약서 리스크 검토\n\n- O&M 단가 인상\n- 면책 조항 삭제 요구\n\n결론: 서명 전 3건 확인 필요."

	t.Run("substantial analysis becomes a doc_analysis deliverable card", func(t *testing.T) {
		feed := &fakeCaptureFeed{}
		deps := MiniappDeps{
			WorkFeed: feed,
			PublishDeliverable: func(text string) (bool, error) {
				feed.items = append(feed.items, workfeed.Item{Source: workfeed.SourceDocAnalysis, Body: text})
				return true, nil
			},
		}
		cardCapturedDocument(deps, "client:main", &chatport.SyncResult{Text: substantial, BestText: substantial}, 1000)
		if len(feed.items) != 1 || feed.items[0].Source != workfeed.SourceDocAnalysis {
			t.Fatalf("want 1 doc_analysis card, got %+v", feed.items)
		}
	})

	t.Run("thin analysis falls back to the raw capture card", func(t *testing.T) {
		feed := &fakeCaptureFeed{}
		deps := MiniappDeps{
			WorkFeed:           feed,
			PublishDeliverable: func(string) (bool, error) { return false, nil }, // suppressed
		}
		cardCapturedDocument(deps, "client:main", &chatport.SyncResult{Text: "3페이지입니다", BestText: "3페이지입니다"}, 1000)
		if len(feed.items) != 1 || feed.items[0].Source != workfeed.SourceCaptureDocument {
			t.Fatalf("want 1 capture_document fallback card, got %+v", feed.items)
		}
	})

	t.Run("skips when the model already published this turn (dedup)", func(t *testing.T) {
		feed := &fakeCaptureFeed{items: []workfeed.Item{
			{Source: workfeed.SourceDocAnalysis, SessionKey: "client:main", CreatedAtMs: 2000},
		}}
		published := false
		deps := MiniappDeps{
			WorkFeed:           feed,
			PublishDeliverable: func(string) (bool, error) { published = true; return true, nil },
		}
		cardCapturedDocument(deps, "client:main", &chatport.SyncResult{Text: substantial, BestText: substantial}, 1000)
		if published {
			t.Error("must not publish again when the model already carded this turn")
		}
		if len(feed.items) != 1 {
			t.Errorf("must not add a second card, got %d", len(feed.items))
		}
	})

	t.Run("an older card from a previous turn does not block this turn", func(t *testing.T) {
		feed := &fakeCaptureFeed{items: []workfeed.Item{
			{Source: workfeed.SourceDocAnalysis, SessionKey: "client:main", CreatedAtMs: 500},
		}}
		deps := MiniappDeps{
			WorkFeed: feed,
			PublishDeliverable: func(string) (bool, error) {
				feed.items = append(feed.items, workfeed.Item{Source: workfeed.SourceDocAnalysis})
				return true, nil
			},
		}
		cardCapturedDocument(deps, "client:main", &chatport.SyncResult{Text: substantial, BestText: substantial}, 1000)
		if len(feed.items) != 2 {
			t.Errorf("older card (t=500 < turnStart=1000) must not block this turn, got %d", len(feed.items))
		}
	})
}
