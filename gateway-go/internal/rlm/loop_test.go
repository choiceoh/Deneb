package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
)

// mockLLMClient implements agent.LLMStreamer with scripted responses.
type mockLLMClient struct {
	responses []string
	callIdx   int
}

func (m *mockLLMClient) StreamChat(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("StreamChat not implemented in mock")
}

func (m *mockLLMClient) Complete(_ context.Context, _ llm.ChatRequest) (string, error) {
	if m.callIdx >= len(m.responses) {
		return "FINAL(\"no more responses\")", nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

// newTestREPLEnv creates a minimal REPL environment for testing.
func newTestREPLEnv() *repl.Env {
	return repl.NewEnv(context.Background(), repl.EnvConfig{
		Messages: []repl.MessageEntry{
			{Seq: 1, Role: "user", Content: "hello", CreatedAt: 1700000000000},
			{Seq: 2, Role: "assistant", Content: "hi there", CreatedAt: 1700000001000},
		},
		LLMQueryFn: func(_ context.Context, prompt string) (string, error) {
			return "mock llm response to: " + prompt[:min(len(prompt), 50)], nil
		},
		Timeout: 10 * time.Second,
	})
}

func TestRunLoop_SimpleFinal(t *testing.T) {
	mock := &mockLLMClient{
		responses: []string{
			"The user said hello. FINAL(\"안녕하세요!\")",
		},
	}

	result, err := RunLoop(context.Background(), LoopConfig{
		Client:  mock,
		Model:   "test-model",
		System:  llm.SystemString("test"),
		REPLEnv: newTestREPLEnv(),
		MaxIter: 10,
	}, "인사해줘")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "final" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "final")
	}
	if result.FinalAnswer != "안녕하세요!" {
		t.Errorf("final_answer = %q, want %q", result.FinalAnswer, "안녕하세요!")
	}
	if result.Iterations != 1 {
		t.Errorf("iterations = %d, want 1", result.Iterations)
	}
}

func TestRunLoop_CodeBlockExecution(t *testing.T) {
	mock := &mockLLMClient{
		responses: []string{
			// First iteration: explore context
			"Let me check the context.\n```starlark\nprint(len(context))\n```",
			// Second iteration: produce final answer
			"```starlark\nFINAL(\"context has 2 messages\")\n```",
		},
	}

	result, err := RunLoop(context.Background(), LoopConfig{
		Client:  mock,
		Model:   "test-model",
		System:  llm.SystemString("test"),
		REPLEnv: newTestREPLEnv(),
		MaxIter: 10,
	}, "context 크기 알려줘")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "final" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "final")
	}
	if result.FinalAnswer != "context has 2 messages" {
		t.Errorf("final_answer = %q, want %q", result.FinalAnswer, "context has 2 messages")
	}
	if result.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", result.Iterations)
	}
}

func TestRunLoop_MaxIterationsFallback(t *testing.T) {
	mock := &mockLLMClient{
		responses: []string{
			"Hmm, let me think...",
			"Still thinking...",
			"Almost there...",
			// Fallback call: LLM generates the final answer
			"Based on my analysis, the answer is 42.",
		},
	}

	result, err := RunLoop(context.Background(), LoopConfig{
		Client:          mock,
		Model:           "test-model",
		System:          llm.SystemString("test"),
		REPLEnv:         newTestREPLEnv(),
		MaxIter:         3,
		FallbackEnabled: true,
	}, "answer to life")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "max_iterations" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "max_iterations")
	}
	if !result.FallbackUsed {
		t.Error("fallback_used should be true")
	}
	if result.FinalAnswer == "" {
		t.Error("final_answer should not be empty when fallback is enabled")
	}
}

func TestRunLoop_ConsecutiveErrors(t *testing.T) {
	mock := &mockLLMClient{
		responses: []string{
			"```starlark\nundefined_var\n```",
			"```starlark\nalso_undefined\n```",
			"```starlark\nstill_undefined\n```",
		},
	}

	result, err := RunLoop(context.Background(), LoopConfig{
		Client:          mock,
		Model:           "test-model",
		System:          llm.SystemString("test"),
		REPLEnv:         newTestREPLEnv(),
		MaxIter:         10,
		MaxConsecErrors: 3,
		FallbackEnabled: false,
	}, "test errors")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "max_errors" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "max_errors")
	}
	if result.ErrorCount < 3 {
		t.Errorf("error_count = %d, want >= 3", result.ErrorCount)
	}
}

func TestRunLoop_BudgetExhaustion(t *testing.T) {
	mock := &mockLLMClient{
		responses: []string{
			"thinking about it...",
		},
	}

	budget := NewTokenBudget(10) // Very small budget
	budget.TryReserve(10)        // Exhaust it

	result, err := RunLoop(context.Background(), LoopConfig{
		Client:  mock,
		Model:   "test-model",
		System:  llm.SystemString("test"),
		REPLEnv: newTestREPLEnv(),
		MaxIter: 10,
		Budget:  budget,
	}, "test budget")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "budget" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "budget")
	}
}

