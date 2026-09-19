package pilot

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// YesNo is a role's one-word YES/NO verdict, with the probability behind it
// when the backend that answered reports token logprobs.
type YesNo struct {
	// PYes is P(YES) renormalized over the YES and NO alternatives of the
	// answer token. Meaningful only when HasProb.
	PYes    float64
	HasProb bool
	// Token is the answer token itself. Backends without logprobs (the
	// OpenRouter rungs of the tiny chain report none) leave only this, and the
	// caller reads it the way it read the one-word answer before.
	Token string
	// Model is the candidate that answered.
	Model string
}

// yesNoTopN is how many alternatives to ask for. A model answering the
// question puts YES and NO at the head of the list.
const yesNoTopN = 10

// minYesNoMass is the share of the answer token that YES and NO must hold
// together before their ratio is trusted. Below it the model was about to write
// something else (markdown, a preamble) and the ratio of two long shots says
// little, so the verdict falls back to the token.
const minYesNoMass = 0.5

// CallRoleYesNo asks role a question whose instructions demand a one-word YES
// or NO, and reads the verdict off the first generated token: P(YES) from its
// top alternatives when the backend reports logprobs, the token alone when it
// does not. Candidates and request shaping are CallRoleLLM's (the role's model,
// then Registry.HelperFallbacks; per-candidate thinking-off fields and the
// server timeout). The call itself is a one-token, non-streaming completion at
// temperature 0 — logprobs arrive in the final body, so the stream translator
// the chat path shares stays untouched.
//
// Greedy decoding already picks YES exactly when P(YES) > P(NO), so the text
// answer was always a hidden 0.5 threshold. Returning the probability lets the
// caller choose the threshold, and removes the coin flip on cases that sit at
// 0.5 (a template notification whose verdict flipped with its amount).
func CallRoleYesNo(ctx context.Context, role modelrole.Role, system, userMessage string) (YesNo, error) {
	ctx, cancel := context.WithTimeout(ctx, pilotTimeout)
	defer cancel()

	temperature := 0.0
	req := llm.ChatRequest{
		Messages:    []llm.Message{llm.NewTextMessage("user", userMessage)},
		System:      llm.SystemString(system),
		MaxTokens:   1,
		Temperature: &temperature,
	}
	var lastErr error
	for _, c := range roleCandidates(role) {
		req.Model = c.model
		req.ExtraBody = pilotExtraBody(shapeRoleExtra(ctx, role, c.providerID, c.model, nil))
		first, err := c.client.CompleteFirstToken(ctx, req, yesNoTopN)
		if err != nil {
			// A one-token call has no partial answer to protect: any failure
			// means this model gave no verdict, so the next candidate is tried.
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		emitHelperUsage(role, c.model, c.providerID, first.Usage)
		verdict := YesNo{Token: first.Text, Model: c.model}
		verdict.PYes, verdict.HasProb = yesNoProbability(first.Top)
		return verdict, nil
	}
	return YesNo{}, fmt.Errorf("all models failed: %w", lastErr)
}

// CallTinyYesNo is CallRoleYesNo on the tiny role.
func CallTinyYesNo(ctx context.Context, system, userMessage string) (YesNo, error) {
	return CallRoleYesNo(ctx, modelrole.RoleTiny, system, userMessage)
}

// yesNoProbability folds the answer token's alternatives into P(YES). Case and
// surrounding space are ignored (" YES", "Yes" count as YES); ok is false when
// YES and NO together hold less than minYesNoMass.
func yesNoProbability(top []llm.TokenProb) (pYes float64, ok bool) {
	var yes, no float64
	for _, alt := range top {
		switch strings.ToUpper(strings.TrimSpace(alt.Token)) {
		case "YES":
			yes += math.Exp(alt.Logprob)
		case "NO":
			no += math.Exp(alt.Logprob)
		}
	}
	if yes+no < minYesNoMass {
		return 0, false
	}
	return yes / (yes + no), true
}
