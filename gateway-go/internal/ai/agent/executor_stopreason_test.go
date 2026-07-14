package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// A trailing usage-only message_delta (no stop_reason) must not clobber a
// stop_reason set by an earlier delta. Before the guard, the unconditional
// assignment reset stopReason to "", which (e.g.) defeats the max_tokens resume
// path and muddies terminal-stop detection. The final output-token count from
// the trailing delta is still applied.
func TestConsumeStreamInto_StopReasonNotClobberedByTrailingUsage(t *testing.T) {
	events := make(chan llm.StreamEvent, 8)
	events <- llm.StreamEvent{Type: "message_delta", Payload: json.RawMessage(`{"delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`)}
	// Trailing usage-only delta: carries final usage, no stop_reason.
	events <- llm.StreamEvent{Type: "message_delta", Payload: json.RawMessage(`{"delta":{"stop_reason":""},"usage":{"output_tokens":12}}`)}
	events <- makeStreamEvent("message_stop")
	close(events)

	result := &turnResult{}
	if err := consumeStreamInto(context.Background(), events, StreamHooks{}, result, -1, nil); err != nil {
		t.Fatalf("consumeStreamInto: %v", err)
	}

	if result.stopReason != "tool_use" {
		t.Errorf("stopReason = %q, want %q (clobbered by trailing usage-only delta?)", result.stopReason, "tool_use")
	}
	if result.usage.OutputTokens != 12 {
		t.Errorf("OutputTokens = %d, want 12 (final usage should still apply)", result.usage.OutputTokens)
	}
}

func TestConsumeStreamInto_ZeroUsageTrailerDoesNotEraseProviderCount(t *testing.T) {
	events := make(chan llm.StreamEvent, 4)
	events <- llm.StreamEvent{Type: "message_delta", Payload: json.RawMessage(`{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":19}}`)}
	events <- llm.StreamEvent{Type: "message_delta", Payload: json.RawMessage(`{"delta":{},"usage":{"output_tokens":0}}`)}
	events <- makeStreamEvent("message_stop")
	close(events)

	result := &turnResult{}
	if err := consumeStreamInto(context.Background(), events, StreamHooks{}, result, -1, nil); err != nil {
		t.Fatal(err)
	}
	if result.usage.OutputTokens != 19 {
		t.Fatalf("OutputTokens = %d, want retained provider maximum 19", result.usage.OutputTokens)
	}
}

func TestConsumeStreamInto_ProviderModelIsStableWithinTurn(t *testing.T) {
	t.Run("blank usage trailer does not erase model", func(t *testing.T) {
		events := make(chan llm.StreamEvent, 3)
		events <- llm.StreamEvent{Type: "message_start", Payload: json.RawMessage(`{"message":{"model":"served-model"}}`)}
		events <- llm.StreamEvent{Type: "message_start", Payload: json.RawMessage(`{"message":{"model":""}}`)}
		events <- makeStreamEvent("message_stop")
		close(events)

		result := &turnResult{}
		if err := consumeStreamInto(context.Background(), events, StreamHooks{}, result, -1, nil); err != nil {
			t.Fatal(err)
		}
		if result.providerModel != "served-model" {
			t.Fatalf("providerModel = %q, want served-model", result.providerModel)
		}
	})

	t.Run("different nonblank models fail closed", func(t *testing.T) {
		events := make(chan llm.StreamEvent, 2)
		events <- llm.StreamEvent{Type: "message_start", Payload: json.RawMessage(`{"message":{"model":"served-model-a"}}`)}
		events <- llm.StreamEvent{Type: "message_start", Payload: json.RawMessage(`{"message":{"model":"served-model-b"}}`)}
		close(events)

		err := consumeStreamInto(context.Background(), events, StreamHooks{}, &turnResult{}, -1, nil)
		if err == nil || !strings.Contains(err.Error(), "provider model changed within turn") {
			t.Fatalf("error = %v, want provider-model mismatch", err)
		}
	})
}

