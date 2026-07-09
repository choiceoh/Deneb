package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func newTestWorkFeed(t *testing.T) *workfeed.Store {
	t.Helper()
	return workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
}

func TestToolWorkFeed_ListReadAck(t *testing.T) {
	store := newTestWorkFeed(t)
	item, err := store.Append(workfeed.Item{
		Source: "mail_report",
		Title:  "납품 일정 질문",
		Body:   "탑솔라 케이블 납기가 다음 주로 당겨졌습니다.\n확인이 필요합니다.",
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

	out, err = tool(ctx, json.RawMessage(`{"action":"read","id":"`+item.ID+`"}`))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "탑솔라 케이블") {
		t.Errorf("read missing body: %q", out)
	}
	// read stamps ReadAtMs — the list must no longer flag it unread.
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

func TestToolWorkFeed_Publish(t *testing.T) {
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

	// The published card must be in the feed with the doc-analysis source + ref link.
	items, _, err := store.List(10, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 published card, got %d", len(items))
	}
	got := items[0]
	if got.Source != workfeed.SourceDocAnalysis {
		t.Errorf("source = %q, want %q", got.Source, workfeed.SourceDocAnalysis)
	}
	if got.Priority != workfeed.PriorityHigh {
		t.Errorf("priority = %d, want %d", got.Priority, workfeed.PriorityHigh)
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
	// publish requires both title and body.
	if _, err := tool(ctx, json.RawMessage(`{"action":"publish","body":"본문만"}`)); err == nil {
		t.Error("publish without title must error")
	}
	if _, err := tool(ctx, json.RawMessage(`{"action":"publish","title":"제목만"}`)); err == nil {
		t.Error("publish without body must error")
	}
}
