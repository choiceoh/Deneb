package autoreply

import (
	"context"
	"fmt"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// mockLLM implements LLMClient for testing.
type mockLLM struct {
	responses []*LLMResponse
	callCount int
}

func (m *mockLLM) Chat(_ context.Context, _ AgentRunnerPayload) (*LLMResponse, error) {
	if m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("no more mock responses")
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockLLM) ChatStream(_ context.Context, _ AgentRunnerPayload) (LLMStreamIterator, error) {
	return nil, fmt.Errorf("streaming not mocked")
}

// mockTools implements ToolExecutor for testing.
type mockTools struct {
	results map[string]string
}

func (m *mockTools) Execute(_ context.Context, call ToolCall) (string, bool, error) {
	if result, ok := m.results[call.Name]; ok {
		return result, false, nil
	}
	return "", true, fmt.Errorf("unknown tool: %s", call.Name)
}

func TestDefaultAgentRunner_SimpleReply(t *testing.T) {
	runner := NewDefaultAgentRunner(AgentRunnerConfig{
		LLM: &mockLLM{responses: []*LLMResponse{
			{Content: "Hello!", StopReason: "end_turn", Usage: session.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}},
		}},
		Logger: testSlogLogger(),
	})

	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey: "test",
		Model:      "test-model",
		Message:    "Hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OutputText != "Hello!" {
		t.Errorf("output = %q, want 'Hello!'", result.OutputText)
	}
	if result.TokensUsed.TotalTokens != 15 {
		t.Errorf("tokens = %d, want 15", result.TokensUsed.TotalTokens)
	}
	if len(result.Payloads) != 1 {
		t.Errorf("payloads = %d, want 1", len(result.Payloads))
	}
	if result.TurnCount != 1 {
		t.Errorf("turns = %d, want 1", result.TurnCount)
	}
}

func TestDefaultAgentRunner_ToolExecution(t *testing.T) {
	runner := NewDefaultAgentRunner(AgentRunnerConfig{
		LLM: &mockLLM{responses: []*LLMResponse{
			{
				Content:    "",
				StopReason: "tool_use",
				ToolCalls:  []ToolCall{{ID: "t1", Name: "bash", Input: map[string]any{"command": "ls"}}},
				Usage:      session.TokenUsage{TotalTokens: 20},
			},
			{
				Content:    "Here are your files.",
				StopReason: "end_turn",
				Usage:      session.TokenUsage{TotalTokens: 10},
			},
		}},
		Tools:  &mockTools{results: map[string]string{"bash": "file1.txt\nfile2.txt"}},
		Logger: testSlogLogger(),
	})

	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey:    "test",
		Model:         "test-model",
		Message:       "List files",
		ElevatedLevel: types.ElevatedOn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TurnCount != 2 {
		t.Errorf("turns = %d, want 2", result.TurnCount)
	}
	if result.ToolMeta.Count() != 1 {
		t.Errorf("tool calls = %d, want 1", result.ToolMeta.Count())
	}
	if !result.ToolMeta.HasTool("bash") {
		t.Error("expected bash tool to be recorded")
	}
	if result.TokensUsed.TotalTokens != 30 {
		t.Errorf("total tokens = %d, want 30", result.TokensUsed.TotalTokens)
	}
}

func TestDefaultAgentRunner_ElevatedBlocked(t *testing.T) {
	runner := NewDefaultAgentRunner(AgentRunnerConfig{
		LLM: &mockLLM{responses: []*LLMResponse{
			{
				StopReason: "tool_use",
				ToolCalls:  []ToolCall{{ID: "t1", Name: "bash", Input: map[string]any{}}},
			},
			{Content: "OK", StopReason: "end_turn"},
		}},
		Tools:  &mockTools{results: map[string]string{}},
		Logger: testSlogLogger(),
	})

	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey:    "test",
		Model:         "test-model",
		Message:       "run something",
		ElevatedLevel: types.ElevatedOff, // blocked
	})
	if err != nil {
		t.Fatal(err)
	}
	// Tool should be blocked, but agent continues.
	if result.ToolMeta.Count() != 1 {
		t.Errorf("tool calls = %d, want 1", result.ToolMeta.Count())
	}
	if result.ToolMeta.ErrorCount() != 1 {
		t.Errorf("tool errors = %d, want 1", result.ToolMeta.ErrorCount())
	}
}

