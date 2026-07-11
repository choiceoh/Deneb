package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// FinalizeGate holds the first finish (injecting its prompt as a user turn)
// and lets the second finish through — mirroring the max_tokens recovery
// shape. The run must terminate with the second turn's text.
func TestRunAgent_FinalizeGateHoldsFirstFinish(t *testing.T) {
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildTextTurnEvents("끝났습니다 (검증 안 함)", 100, 20),
			buildTextTurnEvents("검증 완료: make check 통과", 120, 25),
		},
	}

	var gateCalls atomic.Int32
	cfg := AgentConfig{
		MaxTurns:  5,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
		System:    llm.SystemString("test"),
		FinalizeGate: func(turn int) string {
			if gateCalls.Add(1) == 1 {
				return "[검증 게이트] 검증을 실행하세요."
			}
			return ""
		},
	}

	messages := []llm.Message{llm.NewTextMessage("user", "파일 고쳐줘")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if result.Text != "검증 완료: make check 통과" {
		t.Errorf("Text = %q, want the post-gate turn's text", result.Text)
	}
	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", result.StopReason)
	}
	if got := gateCalls.Load(); got != 2 {
		t.Errorf("gate consulted %d times, want 2 (hold + pass)", got)
	}
	// The injected user-role demand must be in the message history the
	// second LLM call saw.
	var sawGatePrompt bool
	for _, m := range result.FinalMessages {
		// Content is a raw JSON string or block array — substring match is enough.
		if m.Role == "user" && strings.Contains(string(m.Content), "검증 게이트") {
			sawGatePrompt = true
		}
	}
	if !sawGatePrompt {
		t.Error("gate prompt not found in message history")
	}
}

// A nil FinalizeGate (default) leaves the single-turn happy path untouched.
func TestRunAgent_NilFinalizeGateUnchanged(t *testing.T) {
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{buildTextTurnEvents("done", 10, 5)},
	}
	cfg := AgentConfig{MaxTurns: 3, Timeout: 5 * time.Second, MaxTokens: 1024, System: llm.SystemString("t")}
	result := testutil.Must(RunAgent(context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil))
	if result.Text != "done" {
		t.Errorf("Text = %q, want done", result.Text)
	}
}
