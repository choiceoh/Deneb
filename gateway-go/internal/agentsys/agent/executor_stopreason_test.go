package agent

import (
	"context"
	"encoding/json"
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
