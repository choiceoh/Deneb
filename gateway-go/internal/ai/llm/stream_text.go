package llm

import (
	"encoding/json"
	"strings"
)

// DrainStreamText collects assistant text from content-block delta events.
// Malformed and non-text events are ignored, matching streaming consumers'
// best-effort assembly semantics.
func DrainStreamText(events <-chan StreamEvent) string {
	var text strings.Builder
	for event := range events {
		if event.Type != "content_block_delta" {
			continue
		}
		var payload struct {
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal(event.Payload.Bytes(), &payload) == nil {
			text.WriteString(payload.Delta.Text)
		}
	}
	return text.String()
}
