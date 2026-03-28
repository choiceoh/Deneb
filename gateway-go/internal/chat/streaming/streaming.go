package streaming

import (
	"encoding/json"
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// BroadcastRawFunc sends pre-serialized event data to all matching subscribers.
type BroadcastRawFunc func(event string, data []byte) int

// Stream event names matching the TypeScript wire format.
const (
	eventChat  = "chat"
	eventDelta = "chat.delta"
	eventTool  = "chat.tool"
)

// Limits for broadcast payloads.
const (
	maxBroadcastResultLen = 4096
)

// Broadcaster relays agent streaming events to WebSocket clients
// via the gateway's raw broadcast function. All methods are safe to call
// when broadcastRaw is nil (they silently no-op).
type Broadcaster struct {
	broadcastRaw BroadcastRawFunc
	sessionKey   string
	clientRunID  string
	seq          atomic.Int64
}

// NewBroadcaster creates a new Broadcaster for a given session/run.
func NewBroadcaster(broadcastRaw BroadcastRawFunc, sessionKey, clientRunID string) *Broadcaster {
	return &Broadcaster{
		broadcastRaw: broadcastRaw,
		sessionKey:   sessionKey,
		clientRunID:  clientRunID,
	}
}

// EmitDelta broadcasts a streaming text delta to WS clients.
func (sb *Broadcaster) EmitDelta(text string) {
	if text == "" {
		return
	}
	sb.emit(eventDelta, map[string]any{
		"delta": text,
	})
}

// EmitToolStart broadcasts a tool invocation start event.
func (sb *Broadcaster) EmitToolStart(name, toolUseID string) {
	sb.emit(eventTool, map[string]any{
		"state":     "started",
		"tool":      name,
		"toolUseId": toolUseID,
	})
}

// EmitToolResult broadcasts a tool execution result event.
func (sb *Broadcaster) EmitToolResult(name, toolUseID, result string, isError bool) {
	sb.emit(eventTool, map[string]any{
		"state":     "completed",
		"tool":      name,
		"toolUseId": toolUseID,
		"result":    truncateForBroadcast(result, maxBroadcastResultLen),
		"isError":   isError,
	})
}

// EmitComplete broadcasts the final chat completion event.
func (sb *Broadcaster) EmitComplete(text string, usage llm.TokenUsage) {
	sb.emit(eventChat, map[string]any{
		"state": "done",
		"text":  text,
		"usage": map[string]int{
			"inputTokens":  usage.InputTokens,
			"outputTokens": usage.OutputTokens,
		},
	})
}

// EmitError broadcasts an error event for the run.
func (sb *Broadcaster) EmitError(errMsg string) {
	sb.emit(eventChat, map[string]any{
		"state": "error",
		"error": errMsg,
	})
}

// EmitStarted broadcasts that the agent run has started.
func (sb *Broadcaster) EmitStarted() {
	sb.emit(eventChat, map[string]any{
		"state": "started",
	})
}

// EmitAborted broadcasts that the agent run was aborted.
func (sb *Broadcaster) EmitAborted(partialText string) {
	sb.emit(eventChat, map[string]any{
		"state": "aborted",
		"text":  partialText,
	})
}

// emit is the shared broadcast path. It injects common fields (sessionKey,
// clientRunId, seq) and serializes to JSON. No-ops when broadcastRaw is nil.
func (sb *Broadcaster) emit(event string, payload map[string]any) {
	if sb.broadcastRaw == nil {
		return
	}
	payload["sessionKey"] = sb.sessionKey
	payload["clientRunId"] = sb.clientRunID
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
