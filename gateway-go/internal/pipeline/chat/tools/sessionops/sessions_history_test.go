package sessionops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// historyFakeTranscript backs the history tests with real Load/ListKeys
// semantics (tail slice on limit>0, full load on limit<=0) so the resolver and
// window math are exercised against the store contract, not a stub.
type historyFakeTranscript struct {
	fakeSessionTranscript
	sessions map[string][]toolport.ChatMessage
}

func (f *historyFakeTranscript) Load(key string, limit int) ([]toolport.ChatMessage, int, error) {
	msgs := f.sessions[key]
	total := len(msgs)
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, total, nil
}

func (f *historyFakeTranscript) ListKeys() ([]string, error) {
	keys := make([]string, 0, len(f.sessions))
	for k := range f.sessions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func historyMessages(n int) []toolport.ChatMessage {
	msgs := make([]toolport.ChatMessage, 0, n)
	for i := 1; i <= n; i++ {
		msgs = append(msgs, toolport.NewTextChatMessage("user", fmt.Sprintf("msg %d", i), int64(i)))
	}
	return msgs
}

func TestSessionsHistoryResolvesRecallRefTailAndOpensWindow(t *testing.T) {
	fake := &historyFakeTranscript{sessions: map[string][]toolport.ChatMessage{
		"client:lm:6f9b354f:s38": historyMessages(30),
	}}
	out, err := toolSessionsHistory(fake)(context.Background(),
		sessionSearchJSON(t, map[string]any{"action": "history", "sessionKey": "s38#12/user", "limit": 6}))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, `"client:lm:6f9b354f:s38"`) {
		t.Fatalf("tail ref did not resolve to the full key:\n%s", out)
	}
	if !strings.Contains(out, "window around #12") || !strings.Contains(out, "messages 9..14 of 30") {
		t.Fatalf("expected a 6-message window centered on #12:\n%s", out)
	}
	if !strings.Contains(out, "9. [user] msg 9") || strings.Contains(out, "8. [user]") {
		t.Fatalf("window rows must carry absolute message numbers:\n%s", out)
	}
}

func TestSessionsHistoryAnchoredReadIsDeepNotPreview(t *testing.T) {
	long := strings.Repeat("파라미터 목록 항목입니다. ", 300) // ~4200 runes ≈ 12KB — beyond the old 1200B cap
	msgs := historyMessages(30)
	msgs[14] = toolport.NewTextChatMessage("assistant", long, 15)
	fake := &historyFakeTranscript{sessions: map[string][]toolport.ChatMessage{
		"client:deep": msgs,
	}}
	out, err := toolSessionsHistory(fake)(context.Background(),
		sessionSearchJSON(t, map[string]any{"action": "history", "sessionKey": "deep#15/assistant"}))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "messages 10..19 of 30") {
		t.Fatalf("anchored read without limit must default to a 10-message window:\n%s", out[:200])
	}
	// The deep read exists to carry long answers: well past the old 1200B cap.
	if !strings.Contains(out, strings.Repeat("파라미터 목록 항목입니다. ", 80)) {
		t.Fatalf("long assistant message must survive far beyond 1200 bytes in the window")
	}
}

func TestSessionsHistoryReportsAmbiguousRefTail(t *testing.T) {
	fake := &historyFakeTranscript{sessions: map[string][]toolport.ChatMessage{
		"lane-a:s38": historyMessages(3),
		"lane-b:s38": historyMessages(3),
	}}
	out, err := toolSessionsHistory(fake)(context.Background(),
		sessionSearchJSON(t, map[string]any{"action": "history", "sessionKey": "s38#1/user"}))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "matches 2 sessions") ||
		!strings.Contains(out, "lane-a:s38") || !strings.Contains(out, "lane-b:s38") {
		t.Fatalf("ambiguous tail must list the candidate keys:\n%s", out)
	}
}

func TestSessionsHistoryExactKeyAndAbbreviationStillWork(t *testing.T) {
	fake := &historyFakeTranscript{sessions: map[string][]toolport.ChatMessage{
		"client:abc-123": historyMessages(5),
	}}
	for _, key := range []string{"client:abc-123", "cl:abc-123"} {
		out, err := toolSessionsHistory(fake)(context.Background(),
			sessionSearchJSON(t, map[string]any{"action": "history", "sessionKey": key}))
		if err != nil {
			t.Fatalf("history(%s): %v", key, err)
		}
		if !strings.Contains(out, "5 of 5 messages") {
			t.Fatalf("key %q must load the tail preview:\n%s", key, out)
		}
	}
}

// semanticFakeTranscript layers the OPTIONAL meaning-search capability on the
// history fake so the merge path is exercised through the real type assertion.
type semanticFakeTranscript struct {
	historyFakeTranscript
	results []toolport.SearchResult
	hits    []toolport.SemanticSessionHit
}

func (f *semanticFakeTranscript) Search(string, int) ([]toolport.SearchResult, error) {
	return f.results, nil
}

func (f *semanticFakeTranscript) SearchSessionsSemantic(_ context.Context, _, _ string, _ int) []toolport.SemanticSessionHit {
	return f.hits
}

func TestSessionsSearchMergesSemanticAndDatesHeaders(t *testing.T) {
	fake := &semanticFakeTranscript{
		results: []toolport.SearchResult{{
			SessionKey: "client:kw",
			Matches: []toolport.MatchedMsg{{
				Index:   0,
				Message: toolport.NewTextChatMessage("user", "요가 수업 등록했어", 1684540800000), // 2023-05-20
			}},
		}},
		hits: []toolport.SemanticSessionHit{
			{SessionKey: "client:kw", Snippet: "중복 — 키워드가 이미 찾음", At: 1, Score: 0.9},
			{SessionKey: "client:sem", Snippet: "필라테스 체험 후기", At: 1684627200000, Score: 0.81},
		},
	}
	out, err := toolSessionsSearch(fake)(context.Background(),
		sessionSearchJSON(t, map[string]any{"action": "search", "query": "운동 수업"}))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "### Session: client:kw (2023-05-2") {
		t.Fatalf("keyword session header must carry the conversation date:\n%s", out)
	}
	if !strings.Contains(out, "의미 일치 대화") || !strings.Contains(out, "client:sem") {
		t.Fatalf("semantic section must render un-covered sessions:\n%s", out)
	}
	if strings.Count(out, "client:kw") != 1 {
		t.Fatalf("semantic hits covered by keyword results must dedup:\n%s", out)
	}
}