func TestRunAgent_RequireProviderModelRejectsMissingOrChangedModel(t *testing.T) {
	t.Run("missing model fails before completion", func(t *testing.T) {
		events := buildTextTurnEvents("answer", 1, 1)
		events[0] = llm.StreamEvent{
			Type:    "message_start",
			Payload: json.RawMessage(`{"message":{"usage":{"input_tokens":1}}}`),
		}
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{events}}

		_, err := RunAgent(context.Background(), AgentConfig{
			MaxTurns: 1, MaxTokens: 32, RequireProviderModel: true,
		}, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "did not report a model identifier") {
			t.Fatalf("error = %v, want missing provider-model failure", err)
		}
	})

	t.Run("model change between turns fails closed", func(t *testing.T) {
		first := buildToolUseTurnEventsWithNames([]toolUseSpec{{
			id: "toolu_1", name: "read", inputJSON: `{}`,
		}}, 1, 1)
		second := buildTextTurnEvents("answer", 1, 1)
		second[0] = llm.StreamEvent{
			Type:    "message_start",
			Payload: json.RawMessage(`{"message":{"model":"other-model","usage":{"input_tokens":1}}}`),
		}
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{first, second}}

		_, err := RunAgent(context.Background(), AgentConfig{
			MaxTurns: 2, MaxTokens: 32, RequireProviderModel: true,
		}, []llm.Message{llm.NewTextMessage("user", "read")}, streamer, newFakeToolExecutor(), StreamHooks{}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), `provider model changed from "test-model" to "other-model"`) {
			t.Fatalf("error = %v, want cross-turn provider-model failure", err)
		}
	})
}

func TestRunAgent_RequireExplicitStopReason(t *testing.T) {
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{{
		messageStartEvent(1), contentBlockStartEvent(0, "text", ""), textDeltaEvent(0, "answer"),
		contentBlockStopEvent(0),
		{Type: "message_stop"},
	}}}
	_, err := RunAgent(context.Background(), AgentConfig{
		MaxTurns: 1, MaxTokens: 32, RequireExplicitStopReason: true,
	}, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "explicit stop reason") {
		t.Fatalf("error = %v, want explicit stop-reason failure", err)
	}
}

func TestRunAgent_StrictStopShapeRejectsToolCallsBeforeExecution(t *testing.T) {
	for _, stopReason := range []string{"unknown", "content_filter", "max_tokens", "end_turn"} {
		t.Run(stopReason, func(t *testing.T) {
			events := buildToolUseTurnEventsWithNames([]toolUseSpec{{
				id: "toolu_1", name: "read", inputJSON: `{"path":"safe"}`,
			}}, 1, 1)
			events[len(events)-2] = messageDeltaEvent(stopReason, 1)
			streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{events}}
			tools := newFakeToolExecutor()
			beforeCalls := 0
			hooks := StreamHooks{OnBeforeToolCall: func(string, string, []byte) (bool, string) {
				beforeCalls++
				return false, ""
			}}

			_, err := RunAgent(context.Background(), AgentConfig{
				MaxTurns: 1, MaxTokens: 32, RequireStrictStopShape: true,
			}, []llm.Message{llm.NewTextMessage("user", "read")}, streamer, tools, hooks, nil, nil)
			if !errors.Is(err, ErrInvalidStopShape) {
				t.Fatalf("error = %v, want ErrInvalidStopShape", err)
			}
			if tools.callCount() != 0 || beforeCalls != 0 {
				t.Fatalf("invalid stop shape reached tools/hooks: executions=%d hooks=%d", tools.callCount(), beforeCalls)
			}
		})
	}
}

func TestRunAgent_StrictStopShapeRejectsToolUseWithoutCalls(t *testing.T) {
	events := buildTextTurnEvents("answer", 1, 1)
	events[len(events)-2] = messageDeltaEvent("tool_use", 1)
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{events}}

	_, err := RunAgent(context.Background(), AgentConfig{
		MaxTurns: 1, MaxTokens: 32, RequireStrictStopShape: true,
	}, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil)
	if !errors.Is(err, ErrInvalidStopShape) {
		t.Fatalf("error = %v, want ErrInvalidStopShape", err)
	}
}

