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
	"github.com/choiceoh/deneb/gateway-go/internal/localai"
)

const (
	// compactionSystemPrompt instructs the LLM to produce structured summaries.
	compactionSystemPrompt = `You are a context compaction assistant. Summarize conversation segments into structured XML summaries.

CRITICAL: Respond with TEXT ONLY. Do NOT call any tools. Tool calls will be REJECTED.

Before your final summary, wrap your analysis in <analysis> tags to organize your thoughts:
<analysis>
- What are the key topics discussed?
- What decisions were made?
- What files/paths are referenced?
- What is still pending?
- What was the causal flow? (action → outcome → next action)
</analysis>

Then provide your summary. The <analysis> block will be removed from the final output.

Output format — use only the sections that apply, omit empty sections:

<summary>
Concise chronological narrative of what happened. Always include this section.
</summary>

<timeline>
Causal chain of actions and outcomes, preserving the episodic flow:
- [HH:mm] user requested X
- [HH:mm] agent tried A → failed (reason)
- [HH:mm] agent tried B → succeeded
- [HH:mm] user confirmed / requested next step
Keep only significant steps (skip routine reads/writes). Focus on causality: what led to what.
</timeline>

<decisions>
- What was decided and why. Include rejected alternatives if discussed.
- Preserve the reasoning or constraint behind each choice.
</decisions>

<pending>
- [TODO] Unresolved questions or tasks.
- [BLOCKED] Items waiting on external input.
</pending>

<references>
- file:line paths, tool usage annotations (— NAME 사용: args), URLs.
</references>

Rules:
- CRITICAL: Extract and preserve ALL user-stated facts verbatim: names, numbers, dates, codes, IPs, passwords, scores, versions, identifiers. These are the highest-priority information — never drop them.
- Preserve file paths, function names, variable names, URLs, and numeric values exactly.
- Preserve tool execution results (command outputs, error messages, return values) — they are evidence the user may recall later.
- Preserve user instructions about response style, format, or behavior (e.g., "use numbered lists", "speak formally").
- Preserve speaker attribution: "사용자가 요청함" vs "AI가 제안함".
- Write in the same language as the source material.
- Target the specified token count.
- When space is tight, preserve facts and tool results first, then compress narrative and filler content.
- CRITICAL: Always include <summary>. Omit other sections if they have no content.
- The input includes [timestamp | role] headers — use them to reconstruct the conversation flow for <timeline>.`

	// aggressiveAddendum is appended for aggressive compression passes.
	aggressiveAddendum = `

IMPORTANT: Aggressive compression pass. Respect the XML structure but be much more concise:
- <summary>: 2-3 sentences maximum. But ALWAYS preserve user-stated facts (names, numbers, dates, codes, IPs, scores, keys) verbatim — these are non-compressible.
- <timeline>: Keep only pivotal cause-effect pairs (drop routine steps). 3-5 entries max.
- <decisions>: Keep only critical decisions (drop obvious/minor ones).
- <pending>: Merge related items.
- <references>: Keep only files that were actually modified.
- Aim for 40-60% of the previous summary length, but never sacrifice user-stated facts for length reduction.`

	// summarizeTimeout is the max time for a single LLM summarization call.
	summarizeTimeout = 90 * time.Second
)

