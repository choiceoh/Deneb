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
}
