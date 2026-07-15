package textprep

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	tokens "github.com/choiceoh/deneb/gateway-go/internal/core/replytokens"
)

// StripSilentReply removes the silent-reply token from proactive bodies.
func StripSilentReply(content string) string {
	return tokens.StripSilentToken(content, tokens.SilentReplyToken)
}

// SubstituteLetterTokens expands morning-letter template tokens.
func SubstituteLetterTokens(content string) string {
	return market.SubstituteLetterTokens(content)
}
