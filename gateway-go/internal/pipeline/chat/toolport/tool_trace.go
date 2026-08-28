// tool_trace.go — reconstruct completed tool calls from a stored transcript.
//
// The live stream shows tool activity as chips (started/completed frames with
// a detail hint, a one-line summary, and a bounded preview). The transcript
// persists the same activity as tool_use blocks (assistant message) paired
// with tool_result blocks (a later user-role message, Anthropic convention).
// On restore the display strips drop those blocks — which erased the chips
// entirely. CollectToolTraces rebuilds the chip data from the raw messages
// BEFORE the strips run, using the same digest helpers as the live stream so
// wording is identical across live and restored views.
package toolport

import "encoding/json"

// ToolTraceItem is one completed tool call, in chip vocabulary.
type ToolTraceItem struct {
	Tool    string
	Detail  string
	Summary string
	Preview string
	IsError bool
}

// CollectToolTraces walks raw transcript messages and returns completed tool
// calls grouped by the ID of the assistant message that issued them, in
// tool_use order. Calls with no matching tool_result (interrupted turns) are
// omitted — an unfinished call has no result to show.
func CollectToolTraces(msgs []ChatMessage) map[string][]ToolTraceItem {
	type pendingCall struct {
		msgID string
		slot  int // index into out[msgID], reserved at tool_use time to keep call order
	}
	pending := make(map[string]pendingCall)
	out := make(map[string][]ToolTraceItem)
	filled := make(map[string][]bool)

	for _, m := range msgs {
		if m.ID == "" {
			continue // a trace needs a stable message anchor
		}
		var blocks []json.RawMessage
		if json.Unmarshal(m.Content, &blocks) != nil {
			continue // plain-string content carries no tool blocks
		}
		for _, b := range blocks {
			var head struct {
				Type      string          `json:"type"`
				ID        string          `json:"id"`          // tool_use
				Name      string          `json:"name"`        // tool_use
				Input     json.RawMessage `json:"input"`       // tool_use
				ToolUseID string          `json:"tool_use_id"` // tool_result
				IsError   bool            `json:"is_error"`    // tool_result
				Content   json.RawMessage `json:"content"`     // tool_result
			}
			if json.Unmarshal(b, &head) != nil {
				continue
			}
			switch head.Type {
			case "tool_use":
				if head.ID == "" || head.Name == "" {
					continue
				}
				out[m.ID] = append(out[m.ID], ToolTraceItem{
					Tool:   head.Name,
					Detail: ToolStreamDetail(head.Name, head.Input),
				})
				filled[m.ID] = append(filled[m.ID], false)
				pending[head.ID] = pendingCall{msgID: m.ID, slot: len(out[m.ID]) - 1}
			case "tool_result":
				call, ok := pending[head.ToolUseID]
				if !ok {
					continue
				}
				delete(pending, head.ToolUseID)
				text := toolResultText(head.Content)
				item := &out[call.msgID][call.slot]
				item.Summary = SummarizeToolResult(text)
				item.Preview = ToolResultPreview(text)
				item.IsError = head.IsError
				filled[call.msgID][call.slot] = true
			}
		}
	}

	// Drop calls that never completed; drop message entries that end up empty.
	for msgID, items := range out {
		kept := items[:0]
		for i, item := range items {
			if filled[msgID][i] {
				kept = append(kept, item)
			}
		}
		if len(kept) == 0 {
			delete(out, msgID)
			continue
		}
		out[msgID] = kept
	}
	return out
}

// toolResultText flattens a tool_result block's content — a plain JSON string
// or an array of {type:"text",text:...} blocks — into one string for the
// digest helpers. Non-text blocks (images) contribute nothing.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var joined string
	for _, b := range blocks {
		if b.Type != "text" || b.Text == "" {
			continue
		}
		if joined != "" {
			joined += "\n"
		}
		joined += b.Text
	}
	return joined
}
