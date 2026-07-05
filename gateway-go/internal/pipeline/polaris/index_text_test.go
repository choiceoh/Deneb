package polaris

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// A thinking+tool_use-only assistant message (the common shape of tool-heavy
// agent turns) must index as readable prose, not the raw JSON fallback of
// ChatMessage.TextContent — raw JSON made search snippets unreadable and
// polluted the FTS index with JSON syntax tokens.
func TestIndexableText_ThinkingAndToolOnlyMessage(t *testing.T) {
	content := `[{"type":"thinking","thinking":"주간보고는 /weekly로 트리거해야 한다"},` +
		`{"type":"tool_use","id":"t1","name":"cron","input":{"action":"list"}}]`
	msg := toolctx.ChatMessage{Role: "assistant", Content: json.RawMessage(content)}

	got := indexableText(msg)
	if strings.Contains(got, `"type"`) || (strings.Contains(got, "{") && strings.Contains(got, `"thinking"`)) {
		t.Fatalf("raw JSON leaked into index text: %q", got)
	}
	if !strings.Contains(got, "주간보고는 /weekly로 트리거해야 한다") {
		t.Errorf("thinking prose missing from index text: %q", got)
	}
	if !strings.Contains(got, "[도구 cron]") {
		t.Errorf("tool call marker missing from index text: %q", got)
	}
}

func TestIndexableText_PlainAndTextBlocksUnchanged(t *testing.T) {
	plain := toolctx.ChatMessage{Role: "user", Content: json.RawMessage(`"안녕하세요"`)}
	if got := indexableText(plain); got != "안녕하세요" {
		t.Errorf("plain string altered: %q", got)
	}
	rich := toolctx.ChatMessage{
		Role:    "assistant",
		Content: json.RawMessage(`[{"type":"text","text":"결과 요약"}]`),
	}
	if got := indexableText(rich); got != "결과 요약" {
		t.Errorf("text block altered: %q", got)
	}
}

// End-to-end through the store: the appended thinking-only message must be
// findable by its thinking keywords AND return a clean snippet.
func TestSearchMessages_ThinkingContentSearchableWithCleanSnippet(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	content := `[{"type":"thinking","thinking":"진코솔라 잔금 회신은 화요일까지"},` +
		`{"type":"tool_use","id":"t1","name":"wiki","input":{"action":"search"}}]`
	if err := store.AppendMessage("s1", toolctx.ChatMessage{
		Role: "assistant", Content: json.RawMessage(content), Timestamp: 1000,
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	hits, err := store.SearchMessages("s1", "진코솔라 잔금", 5)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if strings.Contains(hits[0].Snippet, `"type"`) {
		t.Errorf("snippet still raw JSON: %q", hits[0].Snippet)
	}
	if !strings.Contains(hits[0].Snippet, "진코솔라") {
		t.Errorf("snippet missing matched prose: %q", hits[0].Snippet)
	}
}
