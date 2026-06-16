// display.go — display-only sanitation of transcript messages before they are
// returned to a client surface (chat.history and miniapp.sessions.transcript).
//
// The transcript on disk is the LLM's context: it stores tool_use, tool_result,
// and thinking blocks verbatim because the prompt-cache rule requires that what
// the model saw is what history reloads. The native client, however, renders a
// user-role message as a chat bubble — and tool results are persisted as
// user-role messages carrying a tool_result block (the Anthropic API
// convention). Left unfiltered, raw tool output (command stdout, ps dumps,
// systemd errors) surfaces in the chat as an ordinary bubble the user can even
// quote. These helpers rewrite only the RPC response; the JSONL is untouched.
//
// They live in toolctx (not chat) because both the chat pipeline's History RPC
// and handlerminiapp's sessions.transcript RPC return transcripts to the
// client, and handlerminiapp deliberately depends only on this leaf package.
package toolctx

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/hanja"
)

// LinkEnrichmentHeader marks the start of a link-enrichment block appended to
// an interactive user message. chat.maybeEnrichLinks writes it and the display
// strips look for it; the generator and the strippers stay in sync through
// this constant.
const LinkEnrichmentHeader = "Link content from URLs in this message:"

// StripToolResultBlocksForDisplay removes tool_result content blocks from the
// messages handed to the client. A message whose blocks are *all* tool_result
// (the usual case — a tool turn has no user-visible text) is dropped entirely so
// no empty bubble remains; a mixed message keeps its other blocks. Plain-string
// content never holds a tool_result, so it passes through untouched. The input
// slice is not mutated — a fresh slice is returned.
func StripToolResultBlocksForDisplay(msgs []ChatMessage) []ChatMessage {
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

// StripLinkEnrichmentForDisplay removes appended enrichment blocks from user
// messages so history surfaces (native client bubbles) show what the user
// typed, not the fetched page dump. Only plain-string content is touched and
// only the RPC response is rewritten — the transcript itself never changes.
func StripLinkEnrichmentForDisplay(msgs []ChatMessage) []ChatMessage {
	marker := "\n\n---\n" + LinkEnrichmentHeader
	for i := range msgs {
		if msgs[i].Role != "user" {
			continue
		}
		var text string
		if err := json.Unmarshal(msgs[i].Content, &text); err != nil {
			continue // rich block content — enrichment only appends to plain text
		}
		idx := strings.Index(text, marker)
		if idx < 0 || !strings.HasSuffix(text, "\n---") {
			continue
		}
		msgs[i].Content = MarshalJSONString(strings.TrimRight(text[:idx], " \n"))
	}
	return msgs
}

// TransliterateAssistantTextForDisplay rewrites Han characters in assistant-role
// message text into their Sino-Korean Hangul readings (報告書 → 보고서) for the
// client surface only. Chinese-lineage models (GLM/MiMo/DeepSeek) sometimes write
// Sino-Korean vocabulary in Hanja, which reads as Chinese to the user. Like the
// other display strippers, it rewrites only the RPC response — the stored
// transcript keeps the model's raw output so history reloads stay byte-identical
// for the LLM (prompt-cache invariant). Only assistant text is touched: user
// input, tool_use args, and tool_result data pass through verbatim (a code/data
// surface where Han is not Korean prose to read aloud).
func TransliterateAssistantTextForDisplay(msgs []ChatMessage) []ChatMessage {
	for i := range msgs {
		if msgs[i].Role != "assistant" {
			continue
		}
		// Plain-string content (the common text-only assistant turn).
		var text string
		if json.Unmarshal(msgs[i].Content, &text) == nil {
			if conv := hanja.Transliterate(text); conv != text {
				msgs[i].Content = MarshalJSONString(conv)
			}
			continue
		}
		// Block-array content: transliterate text blocks only, leaving tool_use
		// blocks (and any non-text block) byte-for-byte intact.
		var blocks []json.RawMessage
		if json.Unmarshal(msgs[i].Content, &blocks) != nil {
			continue
		}
		changed := false
		for j, b := range blocks {
			conv, ok := transliterateTextBlock(b)
			if !ok {
				continue
			}
			blocks[j] = conv
			changed = true
		}
		if changed {
			if c, err := json.Marshal(blocks); err == nil {
				msgs[i].Content = c
			}
		}
	}
	return msgs
}

// transliterateTextBlock returns a rewritten copy of a content block when it is a
// {"type":"text"} block whose text contains Han characters, preserving any other
// fields on the block (e.g. cache_control). ok is false when the block is not a
// text block or needs no change.
func transliterateTextBlock(b json.RawMessage) (json.RawMessage, bool) {
	var head struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(b, &head) != nil || head.Type != "text" {
		return nil, false
	}
	conv := hanja.Transliterate(head.Text)
	if conv == head.Text {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil {
		return nil, false
	}
	t, err := json.Marshal(conv)
	if err != nil {
		return nil, false
	}
	fields["text"] = t
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, false
	}
	return out, true
}

// StripUserMessageTimestamp removes the leading "[<RFC3339>] " wall-clock
// prefix from a user message text. executeAgentRun bakes that prefix in at
// transcript-persist time: the model needs per-turn wall-clock while the
// system prompt's date field stays day-only precision for cache stability
// (see prompt-cache.md § 1). The bracketed segment must parse as RFC3339 —
// user-typed brackets ("[중요] 회의 메모") and messages persisted before the
// timestamp policy pass through unchanged.
func StripUserMessageTimestamp(text string) string {
	if !strings.HasPrefix(text, "[") {
		return text
	}
	end := strings.Index(text, "] ")
	if end < 0 {
		return text
	}
	if _, err := time.Parse(time.RFC3339, text[1:end]); err != nil {
		return text
	}
	return text[end+len("] "):]
}

// StripUserMessageTimestampsForDisplay removes the baked timestamp prefix
// from user bubbles handed to a client surface, so the timeline shows what
// the user actually typed. Only plain-string content is touched — the prepend
// site only writes plain text — and only the RPC response is rewritten; the
// stored transcript keeps the prefix so history reloads stay byte-identical
// for the LLM (prompt-cache invariant).
func StripUserMessageTimestampsForDisplay(msgs []ChatMessage) []ChatMessage {
	for i := range msgs {
		if msgs[i].Role != "user" {
			continue
		}
		var text string
		if err := json.Unmarshal(msgs[i].Content, &text); err != nil {
			continue // rich block content never carries the prefix
		}
		stripped := StripUserMessageTimestamp(text)
		if stripped == text {
			continue
		}
		msgs[i].Content = MarshalJSONString(stripped)
	}
	return msgs
}
