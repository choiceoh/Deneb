package chat

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

func TestChatPortRejectsTypedNilHandler(t *testing.T) {
	var handler *Handler
	var runner chatport.SyncRunner = handler
	var streamRunner chatport.SyncStreamRunner = handler
	if runner.ChatReady() {
		t.Fatal("typed nil handler reported ready")
	}
	if streamRunner.ChatReady() {
		t.Fatal("typed nil stream handler reported ready")
	}
	if _, err := runner.RunSync(context.Background(), chatport.SyncRequest{}); err == nil {
		t.Fatal("typed nil RunSync returned nil error")
	}
	if _, err := streamRunner.RunSyncStream(context.Background(), chatport.SyncRequest{}, nil); err == nil {
		t.Fatal("typed nil RunSyncStream returned nil error")
	}
}

func TestSyncOptionsFromPortPreservesRuntimeAndStreamContract(t *testing.T) {
	maxTokens, maxTurns, maxCalls := 10, 2, 3
	delivery := &chatport.DeliveryContext{Channel: "client", To: "client:main"}
	var gotEvent chatport.ToolStreamEvent
	req := chatport.SyncRequest{
		MaxTokens:           &maxTokens,
		MaxTurns:            &maxTurns,
		MaxToolCallAttempts: &maxCalls,
		SystemPrompt:        "system",
		Thinking:            "off",
		ToolPreset:          "boot",
		MaxHistoryTokens:    42,
		Delivery:            delivery,
		EphemeralUser:       true,
		EphemeralAssistant:  true,
		AutoDeliveredOutput: true,
		SkipRecall:          true,
		FeedContext:         "feed",
		GateUntrustedTools:  true,
		BeforeToolCall:      func(string, string, []byte) (bool, string) { return true, "blocked" },
		OnToolResult:        func(string, string, string, bool) {},
		OnToolEvent:         func(event chatport.ToolStreamEvent) { gotEvent = event },
		OnThinking:          func(string) {},
	}

	got := syncOptionsFromPort(req)
	if got.MaxTokens != req.MaxTokens || got.MaxTurns != req.MaxTurns ||
		got.MaxToolCallAttempts != req.MaxToolCallAttempts || got.SystemPrompt != req.SystemPrompt ||
		got.Thinking != req.Thinking || got.ToolPreset != req.ToolPreset ||
		got.MaxHistoryTokens != req.MaxHistoryTokens || got.Delivery != delivery ||
		!got.EphemeralUser || !got.EphemeralAssistant || !got.AutoDeliveredOutput ||
		!got.SkipRecall || got.FeedContext != req.FeedContext || !got.GateUntrustedTools ||
		got.BeforeToolCall == nil || got.OnToolResult == nil || got.OnThinking == nil || got.OnToolEvent == nil {
		t.Fatalf("port request mapping lost fields: %#v", got)
	}
	got.OnToolEvent(ToolStreamEvent{
		State: "completed", Tool: "wiki", ToolUseID: "tool-1", Detail: "done", IsError: true,
	})
	if gotEvent != (chatport.ToolStreamEvent{
		State: "completed", Tool: "wiki", ToolUseID: "tool-1", Detail: "done", IsError: true,
	}) {
		t.Fatalf("stream event = %#v", gotEvent)
	}
}

func TestSyncResultToPortPreservesWireResultAndSelectedText(t *testing.T) {
	result := syncResultToPort(&SyncResult{
		Text:            "wrap-up",
		AllText:         "working\nanswer",
		DeliverableText: "answer NO_REPLY",
		Model:           "vllm/model",
		ProviderModel:   "served-model",
		FellBack:        true,
		InputTokens:     11,
		OutputTokens:    7,
		Turns:           3,
		StopReason:      "end_turn",
	})
	if result == nil || result.BestText != "answer" || result.Text != "wrap-up" ||
		result.AllText != "working\nanswer" || result.DeliverableText != "answer NO_REPLY" ||
		result.Model != "vllm/model" || result.ProviderModel != "served-model" ||
		!result.FellBack || result.InputTokens != 11 || result.OutputTokens != 7 ||
		result.Turns != 3 || result.StopReason != "end_turn" {
		t.Fatalf("port result = %#v", result)
	}
}