func TestRunLoop_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	mock := &mockLLMClient{
		responses: []string{"should not be called"},
	}

	result, err := RunLoop(ctx, LoopConfig{
		Client:  mock,
		Model:   "test-model",
		System:  llm.SystemString("test"),
		REPLEnv: newTestREPLEnv(),
		MaxIter: 10,
	}, "test cancel")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "cancelled" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "cancelled")
	}
}

func TestRunLoop_VariablePersistence(t *testing.T) {
	mock := &mockLLMClient{
		responses: []string{
			// First: assign a variable
			"```starlark\nmy_var = 42\nprint(my_var)\n```",
			// Second: use the variable from previous iteration
			"```starlark\nresult = my_var * 2\nFINAL(str(result))\n```",
		},
	}

	result, err := RunLoop(context.Background(), LoopConfig{
		Client:  mock,
		Model:   "test-model",
		System:  llm.SystemString("test"),
		REPLEnv: newTestREPLEnv(),
		MaxIter: 10,
	}, "double 42")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinalAnswer != "84" {
		t.Errorf("final_answer = %q, want %q", result.FinalAnswer, "84")
	}
}

func TestRunLoop_OnTextDeltaCallback(t *testing.T) {
	mock := &mockLLMClient{
		responses: []string{
			"FINAL(\"done\")",
		},
	}

	var captured []string
	result, err := RunLoop(context.Background(), LoopConfig{
		Client:  mock,
		Model:   "test-model",
		System:  llm.SystemString("test"),
		REPLEnv: newTestREPLEnv(),
		MaxIter: 10,
		OnTextDelta: func(text string) {
			captured = append(captured, text)
		},
	}, "test callback")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "final" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "final")
	}
	if len(captured) == 0 {
		t.Error("OnTextDelta should have been called")
	}
}

func TestExtractCodeBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"no blocks", "just text", 0},
		{"starlark block", "```starlark\nprint(1)\n```", 1},
		{"python block", "```python\nprint(1)\n```", 1},
		{"repl block", "```repl\nprint(1)\n```", 1},
		{"bare block", "```\nprint(1)\n```", 1},
		{"multiple blocks", "```starlark\nx=1\n```\ntext\n```python\ny=2\n```", 2},
		{"empty block", "```starlark\n\n```", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := extractCodeBlocks(tt.input)
			if len(blocks) != tt.want {
				t.Errorf("got %d blocks, want %d", len(blocks), tt.want)
			}
		})
	}
}

func TestExtractTextFinal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no final", "just text", ""},
		{"double quote", `FINAL("hello world")`, "hello world"},
		{"single quote", `FINAL('hello world')`, "hello world"},
		{"inside code block ignored", "```starlark\nFINAL(\"inside\")\n```", ""},
		{"outside code block", "text\n```starlark\ncode\n```\nFINAL(\"outside\")", "outside"},
		{"with whitespace", `FINAL( "spaced" )`, "spaced"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextFinal(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "hello world"),
	}
	est := estimateTokens(msgs)
	if est <= 0 {
		t.Errorf("estimate should be > 0, got %d", est)
	}
}

func TestLoopSystemPrompt_ContainsFence(t *testing.T) {
	cfg := Config{
		MaxIterations: 25,
		FreshTailCount:    48,
	}
	prompt := LoopSystemPrompt(cfg)
	if !strings.Contains(prompt, "```starlark") {
		t.Error("prompt should contain code fence examples")
	}
	if !strings.Contains(prompt, "25") {
		t.Error("prompt should contain max iterations")
	}
	if !strings.Contains(prompt, "48") {
		t.Error("prompt should contain fresh tail count")
	}
}

func TestCompactHistory(t *testing.T) {
	// Test that compaction preserves head and tail.
	mock := &mockLLMClient{
		responses: []string{
			"Summary of iterations 1-3: explored context and found key data.",
		},
	}

	messages := []llm.Message{
		llm.NewTextMessage("user", "original prompt"),
		llm.NewTextMessage("assistant", "iter 1 response"),
		llm.NewTextMessage("user", "iter 1 result"),
		llm.NewTextMessage("assistant", "iter 2 response"),
		llm.NewTextMessage("user", "iter 2 result"),
		llm.NewTextMessage("assistant", "iter 3 response"),
		llm.NewTextMessage("user", "iter 3 result"),
		llm.NewTextMessage("assistant", "iter 4 response"),
		llm.NewTextMessage("user", "iter 4 result"),
	}

	cfg := LoopConfig{
		Client: mock,
		Model:  "test-model",
		System: llm.SystemString("test"),
	}

	compacted, err := compactHistory(context.Background(), cfg, cfg.System, messages)
	if err != nil {
		t.Fatalf("compaction failed: %v", err)
	}

	// Should have: head(1) + summary(1) + tail(4) = 6
	if len(compacted) != 6 {
		t.Errorf("compacted length = %d, want 6", len(compacted))
	}

	// First message should be original prompt.
	var firstContent string
	json.Unmarshal(compacted[0].Content, &firstContent)
	if firstContent != "original prompt" {
		t.Errorf("first message = %q, want %q", firstContent, "original prompt")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
