// LLM summarizer adapter for Aurora compaction sweep.
//
// Bridges the Summarizer callback expected by RunSweep with the gateway's
// LLM client. Uses a dedicated compaction system prompt that instructs the
// model to produce concise, information-dense summaries.
package aurora

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	// compactionSystemPrompt instructs the LLM to produce summaries for compaction.
	compactionSystemPrompt = `You are a context compaction assistant. Your task is to summarize conversation segments into concise, information-dense summaries that preserve all actionable details.

Rules:
- Preserve all file paths, function names, variable names, URLs, and numeric values exactly.
- Preserve the chronological order of events and decisions.
- Preserve all unresolved questions, pending tasks, and action items.
- Remove redundant greetings, filler, and repeated information.
- Use bullet points for distinct facts; use prose for connected narratives.
- Write in the same language as the source material.
- Target the specified token count; do not pad or truncate artificially.
- For tool calls: preserve tool name, key arguments, and outcome (success/failure). Format: [tool:NAME → result]
- When summarizing decisions, include the reasoning or constraint that led to the choice. Preserve rejected alternatives if explicitly discussed.
- Preserve file:line references and function/method names exactly. When code changes are discussed, keep the before→after pattern.
- CRITICAL: Preserve speaker attribution. Clearly distinguish who said or did what — "사용자가 요청함" vs "AI가 제안함". Never attribute AI actions/statements to the user or vice versa.`

	// aggressiveAddendum is appended for aggressive compression passes.
	aggressiveAddendum = `

IMPORTANT: This is an aggressive compression pass. Be significantly more concise:
- Merge related points into single sentences.
- Drop examples that merely illustrate already-stated facts.
- Keep only the most critical details: decisions made, files changed, errors encountered, next steps.
- Aim for 40-60% of the previous summary length.`

	// summarizeTimeout is the max time for a single LLM summarization call.
	summarizeTimeout = 90 * time.Second
)

// NewLLMSummarizer creates a Summarizer backed by the given LLM client.
// The model parameter specifies which model to use for summarization.
// The apiType parameter selects the streaming method: "anthropic" for the
// Anthropic Messages API, anything else for OpenAI-compatible /chat/completions.
func NewLLMSummarizer(client *llm.Client, model string, apiType string) Summarizer {
	return func(text string, aggressive bool, opts *SummarizeOptions) (string, error) {
		if client == nil {
			return deterministicFallback(text), nil
		}

		system := compactionSystemPrompt
		if aggressive {
			system += aggressiveAddendum
		}

		// Build the user message with context hints from options.
		var userMsg strings.Builder
		if opts != nil {
			if opts.IsCondensed != nil && *opts.IsCondensed {
				fmt.Fprintf(&userMsg, "[Condensed summary pass, depth=%d]\n", safeUint32(opts.Depth))
			}
			if opts.TargetTokens != nil {
				fmt.Fprintf(&userMsg, "[Target: ~%d tokens]\n", *opts.TargetTokens)
			}
			if opts.PreviousSummary != nil && *opts.PreviousSummary != "" {
				fmt.Fprintf(&userMsg, "[Previous summary for context:]\n%s\n\n", *opts.PreviousSummary)
			}
		}
		userMsg.WriteString("Summarize the following conversation segment:\n\n")
		userMsg.WriteString(text)

		ctx, cancel := context.WithTimeout(context.Background(), summarizeTimeout)
		defer cancel()

		// Use non-streaming chat for summarization.
		req := llm.ChatRequest{
			Model:     model,
			System:    llm.SystemString(system),
			Messages:  []llm.Message{llm.NewTextMessage("user", userMsg.String())},
			MaxTokens: 4096,
			Stream:    true,
		}

		var ch <-chan llm.StreamEvent
		var streamErr error
		if apiType == "anthropic" {
			ch, streamErr = client.StreamChat(ctx, req)
		} else {
			ch, streamErr = client.StreamChatOpenAI(ctx, req)
		}
		if streamErr != nil {
			return "", fmt.Errorf("summarize LLM call: %w", streamErr)
		}

		// Collect streamed text from content_block_delta events.
		var result strings.Builder
		for ev := range ch {
			if ev.Type == "content_block_delta" && len(ev.Payload) > 0 {
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil {
					result.WriteString(delta.Delta.Text)
				}
			}
		}

		summary := strings.TrimSpace(result.String())
		if summary == "" {
			return deterministicFallback(text), nil
		}
		return summary, nil
	}
}

// deterministicFallback produces a truncated version of the source text
// when LLM summarization is unavailable or fails.
func deterministicFallback(text string) string {
	const maxChars = 512 * 4 // ~512 tokens
	if len(text) <= maxChars {
		return text
	}
	// Take first and last portions.
	half := maxChars / 2
	return text[:half] + "\n...[truncated]...\n" + text[len(text)-half:]
}

func safeUint32(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}
