package localai

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// SubmitJSON sends a request through the hub and unmarshals the JSON response
// into T. Retries once on parse failure (local AI sampling is non-deterministic).
// Used by dreaming phases and fact extraction.
func SubmitJSON[T any](h *Hub, ctx context.Context, req Request) (T, error) {
	var zero T

	// Force JSON response format.
	if req.ResponseFormat == nil {
		req.ResponseFormat = &llm.ResponseFormat{Type: "json_object"}
	}
	// JSON-mode calls are non-deterministic; disable cache by default.
	req.NoCache = true

	for attempt := range 2 {
		resp, err := h.Submit(ctx, req)
		if err != nil {
			return zero, err
		}

		raw := strings.TrimSpace(resp.Text)
		if raw == "" {
			return zero, fmt.Errorf("submitJSON: empty response from model")
		}

		result, err := jsonutil.UnmarshalLLM[T](raw)
		if err == nil {
			return result, nil
		}

		if attempt == 0 {
			continue
		}
		return zero, fmt.Errorf("submitJSON: parse failed after retry: raw=%s", jsonutil.Truncate(raw, 300))
	}

	return zero, fmt.Errorf("submitJSON: unreachable")
}
