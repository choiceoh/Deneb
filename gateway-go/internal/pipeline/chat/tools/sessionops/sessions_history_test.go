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
