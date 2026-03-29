// zen_decoder.go — Zen arch: Instruction Decoder for request pre-processing.
//
// CPU analogy: The instruction decoder translates complex CISC instructions into
// simpler micro-ops that the execution engine can process efficiently. It also
// performs early validation and branch hint extraction before the instruction
// reaches the execution pipeline.
//
// Application: Before a user message enters the agent pipeline, the decoder
// performs lightweight pre-processing that extracts hints and normalizes the
// input. This avoids doing this work inside the hot path of executeAgentRun.
//
// Decode stages:
//   1. Slash command detection (fast reject — avoids entering the full pipeline)
//   2. Message classification (short/greeting → skip expensive prefetch)
//   3. Attachment pre-validation (detect types before pipeline stages need them)
//   4. Intent hints (extract keywords that guide knowledge prefetch)
package chat

import (
	"strings"
	"unicode/utf8"
)

// DecodedMessage is the pre-processed form of a user message.
// The decoder extracts hints that downstream pipeline stages use to
// skip unnecessary work or optimize their behavior.
type DecodedMessage struct {
	// Original message text.
	Text string

	// IsSlashCommand is true if the message starts with "/" — the pipeline
	// should handle it via ParseSlashCommand instead of the agent loop.
	IsSlashCommand bool

	// IsShort is true if the message is too short for expensive processing
	// (knowledge prefetch, proactive context). Threshold: < 20 chars.
	IsShort bool

	// IsGreeting is true if the message looks like a simple greeting.
	// These skip knowledge search (no useful context to retrieve).
	IsGreeting bool

	// HasAttachments is true if the request includes image/document attachments.
	HasAttachments bool

	// HasImageAttachment is true if any attachment is an image (triggers image model selection).
	HasImageAttachment bool

	// KeywordHints are extracted keywords that can guide knowledge prefetch.
	// Extracted cheaply via simple tokenization (no LLM call).
	KeywordHints []string
}

// greetings is a set of common Korean and English greetings for fast detection.
var greetings = map[string]bool{
	"안녕": true, "안녕하세요": true, "ㅎㅇ": true, "하이": true,
	"hi": true, "hello": true, "hey": true, "yo": true,
	"ㅋ": true, "ㅋㅋ": true, "ㅋㅋㅋ": true, "ㅎㅎ": true,
	"네": true, "응": true, "ok": true, "ㅇㅇ": true,
	"감사": true, "고마워": true, "ㄱㅅ": true, "thanks": true,
}

// DecodeMessage pre-processes a user message before it enters the agent pipeline.
// This is the "instruction decode" stage — cheap analysis that guides the
// execution pipeline's behavior.
func DecodeMessage(text string, attachments []ChatAttachment) DecodedMessage {
	trimmed := strings.TrimSpace(text)

	dm := DecodedMessage{
		Text:           text,
		IsSlashCommand: strings.HasPrefix(trimmed, "/"),
		IsShort:        utf8.RuneCountInString(trimmed) < 20,
		HasAttachments: len(attachments) > 0,
	}

	// Greeting detection — check if the entire message (lowered, trimmed) is a greeting.
	if dm.IsShort {
		lower := strings.ToLower(trimmed)
		dm.IsGreeting = greetings[lower]
	}

	// Image attachment detection.
	for _, att := range attachments {
		if att.Type == "image" {
			dm.HasImageAttachment = true
			break
		}
	}

	// Keyword hint extraction — simple word tokenization for messages
	// long enough to benefit from knowledge search.
	if !dm.IsShort && !dm.IsSlashCommand {
		dm.KeywordHints = extractKeywordHints(trimmed)
	}

	return dm
}

// extractKeywordHints extracts significant words from a message for
// knowledge prefetch guidance. Filters out common Korean particles
// and short words that don't add search value.
func extractKeywordHints(text string) []string {
	words := strings.Fields(text)
	hints := make([]string, 0, len(words)/2)

	for _, w := range words {
		// Skip short words and common particles.
		if utf8.RuneCountInString(w) < 2 {
			continue
		}
		// Skip common Korean particles/connectors.
		if isKoreanParticle(w) {
			continue
		}
		hints = append(hints, w)
		if len(hints) >= 10 {
			break // cap at 10 keywords to keep it lightweight
		}
	}
	return hints
}

// isKoreanParticle returns true for common Korean grammatical particles
// that don't carry searchable meaning.
var koreanParticles = map[string]bool{
	"은": true, "는": true, "이": true, "가": true,
	"을": true, "를": true, "에": true, "에서": true,
	"의": true, "로": true, "으로": true, "와": true,
	"과": true, "도": true, "만": true, "부터": true,
	"까지": true, "처럼": true, "같이": true, "보다": true,
	"한테": true, "에게": true, "께": true, "하고": true,
	"그": true, "저": true, "그리고": true,
	"하지만": true, "그런데": true, "그래서": true,
	"좀": true, "잘": true, "더": true, "다": true,
	"해줘": true, "해주세요": true, "할래": true, "할게": true,
}

func isKoreanParticle(w string) bool {
	return koreanParticles[w]
}
