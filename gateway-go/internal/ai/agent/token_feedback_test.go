package agent

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
)

// The calibrator corrects an estimate of the prompt against the count the
// provider reports for that same prompt. Both guards below exist because each
// side was, at different times, measuring something else: the actual excluded
// cache reads (so the factor tracked the cache hit rate) and the estimate
// excluded tools (so it would have tracked the size of the tool head).

func TestPromptTokensFromUsageCountsTheWholePrompt(t *testing.T) {
	// A cache-warm turn: most of the prompt was read from the provider's cache
	// and only the tail was fresh. InputTokens alone would report 400.
	u := llm.TokenUsage{InputTokens: 400, CacheReadInputTokens: 12_000, CacheCreationInputTokens: 600}
	if got, want := promptTokensFromUsage(u), 13_000; got != want {
		t.Errorf("promptTokensFromUsage = %d, want %d", got, want)
	}
	// A cold turn reports the same prompt with nothing cached.
	cold := llm.TokenUsage{InputTokens: 13_000}
	if got := promptTokensFromUsage(cold); got != 13_000 {
		t.Errorf("cold turn = %d, want 13000", got)
	}
}

func TestEstimatePromptTokensCountsTools(t *testing.T) {
	est := tokenest.ForFamily(tokenest.FamilyDefault)
	base := llm.ChatRequest{
		System:   llm.FlexibleFromRaw(json.RawMessage(`"You are a careful assistant."`)),
		Messages: []llm.Message{{Role: "user", Content: llm.FlexibleFromRaw(json.RawMessage(`"오늘 일정 알려줘"`))}},
	}
	withoutTools := estimatePromptTokens(est, base)

	schema := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"The page to fetch"}},"required":["url"]}`)
	withTools := base
	withTools.Tools = []llm.Tool{
		{Name: "web", Description: "Fetch a URL and return readable text", RawInputSchema: llm.FlexibleFromRaw(schema)},
		{Name: "calendar", Description: "Read the operator's calendar", RawInputSchema: llm.FlexibleFromRaw(schema)},
	}
	got := estimatePromptTokens(est, withTools)

	if got <= withoutTools {
		t.Fatalf("tools did not reach the estimate: without=%d with=%d", withoutTools, got)
	}
	// The schemas are the larger half of this tiny request; a change that drops
	// them would leave the estimate near the tools-free number.
	if got < withoutTools*2 {
		t.Errorf("tool schemas barely counted: without=%d with=%d", withoutTools, got)
	}
	t.Logf("estimate without tools=%d with tools=%d", withoutTools, got)
}
