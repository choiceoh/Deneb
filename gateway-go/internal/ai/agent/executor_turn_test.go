package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestTurnRequestPreparerPreservesPreparationOrder(t *testing.T) {
	var order []string
	cfg := AgentConfig{
		OnTurnInit: func(ctx context.Context) context.Context {
			order = append(order, "context")
			return ctx
		},
		DynamicToolsProvider: func() []llm.Tool {
			order = append(order, "tools")
			return []llm.Tool{{Name: "read"}}
		},
		BeforeAPICall: func(messages []llm.Message) []llm.Message {
			order = append(order, "messages")
			return messages
		},
		ThinkingModulator: func(_ int, _ []ToolActivity) *llm.ThinkingConfig {
			order = append(order, "thinking")
			return nil
		},
	}
	preparer := newTurnRequestPreparer(&cfg)
	messages := []llm.Message{llm.NewTextMessage("user", "hello")}

	preparer.prepare(context.Background(), 0, messages, nil)
	if want := []string{"context", "messages", "thinking"}; !slices.Equal(order, want) {
		t.Fatalf("turn 0 preparation order = %v, want %v", order, want)
	}

	order = nil
	preparer.prepare(context.Background(), 1, messages, nil)
	if want := []string{"context", "tools", "messages", "thinking"}; !slices.Equal(order, want) {
		t.Fatalf("turn 1 preparation order = %v, want %v", order, want)
	}
}

func TestTurnRequestPreparerNilTurnContextKeepsPreviousContext(t *testing.T) {
	type contextKey struct{}
	base := context.WithValue(context.Background(), contextKey{}, "preserved")
	cfg := AgentConfig{
		OnTurnInit: func(context.Context) context.Context { return nil },
	}

	prepared := newTurnRequestPreparer(&cfg).prepare(base, 0, nil, nil)
	if got := prepared.ctx.Value(contextKey{}); got != "preserved" {
		t.Fatalf("prepared context value = %v, want preserved base context", got)
	}
}

func TestTurnRequestPreparerLoadsDeferredInputsAfterFirstTurn(t *testing.T) {
	toolSets := [][]llm.Tool{
		{{Name: "read"}},
		{{Name: "read"}, {Name: "search"}},
	}
	dynamicCall := 0
	baseSystem := json.RawMessage(llm.SystemString("base").Bytes())
	cfg := AgentConfig{
		System: baseSystem,
		Tools:  []llm.Tool{{Name: "write"}},
		DynamicToolsProvider: func() []llm.Tool {
			tools := toolSets[dynamicCall]
			dynamicCall++
			return tools
		},
	}
	preparer := newTurnRequestPreparer(&cfg)

	preparer.prepare(context.Background(), 0, nil, nil)
	if dynamicCall != 0 {
		t.Fatalf("turn 0 polled deferred tools: %d", dynamicCall)
	}
	preparer.prepare(context.Background(), 1, nil, nil)
	prepared := preparer.prepare(context.Background(), 2, nil, nil)

	if dynamicCall != 2 {
		t.Fatalf("later turns polled deferred tools: %d, want 2", dynamicCall)
	}
	// The system prompt must stay byte-identical mid-run: content-prefix
	// provider caches (kimi) cold-start on any (system, tools) byte change,
	// so late notifications ride DeferredTurnNotices instead of System.
	if got, want := llm.ExtractSystemText(prepared.request.System), "base"; got != want {
		t.Fatalf("system text = %q, want unchanged %q", got, want)
	}
	toolNames := make([]string, 0, len(prepared.request.Tools))
	for _, tool := range prepared.request.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	if want := []string{"write", "read", "search"}; !slices.Equal(toolNames, want) {
		t.Fatalf("tool names = %v, want deduplicated %v", toolNames, want)
	}
}

func TestTurnRequestPreparerBeforeAPICallUsesRequestOnlyViewAndNilFallback(t *testing.T) {
	history := []llm.Message{llm.NewTextMessage("user", "original")}
	cfg := AgentConfig{
		BeforeAPICall: func(messages []llm.Message) []llm.Message {
			requestMessages := append([]llm.Message(nil), messages...)
			return append(requestMessages, llm.NewTextMessage("user", "one-shot steer"))
		},
	}
	preparer := newTurnRequestPreparer(&cfg)

	prepared := preparer.prepare(context.Background(), 0, history, nil)
	if len(prepared.request.Messages) != 2 {
		t.Fatalf("request messages = %d, want original + one-shot message", len(prepared.request.Messages))
	}
	if len(history) != 1 {
		t.Fatalf("history messages = %d, want unchanged prompt-cache history", len(history))
	}

	cfg.BeforeAPICall = func([]llm.Message) []llm.Message { return nil }
	prepared = preparer.prepare(context.Background(), 1, history, nil)
	if !reflect.DeepEqual(prepared.request.Messages, history) {
		t.Fatalf("nil hook result messages = %+v, want original history %+v", prepared.request.Messages, history)
	}
}

