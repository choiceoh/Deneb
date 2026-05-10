// cache_breakpoints.go implements Hermes Agent's "system_and_3" prompt-cache
// strategy on the messages array. The system prompt blocks already carry
// cache_control markers (assembled in prompt/system_prompt.go); this hook
// adds ephemeral markers to the last N non-system messages right before each
// LLM call so multi-turn cache hits stay warm.
//
// Anthropic limits a single request to 4 cache_control breakpoints. The
// system blocks consume up to 2 (Static + Semi-static), so this hook attaches
// 2 trailing markers — total 4 = limit. The Hermes reference pattern uses 3
// trailing markers (system 1 + msg 3); we trade one of those for keeping
// Semi-static cached because the skills prompt is ~10-15K tokens.
//
// Non-Anthropic providers (OpenAI, etc.) ignore cache_control; this returns
// nil for those so ComposeBeforeAPICall filters it out.
package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// trailingCacheCount is the number of trailing non-system messages that
// receive an ephemeral cache_control marker per request. Together with the
// 2 system-block markers (Static + Semi-static), this exactly fills the
// 4-breakpoint per-request limit.
const trailingCacheCount = 2

// buildTrailingCacheHook returns a BeforeAPICall hook that attaches an
// ephemeral cache_control marker to the last trailingCacheCount non-system
// messages. Returns nil for non-Anthropic providers so ComposeBeforeAPICall
// can filter it out.
//
// The hook produces a per-request copy — the source messages slice and its
// elements are never mutated. This satisfies the prompt-cache doctrine rule
// that historical messages must not be modified mid-conversation: only the
// per-request payload gains markers, the transcript stays clean.
func buildTrailingCacheHook(apiMode string) func([]llm.Message) []llm.Message {
	if apiMode != llm.APIModeAnthropic {
		return nil
	}
	return func(messages []llm.Message) []llm.Message {
		targets := pickTrailingCacheTargets(messages, trailingCacheCount)
		if len(targets) == 0 {
			return messages
		}
		out := make([]llm.Message, len(messages))
		copy(out, messages)
		for _, idx := range targets {
			out[idx] = withTrailingCacheControl(out[idx])
		}
		return out
	}
}

// pickTrailingCacheTargets returns the indices of the last n non-system
// messages, in ascending order. If fewer than n non-system messages exist,
// returns whatever was found.
func pickTrailingCacheTargets(messages []llm.Message, n int) []int {
	if n <= 0 || len(messages) == 0 {
		return nil
	}
	picks := make([]int, 0, n)
	for i := len(messages) - 1; i >= 0 && len(picks) < n; i-- {
		if messages[i].Role == "system" {
			continue
		}
		picks = append(picks, i)
	}
	for i, j := 0, len(picks)-1; i < j; i, j = i+1, j-1 {
		picks[i], picks[j] = picks[j], picks[i]
	}
	return picks
}

// withTrailingCacheControl returns a copy of msg with an ephemeral
// cache_control marker on its last content block. String content is converted
// into a single text block first. Returns msg unchanged when the content
// cannot be decoded into blocks (preserves wire compatibility).
func withTrailingCacheControl(msg llm.Message) llm.Message {
	blocks, ok := decodeMessageBlocks(msg.Content)
	if !ok || len(blocks) == 0 {
		return msg
	}
	last := blocks[len(blocks)-1]
	last.CacheControl = &llm.CacheControl{Type: "ephemeral"}
	blocks[len(blocks)-1] = last

	raw, err := json.Marshal(blocks)
	if err != nil {
		return msg
	}
	msg.Content = raw
	return msg
}

// decodeMessageBlocks decodes a Message.Content payload (JSON string or
// []ContentBlock) into a fresh []ContentBlock slice safe to mutate. String
// content is wrapped in a single text block.
func decodeMessageBlocks(raw json.RawMessage) ([]llm.ContentBlock, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil, false
		}
		return []llm.ContentBlock{{Type: "text", Text: s}}, true
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil && len(blocks) > 0 {
		return blocks, true
	}
	return nil, false
}
