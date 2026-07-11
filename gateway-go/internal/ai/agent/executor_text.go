// executor_text.go — text/content-block utilities for the agent loop:
// interim-narration detection (DeliverableText filtering), thinking-block
// extraction, stop-reason context, and base64-image history stripping.
// Split from executor.go (RunAgent core loop).
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func isInterimNarration(text string, toolCallCount int) bool {
	return toolCallCount > 0 && utf8.RuneCountInString(text) < deliverableNarrationMaxRunes
}

// deliverableNarrationHeadMaxRunes bounds the leading self-narration preamble
// stripNarrationHead may peel off a turn's deliverable contribution. The
// observed leak ("이제 분석 보고를 정리해.") ran 16 runes; 80 leaves room for a
// two-sentence preamble while staying below any summary that carries content.
const deliverableNarrationHeadMaxRunes = 80

// stripNarrationHead removes a short self-narration preamble from a turn's
// deliverable contribution: leading plain-prose sentence(s) immediately
// followed by a horizontal rule or markdown heading that opens the actual
// report ("이제 분석 보고를 정리해.\n\n---\n\n## 📧 메일 분석…" → "## 📧 …").
// isInterimNarration drops whole tool-call turns, but a model can bake the
// same narration into the head of its *final* answer turn, which then opened
// the user-visible cron report (step3p7, 2026-06-10).
//
// Deliberately conservative — the text is returned unchanged unless ALL hold:
//   - every head line passes isNarrationSentenceLine (plain prose, no digits
//     or colons, sentence-final punctuation)
//   - the head totals ≤ deliverableNarrationHeadMaxRunes runes
//   - the first structural line after it is a horizontal rule or a # heading
//   - a non-empty body remains. The rule, being the model's narration/body
//     divider, is consumed with the head; a heading stays (it IS the body).
func stripNarrationHead(text string) string {
	lines := strings.Split(text, "\n")
	headRunes := 0
	sawHead := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case isHorizontalRule(line) || isMarkdownHeading(line):
			if !sawHead {
				return text // already opens with the body
			}
			bodyStart := i
			if isHorizontalRule(line) {
				bodyStart++
			}
			if body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n")); body != "" {
				return body
			}
			return text
		case isNarrationSentenceLine(line):
			headRunes += utf8.RuneCountInString(line)
			if headRunes > deliverableNarrationHeadMaxRunes {
				return text
			}
			sawHead = true
		default:
			return text // head line looks like content — keep everything
		}
	}
	return text // no body boundary found
}

// isNarrationSentenceLine reports whether a trimmed line could be model
// self-narration rather than report content: a plain prose sentence that
// starts with a letter and ends with sentence punctuation, carrying none of
// the factual-summary tells (digits, colon labels). Markdown structure and
// emoji-led content markers (📬/🔴) start with non-letters and fail the first
// check, so they are always kept.
func isNarrationSentenceLine(line string) bool {
	first, _ := utf8.DecodeRuneInString(line)
	if !unicode.IsLetter(first) {
		return false
	}
	if strings.ContainsAny(line, ":：0123456789") {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(line)
	return strings.ContainsRune(".!?。！？", last)
}

// isHorizontalRule reports whether a trimmed line is a markdown thematic
// break: three or more of the same rule character (-, *, _) and nothing else.
func isHorizontalRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	c := line[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 1; i < len(line); i++ {
		if line[i] != c {
			return false
		}
	}
	return true
}

// isMarkdownHeading reports whether a trimmed line is an ATX heading: one to
// six # followed by whitespace and at least one more character.
func isMarkdownHeading(line string) bool {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && n < len(line) && (line[n] == ' ' || line[n] == '\t')
}

// joinAllThinkingTexts concatenates every thinking block in the turn in order.
// Empty when no thinking blocks are present (extended thinking disabled).
func joinAllThinkingTexts(blocks []llm.ContentBlock) string {
	var b strings.Builder
	for i := range blocks {
		if blocks[i].Thinking == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(blocks[i].Thinking)
	}
	return b.String()
}

// extractThinkingText returns the raw reasoning text from a turn's content
// blocks. Prefers thinking blocks (Anthropic extended thinking), but falls
// back to the last text block (OpenAI-compatible models that explain their
// reasoning in plain text before tool calls). The caller (e.g. channel adapters
// channel adapters) is responsible for summarizing it.
func extractThinkingText(blocks []llm.ContentBlock) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Thinking != "" {
			return blocks[i].Thinking
		}
	}
	// Fallback: use the last text block as reasoning context.
	// OpenAI-compatible models express intent in text before tool calls.
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Type == "text" && blocks[i].Text != "" {
			return blocks[i].Text
		}
	}
	return ""
}

// stopReasonFromCtx determines the stop reason from a cancelled context.
func stopReasonFromCtx(ctx context.Context) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout"
	}
	return "aborted"
}

// stripBase64ImagesFromHistory replaces base64-encoded image blocks in the
// message history with a lightweight text placeholder. Called after turn 0
// when StripImagesAfterFirstTurn is set so that subsequent turns don't
// retransmit large image payloads to the LLM.
//
// Only "base64" source images are stripped; URL-referenced images are left
// intact because they don't carry inline bytes.
func stripBase64ImagesFromHistory(messages []llm.Message) []llm.Message {
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	for i, msg := range result {
		// Only process user messages; assistant/tool messages never contain images.
		if msg.Role != "user" {
			continue
		}

		// Parse as content block array. If it's a plain string there are no images.
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}

		changed := false
		for j, b := range blocks {
			if b.Type == "image" && b.Source != nil && b.Source.Type == "base64" {
				// Replace the heavy data payload with a text note.
				blocks[j] = llm.ContentBlock{
					Type: "text",
					Text: fmt.Sprintf("[image/%s already analyzed — not retransmitted]", b.Source.MediaType),
				}
				changed = true
			}
		}

		if changed {
			newContent, err := json.Marshal(blocks)
			if err == nil {
				result[i] = llm.Message{
					Role:    msg.Role,
					Content: newContent,
				}
			}
		}
	}

	return result
}
