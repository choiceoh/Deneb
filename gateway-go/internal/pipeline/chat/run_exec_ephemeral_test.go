package chat

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Regression for the heartbeat blind-turn bug: an EphemeralUser turn on a
// history-bearing session (client:main) never persists its trigger message,
// and before the explicit-append fix nothing else put it into the working
// message list — the model saw pure history with no new input.
func TestEphemeralNeedsExplicitAppendReturnsWhetherAppendNeeded(t *testing.T) {
	history := prepResult{Messages: []llm.Message{
		llm.NewTextMessage("user", "이전 질문"),
		llm.NewTextMessage("assistant", "이전 답변"),
	}}
	empty := prepResult{}

	cases := []struct {
		name   string
		params runParams
		prep   prepResult
		want   bool
	}{
		{
			name:   "heartbeat on history-bearing session",
			params: runParams{SessionKey: "client:main", Message: "[시스템 하트비트] 점검", EphemeralUser: true},
			prep:   history,
			want:   true,
		},
		{
			name:   "fresh session keeps scratch-build path",
			params: runParams{SessionKey: "system:boot", Message: "부팅 점검", EphemeralUser: true},
			prep:   empty,
			want:   false,
		},
		{
			name:   "persisted interactive turn is in history already",
			params: runParams{SessionKey: "client:main", Message: "안녕"},
			prep:   history,
			want:   false,
		},
		{
			name: "prebuilt API history carries its own message",
			params: runParams{
				SessionKey: "api:x", Message: "hi", EphemeralUser: true,
				PrebuiltMessages: []llm.Message{llm.NewTextMessage("user", "hi")},
			},
			prep: history,
			want: false,
		},
		{
			name: "enrichment join already handled the append",
			params: runParams{
				SessionKey: "client:main", Message: "링크", EphemeralUser: true,
				AppendCurrentMessage: true,
			},
			prep: history,
			want: false,
		},
	}
	for _, tc := range cases {
		if got := ephemeralNeedsExplicitAppend(tc.params, tc.prep); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFormatTurnUserMessage(t *testing.T) {
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.FixedZone("KST", 9*3600))
	got := formatTurnUserMessage("안녕", now)
	want := "[2026-07-05T10:00:00+09:00] 안녕"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
