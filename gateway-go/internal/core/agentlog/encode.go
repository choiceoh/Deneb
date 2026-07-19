package agentlog

import (
	"encoding/json"
	"log/slog"
)

func EncodeEvent[T any](v T) (json.RawMessage, error) {
	return json.Marshal(v)
}

func LogTyped[T any](w *Writer, sessionKey, eventType string, data T) {
	raw, err := EncodeEvent(data)
	if err != nil {
		slog.Warn("agentlog: event marshal failed", "type", eventType, "error", err)
		return
	}
	w.LogEvent(sessionKey, eventType, raw)
}
