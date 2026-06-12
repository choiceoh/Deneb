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
	"bytes"
	"encoding/json"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
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
// cache_control marker on its last cacheable content block. String content is
// converted into a single text block first. Returns msg unchanged when the
// content cannot be decoded into blocks (preserves wire compatibility).
//
// Anthropic rejects cache_control on thinking/redacted_thinking blocks with
// HTTP 400, and an assistant message can legitimately END with one (a turn cut
// by max_tokens mid-reasoning persists as [thinking] only) — so the marker
// walks back to the last block that may carry it.
func withTrailingCacheControl(msg llm.Message) llm.Message {
	blocks, ok := decodeMessageBlocks(msg.Content)
	if !ok || len(blocks) == 0 {
		return msg
	}
	target := -1
	for i := len(blocks) - 1; i >= 0; i-- {
		if t := blocks[i].Type; t != "thinking" && t != "redacted_thinking" {
			target = i
			break
		}
	}
	if target < 0 {
		return msg // thinking-only message — nothing cacheable
	}
	blocks[target].CacheControl = &llm.CacheControl{Type: "ephemeral"}

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

// stripMessageCacheMarkersHook is a BeforeAPICall hook that removes every
// cache_control marker from the per-request message copy. Composed AFTER an
// existing hook chain, it neutralizes markers attached earlier in that chain
// (a trailing-cache hook built for the original provider) as well as any
// marker already present in the message payloads. Messages without the
// literal "cache_control" byte sequence pass through untouched — this both
// skips the decode and avoids a lossy re-marshal of block shapes our
// ContentBlock struct does not fully model.
func stripMessageCacheMarkersHook(messages []llm.Message) []llm.Message {
	out := messages
	copied := false
	for i := range messages {
		if !bytes.Contains(messages[i].Content, []byte(`"cache_control"`)) {
			continue
		}
		blocks, ok := decodeMessageBlocks(messages[i].Content)
		if !ok {
			continue
		}
		changed := false
		for j := range blocks {
			if blocks[j].CacheControl != nil {
				blocks[j].CacheControl = nil
				changed = true
			}
		}
		if !changed {
			continue
		}
		raw, err := json.Marshal(blocks)
		if err != nil {
			continue
		}
		if !copied {
			out = make([]llm.Message, len(messages))
			copy(out, messages)
			copied = true
		}
		out[i].Content = raw
	}
	return out
}

// reconcileFallbackCacheMarkers adjusts a fallback attempt's agent config when
// the fallback provider's cache_control policy differs from the provider the
// run was prepared for. run_exec.go applies the cache policy (system-marker
// strip + trailing-marker hook) for the ORIGINAL provider only; the fallback
// chain can cross to a provider with the opposite policy:
//
//   - fallback REJECTS cache_control (Kimi): the inherited system markers and
//     the trailing-marker hook would 400 every attempt — strip the system
//     copy and append a message-marker strip after the existing hook chain.
//   - fallback speaks Anthropic but the run was prepared for a non-Anthropic
//     wire (or a rejecting provider): the trailing hook was never installed —
//     append it so the fallback turn still gets prompt-cache reuse. (When the
//     original provider rejected markers its system copy stays stripped;
//     trailing markers alone still cache the conversation prefix.)
//
// OpenAI-mode fallbacks need nothing: their converters drop cache_control
// during message translation.
func reconcileFallbackCacheMarkers(agentCfg *agent.AgentConfig, deps runDeps, origProviderID, origModel, fbProviderID, fbModel string, fbClient *llm.Client, logger *slog.Logger) {
	if fbClient == nil {
		return
	}
	if modelCapability(deps, fbProviderID, fbModel).RejectsCacheControl {
		agentCfg.System = stripCacheControlMarkers(agentCfg.System)
		agentCfg.BeforeAPICall = agent.ComposeBeforeAPICall(agentCfg.BeforeAPICall, stripMessageCacheMarkersHook)
		logger.Info("fallback provider rejects cache_control; stripping markers",
			"fallbackProvider", fbProviderID, "fallbackModel", fbModel)
		return
	}
	hookInstalled := resolveAPIMode(deps, origProviderID) == llm.APIModeAnthropic &&
		!modelCapability(deps, origProviderID, origModel).RejectsCacheControl
	if fbClient.APIMode() == llm.APIModeAnthropic && !hookInstalled {
		agentCfg.BeforeAPICall = agent.ComposeBeforeAPICall(
			agentCfg.BeforeAPICall, buildTrailingCacheHook(llm.APIModeAnthropic))
	}
}

// stripCacheControlMarkers removes every cache_control field from a system
// prompt payload (a JSON array of ContentBlocks). A plain-string system prompt
// carries no markers and is returned unchanged. Used for cache-incompatible
// providers (Kimi) whose Anthropic-wire endpoint returns HTTP 400 when any
// cache_control is present — mirrors OpenClaw's per-provider strip. The
// trailing-message markers are handled separately by skipping
// buildTrailingCacheHook for those providers.
func stripCacheControlMarkers(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil || len(blocks) == 0 {
		return raw // string system prompt or undecodable — nothing to strip
	}
	changed := false
	for i := range blocks {
		if blocks[i].CacheControl != nil {
			blocks[i].CacheControl = nil
			changed = true
		}
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(blocks)
	if err != nil {
		return raw
	}
	return out
}
