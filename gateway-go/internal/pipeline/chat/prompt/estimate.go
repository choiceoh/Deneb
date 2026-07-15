package prompt

import "github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"

// EstimateTokens returns the chat pipeline's shared token estimate for text.
func EstimateTokens(text string) int { return tokenest.Estimate(text) }