func TestDefaultAgentRunner_Timeout(t *testing.T) {
	runner := NewDefaultAgentRunner(AgentRunnerConfig{
		LLM:    &mockLLM{responses: []*LLMResponse{}}, // no responses → will error
		Logger: testSlogLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	result, err := runner.RunTurn(ctx, AgentTurnConfig{
		SessionKey: "test",
		Model:      "test-model",
		Message:    "Hi",
		TimeoutMs:  1, // very short
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.WasAborted {
		t.Error("expected WasAborted = true")
	}
}

func TestAgentRunnerMemory_Compaction(t *testing.T) {
	mem := NewAgentRunnerMemory(100) // tiny budget
	mem.Append(AgentMessage{Role: "system", Content: "You are helpful."})
	for i := 0; i < 20; i++ {
		mem.Append(AgentMessage{Role: "user", Content: fmt.Sprintf("Message %d with some padding text to use tokens", i)})
	}

	before := mem.MessageCount()
	removed := mem.Compact()
	after := mem.MessageCount()

	if removed == 0 {
		t.Error("expected some messages to be compacted")
	}
	if after >= before {
		t.Errorf("after (%d) should be less than before (%d)", after, before)
	}
	// System message should be preserved.
	history := mem.History()
	if len(history) > 0 && history[0].Role != "system" {
		t.Error("system message should be preserved after compaction")
	}
}

func TestAgentRunnerMemory_CompactWithSummary(t *testing.T) {
	mem := NewAgentRunnerMemory(50) // tiny budget
	mem.Append(AgentMessage{Role: "system", Content: "System."})
	for i := 0; i < 10; i++ {
		mem.Append(AgentMessage{Role: "user", Content: fmt.Sprintf("Long message %d padding padding padding", i)})
	}

	removed := mem.CompactWithSummary("User discussed 10 topics.")
	if removed == 0 {
		t.Error("expected messages to be removed")
	}

	history := mem.History()
	if len(history) < 3 {
		t.Errorf("expected at least 3 messages (system + summary + recent), got %d", len(history))
	}
	// Check summary is present.
	foundSummary := false
	for _, m := range history {
		if m.Role == "system" && m.Content != "System." {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Error("expected summary message in history")
	}
}

func TestBuildAgentPayload_ThinkingConfig(t *testing.T) {
	cfg := AgentTurnConfig{
		Model:      "claude-3",
		ThinkLevel: types.ThinkHigh,
		MaxTokens:  4096,
	}
	payload := BuildAgentPayload(cfg, nil, nil)
	if payload.Thinking == nil {
		t.Fatal("expected thinking config")
	}
	if payload.Thinking.Type != "enabled" {
		t.Errorf("thinking type = %q, want 'enabled'", payload.Thinking.Type)
	}
	if payload.Thinking.BudgetTokens != 32768 {
		t.Errorf("budget = %d, want 32768", payload.Thinking.BudgetTokens)
	}
}

func TestReminderGuard(t *testing.T) {
	g := NewReminderGuard(2)
	if !g.TryRemind() {
		t.Error("first remind should succeed")
	}
	if !g.TryRemind() {
		t.Error("second remind should succeed")
	}
	if g.TryRemind() {
		t.Error("third remind should fail")
	}
	g.Reset()
	if !g.TryRemind() {
		t.Error("after reset, remind should succeed")
	}
}

func TestFormatUsageSummary(t *testing.T) {
	if FormatUsageSummary(session.TokenUsage{}) != "" {
		t.Error("zero usage should return empty")
	}
	got := FormatUsageSummary(session.TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150})
	if got != "150 tokens (in: 100, out: 50)" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultAgentRunner_ContextOverflowRecovery(t *testing.T) {
	resetCalled := false
	runner := NewDefaultAgentRunner(AgentRunnerConfig{
		LLM:    &mockLLM{responses: []*LLMResponse{}}, // will error
		Logger: testSlogLogger(),
	})
	runner.onSessionReset = func(key, reason string) {
		resetCalled = true
		if reason != "context_overflow" {
			t.Errorf("expected 'context_overflow', got %q", reason)
		}
	}
	// Override llm to return context overflow error.
	runner.llm = &errorLLM{err: fmt.Errorf("context window exceeded: too large")}

	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey: "test", Model: "m", Message: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resetCalled {
		t.Error("expected session reset callback")
	}
	if len(result.Payloads) == 0 || !result.Payloads[0].IsError {
		t.Error("expected error payload")
	}
}

func TestDefaultAgentRunner_BillingError(t *testing.T) {
	runner := NewDefaultAgentRunner(AgentRunnerConfig{
		LLM:    &errorLLM{err: fmt.Errorf("billing: insufficient_quota")},
		Logger: testSlogLogger(),
	})
	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey: "test", Model: "m", Message: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("payloads=%d, error=%v, wasAborted=%v", len(result.Payloads), result.Error, result.WasAborted)
	for i, p := range result.Payloads {
		t.Logf("  payload[%d]: text=%q isError=%v", i, p.Text, p.IsError)
	}
	if len(result.Payloads) == 0 {
		t.Fatal("expected payloads")
	}
	if result.Payloads[0].Text != BillingErrorMessage {
		t.Errorf("got %q", result.Payloads[0].Text)
	}
}

func TestIsContextOverflowError(t *testing.T) {
	if !IsContextOverflowError("context window exceeded") {
		t.Error("should detect overflow")
	}
	if IsContextOverflowError("normal error") {
		t.Error("should not detect overflow")
	}
}

func TestIsTransientHTTPError(t *testing.T) {
	if !IsTransientHTTPError("HTTP 502 Bad Gateway") {
		t.Error("should detect 502")
	}
	if !IsTransientHTTPError("rate limited 429") {
		t.Error("should detect 429")
	}
	if IsTransientHTTPError("normal error") {
		t.Error("should not detect")
	}
}

// errorLLM always returns an error.
type errorLLM struct{ err error }

func (m *errorLLM) Chat(_ context.Context, _ AgentRunnerPayload) (*LLMResponse, error) {
	return nil, m.err
}
func (m *errorLLM) ChatStream(_ context.Context, _ AgentRunnerPayload) (LLMStreamIterator, error) {
	return nil, m.err
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{500, "500ms"},
		{1500, "1.5s"},
		{65000, "1m5s"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.ms)
		if got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}
