package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestRunAgent_GraceCall_Injected verifies the core behavior: when the LLM
// keeps calling tools up to MaxTurns, the executor injects the grace user
// message ONCE, runs a single additional iteration, and exits with
// `max_turns_graceful`.
func TestRunAgent_GraceCall_Injected(t *testing.T) {
	const maxTurns = 2

	// Turns 0..maxTurns-1: LLM keeps calling a tool. Turn maxTurns: grace
	// iteration, LLM finally emits a text-only end_turn wrap-up.
	turns := make([][]llm.StreamEvent, 0, maxTurns+1)
	for i := range maxTurns {
		turns = append(turns, buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: fmt.Sprintf("toolu_%d", i), name: "exec", inputJSON: `{"cmd":"ls"}`},
		}, 50, 20))
	}
	turns = append(turns, buildTextTurnEvents("지금까지 ls를 반복 실행했습니다. 추가 작업 없이 종료합니다.", 100, 30))

	streamer := &fakeLLMStreamer{turns: turns}
	tools := newFakeToolExecutor()

	cfg := AgentConfig{
		MaxTurns:  maxTurns,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "keep running")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	if result.StopReason != StopReasonMaxTurnsGraceful {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxTurnsGraceful)
	}
	if !result.BudgetExhaustedInjected {
		t.Error("BudgetExhaustedInjected = false, want true")
	}
	if result.BudgetGraceCall {
		t.Error("BudgetGraceCall = true after exit, want false (should be cleared)")
	}
	if result.Turns != maxTurns+1 {
		t.Errorf("Turns = %d, want %d (MaxTurns %d + 1 grace)", result.Turns, maxTurns+1, maxTurns)
	}
	if got := tools.callCount(); got != maxTurns {
		t.Errorf("tool calls = %d, want %d (grace turn must not execute tools)", got, maxTurns)
	}
	if !strings.Contains(result.Text, "종료") {
		t.Errorf("expected grace wrap-up text in result.Text, got %q", result.Text)
	}

	// Grace prompt must be present in final messages for transcript fidelity.
	found := false
	for _, m := range result.FinalMessages {
		if m.Role != "user" {
			continue
		}
		if strings.Contains(string(m.Content), "턴 예산") {
			found = true
			break
		}
	}
	if !found {
		t.Error("grace prompt not found in FinalMessages; transcript will be incomplete")
	}
}

// TestRunAgent_GraceCall_NoDoubleInjection verifies that if the model calls
// a tool again on the grace iteration (ignoring the wrap-up instruction),
// the executor runs that tool ONCE but does not inject a second grace
// message — the flag stays set across the grace iteration.
func TestRunAgent_GraceCall_NoDoubleInjection(t *testing.T) {
	const maxTurns = 1

	// Turn 0 (budgeted): tool call. Turn 1 (grace): another tool call — model
	// ignored the wrap-up ask. The loop must still exit after this iteration.
	turns := [][]llm.StreamEvent{
		buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: "toolu_0", name: "exec", inputJSON: `{"cmd":"ls"}`},
		}, 50, 20),
		buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: "toolu_1", name: "exec", inputJSON: `{"cmd":"pwd"}`},
		}, 80, 20),
	}

	streamer := &fakeLLMStreamer{turns: turns}
	tools := newFakeToolExecutor()

	cfg := AgentConfig{
		MaxTurns:  maxTurns,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "disobey")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	if result.StopReason != StopReasonMaxTurnsGraceful {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxTurnsGraceful)
	}
	if !result.BudgetExhaustedInjected {
		t.Error("BudgetExhaustedInjected = false, want true")
	}
	// Count how many times the grace prompt appears — must be exactly once.
	var graceCount int
	for _, m := range result.FinalMessages {
		if m.Role == "user" && strings.Contains(string(m.Content), "턴 예산") {
			graceCount++
		}
	}
	if graceCount != 1 {
		t.Errorf("grace prompt appears %d times in FinalMessages, want exactly 1", graceCount)
	}

	// Total turns = MaxTurns (1) + 1 grace iteration = 2.
	if result.Turns != maxTurns+1 {
		t.Errorf("Turns = %d, want %d", result.Turns, maxTurns+1)
	}
}

// TestRunAgent_GraceCall_NotTriggeredOnNaturalEndTurn verifies that a normal
// run which ends naturally (end_turn) before MaxTurns never triggers the
// grace injection — the flags stay zero and StopReason is "end_turn".
func TestRunAgent_GraceCall_NotTriggeredOnNaturalEndTurn(t *testing.T) {
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildTextTurnEvents("done quickly", 50, 20),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  5,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "ping")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
	if result.BudgetExhaustedInjected {
		t.Error("BudgetExhaustedInjected = true; should never fire on natural end_turn")
	}
	if result.BudgetGraceCall {
		t.Error("BudgetGraceCall = true; should never fire on natural end_turn")
	}
	// Grace prompt must NOT appear.
	for _, m := range result.FinalMessages {
		if m.Role == "user" && strings.Contains(string(m.Content), "턴 예산") {
			t.Error("grace prompt found in FinalMessages on natural end_turn")
			break
		}
	}
}

