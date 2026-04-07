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
		{"mixed quotes in double", `FINAL("he said 'hello'")`, "he said 'hello'"},
		{"mixed quotes in single", `FINAL('she said "hi"')`, `she said "hi"`},
		{"multiline double", "FINAL(\"line1\\nline2\")", "line1\\nline2"},
		{"escaped quote", `FINAL("it\'s fine")`, `it\'s fine`},
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
		MaxIterations:  30,
		FreshTailCount: 48,
	}
	prompt := LoopSystemPrompt(cfg)
	if !strings.Contains(prompt, "```starlark") {
		t.Error("prompt should contain code fence examples")
	}
	if !strings.Contains(prompt, "30") {
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
			"Summary of iterations 1-6: explored context and found key data.",
		},
	}

	// Need >10 messages (keepTail) + 1 head for compaction to trigger.
	messages := []llm.Message{
		llm.NewTextMessage("user", "original prompt"),
	}
	for i := 1; i <= 7; i++ {
		messages = append(messages,
			llm.NewTextMessage("assistant", fmt.Sprintf("iter %d response", i)),
			llm.NewTextMessage("user", fmt.Sprintf("iter %d result", i)),
		)
	}
	// Total: 1 + 14 = 15 messages

	cfg := LoopConfig{
		Client: mock,
		Model:  "test-model",
		System: llm.SystemString("test"),
	}

	compacted, err := compactHistory(context.Background(), cfg, cfg.System, messages)
	if err != nil {
		t.Fatalf("compaction failed: %v", err)
	}

	// Should have: head(1) + summary(1) + tail(10) = 12
	if len(compacted) != 12 {
		t.Errorf("compacted length = %d, want 12", len(compacted))
	}

	// First message should be original prompt.
	var firstContent string
	json.Unmarshal(compacted[0].Content, &firstContent)
	if firstContent != "original prompt" {
		t.Errorf("first message = %q, want %q", firstContent, "original prompt")
	}
}

func TestCompactHistory_TooShort(t *testing.T) {
	// Messages <= keepTail should not be compacted.
	messages := []llm.Message{
		llm.NewTextMessage("user", "prompt"),
		llm.NewTextMessage("assistant", "response"),
	}

	cfg := LoopConfig{
		Client: &mockLLMClient{},
		Model:  "test-model",
		System: llm.SystemString("test"),
	}

	compacted, err := compactHistory(context.Background(), cfg, cfg.System, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compacted) != len(messages) {
		t.Errorf("short history should not be compacted: got %d, want %d", len(compacted), len(messages))
	}
}

func TestIsPrematureFinal(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		iter   int
		want   bool
	}{
		// Short answers are always accepted.
		{"short greeting iter0", "안녕하세요!", 0, false},
		{"short fact iter0", "42", 0, false},
		{"short answer iter1", "네, 맞습니다.", 1, false},

		// After iter 2, always accepted.
		{"long plan iter2", "먼저 context를 살펴보겠습니다. 그다음 분석하겠습니다. " + strings.Repeat("내용", 200), 2, false},
		{"question iter3", "이게 맞나요?", 3, false},

		// Question-form on early iters → rejected.
		{"question iter0", "이 데이터를 어떻게 분석해야 하나요?", 0, true},
		{"question iter1", "What should I do next?", 1, true},
		{"fullwidth question iter0", "어떻게 할까요？", 0, true},

		// Long + plan language on iter 0 → rejected.
		{"plan iter0", "먼저 context를 살펴보겠습니다. 그 다음 데이터를 분석하겠습니다. " + strings.Repeat("내용 ", 100), 0, true},
		{"english plan iter0", "I will analyze the context first. I need to check the data carefully. " + strings.Repeat("content ", 50), 0, true},

		// Long but no plan indicators → accepted.
		{"long real answer iter0", "서울의 인구는 약 950만 명이며, 대한민국의 수도입니다. " + strings.Repeat("추가 정보 ", 100), 0, false},

		// Single plan indicator (not enough) → accepted.
		{"single plan word iter0", "먼저 이 결과를 보시면 데이터가 명확합니다. " + strings.Repeat("결과 ", 100), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPrematureFinal(tt.answer, tt.iter)
			if got != tt.want {
				t.Errorf("isPrematureFinal(%q..., iter=%d) = %v, want %v",
					truncate(tt.answer, 60), tt.iter, got, tt.want)
			}
		})
	}
}

func TestRunLoop_PrematureFinalRejected(t *testing.T) {
	// Iter 0: model outputs a long plan in FINAL — should be rejected.
	// Iter 1: model explores context.
	// Iter 2: model gives real answer — should be accepted.
	mock := &mockLLMClient{
		responses: []string{
			// Iter 0: premature — long plan with intent language
			`FINAL("먼저 context를 살펴보겠습니다. 데이터를 분석하겠습니다. 그런 다음 결과를 정리하고 패턴을 찾아서 최종 결론을 도출하겠습니다. 이를 위해 먼저 context 변수의 크기를 확인하고 메시지 목록을 순회하면서 각 역할별 메시지 수를 집계하겠습니다.")`,
			// Iter 1: explores
			"```starlark\nprint(len(context))\n```",
			// Iter 2: real answer
			`FINAL("context에 2개의 메시지가 있습니다.")`,
		},
	}

	result, err := RunLoop(context.Background(), LoopConfig{
		Client:  mock,
		Model:   "test-model",
		System:  llm.SystemString("test"),
		REPLEnv: newTestREPLEnv(),
		MaxIter: 10,
	}, "context 분석해줘")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "final" {
		t.Errorf("stop_reason = %q, want %q", result.StopReason, "final")
	}
	// The premature FINAL should have been rejected, so the real answer comes from iter 2.
	if result.FinalAnswer != "context에 2개의 메시지가 있습니다." {
		t.Errorf("final_answer = %q, want the real answer from iter 2", result.FinalAnswer)
	}
	if result.Iterations < 3 {
		t.Errorf("iterations = %d, want >= 3 (premature FINAL should not stop early)", result.Iterations)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
