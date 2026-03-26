package chat

import (
	"regexp"
	"strings"
)

// SilentReplyToken is the token that suppresses message delivery when the LLM
// replies with exactly this value (with optional surrounding whitespace).
const SilentReplyToken = "NO_REPLY"

var (
	// silentExactRe matches the exact silent reply token with optional whitespace.
	silentExactRe = regexp.MustCompile(`^\s*NO_REPLY\s*$`)
	// silentTrailingRe matches a trailing NO_REPLY token at the end of mixed content.
	silentTrailingRe = regexp.MustCompile(`(?:^|\s+|\*+)NO_REPLY\s*$`)
)

// IsSilentReply returns true if the text is exactly the silent reply token
// (with optional surrounding whitespace). This prevents substantive replies
// ending with NO_REPLY from being suppressed.
func IsSilentReply(text string) bool {
	if text == "" {
		return false
	}
	return silentExactRe.MatchString(text)
}

// StripSilentToken removes a trailing NO_REPLY token from mixed-content text.
// Returns the remaining text trimmed. If the result is empty, the entire
// message should be treated as silent.
func StripSilentToken(text string) string {
	return strings.TrimSpace(silentTrailingRe.ReplaceAllString(text, ""))
}
