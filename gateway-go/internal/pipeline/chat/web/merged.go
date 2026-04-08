// merged.go — Unified web tool that dispatches to search, request (HTTP), or research mode.
package web

import (
	"context"
	"encoding/json"
	"fmt"
)

// HTTPToolFunc is the raw HTTP tool handler (formerly the standalone "http" tool).
// Uses the same signature as toolctx.ToolFunc but is a named type in this package
// to avoid importing toolctx from web/.
type HTTPToolFunc = func(ctx context.Context, input json.RawMessage) (string, error)

// MergedTool returns a unified web tool that dispatches based on the "mode" parameter:
//   - "search" (default): web search/fetch via the existing Tool handler
//   - "request": raw HTTP requests via the HTTP tool handler
//   - "research": deep multi-query research via DeepResearchTool
func MergedTool(cache *FetchCache, localAI *LocalAIExtractor, httpFn HTTPToolFunc, drLLM DeepResearchLLM, defaultModel string) func(context.Context, json.RawMessage) (string, error) {
	searchFn := Tool(cache, localAI)
	researchFn := DeepResearchTool(cache, localAI, drLLM, defaultModel)

	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}

		switch p.Mode {
		case "", "search":
			return searchFn(ctx, input)
		case "request":
			return httpFn(ctx, input)
		case "research":
			return researchFn(ctx, input)
		default:
			return fmt.Sprintf("unknown mode %q: use search, request, or research", p.Mode), nil
		}
	}
}