func TestRunAgent_HardTotalBudgetRejectsOverrunsViaLocalEstimate(t *testing.T) {
	t.Run("zero provider usage is replaced and propagated", func(t *testing.T) {
		const answer = "provider omitted usage for this generated answer"
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{buildTextTurnEvents(answer, 1, 0)}}
		result, err := RunAgent(context.Background(), AgentConfig{
			MaxTurns: 1, MaxTokens: 256, MaxTotalOutputTokens: 256, RequireStrictStopShape: true,
		}, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := generatedOutputTokenCharge([]llm.ContentBlock{{Type: "text", Text: answer}}, 0)
		if want <= 0 || result.Usage.OutputTokens != want {
			t.Fatalf("OutputTokens = %d, want local estimate %d", result.Usage.OutputTokens, want)
		}
	})

	t.Run("thinking and tool arguments cannot bypass with zero usage", func(t *testing.T) {
		thinking := strings.Repeat("reasoning ", 256)
		arguments := `{"blob":"` + strings.Repeat("x", 4096) + `"}`
		var thinkingDelta llm.ContentBlockDelta
		thinkingDelta.Index = 0
		thinkingDelta.Delta.Type = "thinking_delta"
		thinkingDelta.Delta.Thinking = thinking
		thinkingPayload, err := json.Marshal(thinkingDelta)
		if err != nil {
			t.Fatal(err)
		}
		events := []llm.StreamEvent{
			messageStartEvent(1),
			contentBlockStartEvent(0, "thinking", ""),
			{Type: "content_block_delta", Payload: thinkingPayload},
			contentBlockStopEvent(0),
			contentBlockStartToolUseEvent(1, "toolu_large", "read"),
			toolInputDeltaEvent(1, "read", arguments),
			contentBlockStopEvent(1),
			messageDeltaEvent("tool_use", 0),
			{Type: "message_stop"},
		}
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{events}}
		tools := newFakeToolExecutor()
		beforeCalls := 0
		hooks := StreamHooks{OnBeforeToolCall: func(string, string, []byte) (bool, string) {
			beforeCalls++
			return false, ""
		}}

		_, err = RunAgent(context.Background(), AgentConfig{
			MaxTurns: 1, MaxTokens: 64, MaxTotalOutputTokens: 64, RequireStrictStopShape: true,
		}, []llm.Message{llm.NewTextMessage("user", "read")}, streamer, tools, hooks, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "total output-token budget") {
			t.Fatalf("error = %v, want local total-token overrun", err)
		}
		if tools.callCount() != 0 || beforeCalls != 0 {
			t.Fatalf("local token overrun reached tools/hooks: executions=%d hooks=%d", tools.callCount(), beforeCalls)
		}
	})

	t.Run("punctuation cannot undercharge the hard upper bound", func(t *testing.T) {
		answer := strings.Repeat("!", 80)
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{buildTextTurnEvents(answer, 1, 0)}}
		_, err := RunAgent(context.Background(), AgentConfig{
			MaxTurns: 1, MaxTokens: 64, MaxTotalOutputTokens: 64, RequireStrictStopShape: true,
		}, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "total output-token budget") {
			t.Fatalf("error = %v, want conservative byte upper-bound overrun", err)
		}
	})

	t.Run("positive provider usage keeps token semantics", func(t *testing.T) {
		answer := strings.Repeat("!", 80)
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{buildTextTurnEvents(answer, 1, 20)}}
		result, err := RunAgent(context.Background(), AgentConfig{
			MaxTurns: 1, MaxTokens: 64, MaxTotalOutputTokens: 64, RequireStrictStopShape: true,
		}, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil)
		if err != nil {
			t.Fatalf("positive provider usage was charged as raw bytes: %v", err)
		}
		if result.Usage.OutputTokens < 20 || result.Usage.OutputTokens > 64 {
			t.Fatalf("output charge = %d, want provider/local token charge within budget", result.Usage.OutputTokens)
		}
	})
}

func TestRunAgent_TotalOutputAndStreamCaps(t *testing.T) {
	t.Run("provider token overrun", func(t *testing.T) {
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{buildTextTurnEvents("answer", 1, 6)}}
		_, err := RunAgent(context.Background(), AgentConfig{
			MaxTurns: 1, MaxTokens: 5, MaxTotalOutputTokens: 5,
		}, []llm.Message{llm.NewTextMessage("user", "hi")}, streamer, nil, StreamHooks{}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "total output-token budget") {
			t.Fatalf("error = %v, want total-token overrun", err)
		}
	})

	t.Run("stream bytes", func(t *testing.T) {
		events := make(chan llm.StreamEvent, 1)
		events <- llm.StreamEvent{Type: "content_block_delta", Payload: json.RawMessage(`{"payload":"too-large"}`)}
		close(events)
		err := consumeStreamInto(context.Background(), events, StreamHooks{}, &turnResult{maxStreamBytes: 8}, -1, nil)
		if !errors.Is(err, ErrStreamLimit) {
			t.Fatalf("error = %v, want ErrStreamLimit", err)
		}
	})
}