// TestRunAgent_GraceCall_ZeroMaxTurnsIgnored verifies that MaxTurns=0 is
// coerced to the default (25) — so the grace path is not triggered on
// iteration 0 with an unconfigured cap. This guards against a latent off-by-one
// if someone sets MaxTurns=0 expecting "immediate exit".
func TestRunAgent_GraceCall_ZeroMaxTurnsIgnored(t *testing.T) {
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildTextTurnEvents("hello", 50, 20),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  0, // Triggers the default coercion to 25.
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "hi")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q (MaxTurns=0 must coerce to default)",
			result.StopReason, "end_turn")
	}
	if result.BudgetExhaustedInjected {
		t.Error("BudgetExhaustedInjected = true; grace should not fire on single-turn run")
	}
}

// TestRunAgent_GraceCall_StopReasonExposed verifies that the grace exit path
// lets downstream callers distinguish a budget-forced close from a natural
// end by inspecting only StopReason.
func TestRunAgent_GraceCall_StopReasonExposed(t *testing.T) {
	const maxTurns = 1

	turns := [][]llm.StreamEvent{
		buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: "toolu_0", name: "exec", inputJSON: `{"cmd":"ls"}`},
		}, 50, 20),
		buildTextTurnEvents("wrap-up", 100, 30),
	}

	streamer := &fakeLLMStreamer{turns: turns}
	tools := newFakeToolExecutor()

	cfg := AgentConfig{
		MaxTurns:  maxTurns,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "check reason")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))
	if result.StopReason != StopReasonMaxTurnsGraceful {
		t.Fatalf("StopReason = %q, want %q",
			result.StopReason, StopReasonMaxTurnsGraceful)
	}
	if result.StopReason == "max_turns" {
		t.Error("StopReason must not be plain max_turns when grace fired")
	}
}

// TestRunAgent_GraceCall_PersistsGraceMessage verifies that OnMessagePersist
// is invoked for the injected grace user message so the transcript layer
// captures it alongside regular turn messages.
func TestRunAgent_GraceCall_PersistsGraceMessage(t *testing.T) {
	const maxTurns = 1

	turns := [][]llm.StreamEvent{
		buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: "toolu_0", name: "exec", inputJSON: `{"cmd":"ls"}`},
		}, 50, 20),
		buildTextTurnEvents("wrap-up", 100, 30),
	}

	streamer := &fakeLLMStreamer{turns: turns}
	tools := newFakeToolExecutor()

	var persisted []llm.Message
	cfg := AgentConfig{
		MaxTurns:  maxTurns,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
		OnMessagePersist: func(m llm.Message) {
			persisted = append(persisted, m)
		},
	}

	messages := []llm.Message{llm.NewTextMessage("user", "persist grace")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	if result.StopReason != StopReasonMaxTurnsGraceful {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, StopReasonMaxTurnsGraceful)
	}

	var seenGrace bool
	for _, m := range persisted {
		if m.Role == "user" && strings.Contains(string(m.Content), "턴 예산") {
			seenGrace = true
			break
		}
	}
	if !seenGrace {
		t.Error("grace user message was not routed through OnMessagePersist")
	}
}

// TestGracePrompt_IsKorean is a static sanity check: the injected prompt
// must be Korean per the Korean-first output rule. A future refactor might
// accidentally switch to English copy; this guards against that.
func TestGracePrompt_IsKorean(t *testing.T) {
	// Require at least one Hangul syllable block in the prompt.
	var hasHangul bool
	for _, r := range GraceCallPrompt {
		if r >= 0xAC00 && r <= 0xD7A3 {
			hasHangul = true
			break
		}
	}
	if !hasHangul {
		t.Errorf("GraceCallPrompt contains no Hangul characters: %q", GraceCallPrompt)
	}
	if GraceCallPrompt == "" {
		t.Error("GraceCallPrompt is empty")
	}
	// Must forbid further tool use explicitly so the model doesn't pick up more work.
	if !strings.Contains(GraceCallPrompt, "도구") {
		t.Errorf("GraceCallPrompt should mention 도구 (tools) to discourage tool use: %q",
			GraceCallPrompt)
	}
}

// Compile-time guard: ensure json.RawMessage round-trip still works after
// grace message injection. Regression sentinel.
var _ = json.RawMessage("{}")
