package polaris

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// A thinking+tool_use-only assistant message (the common shape of tool-heavy
// agent turns) must index as readable prose, not the raw JSON fallback of
// ChatMessage.TextContent — raw JSON made search snippets unreadable and
// polluted the FTS index with JSON syntax tokens. Since the 2026-09-17
// incident the thinking prose is split OUT of the visible text (it is indexed
// hidden, see indexMessage): it must never ride a snippet, a Wide window or a
// NextText "answer head" back into a prompt.
func TestIndexableTextFormatsThinkingAndToolOnlyMessage(t *testing.T) {
	content := `[{"type":"thinking","thinking":"주간보고는 /weekly로 트리거해야 한다"},` +
		`{"type":"tool_use","id":"t1","name":"cron","input":{"action":"list"}}]`
	msg := toolport.ChatMessage{Role: "assistant", Content: json.RawMessage(content)}

	got := indexableText(msg)
	if strings.Contains(got, `"type"`) || (strings.Contains(got, "{") && strings.Contains(got, `"thinking"`)) {
		t.Fatalf("raw JSON leaked into index text: %q", got)
	}
	if strings.Contains(got, "주간보고는 /weekly로 트리거해야 한다") {
		t.Errorf("thinking prose must not be in the VISIBLE index text: %q", got)
	}
	if !strings.Contains(got, "[도구 cron]") {
		t.Errorf("tool call marker missing from index text: %q", got)
	}
	if hidden := indexableThinking(msg); hidden != "주간보고는 /weekly로 트리거해야 한다" {
		t.Errorf("indexableThinking = %q, want the thinking prose", hidden)
	}
	if indexableThinking(toolport.ChatMessage{Role: "user", Content: json.RawMessage(`"안녕"`)}) != "" {
		t.Error("plain string content has no thinking")
	}
}

func TestIndexableTextPreservesPlainAndTextBlockContent(t *testing.T) {
	plain := toolport.ChatMessage{Role: "user", Content: json.RawMessage(`"안녕하세요"`)}
	if got := indexableText(plain); got != "안녕하세요" {
		t.Errorf("plain string altered: %q", got)
	}
	rich := toolport.ChatMessage{
		Role:    "assistant",
		Content: json.RawMessage(`[{"type":"text","text":"결과 요약"}]`),
	}
	if got := indexableText(rich); got != "결과 요약" {
		t.Errorf("text block altered: %q", got)
	}
}

// End-to-end through the store: the appended thinking-only message must be
// findable by its thinking keywords (hidden field) AND return a snippet that
// is clean of raw JSON and clean of the reasoning itself.
func TestSearchMessages_ThinkingContentSearchableWithCleanSnippet(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	content := `[{"type":"thinking","thinking":"진코솔라 잔금 회신은 화요일까지"},` +
		`{"type":"tool_use","id":"t1","name":"wiki","input":{"action":"search"}}]`
	if err := store.AppendMessage("s1", toolport.ChatMessage{
		Role: "assistant", Content: json.RawMessage(content), Timestamp: 1000,
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	hits, err := store.SearchMessages("s1", "진코솔라 잔금", 5)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1 (thinking must stay matchable)", len(hits))
	}
	if strings.Contains(hits[0].Snippet, `"type"`) {
		t.Errorf("snippet still raw JSON: %q", hits[0].Snippet)
	}
	if strings.Contains(hits[0].Snippet, "진코솔라") || strings.Contains(hits[0].Wide, "진코솔라") {
		t.Errorf("reasoning surfaced in snippet/wide: snippet=%q wide=%q", hits[0].Snippet, hits[0].Wide)
	}
}

// The Q→A stitch: NextText is the head of the assistant reply that answers a
// matched user turn. A thinking model's reply STARTS with its thinking block,
// so before the split the "answer head" was the reasoning — the recall row
// then rendered `⏩ <reasoning>` as the answer. It must be the answer text.
func TestSearchMessages_NextTextIsTheAnswerNotTheThinking(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.AppendMessage("s1", toolport.NewTextChatMessage("user", "진코솔라 잔금 회신 기한이 언제지?", 1)); err != nil {
		t.Fatal(err)
	}
	reply := `[{"type":"thinking","thinking":"먼저 위키에서 진코솔라 계약 조건을 확인하자. 잔금 회신 기한은 계약서에 있다."},` +
		`{"type":"text","text":"잔금 회신은 화요일까지입니다."}]`
	if err := store.AppendMessage("s1", toolport.ChatMessage{Role: "assistant", Content: json.RawMessage(reply), Timestamp: 2}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.SearchMessages("s1", "잔금 회신 기한", 5)
	if err != nil {
		t.Fatal(err)
	}
	var userHit *SearchHit
	for i := range hits {
		if hits[i].Role == "user" {
			userHit = &hits[i]
		}
		if hits[i].Role == "assistant" && strings.Contains(hits[i].Wide, "먼저 위키에서") {
			t.Errorf("assistant Wide window carries reasoning: %q", hits[i].Wide)
		}
	}
	if userHit == nil {
		t.Fatalf("user turn not matched: %+v", hits)
	}
	if !strings.HasPrefix(userHit.NextText, "잔금 회신은 화요일까지입니다") {
		t.Errorf("NextText = %q, want the answer text head", userHit.NextText)
	}
	if strings.Contains(userHit.NextText, "먼저 위키에서") {
		t.Errorf("NextText carries the reasoning: %q", userHit.NextText)
	}
}

// Rows persisted before the split carry the thinking prose inside TextContent.
// Load recomputes both texts from the raw content, so an old store heals in
// memory without a rewrite: still findable by the reasoning, never showing it.
func TestLoadHealsThinkingOutOfPersistedTextContent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	reply := `[{"type":"thinking","thinking":"에이팩스 견적은 다시 계산해야 한다"},{"type":"text","text":"견적을 다시 보내드릴게요."}]`
	path := store.messagesPath("old")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Old-format row: TextContent = thinking + text, as indexableText once rendered it.
	old := messageRecord{
		Role: "assistant", Content: json.RawMessage(reply),
		TextContent: "에이팩스 견적은 다시 계산해야 한다\n견적을 다시 보내드릴게요.", Timestamp: 5, TokenEst: 20, MsgIndex: 0,
	}
	if err := jsonlstore.Append(path, old); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore(reload): %v", err)
	}
	hits, err := reloaded.SearchMessages("old", "에이팩스 견적 계산", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("old row not findable by its reasoning after heal: %+v", hits)
	}
	if strings.Contains(hits[0].Snippet, "계산해야") || strings.Contains(hits[0].Wide, "계산해야") {
		t.Errorf("healed row still surfaces reasoning: snippet=%q wide=%q", hits[0].Snippet, hits[0].Wide)
	}
	if !strings.Contains(hits[0].Wide, "견적을 다시 보내드릴게요") && !strings.Contains(hits[0].Snippet, "견적을 다시") {
		t.Errorf("visible answer text lost from the healed row: snippet=%q wide=%q", hits[0].Snippet, hits[0].Wide)
	}
}