func TestTurnRequestPreparerThinkingOverrideAppliesOnceThenRestoresModulator(t *testing.T) {
	baseline := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 5000}
	modulated := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 32000}
	override := &llm.ThinkingConfig{Type: "disabled"}
	modulatorCalls := 0
	cfg := AgentConfig{
		Thinking: baseline,
		ThinkingModulator: func(_ int, _ []ToolActivity) *llm.ThinkingConfig {
			modulatorCalls++
			return modulated
		},
	}
	preparer := newTurnRequestPreparer(&cfg)
	preparer.overrideThinkingOnce(override)

	first := preparer.prepare(context.Background(), 3, nil, nil)
	if first.thinking != override || first.request.Thinking != override {
		t.Fatalf("first thinking = %+v, want one-shot override %+v", first.thinking, override)
	}
	if modulatorCalls != 0 {
		t.Fatalf("modulator called %d times while override was active, want 0", modulatorCalls)
	}

	second := preparer.prepare(context.Background(), 4, nil, nil)
	if second.thinking != modulated || second.request.Thinking != modulated {
		t.Fatalf("second thinking = %+v, want modulator result %+v", second.thinking, modulated)
	}
	if modulatorCalls != 1 {
		t.Fatalf("modulator calls = %d after override consumption, want 1", modulatorCalls)
	}

	cfg.ThinkingModulator = func(_ int, _ []ToolActivity) *llm.ThinkingConfig { return nil }
	third := preparer.prepare(context.Background(), 5, nil, nil)
	if third.thinking != baseline {
		t.Fatalf("third thinking = %+v, want baseline fallback %+v", third.thinking, baseline)
	}
}

func TestTurnRequestPreparerCreatesRequestWithAllConfigFields(t *testing.T) {
	temperature := 0.2
	topP := 0.8
	topK := 20
	frequencyPenalty := 0.1
	presencePenalty := 0.3
	responseFormat := &llm.ResponseFormat{Type: "json_object"}
	thinking := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	messages := []llm.Message{llm.NewTextMessage("user", "hello")}
	cfg := AgentConfig{
		Model:            "model",
		System:           json.RawMessage(llm.SystemString("system").Bytes()),
		MaxTokens:        1234,
		Tools:            []llm.Tool{{Name: "read"}},
		Thinking:         thinking,
		Temperature:      &temperature,
		TopP:             &topP,
		TopK:             &topK,
		FrequencyPenalty: &frequencyPenalty,
		PresencePenalty:  &presencePenalty,
		StopSequences:    []string{"stop"},
		ResponseFormat:   responseFormat,
		ToolChoice:       json.RawMessage(`"required"`),
	}

	request := newTurnRequestPreparer(&cfg).prepare(context.Background(), 0, messages, nil).request
	want := llm.ChatRequest{
		Model:            cfg.Model,
		Messages:         messages,
		System:           llm.FlexibleFromRaw(cfg.System),
		MaxTokens:        cfg.MaxTokens,
		Tools:            cfg.Tools,
		Stream:           true,
		Thinking:         thinking,
		Temperature:      cfg.Temperature,
		TopP:             cfg.TopP,
		TopK:             cfg.TopK,
		FrequencyPenalty: cfg.FrequencyPenalty,
		PresencePenalty:  cfg.PresencePenalty,
		StopSequences:    cfg.StopSequences,
		ResponseFormat:   cfg.ResponseFormat,
		ToolChoice:       llm.FlexibleFromValue(cfg.ToolChoice),
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("request = %+v, want %+v", request, want)
	}
}

func TestTurnRequestPreparerAppendsBudgetWarningWhenNearMaxTurns(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns:      25,
		SpawnDetected: func() bool { return true },
	}
	preparer := newTurnRequestPreparer(&cfg)
	blocks := []llm.ContentBlock{
		{Type: "tool_result", ToolUseID: "tool-1", Content: "ok"},
		{Type: "text", Text: "edit-thrash nudge"},
	}

	got := preparer.appendBudgetWarning(19, blocks)
	if len(got) != 3 {
		t.Fatalf("blocks = %d, want tool result + nudge + budget warning", len(got))
	}
	if !reflect.DeepEqual(got[:2], blocks) {
		t.Fatalf("existing block order changed: got %+v, want prefix %+v", got[:2], blocks)
	}
	if got[2].Type != "text" || got[2].Text != buildTurnBudgetWarning(19, cfg.MaxTurns) {
		t.Fatalf("last block = %+v, want budget warning", got[2])
	}

	if early := preparer.appendBudgetWarning(0, blocks); len(early) != len(blocks) {
		t.Fatalf("early turn appended warning: got %d blocks, want %d", len(early), len(blocks))
	}
}
