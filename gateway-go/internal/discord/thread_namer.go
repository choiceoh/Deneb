// Package discord — LLM-based thread name generation for Discord conversations.
//
// When a new coding session starts in Discord, ThreadNamer calls a fast LLM
// (Haiku) to produce a short, descriptive thread title from the first message.
// Falls back to a truncated excerpt if the LLM call fails or times out.
package discord

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	// threadNameModel is a fast, cheap model suited for short title generation.
	threadNameModel = "claude-haiku-4-5-20251001"
	// threadNameMaxTokens caps the response to a short title (roughly 7 words).
	threadNameMaxTokens = 25
	// discordThreadNameLimit is Discord's maximum thread name length in characters.
	discordThreadNameLimit = 100
)

// ThreadNamer generates Discord thread titles from message content via an LLM.
type ThreadNamer struct {
	client *llm.Client
}

// NewThreadNamer creates a ThreadNamer backed by the given Anthropic LLM client.
// Returns nil if client is nil (feature disabled — replies go to channel directly).
func NewThreadNamer(client *llm.Client) *ThreadNamer {
	if client == nil {
		return nil
	}
	return &ThreadNamer{client: client}
}

// Generate produces a short, descriptive thread name for the given message.
// The result is guaranteed to fit within Discord's 100-character thread name limit.
// Falls back to a truncated excerpt on LLM error or timeout.
func (n *ThreadNamer) Generate(ctx context.Context, message string) string {
	if n == nil {
		return fallbackThreadName(message)
	}

	// Trim very long messages before sending to the LLM.
	input := message
	if len(input) > 400 {
		input = input[:400] + "…"
	}

	req := llm.ChatRequest{
		Model: threadNameModel,
		System: llm.SystemString(
			"Generate a short Discord thread title for the user's message. " +
				"Reply with ONLY the title — no quotes, no trailing punctuation, no explanation. " +
				"4–7 words max. Use the same language as the message.",
		),
		Messages:  []llm.Message{llm.NewTextMessage("user", fmt.Sprintf("Message:\n%s", input))},
		MaxTokens: threadNameMaxTokens,
	}

	title, err := n.client.Complete(ctx, req)
	if err != nil || title == "" {
		return fallbackThreadName(message)
	}

	// Strip stray surrounding quotes the model occasionally adds.
	title = strings.Trim(strings.TrimSpace(title), `"'`)
	return truncateThreadName(title)
}

// fallbackThreadName builds a thread name from the first line of the message.
func fallbackThreadName(message string) string {
	title := strings.TrimSpace(message)
	if i := strings.IndexByte(title, '\n'); i > 0 {
		title = title[:i]
	}
	return truncateThreadName(title)
}

// truncateThreadName trims a title to Discord's 100-char name limit, appending
// an ellipsis if truncation occurs.
func truncateThreadName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "New Thread"
	}
	if utf8.RuneCountInString(name) <= discordThreadNameLimit {
		return name
	}
	runes := []rune(name)
	return string(runes[:discordThreadNameLimit-1]) + "…"
}
