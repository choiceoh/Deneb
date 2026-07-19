package surface

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// memWorkFeed is a tiny in-memory WorkFeedRW for tool tests.
type memWorkFeed struct {
	mu    sync.Mutex
	items []tooldeps.WorkFeedItem
	seq   int
}

func newTestWorkFeed(_ *testing.T) *memWorkFeed {
	return &memWorkFeed{}
}

func (s *memWorkFeed) List(limit int, includeAcked bool) ([]tooldeps.WorkFeedItem, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []tooldeps.WorkFeedItem
	for _, it := range s.items {
		if !includeAcked && it.Status == "acked" {
			continue
		}
		out = append(out, it)
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (s *memWorkFeed) MarkRead(id string) (tooldeps.WorkFeedItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			if s.items[i].ReadAtMs == 0 {
				s.items[i].ReadAtMs = time.Now().UnixMilli()
			}
			return s.items[i], nil
		}
	}
	return tooldeps.WorkFeedItem{}, fmt.Errorf("not found: %s", id)
}

func (s *memWorkFeed) Ack(id string) (tooldeps.WorkFeedItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Status = "acked"
			return s.items[i], nil
		}
	}
	return tooldeps.WorkFeedItem{}, fmt.Errorf("not found: %s", id)
}

func (s *memWorkFeed) Append(item tooldeps.WorkFeedItem) (tooldeps.WorkFeedItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	item.ID = fmt.Sprintf("wf-%d", s.seq)
	if item.Status == "" {
		item.Status = "unread"
	}
	if item.CreatedAtMs == 0 {
		item.CreatedAtMs = time.Now().UnixMilli()
	}
	s.items = append(s.items, item)
	return item, nil
}

func TestToolWorkFeed_ListReadAck(t *testing.T) {
	store := newTestWorkFeed(t)
	item, err := store.Append(tooldeps.WorkFeedItem{
		Source:     "mail_report",
		Title:      "납품 일정 질문",
		Body:       "탑솔라 케이블 납기가 다음 주로 당겨졌습니다.\n확인이 필요합니다.",
		RelatedIDs: []string{"wf-related-a", "wf-related-b"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := ToolWorkFeed(store)
	ctx := context.Background()

	out, err := tool(ctx, json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, item.ID) || !strings.Contains(out, "납품 일정 질문") {
		t.Errorf("list missing card: %q", out)
	}
	if !strings.Contains(out, "미열람") {
		t.Errorf("unread card must be flagged: %q", out)
	}
	if !strings.Contains(out, "관련 2건") {
		t.Errorf("related card count missing: %q", out)
	}

	out, err = tool(ctx, json.RawMessage(`{"action":"read","id":"`+item.ID+`"}`))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "탑솔라 케이블") {
		t.Errorf("read missing body: %q", out)
	}
	if !strings.Contains(out, "관련 카드: wf-related-a, wf-related-b") {
		t.Errorf("read missing related IDs: %q", out)
	}
	out, _ = tool(ctx, json.RawMessage(`{"action":"list"}`))
	if strings.Contains(out, "미열람") {
		t.Errorf("read card still flagged unread: %q", out)
	}

	if _, err := tool(ctx, json.RawMessage(`{"action":"ack","id":"`+item.ID+`"}`)); err != nil {
		t.Fatalf("ack: %v", err)
	}
	out, _ = tool(ctx, json.RawMessage(`{"action":"list"}`))
	if !strings.Contains(out, "카드가 없습니다") {
		t.Errorf("acked card must leave the default list: %q", out)
	}
	out, _ = tool(ctx, json.RawMessage(`{"action":"list","include_acked":true}`))
	if !strings.Contains(out, item.ID) {
		t.Errorf("include_acked must show the acked card: %q", out)
	}
}

func TestToolWorkFeedCreatesPublishedCard(t *testing.T) {
	store := newTestWorkFeed(t)
	tool := ToolWorkFeed(store)
	ctx := context.Background()

	out, err := tool(ctx, json.RawMessage(`{"action":"publish","title":"당진 솔라빌리지 EPC 검토","summary":"확정 ₩1,041억 · 미해결 3건","body":"## 결론\nDR 26건 대부분 반영. 미해결 3건 회람 필요.\n\n## 액션\n- O&M 단가 불일치 (차남두 부장)","priority":"high","ref_type":"wiki","ref_id":"프로젝트/당진-솔라빌리지/계약검토.md"}`))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !strings.Contains(out, "발행했습니다") || !strings.Contains(out, "당진 솔라빌리지 EPC 검토") {
		t.Errorf("publish confirmation missing title: %q", out)
	}

	items, _, err := store.List(10, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 published card, got %d", len(items))
	}
	got := items[0]
	if got.Source != tooldeps.WorkFeedSourceDocAnalysis {
		t.Errorf("source = %q, want %q", got.Source, tooldeps.WorkFeedSourceDocAnalysis)
	}
	if got.Priority != tooldeps.WorkFeedPriorityHigh {
		t.Errorf("priority = %d, want %d", got.Priority, tooldeps.WorkFeedPriorityHigh)
	}
	if got.RefType != "wiki" || got.RefID != "프로젝트/당진-솔라빌리지/계약검토.md" {
		t.Errorf("ref not linked: %q/%q", got.RefType, got.RefID)
	}
	if !strings.Contains(got.Body, "O&M 단가 불일치") {
		t.Errorf("body not stored: %q", got.Body)
	}
}

func TestToolWorkFeed_Errors(t *testing.T) {
	tool := ToolWorkFeed(newTestWorkFeed(t))
	ctx := context.Background()
	if _, err := tool(ctx, json.RawMessage(`{"action":"read"}`)); err == nil {
		t.Error("read without id must error")
	}
	if _, err := tool(ctx, json.RawMessage(`{"action":"ack","id":"nope"}`)); err == nil {
		t.Error("ack unknown id must error")
	}
	if _, err := tool(ctx, json.RawMessage(`{"action":"purge_all"}`)); err == nil {
		t.Error("unknown action must error")
	}
	if _, err := tool(ctx, json.RawMessage(`{"action":"publish","body":"본문만"}`)); err == nil {
		t.Error("publish without title must error")
	}
	if _, err := tool(ctx, json.RawMessage(`{"action":"publish","title":"제목만"}`)); err == nil {
		t.Error("publish without body must error")
	}
}
