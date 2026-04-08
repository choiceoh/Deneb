package autoreply

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// --- SSE streaming helpers (mirrors chat/agent_test.go) ---

func sseTextResponse(text, stopReason string) string {
	finishReason := stopReason
	if finishReason == "end_turn" {
		finishReason = "stop"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"%s\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0}}\n\n", text)
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"%s\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n", finishReason)
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func sseToolResponse(toolID, toolName, toolInput string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"%s\",\"type\":\"function\",\"function\":{\"name\":\"%s\",\"arguments\":\"\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0}}\n\n", toolID, toolName)
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"%s\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n", toolInput)
	b.WriteString("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":10}}\n\n")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// newSSEServer builds a test HTTP server that cycles through a list of SSE response bodies.
func newSSEServer(responses []string) (*httptest.Server, *int) {
	callCount := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := *callCount
		*callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if idx < len(responses) {
			fmt.Fprint(w, responses[idx])
		}
	}))
	return srv, callCount
}

// newErrorServer builds a test server that returns the given HTTP status code.
func newErrorServer(statusCode int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		fmt.Fprint(w, body)
	}))
}

// newRunner creates a DefaultAgentRunner backed by an llm.Client pointing at srv.
func newRunner(srv *httptest.Server, tools ToolExecutor) *DefaultAgentRunner {
	client := llm.NewClient(srv.URL, "test-key")
	return NewDefaultAgentRunner(AgentRunnerConfig{
		LLM:    client,
		Tools:  tools,
		Logger: testSlogLogger(),
	})
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

// --- Runner tests ---

func TestDefaultAgentRunner_SimpleReply(t *testing.T) {
	srv, _ := newSSEServer([]string{sseTextResponse("Hello!", "end_turn")})
	defer srv.Close()

	runner := newRunner(srv, nil)
	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey: "test",
		Model:      "test-model",
		Message:    "Hi",
	})
	testutil.NoError(t, err)
	if result.OutputText != "Hello!" {
		t.Errorf("output = %q, want 'Hello!'", result.OutputText)
	}
	// Token counts come from the SSE stream (prompt_tokens=10, completion_tokens=5).
	if result.TokensUsed.TotalTokens == 0 {
		t.Error("expected non-zero token usage")
	}
	if len(result.Payloads) != 1 {
		t.Errorf("payloads = %d, want 1", len(result.Payloads))
	}
	if result.TurnCount != 1 {
		t.Errorf("turns = %d, want 1", result.TurnCount)
	}
}

func TestDefaultAgentRunner_ToolExecution(t *testing.T) {
	srv, _ := newSSEServer([]string{
		sseToolResponse("t1", "bash", `{\\\"command\\\":\\\"ls\\\"}`),
		sseTextResponse("Here are your files.", "end_turn"),
	})
	defer srv.Close()

	tools := &mockTools{results: map[string]string{"bash": "file1.txt\nfile2.txt"}}
	runner := newRunner(srv, tools)

	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey:    "test",
		Model:         "test-model",
		Message:       "List files",
		ElevatedLevel: types.ElevatedOn,
	})
	testutil.NoError(t, err)
	if result.TurnCount != 2 {
		t.Errorf("turns = %d, want 2", result.TurnCount)
	}
	if result.ToolMeta.Count() != 1 {
		t.Errorf("tool calls = %d, want 1", result.ToolMeta.Count())
	}
	if !result.ToolMeta.HasTool("bash") {
		t.Error("expected bash tool to be recorded")
	}
}

func TestDefaultAgentRunner_ElevatedBlocked(t *testing.T) {
	// bash is blocked (ElevatedOff), then model gets the error and replies normally.
	srv, _ := newSSEServer([]string{
		sseToolResponse("t1", "bash", `{}`),
		sseTextResponse("OK", "end_turn"),
	})
	defer srv.Close()

	tools := &mockTools{results: map[string]string{}}
	runner := newRunner(srv, tools)

	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey:    "test",
		Model:         "test-model",
		Message:       "run something",
		ElevatedLevel: types.ElevatedOff, // blocked
	})
	testutil.NoError(t, err)
	// Tool should be recorded as an error (blocked).
	if result.ToolMeta.Count() != 1 {
		t.Errorf("tool calls = %d, want 1", result.ToolMeta.Count())
	}
	if result.ToolMeta.ErrorCount() != 1 {
		t.Errorf("tool errors = %d, want 1", result.ToolMeta.ErrorCount())
	}
}

