package chat

import (
	"encoding/json"
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// streamBroadcaster relays agent streaming events to WebSocket clients
// via the gateway's raw broadcast function.
type streamBroadcaster struct {
	broadcastRaw BroadcastRawFunc
	sessionKey   string
	clientRunID  string
	seq          atomic.Int64
}

// newStreamBroadcaster creates a broadcaster bound to a specific run.
func newStreamBroadcaster(broadcastRaw BroadcastRawFunc, sessionKey, clientRunID string) *streamBroadcaster {
	return &streamBroadcaster{
		broadcastRaw: broadcastRaw,
		sessionKey:   sessionKey,
		clientRunID:  clientRunID,
	}
}

// EmitDelta broadcasts a streaming text delta to WS clients.
func (sb *streamBroadcaster) EmitDelta(text string) {
	if sb.broadcastRaw == nil || text == "" {
		return
	}
	sb.emit("chat.delta", map[string]any{
		"sessionKey":  sb.sessionKey,
		"clientRunId": sb.clientRunID,
		"delta":       text,
	})
}

// EmitToolStart broadcasts a tool invocation start event.
func (sb *streamBroadcaster) EmitToolStart(name, toolUseID string) {
	if sb.broadcastRaw == nil {
		return
	}
	sb.emit("chat.tool", map[string]any{
		"sessionKey":  sb.sessionKey,
		"clientRunId": sb.clientRunID,
		"state":       "started",
		"tool":        name,
		"toolUseId":   toolUseID,
	})
}

// EmitToolResult broadcasts a tool execution result event.
func (sb *streamBroadcaster) EmitToolResult(toolUseID, result string, isError bool) {
	if sb.broadcastRaw == nil {
		return
	}
	sb.emit("chat.tool", map[string]any{
		"sessionKey":  sb.sessionKey,
		"clientRunId": sb.clientRunID,
		"state":       "completed",
		"toolUseId":   toolUseID,
		"result":      truncateForBroadcast(result, 4096),
		"isError":     isError,
	})
}

// EmitComplete broadcasts the final chat completion event.
func (sb *streamBroadcaster) EmitComplete(text string, usage llm.TokenUsage) {
	if sb.broadcastRaw == nil {
		return
	}
	sb.emit("chat", map[string]any{
		"sessionKey":  sb.sessionKey,
		"clientRunId": sb.clientRunID,
		"state":       "done",
		"text":        text,
		"usage": map[string]int{
			"inputTokens":  usage.InputTokens,
			"outputTokens": usage.OutputTokens,
		},
	})
}

// EmitError broadcasts an error event for the run.
func (sb *streamBroadcaster) EmitError(errMsg string) {
	if sb.broadcastRaw == nil {
		return
	}
	sb.emit("chat", map[string]any{
		"sessionKey":  sb.sessionKey,
		"clientRunId": sb.clientRunID,
		"state":       "error",
		"error":       errMsg,
	})
}

// EmitStarted broadcasts that the agent run has started.
func (sb *streamBroadcaster) EmitStarted() {
	if sb.broadcastRaw == nil {
		return
	}
	sb.emit("chat", map[string]any{
		"sessionKey":  sb.sessionKey,
		"clientRunId": sb.clientRunID,
		"state":       "started",
	})
}

// EmitAborted broadcasts that the agent run was aborted.
func (sb *streamBroadcaster) EmitAborted(partialText string) {
	if sb.broadcastRaw == nil {
		return
	}
	sb.emit("chat", map[string]any{
		"sessionKey":  sb.sessionKey,
		"clientRunId": sb.clientRunID,
		"state":       "aborted",
		"text":        partialText,
	})
}

func (sb *streamBroadcaster) emit(event string, payload map[string]any) {
	payload["seq"] = sb.seq.Add(1)
	data, err := json.Marshal(map[string]any{
		"event":   event,
		"payload": payload,
	})
	if err != nil {
		return
	}
	sb.broadcastRaw(event, data)
}

// truncateForBroadcast caps a string to maxLen bytes to prevent oversized WS frames.
func truncateForBroadcast(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}
