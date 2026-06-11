package compaction

import (
	"bytes"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// BalanceToolBlocks rewrites any orphaned tool_use / tool_result block into a
// text stub so the compacted transcript always has every tool_use paired with a
// tool_result and vice versa.
//
// Why: the fallback compaction tiers (Embedding/MMR, Recency, Emergency) and the
// LLM summarizer select or cut message windows without snapping to
// tool_use↔tool_result pair boundaries, and nothing on the send path repairs the
// result (anthropic.go runs only NormalizeMessages). An orphan — a tool_use with
// no matching tool_result, or a tool_result with no matching tool_use — makes
// Anthropic's /v1/messages reject the request with a 400, which fails the turn
// and, because the same broken history is re-sent, can wedge the session until
// /reset. This is the single chokepoint that guarantees a balanced output
// regardless of which tier ran.
//
// Orphans are replaced with a short text stub rather than dropped, so no message
// is emptied and role alternation is preserved. No-op when already balanced —
// messages without an orphan keep their exact original Content (cache-stable).
func BalanceToolBlocks(messages []llm.Message) []llm.Message {
	toolUseIDs := map[string]struct{}{}
	resultIDs := map[string]struct{}{}
	parsed := make([][]llm.ContentBlock, len(messages))
	for i := range messages {
		blocks, ok := decodeBlocks(messages[i].Content)
		if !ok {
			continue
		}
		parsed[i] = blocks
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				if b.ID != "" {
					toolUseIDs[b.ID] = struct{}{}
				}
			case "tool_result":
				if b.ToolUseID != "" {
					resultIDs[b.ToolUseID] = struct{}{}
				}
			}
		}
	}

	for i := range messages {
		blocks := parsed[i]
		if blocks == nil {
			continue
		}
		changed := false
		for j := range blocks {
			switch blocks[j].Type {
			case "tool_use":
				if _, paired := resultIDs[blocks[j].ID]; !paired {
					blocks[j] = llm.ContentBlock{Type: "text", Text: "[tool call omitted during compaction]"}
					changed = true
				}
			case "tool_result":
				if _, paired := toolUseIDs[blocks[j].ToolUseID]; !paired {
					blocks[j] = llm.ContentBlock{Type: "text", Text: "[tool result omitted during compaction]"}
					changed = true
				}
			}
		}
		if changed {
			messages[i] = llm.NewBlockMessage(messages[i].Role, blocks)
		}
	}
	return messages
}

// decodeBlocks parses a message's Content as a content-block array. Returns
// (nil, false) for string content (a plain text message), empty content, or any
// parse error, so a non-block or malformed message is left untouched.
func decodeBlocks(content json.RawMessage) ([]llm.ContentBlock, bool) {
	t := bytes.TrimSpace(content)
	if len(t) == 0 || t[0] != '[' {
		return nil, false
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, false
	}
	return blocks, true
}
