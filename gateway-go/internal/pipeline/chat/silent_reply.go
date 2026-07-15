package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"

// SilentReplyToken is the token that suppresses message delivery when the LLM
// replies with exactly this value (with optional surrounding whitespace).
const SilentReplyToken = chatport.SilentReplyToken

// IsSilentReply returns true if the text is exactly the silent reply token
// (with optional surrounding whitespace). This prevents substantive replies
// ending with NO_REPLY from being suppressed.
func IsSilentReply(text string) bool {
	return chatport.IsSilentReply(text)
}

// StripSilentToken removes a trailing NO_REPLY token from mixed-content text.
// Returns the remaining text trimmed. If the result is empty, the entire
// message should be treated as silent.
func StripSilentToken(text string) string {
	return chatport.StripSilentToken(text)
}
