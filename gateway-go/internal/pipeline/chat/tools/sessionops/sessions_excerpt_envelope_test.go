package sessionops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Incident 2026-09-17 (stream_0043): a sessions() result rendered earlier
// assistant reasoning and `[도구 x] {json}` rows as plain text inside a 50K
// prompt and the model continued the document instead of answering. Every
// history/search result is now framed as untrusted DATA and its rows are
// rendered through ExcerptText (tool name only, no reasoning).
func TestSessionsHistoryIsFramedAsDataWithoutReasoningOrToolJSON(t *testing.T) {
	store := &historyFakeTranscript{sessions: map[string][]toolport.ChatMessage{
		"client:main": {
			toolport.NewTextChatMessage("user", "모닝레터 보내줘", 1),
			{Role: "assistant", Timestamp: 2, Content: json.RawMessage(
				`[{"type":"thinking","thinking":"먼저 템플릿을 확인하자","signature":"SIGSIGSIG"},` +
					`{"type":"tool_use","name":"morning_letter","input":{"date":"2026-08-26"}}]`,
			)},
			toolport.NewTextChatMessage("assistant", "보냈어요", 3),
		},
	}}
	out, err := toolSessionsHistory(store)(context.Background(),
		sessionSearchJSON(t, map[string]any{"sessionKey": "client:main"}))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.HasPrefix(out, transcriptExcerptOpenTag+"\n") || !strings.HasSuffix(out, transcriptExcerptCloseTag) {
		t.Fatalf("history not framed as a transcript excerpt:\n%s", out)
	}
	if !strings.Contains(out, "trust=\"untrusted\"") || !strings.Contains(out, "이어쓰거나") {
		t.Errorf("envelope lacks the trust attribute or the do-not-continue note:\n%s", out)
	}
	if !strings.Contains(out, "[도구 morning_letter]") {
		t.Errorf("tool call dropped from history:\n%s", out)
	}
	if strings.Contains(out, "2026-08-26") || strings.Contains(out, "먼저 템플릿을") || strings.Contains(out, "SIGSIG") {
		t.Errorf("tool JSON / reasoning / signature fed back into the excerpt:\n%s", out)
	}
	if !strings.Contains(out, "모닝레터 보내줘") || !strings.Contains(out, "보냈어요") {
		t.Errorf("spoken rows lost:\n%s", out)
	}
}

func TestSessionsHistoryWindowIsFramedAsData(t *testing.T) {
	store := &historyFakeTranscript{sessions: map[string][]toolport.ChatMessage{
		"client:kw": historyMessages(30),
	}}
	out, err := toolSessionsHistory(store)(context.Background(),
		sessionSearchJSON(t, map[string]any{"sessionKey": "client:kw", "around": 15}))
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if !strings.HasPrefix(out, transcriptExcerptOpenTag+"\n") || !strings.HasSuffix(out, transcriptExcerptCloseTag) {
		t.Fatalf("windowed history not framed as a transcript excerpt:\n%s", out)
	}
	if !strings.Contains(out, "window around #15") {
		t.Errorf("window header lost inside the envelope:\n%s", out)
	}
}

// Search matches with SearchableText (reasoning included) but renders with
// ExcerptText: a hit that lived only in reasoning is reported as such, never
// as a blank row and never as the reasoning itself.
func TestSessionsSearchIsFramedAsDataAndNamesReasoningOnlyHits(t *testing.T) {
	reasoningOnly := toolport.ChatMessage{Role: "assistant", Timestamp: 5, Content: json.RawMessage(
		`[{"type":"thinking","thinking":"체리픽 브랜치를 먼저 만들어야겠다","signature":"ZZZ"}]`,
	)}
	spoken := toolport.NewTextChatMessage("assistant", "체리픽 끝났어요", 6)
	transcript := &fakeSessionTranscript{results: map[string][]toolport.SearchResult{
		"체리픽": {{SessionKey: "desktop:abc", Matches: []toolport.MatchedMsg{
			{Index: 1, Message: reasoningOnly, Context: []toolport.ChatMessage{spoken}},
		}}},
	}}
	out, err := toolSessionsSearch(transcript)(context.Background(),
		sessionSearchJSON(t, map[string]any{"query": "체리픽"}))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.HasPrefix(out, transcriptExcerptOpenTag+"\n") || !strings.HasSuffix(out, transcriptExcerptCloseTag) {
		t.Fatalf("search not framed as a transcript excerpt:\n%s", out)
	}
	if strings.Contains(out, "먼저 만들어야겠다") {
		t.Errorf("assistant reasoning rendered into a search excerpt:\n%s", out)
	}
	if !strings.Contains(out, "(내부 추론에서 일치 — 본문 생략)") {
		t.Errorf("reasoning-only hit not named — the search claims a hit it cannot show:\n%s", out)
	}
	if !strings.Contains(out, "체리픽 끝났어요") {
		t.Errorf("spoken context row lost:\n%s", out)
	}
}

// Error and no-match replies are not records and must not wear the envelope.
func TestSessionsNonRecordRepliesStayUnframed(t *testing.T) {
	transcript := &fakeSessionTranscript{results: map[string][]toolport.SearchResult{}}
	out, err := toolSessionsSearch(transcript)(context.Background(),
		sessionSearchJSON(t, map[string]any{"query": "zzz-없는-질의-zzz"}))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(out, transcriptExcerptOpenTag) {
		t.Errorf("no-match reply was framed as a record:\n%s", out)
	}
	store := &historyFakeTranscript{sessions: map[string][]toolport.ChatMessage{}}
	out, err = toolSessionsHistory(store)(context.Background(),
		sessionSearchJSON(t, map[string]any{"sessionKey": "client:none"}))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if strings.Contains(out, transcriptExcerptOpenTag) {
		t.Errorf("empty-history reply was framed as a record:\n%s", out)
	}
}
