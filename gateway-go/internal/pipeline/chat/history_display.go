// history_display.go — display-only sanitation of the transcript before it is
// returned to the native client by History().
//
// The transcript on disk is the LLM's context: it stores tool_use, tool_result,
// and thinking blocks verbatim because the prompt-cache rule requires that what
// the model saw is what history reloads. The native client, however, renders a
// user-role message as a chat bubble — and tool results are persisted as
// user-role messages carrying a tool_result block (the Anthropic API
// convention). Left unfiltered, raw tool output (command stdout, ps dumps,
// systemd errors) surfaces in the chat as an ordinary bubble the user can even
// quote. These strippers rewrite only the RPC response; the JSONL is untouched.
package chat

import "encoding/json"

// stripToolResultBlocksForDisplay removes tool_result content blocks from the
// messages handed to the client. A message whose blocks are *all* tool_result
// (the usual case — a tool turn has no user-visible text) is dropped entirely so
// no empty bubble remains; a mixed message keeps its other blocks. Plain-string
// content never holds a tool_result, so it passes through untouched. The input
// slice is not mutated — a fresh slice is returned.
func stripToolResultBlocksForDisplay(msgs []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		// Only rich block-array content can carry a tool_result; plain-string
		// content (the common text case) fails this unmarshal and is kept as-is.
		var blocks []json.RawMessage
		if json.Unmarshal(m.Content, &blocks) != nil {
			out = append(out, m)
			continue
		}
		kept := make([]json.RawMessage, 0, len(blocks))
		for _, b := range blocks {
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(b, &head) == nil && head.Type == "tool_result" {
				continue // drop the raw tool output
			}
			kept = append(kept, b)
		}
		switch {
		case len(kept) == len(blocks):
			out = append(out, m) // nothing stripped — keep the original bytes
		case len(kept) == 0:
			// message was only tool_result(s) → drop the whole bubble
		default:
			c, err := json.Marshal(kept)
			if err != nil {
				out = append(out, m) // re-marshal failed: keep it rather than lose the message
				continue
			}
			m.Content = c
			out = append(out, m)
		}
	}
	return out
}
