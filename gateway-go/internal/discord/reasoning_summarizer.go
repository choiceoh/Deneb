// Package discord — ReasoningSummarizer generates brief Korean summaries of
// LLM thinking blocks using the lightweight local model (sglang).
//
// Used by ProgressTracker to show what the agent is reasoning about alongside
// each tool execution step, e.g. "✅ 파일 읽기 — 설정 파일 구조를 파악하고 있습니다".
package discord

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	// reasoningSummaryMaxTokens caps the LLM response for summary generation.
	reasoningSummaryMaxTokens = 30
	// reasoningSummaryTimeout prevents slow LLM calls from blocking progress.
	reasoningSummaryTimeout = 3 * time.Second
	// reasoningSummaryMaxInput limits thinking text sent to the summarizer.
	reasoningSummaryMaxInput = 800
	// reasoningSummaryMaxRunes caps the final display length.
	reasoningSummaryMaxRunes = 50
)

// ReasoningSummarizer generates brief summaries from agent thinking blocks.
type ReasoningSummarizer struct {
	client *llm.Client
	model  string
}

// NewReasoningSummarizer creates a summarizer backed by the given
// OpenAI-compatible LLM client (e.g. local sglang). Returns nil if client is nil.
func NewReasoningSummarizer(client *llm.Client, model string) *ReasoningSummarizer {
	if client == nil {
		return nil
	}
	return &ReasoningSummarizer{client: client, model: model}
}

// Summarize produces a brief Korean summary of the given thinking text.
// Returns empty string on failure or if thinking is empty.
func (rs *ReasoningSummarizer) Summarize(ctx context.Context, thinking string) string {
	if rs == nil || thinking == "" {
		return ""
	}

	// Strip common LLM thinking headers that models echo back instead of summarizing.
	input := stripThinkingHeaders(thinking)
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
		Model: rs.model,
		System: llm.SystemString(
			"AI 에이전트의 추론 과정이 주어집니다. " +
				"지금 무엇을 하려는지 한국어로 한 문장(15자~30자)으로 요약하세요. " +
				"반드시 '~하고 있습니다' 또는 '~합니다' 체로 끝내세요. " +
				"설명 없이 요약만 출력하세요.",
		),
		Messages:  []llm.Message{llm.NewTextMessage("user", input)},
		MaxTokens: reasoningSummaryMaxTokens,
	}

	summary, err := rs.client.CompleteOpenAI(ctx, req)
	if err != nil || summary == "" {
		return ""
	}

	// Clean up: strip quotes, trim whitespace, collapse to single line.
	summary = strings.Trim(strings.TrimSpace(summary), `"'`)
	summary = collapseNewlines(summary)
	if summary == "" {
		return ""
	}

	// Discard if the model echoed English instead of producing Korean.
	if !containsKorean(summary) {
		return ""
	}

	// Enforce display length limit.
	if utf8.RuneCountInString(summary) > reasoningSummaryMaxRunes {
		runes := []rune(summary)
		summary = string(runes[:reasoningSummaryMaxRunes]) + "…"
	}
	return summary
}

// thinkingHeaderPrefixes are common LLM meta-prefixes that appear at the start
// of thinking blocks but carry no semantic value for summarization.
var thinkingHeaderPrefixes = []string{
	"Thinking Process:",
	"Analyze the Request:",
	"Analysis:",
	"Let me think",
	"Let me analyze",
	"I need to",
	"Okay, let me",
	"Okay, I need",
}

// stripThinkingHeaders removes common boilerplate prefixes from LLM thinking
// text so the summarizer receives the substantive reasoning content.
func stripThinkingHeaders(s string) string {
	s = strings.TrimSpace(s)
	// Strip up to a few header lines from the top.
	for range 5 {
		trimmed := false
		for _, prefix := range thinkingHeaderPrefixes {
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimSpace(s[len(prefix):])
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
		// Also skip leading newlines after stripping a prefix.
		s = strings.TrimLeft(s, "\r\n")
	}
	return strings.TrimSpace(s)
}

// containsKorean reports whether s contains at least one Hangul character.
// Used to discard summaries where the model echoed English instead of Korean.
func containsKorean(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// collapseNewlines replaces all newlines with a single space, producing a
// single-line string suitable for the progress embed.
func collapseNewlines(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	return strings.Join(parts, " ")
}
