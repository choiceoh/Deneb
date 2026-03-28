// acp_stream.go — Translates gateway streaming events (chat.delta, chat.tool,
// chat) into ACP-shaped events (acp.message, acp.tool_call, acp.tool_call_update,
// acp.done, acp.usage_update) for ACP-originated sessions.
package autoreply

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
)

// ACP stream event names.
const (
	ACPEventMessage        = "acp.message"
	ACPEventToolCall       = "acp.tool_call"
	ACPEventToolCallUpdate = "acp.tool_call_update"
	ACPEventDone           = "acp.done"
	ACPEventUsageUpdate    = "acp.usage_update"
)

// BroadcastRawFn sends pre-serialized event data to subscribers.
// Matches the signature of chat.BroadcastRawFunc without importing the package.
type BroadcastRawFn func(event string, data []byte) int

// ACPStreamTranslator intercepts gateway broadcast events for ACP sessions
// and re-emits them as ACP-shaped events.
type ACPStreamTranslator struct {
	emit   BroadcastRawFn
	logger *slog.Logger
	seq    atomic.Int64
}

// NewACPStreamTranslator creates a new stream translator.
func NewACPStreamTranslator(emit BroadcastRawFn, logger *slog.Logger) *ACPStreamTranslator {
	if logger == nil {
		logger = slog.Default()
	}
	return &ACPStreamTranslator{
		emit:   emit,
		logger: logger.With("component", "acp.stream"),
	}
}

// gatewayEvent is the common shape of a gateway broadcast event.
type gatewayEvent struct {
	Event   string         `json:"event"`
	Payload map[string]any `json:"payload"`
}

// TranslateEvent parses a gateway broadcast event and, if it belongs to an
// ACP session, emits the corresponding ACP event. Non-ACP sessions are ignored.
func (t *ACPStreamTranslator) TranslateEvent(event string, data []byte) {
	if t.emit == nil {
		return
	}

	var ge gatewayEvent
	if err := json.Unmarshal(data, &ge); err != nil {
		return
	}

	sessionKey, _ := ge.Payload["sessionKey"].(string)
	if !IsACPSession(sessionKey) {
		return
	}

	switch event {
	case "chat.delta":
		t.translateDelta(sessionKey, ge.Payload)
	case "chat.tool":
		t.translateTool(sessionKey, ge.Payload)
	case "chat":
		t.translateChat(sessionKey, ge.Payload)
	}
}

// translateDelta converts a chat.delta event to acp.message.
func (t *ACPStreamTranslator) translateDelta(sessionKey string, payload map[string]any) {
	delta, _ := payload["delta"].(string)
	if delta == "" {
		return
	}
	t.emitACP(ACPEventMessage, map[string]any{
		"type":       "message",
		"sessionKey": sessionKey,
		"text":       delta,
	})
}

// translateTool converts a chat.tool event to acp.tool_call or acp.tool_call_update.
func (t *ACPStreamTranslator) translateTool(sessionKey string, payload map[string]any) {
	state, _ := payload["state"].(string)
	toolName, _ := payload["tool"].(string)
	toolUseID, _ := payload["toolUseId"].(string)

	switch state {
	case "started":
		t.emitACP(ACPEventToolCall, map[string]any{
			"type":       "tool_call",
			"sessionKey": sessionKey,
			"tool":       toolName,
			"toolUseId":  toolUseID,
		})
	case "completed":
		result, _ := payload["result"].(string)
		isError, _ := payload["isError"].(bool)
		files := ExtractFileLocations(toolName, "", result)

		out := map[string]any{
			"type":       "tool_call_update",
			"sessionKey": sessionKey,
			"tool":       toolName,
			"toolUseId":  toolUseID,
			"result":     result,
			"isError":    isError,
		}
		if len(files) > 0 {
			out["files"] = files
		}
		t.emitACP(ACPEventToolCallUpdate, out)
	}
}

// translateChat converts a chat lifecycle event to acp.done or acp.usage_update.
func (t *ACPStreamTranslator) translateChat(sessionKey string, payload map[string]any) {
	state, _ := payload["state"].(string)

	switch state {
	case "done":
		text, _ := payload["text"].(string)
		t.emitACP(ACPEventDone, map[string]any{
			"type":       "done",
			"sessionKey": sessionKey,
			"stopReason": "stop",
			"text":       text,
		})
		// Emit usage update if available.
		if usage, ok := payload["usage"].(map[string]any); ok {
			t.emitACP(ACPEventUsageUpdate, map[string]any{
				"type":         "usage_update",
				"sessionKey":   sessionKey,
				"inputTokens":  usage["inputTokens"],
				"outputTokens": usage["outputTokens"],
			})
		}
	case "error":
		errMsg, _ := payload["error"].(string)
		t.emitACP(ACPEventDone, map[string]any{
			"type":       "done",
			"sessionKey": sessionKey,
			"stopReason": "error",
			"error":      errMsg,
		})
	case "aborted":
		text, _ := payload["text"].(string)
		t.emitACP(ACPEventDone, map[string]any{
			"type":       "done",
			"sessionKey": sessionKey,
			"stopReason": "cancel",
			"text":       text,
		})
	}
}

// emitACP serializes and broadcasts an ACP event.
func (t *ACPStreamTranslator) emitACP(event string, payload map[string]any) {
	payload["seq"] = t.seq.Add(1)
	data, err := json.Marshal(map[string]any{
		"event":   event,
		"payload": payload,
	})
	if err != nil {
		t.logger.Warn("failed to marshal ACP event", "event", event, "error", err)
		return
	}
	t.emit(event, data)
}

// pathPattern matches absolute and relative file paths in tool output.
var pathPattern = regexp.MustCompile(`(?:^|[\s"'` + "`" + `])(/[a-zA-Z0-9._/-]+\.[a-zA-Z0-9]+)`)

// ExtractFileLocations extracts file paths from tool input/result strings.
// Best-effort extraction for known tool types (read, write, edit, grep, find).
func ExtractFileLocations(toolName, input, result string) []string {
	var files []string
	seen := make(map[string]bool)

	addPath := func(p string) {
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}

	// Extract from input: look for known JSON fields.
	if input != "" {
		var inputObj map[string]any
		if json.Unmarshal([]byte(input), &inputObj) == nil {
			for _, key := range []string{"path", "file_path", "filePath"} {
				if p, ok := inputObj[key].(string); ok && p != "" {
					addPath(p)
				}
			}
		}
	}

	// Extract from result using regex for file-like paths.
	if result != "" {
		matches := pathPattern.FindAllStringSubmatch(result, 20)
		for _, m := range matches {
			p := m[1]
			// Filter out obvious non-file paths.
			if strings.HasPrefix(p, "/proc/") || strings.HasPrefix(p, "/sys/") ||
				strings.HasPrefix(p, "/dev/") || strings.HasPrefix(p, "/tmp/") {
				continue
			}
			addPath(p)
		}
	}

	return files
}
