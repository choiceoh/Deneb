package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// runawayThinkingTurn streams a turn that produces ONLY reasoning (a thinking
// block) and then hits max_tokens with no answer text and no tool call — the
// dsv4 thinking-runaway observed in production (32,768 reasoning tokens, textLen 0).
func runawayThinkingTurn(t *testing.T) []llm.StreamEvent {
	t.Helper()
	start, _ := json.Marshal(llm.ContentBlockStart{Index: 0, ContentBlock: llm.ContentBlock{Type: "thinking"}})
	var cbd llm.ContentBlockDelta
	cbd.Index = 0
	cbd.Delta.Type = "thinking_delta"
	cbd.Delta.Thinking = "끝없이 분석만 하는 중…"
	delta, _ := json.Marshal(cbd)
	stop, _ := json.Marshal(llm.ContentBlockStop{Index: 0})
	return []llm.StreamEvent{
		{Type: "content_block_start", Payload: start},
		{Type: "content_block_delta", Payload: delta},
		{Type: "content_block_stop", Payload: stop},
		{Type: "message_delta", Payload: json.RawMessage(`{"delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":32768}}`)},
		makeStreamEvent("message_stop"),
	}
}

// textTurn streams a plain text answer with the given stop reason.
func textTurn(t *testing.T, text, stopReason string) []llm.StreamEvent {
	t.Helper()
	start, _ := json.Marshal(llm.ContentBlockStart{Index: 0, ContentBlock: llm.ContentBlock{Type: "text"}})
	var cbd llm.ContentBlockDelta
	cbd.Index = 0
	cbd.Delta.Type = "text_delta"
	cbd.Delta.Text = text
	delta, _ := json.Marshal(cbd)
	stop, _ := json.Marshal(llm.ContentBlockStop{Index: 0})
	md, _ := json.Marshal(map[string]any{
		"delta": map[string]any{"stop_reason": stopReason},
		"usage": map[string]any{"output_tokens": 5},
	})
	return []llm.StreamEvent{
		{Type: "content_block_start", Payload: start},
		{Type: "content_block_delta", Payload: delta},
		{Type: "content_block_stop", Payload: stop},
		{Type: "message_delta", Payload: md},
		makeStreamEvent("message_stop"),
	}
}

// A thinking runaway (max_tokens with only reasoning, no answer) must be retried
// with thinking OFF — not by scaling the budget, which on dsv4 (high/max effort
// only) just lets it reason longer.
func TestRunAgent_ThinkingRunaway_RetriesWithThinkingOff(t *testing.T) {
	off := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: "thinking"}
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		runawayThinkingTurn(t),
		textTurn(t, "최종 답변입니다.", "end_turn"),
	}}
	cfg := AgentConfig{
		Model:                   "deepseek-v4-flash",
		MaxTokens:               8192,
		MaxTurns:                5,
		Thinking:                &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 32768},
		ThinkingOffRetry:        off,
		MaxOutputTokensRecovery: 1,
	}
	messages := []llm.Message{llm.NewTextMessage("user", "메일 분석해줘")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if len(streamer.recordedThinking) < 2 {
		t.Fatalf("got %d turns, want >=2 (runaway + retry)", len(streamer.recordedThinking))
	}
	if streamer.recordedThinking[0] == nil || streamer.recordedThinking[0].Type != "enabled" {
		t.Errorf("turn1 thinking = %+v, want enabled (the runaway turn)", streamer.recordedThinking[0])
	}
	if streamer.recordedThinking[1] == nil || streamer.recordedThinking[1].Type != "disabled" {
		t.Errorf("turn2 thinking = %+v, want disabled (runaway retry must turn thinking OFF)", streamer.recordedThinking[1])
	}
	if result.Text != "최종 답변입니다." {
		t.Errorf("final text = %q, want the thinking-off retry's answer", result.Text)
	}
}

// A genuine truncated ANSWER (carries text) keeps the legacy recovery: scale the
// budget + resume, thinking unchanged — it must NOT be mistaken for a runaway and
// switched off.
func TestRunAgent_TruncatedAnswer_KeepsThinkingAndResumes(t *testing.T) {
	off := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: "thinking"}
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		textTurn(t, "부분 답변인데 잘림", "max_tokens"),
		textTurn(t, " 이어서 마무리.", "end_turn"),
	}}
	cfg := AgentConfig{
		Model:                   "deepseek-v4-flash",
		MaxTokens:               8192,
		MaxTurns:                5,
		Thinking:                &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 32768},
		ThinkingOffRetry:        off,
		MaxOutputTokensRecovery: 1,
	}
	messages := []llm.Message{llm.NewTextMessage("user", "길게 써줘")}
	_ = testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if len(streamer.recordedThinking) < 2 {
		t.Fatalf("got %d turns, want >=2", len(streamer.recordedThinking))
	}
	if streamer.recordedThinking[1] == nil || streamer.recordedThinking[1].Type != "enabled" {
		t.Errorf("turn2 thinking = %+v, want enabled (truncated-answer recovery must keep thinking on)", streamer.recordedThinking[1])
	}
}
