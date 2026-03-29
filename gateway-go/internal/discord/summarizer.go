// Package discord — Summarizer consolidates lightweight LLM tasks for Discord:
// thread title generation and reasoning summary for progress tracking.
//
// Both features share the same local sglang client and benefit from central
// thinking-output stripping in CompleteOpenAI().
package discord

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	// Thread title generation.
	threadNameMaxTokens    = 25
	discordThreadNameLimit = 100

	// Reasoning summary generation.
	reasoningSummaryMaxTokens = 40
	reasoningSummaryTimeout   = 3 * time.Second
	reasoningSummaryMaxInput  = 1200
	reasoningSummaryMaxRunes  = 65
)

// Summarizer generates Discord thread titles and reasoning summaries via a
// lightweight local LLM (e.g. sglang). Nil-safe: all methods gracefully
// return fallback values when the receiver is nil.
type Summarizer struct {
	client *llm.Client
	model  string
}

// NewSummarizer creates a Summarizer backed by the given OpenAI-compatible
// LLM client (e.g. local sglang). Returns nil if client is nil.
func NewSummarizer(client *llm.Client, model string) *Summarizer {
	if client == nil {
		return nil
	}
	return &Summarizer{client: client, model: model}
}

// --- Thread title generation ---

// ThreadTitle produces a short, descriptive thread name for the given message.
// The result is guaranteed to fit within Discord's 100-character thread name limit.
// Falls back to a truncated excerpt on LLM error or timeout.
func (s *Summarizer) ThreadTitle(ctx context.Context, message string) string {
	if s == nil {
		return fallbackThreadName(message)
	}

	input := message
	if len(input) > 400 {
		input = input[:400] + "…"
	}

	req := llm.ChatRequest{
		Model: s.model,
		System: llm.SystemString(
			"Generate a short Discord thread title for the user's message. " +
				"Reply with ONLY the title — no quotes, no trailing punctuation, no explanation. " +
				"4–7 words max. Use the same language as the message.",
		),
		Messages:  []llm.Message{llm.NewTextMessage("user", fmt.Sprintf("Message:\n%s", input))},
		MaxTokens: threadNameMaxTokens,
	}

	title, err := s.client.CompleteOpenAI(ctx, req)
	if err != nil || title == "" {
		return fallbackThreadName(message)
	}

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

// --- Reasoning summary generation ---

// ReasoningSummary produces a brief Korean summary of the given thinking text.
// Returns empty string on failure or if thinking is empty.
func (s *Summarizer) ReasoningSummary(ctx context.Context, thinking string) string {
	if s == nil || thinking == "" {
		return ""
	}

	input := strings.TrimSpace(thinking)
	if input == "" {
		return ""
	}

	// Truncate input to avoid sending huge prompts.
	if len(input) > reasoningSummaryMaxInput {
		input = input[len(input)-reasoningSummaryMaxInput:]
	}

	ctx, cancel := context.WithTimeout(ctx, reasoningSummaryTimeout)
	defer cancel()

	req := llm.ChatRequest{
		Model: s.model,
		System: llm.SystemString(
			"AI 에이전트의 추론 과정이 주어집니다. " +
				"입력이 영어여도 반드시 한국어로 번역하여 요약하세요. " +
				"지금 무엇을 하려는지 한국어로 한 문장(20자~40자)으로 요약하세요. " +
				"반드시 '~하고 있습니다' 또는 '~합니다' 체로 끝내세요. " +
				"설명 없이 요약만 출력하세요.",
		),
		Messages:  []llm.Message{llm.NewTextMessage("user", input)},
		MaxTokens: reasoningSummaryMaxTokens,
	}

	summary, err := s.client.CompleteOpenAI(ctx, req)
	if err != nil || summary == "" {
		return ""
	}

	summary = strings.Trim(strings.TrimSpace(summary), `"'`)
	summary = collapseNewlines(summary)
	if summary == "" {
		return ""
	}

	// Discard if the model echoed English instead of producing Korean.
	if !containsKorean(summary) {
		return ""
	}

	if utf8.RuneCountInString(summary) > reasoningSummaryMaxRunes {
		runes := []rune(summary)
		summary = string(runes[:reasoningSummaryMaxRunes]) + "…"
	}
	return summary
}

// --- Utility functions ---

// containsKorean reports whether s contains at least one Hangul character.
func containsKorean(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// collapseNewlines replaces all newlines with a single space.
func collapseNewlines(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	return strings.Join(parts, " ")
}
