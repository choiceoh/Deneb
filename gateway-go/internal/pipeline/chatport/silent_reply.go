package chatport

import tokens "github.com/choiceoh/deneb/gateway-go/internal/core/replytokens"

// SilentReplyToken is the token that suppresses message delivery when the LLM
// replies with exactly this value (with optional surrounding whitespace).
const SilentReplyToken = tokens.SilentReplyToken

// IsSilentReply returns true if the text is exactly the silent reply token
// (with optional surrounding whitespace).
func IsSilentReply(text string) bool {
	return tokens.IsSilentReplyText(text, SilentReplyToken)
}

// StripSilentToken removes a trailing NO_REPLY token from mixed-content text.
// Returns the remaining text trimmed. If the result is empty, the entire
// message should be treated as silent.
func StripSilentToken(text string) string {
	return tokens.StripSilentToken(text, SilentReplyToken)
}
