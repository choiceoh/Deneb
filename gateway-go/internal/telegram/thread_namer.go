package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// defaultMaxTopicLen is the maximum length of a Telegram forum topic name.
	// Telegram enforces a 128-char limit; we stay well under it for readability.
	defaultMaxTopicLen = 40

	// defaultNamerTimeout is the maximum time to wait for LLM inference.
	defaultNamerTimeout = 10 * time.Second

	// fallbackTopicName is used when the LLM fails or returns garbage.
	fallbackTopicName = "새 대화"

	// minMessageLen is the minimum message length to attempt naming.
	// Very short messages (greetings, etc.) get the fallback name.
	minMessageLen = 5

	// maxPromptMessageLen caps the user message length sent to the LLM prompt.
	// Longer messages are truncated to avoid wasting inference tokens.
	maxPromptMessageLen = 500
)

// ThreadNamer generates short topic names for Telegram forum threads using a local LLM.
// It is designed for the single-user DGX Spark deployment where sglang provides fast
// local inference.
type ThreadNamer struct {
	// generateFn calls a local LLM (sglang) for fast inference.
	// It receives a system+user prompt and returns the raw LLM output.
	generateFn func(ctx context.Context, prompt string) (string, error)

	// maxLen is the maximum allowed topic name length in runes.
	maxLen int

	// timeout is the maximum time to wait for LLM generation.
	timeout time.Duration
}

// NewThreadNamer creates a namer backed by the given generation function.
// The generateFn should call a local LLM (sglang) for fast inference.
func NewThreadNamer(generateFn func(ctx context.Context, prompt string) (string, error)) *ThreadNamer {
	return &ThreadNamer{
		generateFn: generateFn,
		maxLen:     defaultMaxTopicLen,
		timeout:    defaultNamerTimeout,
	}
}

// WithMaxLen sets the maximum topic name length in runes.
func (tn *ThreadNamer) WithMaxLen(maxLen int) *ThreadNamer {
	if maxLen > 0 {
		tn.maxLen = maxLen
	}
	return tn
}

// WithTimeout sets the maximum time to wait for LLM generation.
func (tn *ThreadNamer) WithTimeout(timeout time.Duration) *ThreadNamer {
	if timeout > 0 {
		tn.timeout = timeout
	}
	return tn
}

// NameThread generates a short Korean topic name from the first user message.
// Returns the fallback name if the message is too short, the LLM fails, or the
// output cannot be sanitized into a valid topic name.
func (tn *ThreadNamer) NameThread(ctx context.Context, firstMessage string) (string, error) {
	firstMessage = strings.TrimSpace(firstMessage)

	// Very short or empty messages get the fallback name directly.
	if utf8.RuneCountInString(firstMessage) < minMessageLen {
		return fallbackTopicName, nil
	}

	// Apply timeout to the LLM call.
	ctx, cancel := context.WithTimeout(ctx, tn.timeout)
	defer cancel()

	prompt := tn.buildPrompt(firstMessage)

	raw, err := tn.generateFn(ctx, prompt)
	if err != nil {
		return fallbackTopicName, fmt.Errorf("thread name generation failed: %w", err)
	}

	name := sanitizeName(raw, tn.maxLen)
	if name == "" {
		return fallbackTopicName, nil
	}

	return name, nil
}

// buildPrompt creates the prompt for thread name generation.
// The prompt instructs the LLM to produce a concise Korean topic name
// (2-5 words) that summarizes the user's intent.
func (tn *ThreadNamer) buildPrompt(message string) string {
	// Truncate long messages to avoid wasting inference tokens.
	truncated := truncateRunes(message, maxPromptMessageLen)

	var b strings.Builder
	b.Grow(512)

	// System instruction: concise and direct for fast inference.
	b.WriteString("사용자의 첫 메시지를 보고 대화 주제를 요약하는 짧은 한국어 제목을 생성하세요.\n\n")
	b.WriteString("규칙:\n")
	b.WriteString("- 2~5 단어로 작성\n")
	b.WriteString("- 한국어로 작성 (영문 고유명사는 허용)\n")
	b.WriteString("- 따옴표, 마침표, 특수문자 없이 제목만 출력\n")
	b.WriteString("- 명사형 또는 명사구로 끝내기\n")
	b.WriteString("- \"대화\", \"주제\", \"제목\" 같은 메타 단어 사용 금지\n\n")
	b.WriteString("사용자 메시지:\n")
	b.WriteString(truncated)
	b.WriteString("\n\n제목:")

	return b.String()
}

// sanitizeName cleans up LLM output to a valid Telegram forum topic name.
// It removes quotes, trims whitespace, strips control characters, and
// truncates to maxLen runes.
func sanitizeName(raw string, maxLen int) string {
	// Take only the first line (LLM may output extra explanation).
	if idx := strings.IndexByte(raw, '\n'); idx >= 0 {
		raw = raw[:idx]
	}

	// Remove common LLM artifacts: surrounding quotes, leading dash/bullet.
	name := strings.TrimSpace(raw)
	name = stripSurroundingQuotes(name)
	name = strings.TrimSpace(name)

	// Remove leading bullet/dash markers.
	name = strings.TrimLeft(name, "-•·* ")

	// Strip leading "제목:" or "제목 :" prefix the LLM might echo back.
	for _, prefix := range []string{"제목:", "제목 :", "주제:", "주제 :"} {
		if strings.HasPrefix(name, prefix) {
			name = strings.TrimSpace(name[len(prefix):])
		}
	}

	// Remove control characters and normalize whitespace.
	name = removeControlChars(name)
	name = collapseWhitespace(name)

	// Remove trailing punctuation that looks odd in topic names.
	name = strings.TrimRight(name, ".!?。！？,，;；:")

	name = strings.TrimSpace(name)

	// Truncate to max length (rune-safe).
	name = truncateRunes(name, maxLen)

	return name
}

// stripSurroundingQuotes removes matching quote pairs from the string.
func stripSurroundingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}

	quotePairs := [][2]rune{
		{'"', '"'},
		{'\'', '\''},
		{'\u201C', '\u201D'}, // left/right double quotation marks
		{'\u2018', '\u2019'}, // left/right single quotation marks
		{'\u300C', '\u300D'}, // CJK corner brackets
		{'\u300E', '\u300F'}, // CJK white corner brackets
	}

	runes := []rune(s)
	first := runes[0]
	last := runes[len(runes)-1]

	for _, pair := range quotePairs {
		if first == pair[0] && last == pair[1] {
			return string(runes[1 : len(runes)-1])
		}
	}

	return s
}

// removeControlChars strips Unicode control characters except spaces.
func removeControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) && r != ' ' && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// collapseWhitespace replaces runs of whitespace with a single space.
func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// truncateRunes truncates s to at most maxLen runes, preserving valid UTF-8.
// Does not break in the middle of a word if possible.
func truncateRunes(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}

	runes := []rune(s)
	truncated := runes[:maxLen]

	// Try to break at a word boundary (last space within the truncated range).
	result := string(truncated)
	if lastSpace := strings.LastIndexByte(result, ' '); lastSpace > 0 {
		// Only break at word boundary if we keep at least half the content.
		if lastSpace >= len(result)/2 {
			result = result[:lastSpace]
		}
	}

	return strings.TrimSpace(result)
}
