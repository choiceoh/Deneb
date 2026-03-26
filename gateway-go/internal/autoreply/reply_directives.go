// reply_directives.go — Reply directive processing (media splitting, threading, silent).
// Mirrors src/auto-reply/reply/reply-directives.ts (49 LOC).
package autoreply

import (
	"regexp"
	"strings"
)

// ReplyDirectiveParseResult holds the result of parsing reply directives.
type ReplyDirectiveParseResult struct {
	Text           string
	MediaURLs      []string
	MediaURL       string
	ReplyToID      string
	ReplyToCurrent bool
	ReplyToTag     bool
	AudioAsVoice   bool
	IsSilent       bool
}

// mediaURLPattern matches common media URLs in reply text.
var mediaURLPattern = regexp.MustCompile(`(?m)^(https?://\S+\.(?:png|jpe?g|gif|webp|mp4|webm|ogg|mp3|wav|pdf|svg))(?:\s|$)`)

// splitMediaFromOutput extracts media URLs from the start/end of text.
func splitMediaFromOutput(raw string) (text string, mediaURLs []string, mediaURL string, audioAsVoice bool) {
	if raw == "" {
		return "", nil, "", false
	}

	var urls []string
	remaining := raw

	// Extract media URLs from each line.
	lines := strings.Split(remaining, "\n")
	var textLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if mediaURLPattern.MatchString(trimmed) {
			match := mediaURLPattern.FindString(trimmed)
			urls = append(urls, strings.TrimSpace(match))
		} else {
			textLines = append(textLines, line)
		}
	}

	text = strings.TrimSpace(strings.Join(textLines, "\n"))

	if len(urls) > 0 {
		mediaURL = urls[0]
		mediaURLs = urls
	}

	// Detect voice audio hints.
	if strings.Contains(raw, "[[audio_as_voice]]") || strings.Contains(raw, "[[voice]]") {
		audioAsVoice = true
	}

	return text, mediaURLs, mediaURL, audioAsVoice
}

// ParseReplyDirectives parses reply directives from raw agent output text.
// Extracts media URLs, threading tags, and silent tokens.
func ParseReplyDirectives(raw string, currentMessageID string, silentToken string) ReplyDirectiveParseResult {
	text, mediaURLs, mediaURL, audioAsVoice := splitMediaFromOutput(raw)

	// Extract reply threading tags.
	replyToID, replyToCurrent := ApplyReplyThreading(text, "")
	hasReplyTag := replyToCurrent || replyToID != ""

	if hasReplyTag {
		text = StripReplyTags(text)
	}

	// Apply current message ID for reply_to_current.
	if replyToCurrent && currentMessageID != "" {
		replyToID = currentMessageID
	}

	// Check for silent reply token.
	if silentToken == "" {
		silentToken = SilentReplyToken
	}
	isSilent := IsSilentReplyText(text, silentToken)
	if isSilent {
		text = ""
	}

	return ReplyDirectiveParseResult{
		Text:           text,
		MediaURLs:      mediaURLs,
		MediaURL:       mediaURL,
		ReplyToID:      replyToID,
		ReplyToCurrent: replyToCurrent,
		ReplyToTag:     hasReplyTag,
		AudioAsVoice:   audioAsVoice,
		IsSilent:       isSilent,
	}
}
