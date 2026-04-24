// steer_inject.go — Drain /steer notes into the outgoing LLM request.
//
// Called by the agent executor via AgentConfig.BeforeAPICall. Finds the
// LAST tool-result-bearing message in the message list and appends a
// "[사용자 조정: ...]" marker to it. If no tool-result exists yet (first
// turn, pre-tool), the drained notes are Restore()'d so the next turn
// (after the model actually emits tool calls) can inject them.
//
// Why append to an existing tool-result instead of inserting a new user
// message: role alternation and prompt-cache stability. Anthropic's API
// expects strict user/assistant alternation, and an extra user turn mid-
// stream invalidates the KV cache prefix — killing the performance gain
// that prompt caching is meant to provide. Appending to the LAST tool-
// result's content is a surgical addition: it modifies only the tail
// block of the most recent user message, and that tail is already past
// the cache boundary on the next turn.
//
// This mirrors Hermes' `_apply_pending_steer_to_tool_results` and the
// "Pre-API-call steer drain" block in run_agent.py lines 9060-9108.
package chat

import (
	"encoding/json"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// buildSteerHookIfEnabled is the composition-friendly entry point used by
// run_exec.go when wiring agent.ComposeBeforeAPICall. Returns nil when the
// hook has no effect (queue disabled or empty session) so the composer can
// filter it cleanly.
func buildSteerHookIfEnabled(
	queue *SteerQueue,
	sessionKey string,
	logger *slog.Logger,
) func(messages []llm.Message) []llm.Message {
	return buildSteerBeforeAPICall(queue, sessionKey, logger)
}

// buildSteerBeforeAPICall returns an AgentConfig.BeforeAPICall hook bound to
// a specific session's steer queue. The returned function is invoked once per
// LLM request; it drains pending notes and returns a per-request copy of the
// messages with the marker injected into the last tool_result block.
//
// The ORIGINAL messages slice is never mutated (prompt-cache stability).
// When no tool_result exists yet, drained notes are Restored so a later turn
// can pick them up after a tool batch runs.
func buildSteerBeforeAPICall(
	queue *SteerQueue,
	sessionKey string,
	logger *slog.Logger,
) func(messages []llm.Message) []llm.Message {
	if queue == nil || sessionKey == "" {
		return nil
	}
	return func(messages []llm.Message) []llm.Message {
		notes := queue.Drain(sessionKey)
		if len(notes) == 0 {
			return messages
		}
		marker := formatSteerMarker(notes)
		if marker == "" {
			// Defensive: formatSteerMarker filtered everything.
			return messages
		}
		adjusted, injected := injectSteerMarker(messages, marker)
		if !injected {
			// No tool_result to inject into yet — stay pending for next turn.
			// Restore with the original notes so the ordering survives.
			queue.Restore(sessionKey, notes)
			if logger != nil {
				logger.Info("steer deferred: no tool_result yet, will inject after next tool batch",
					"session", sessionKey, "notes", len(notes))
			}
			return messages
		}
		if logger != nil {
			logger.Info("steer injected into tool_result",
				"session", sessionKey, "notes", len(notes))
		}
		return adjusted
	}
}

// injectSteerMarker scans messages tail-to-head for the last message that
// carries at least one tool_result ContentBlock, appends marker to that
// block's Content, and returns a modified copy of messages along with true.
// Returns (messages, false) when no suitable target exists.
//
// The modified messages slice is a shallow copy — only the target message
// (and its blocks array) is reallocated — so callers may compare identity
// to detect whether injection happened.
func injectSteerMarker(messages []llm.Message, marker string) ([]llm.Message, bool) {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		msg := messages[idx]
		// tool_result blocks live inside role="user" messages under the
		// Anthropic block protocol (executor.go emits them as
		// llm.NewBlockMessage("user", toolResults)). Skip non-user messages.
		if msg.Role != "user" {
			continue
		}
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			// Plain text user message (no content blocks) — not a tool result.
			continue
		}
		// Find the LAST tool_result block within this message. There is
		// usually one per tool call; multiple blocks happen in multi-tool
		// turns, where attaching to the final one preserves ordering ("the
		// latest tool finished, by the way here's a nudge").
		target := -1
		for j := len(blocks) - 1; j >= 0; j-- {
			if blocks[j].Type == "tool_result" {
				target = j
				break
			}
		}
		if target < 0 {
			continue
		}

		// Shallow-copy blocks array so we do not mutate the caller's slice.
		newBlocks := make([]llm.ContentBlock, len(blocks))
		copy(newBlocks, blocks)
		newBlocks[target].Content += marker

		raw, err := json.Marshal(newBlocks)
		if err != nil {
			// Fall through: marshaling a known-good block list cannot fail
			// under normal conditions, but if it ever does, play it safe
			// and treat this as a non-injection (caller will Restore).
			return messages, false
		}

		// Shallow-copy message slice, replace target index with the patched
		// message. Downstream code (retry path, logging) still sees the
		// original slice via messages.
		newMessages := make([]llm.Message, len(messages))
		copy(newMessages, messages)
		newMessages[idx] = llm.Message{Role: msg.Role, Content: raw}
		return newMessages, true
	}
	return messages, false
}