func TestDefaultAgentRunner_Timeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	srv, _ := newSSEServer(nil)
	defer srv.Close()

	runner := newRunner(srv, nil)
	result, err := runner.RunTurn(ctx, AgentTurnConfig{
		SessionKey: "test",
		Model:      "test-model",
		Message:    "Hi",
		TimeoutMs:  1,
	})
	testutil.NoError(t, err)
	if !result.WasAborted {
		t.Error("expected WasAborted = true")
	}
}

func TestDefaultAgentRunner_ContextOverflowRecovery(t *testing.T) {
	resetCalled := false
	// 413 triggers context overflow detection in the response body.
	srv := newErrorServer(http.StatusOK, "data: {\"error\":{\"message\":\"context window exceeded: too large\"}}\n\ndata: [DONE]\n\n")
	defer srv.Close()

	runner := newRunner(srv, nil)
	runner.onSessionReset = func(key, reason string) {
		resetCalled = true
		if reason != "context_overflow" {
			t.Errorf("got %q, want 'context_overflow'", reason)
		}
	}
	// Replace the LLM with a streamer that always returns a context-overflow error.
	runner.llm = &alwaysErrorStreamer{err: fmt.Errorf("context window exceeded: too large")}

	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey: "test", Model: "m", Message: "hi",
	})
	testutil.NoError(t, err)
	if !resetCalled {
		t.Error("expected session reset callback")
	}
	if len(result.Payloads) == 0 || !result.Payloads[0].IsError {
		t.Error("expected error payload")
	}
}

func TestDefaultAgentRunner_BillingError(t *testing.T) {
	runner := &DefaultAgentRunner{
		llm:      &alwaysErrorStreamer{err: fmt.Errorf("billing: insufficient_quota")},
		maxTurns: 25,
		logger:   testSlogLogger(),
	}
	result, err := runner.RunTurn(context.Background(), AgentTurnConfig{
		SessionKey: "test", Model: "m", Message: "hi",
	})
	testutil.NoError(t, err)
	if len(result.Payloads) == 0 {
		t.Fatal("expected payloads")
	}
	if result.Payloads[0].Text != BillingErrorMessage {
		t.Errorf("got %q", result.Payloads[0].Text)
	}
}

// alwaysErrorStreamer implements agent.LLMStreamer and always returns an error.
type alwaysErrorStreamer struct{ err error }

func (a *alwaysErrorStreamer) StreamChat(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return nil, a.err
}
func (a *alwaysErrorStreamer) Complete(_ context.Context, _ llm.ChatRequest) (string, error) {
	return "", a.err
}

// Ensure *llm.Client satisfies agent.LLMStreamer (compile-time check).
var _ agent.LLMStreamer = (*llm.Client)(nil)

// --- Memory tests ---

func TestAgentRunnerMemory_Compaction(t *testing.T) {
	mem := NewAgentRunnerMemory(100)
	mem.Append(AgentMessage{Role: "system", Content: "You are helpful."})
	for i := range 20 {
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
	history := mem.History()
	if len(history) > 0 && history[0].Role != "system" {
		t.Error("system message should be preserved after compaction")
	}
}

func TestAgentRunnerMemory_CompactWithSummary(t *testing.T) {
	mem := NewAgentRunnerMemory(50)
	mem.Append(AgentMessage{Role: "system", Content: "System."})
	for i := range 10 {
		mem.Append(AgentMessage{Role: "user", Content: fmt.Sprintf("Long message %d padding padding padding", i)})
	}

	removed := mem.CompactWithSummary("User discussed 10 topics.")
	if removed == 0 {
		t.Error("expected messages to be removed")
	}

	history := mem.History()
	if len(history) < 3 {
		t.Errorf("got %d, want at least 3 messages (system + summary + recent)", len(history))
	}
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

