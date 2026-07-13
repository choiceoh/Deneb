package generation

// L2 genesis-epoch bench support (RSI P5-4 slice 2): the meta-evolution slow
// loop benches a genesis-system-prompt revision by replaying fixed scenarios
// through the incumbent and the proposal and scoring both outputs with the
// SAME deterministic admissibility pipeline production uses. This file
// exposes the two narrow hooks that bench needs — a one-shot generator bound
// to the production genesis model, and the parse+gate pipeline — without
// opening the Service's persistence path.

import (
	"context"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// ShadowGenerate runs one genesis generation with an EXPLICIT system prompt
// (the bench's incumbent or proposal) on the service's production model.
// Never persists, never touches the daily cap. Non-streaming — same
// streaming-JSON unreliability rationale as the evolver benches.
func (s *Service) ShadowGenerate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if s == nil || s.llmClient == nil {
		return "", fmt.Errorf("genesis shadow: service unwired")
	}
	return s.llmClient.Complete(ctx, llm.ChatRequest{
		Model:          s.resolveModel(""),
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(systemPrompt),
		MaxTokens:      2048,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
}

// BenchAdmissibility runs the deterministic genesis admissibility pipeline on
// one raw model response: parse (a skip verdict reports skipped=true), then
// the specificity gate. LLM-free — this is the bench's ground truth, and it
// is exactly the gate a production generation must clear, so a revision that
// benches clean cannot have optimized against a softer stand-in.
func BenchAdmissibility(text string) (skipped bool, issues []string, err error) {
	skill, perr := parseGenesisResponse(text)
	if perr != nil {
		return false, nil, perr
	}
	if skill == nil {
		return true, nil, nil
	}
	return false, skillSpecificityIssues(skill), nil
}
