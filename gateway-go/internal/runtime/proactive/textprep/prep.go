package textprep

import (
	tokens "github.com/choiceoh/deneb/gateway-go/internal/core/replytokens"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
)

// StripSilentReply removes the silent-reply token from proactive bodies.
func StripSilentReply(content string) string {
	return tokens.StripSilentToken(content, tokens.SilentReplyToken)
}

// SubstituteLetterTokens expands morning-letter template tokens.
func SubstituteLetterTokens(content string) string {
	return market.SubstituteLetterTokens(content)
}