// NewLLMSummarizer creates a Summarizer backed by the given LLM client.
// The model parameter specifies which model to use for summarization.
func NewLLMSummarizer(client *llm.Client, model string) Summarizer {
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
				userMsg.WriteString(`The input contains previously structured XML summaries. Merge them:
- Combine <summary> sections into a higher-level narrative. Preserve ALL user-stated facts (names, numbers, dates, codes) from every summary — never drop these during merging.
- Merge <timeline> sections: keep only significant causal chains (attempts that failed → what succeeded). Drop routine steps but preserve pivots and key breakthroughs.
- Deduplicate <decisions> (keep the final decision if a topic was revisited).
- Remove <pending> items that were resolved in later summaries.
- Merge <references>, keeping only still-relevant file paths.
- Preserve tool execution results and user instructions about style/format.
`)
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

		req := llm.ChatRequest{
			Model:     model,
			System:    llm.SystemString(system),
			Messages:  []llm.Message{llm.NewTextMessage("user", userMsg.String())},
			MaxTokens: 4096,
			Stream:    true,
			ExtraBody: map[string]any{
				"chat_template_kwargs": map[string]any{"enable_thinking": false},
			},
		}

		ch, streamErr := client.StreamChat(ctx, req)
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
		// Strip <analysis>...</analysis> scratchpad from the final summary.
		// The scratchpad improves summary quality (the model "thinks" first)
		// without wasting tokens in the surviving context.
		summary = stripAnalysisScratchpad(summary)
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

// stripAnalysisScratchpad removes <analysis>...</analysis> blocks from
// the LLM's output. These blocks are a thinking scratchpad that improves
// summary quality but should not survive into the final context.
func stripAnalysisScratchpad(s string) string {
	for {
		start := strings.Index(s, "<analysis>")
		if start < 0 {
			break
		}
		end := strings.Index(s, "</analysis>")
		if end < 0 {
			// Unclosed tag — remove from <analysis> to end.
			s = strings.TrimSpace(s[:start])
			break
		}
		s = strings.TrimSpace(s[:start] + s[end+len("</analysis>"):])
	}
	return s
}

// NewHubSummarizer creates a Summarizer that routes through the centralized
// local AI hub for token budget management and priority queuing.
// Falls back to deterministic truncation if the hub is nil.
func NewHubSummarizer(hub *localai.Hub) Summarizer {
	return func(text string, aggressive bool, opts *SummarizeOptions) (string, error) {
		if hub == nil {
			return deterministicFallback(text), nil
		}

		system := compactionSystemPrompt
		if aggressive {
			system += aggressiveAddendum
		}

		var userMsg strings.Builder
		if opts != nil {
			if opts.IsCondensed != nil && *opts.IsCondensed {
				fmt.Fprintf(&userMsg, "[Condensed summary pass, depth=%d]\n", safeUint32(opts.Depth))
				userMsg.WriteString(`The input contains previously structured XML summaries. Merge them:
- Combine <summary> sections into a higher-level narrative. Preserve ALL user-stated facts (names, numbers, dates, codes) from every summary — never drop these during merging.
- Merge <timeline> sections: keep only significant causal chains (attempts that failed → what succeeded). Drop routine steps but preserve pivots and key breakthroughs.
- Deduplicate <decisions> (keep the final decision if a topic was revisited).
- Remove <pending> items that were resolved in later summaries.
- Merge <references>, keeping only still-relevant file paths.
- Preserve tool execution results and user instructions about style/format.
`)
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

		resp, err := hub.Submit(ctx, localai.Request{
			System:    system,
			Messages:  []llm.Message{llm.NewTextMessage("user", userMsg.String())},
			MaxTokens: 4096,
			Priority:  localai.PriorityBackground,
			CallerTag: "aurora_compaction",
		})
		if err != nil {
			return "", fmt.Errorf("summarize hub call: %w", err)
		}

		summary := strings.TrimSpace(resp.Text)
		if summary == "" {
			return deterministicFallback(text), nil
		}
		summary = stripAnalysisScratchpad(summary)
		if summary == "" {
			return deterministicFallback(text), nil
		}
		return summary, nil
	}
}

// WithFallback wraps a primary Summarizer with a fallback: if the primary
// returns an error, the fallback is tried before resorting to deterministic
// truncation. This lets compaction survive a single-model failure (e.g.,
// local sglang down) by routing to a cloud model immediately.
func WithFallback(primary, fallback Summarizer) Summarizer {
	if fallback == nil {
		return primary
	}
	return func(text string, aggressive bool, opts *SummarizeOptions) (string, error) {
		result, err := primary(text, aggressive, opts)
		if err == nil {
			return result, nil
		}
		// Primary failed — try fallback model immediately.
		return fallback(text, aggressive, opts)
	}
}

func safeUint32(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}
