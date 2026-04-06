// localai.go — Shared local AI helpers for the memory package.
//
// callLLMJSON is the single entry point for all dreaming LLM calls that expect
// structured JSON output. It handles streaming collection, thinking-tag removal,
// JSON extraction from noisy model output, truncated JSON recovery, and retry.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/localai"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// pkgLocalAIHub is the centralized local AI hub for token budget management.
// Set via SetLocalAIHub during server initialization. When set, callLocalAI
// and callLocalAIJSON route through the hub instead of making direct calls.
var pkgLocalAIHub *localai.Hub

// SetLocalAIHub sets the centralized local AI hub for the memory package.
func SetLocalAIHub(h *localai.Hub) {
	pkgLocalAIHub = h
}

// callLocalAIJSON is like callLocalAI but requests JSON-formatted output
// via response_format. Use for endpoints that must return valid JSON.
// An optional guidedSchema can be passed to enable local AI's native
// guided_json constrained decoding (xgrammar) for reliable structured output.
// When the centralized local AI hub is available, routes through it.
func callLocalAIJSON(ctx context.Context, client *llm.Client, model, system, user string, maxTokens int, guidedSchema ...json.RawMessage) (string, error) {
	rf := &llm.ResponseFormat{Type: "json_object"}

	// Build extra body with guided_json if a schema is provided.
	// guided_json is local AI's native parameter for grammar-constrained
	// decoding, more reliable than response_format json_schema.
	var extra map[string]any
	if len(guidedSchema) > 0 && guidedSchema[0] != nil {
		extra = map[string]any{"guided_json": guidedSchema[0]}
	}

	// Hub path.
	if h := pkgLocalAIHub; h != nil {
		resp, err := h.Submit(ctx, localai.Request{
			System:         system,
			Messages:       []llm.Message{llm.NewTextMessage("user", user)},
			MaxTokens:      maxTokens,
			Priority:       localai.PriorityBackground,
			CallerTag:      "memory_json", // covers fact extraction, dreaming phases
			ResponseFormat: rf,
			ExtraBody:      extra,
			NoCache:        true, // JSON extractions are non-deterministic
		})
		if err != nil {
			return "", err
		}
		return resp.Text, nil
	}

	// Legacy direct path — merge guided_json with NoThinking.
	merged := make(map[string]any, len(localai.NoThinking)+len(extra))
	for k, v := range localai.NoThinking {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	events, err := client.StreamChat(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", user)},
		System:         llm.SystemString(system),
		MaxTokens:      maxTokens,
		Stream:         true,
		ResponseFormat: rf,
		ExtraBody:      merged,
	})
	if err != nil {
		return "", err
	}
	if events == nil {
		return "", fmt.Errorf("localai: nil event channel")
	}
	return collectStream(ctx, events)
}

// callLLMJSON is the single entry point for all dreaming-phase LLM calls.
// It calls the local AI model with JSON mode, extracts the JSON object from
// potentially noisy output (thinking tags, prose, code fences, truncation), and
// unmarshals into T. Retries once on parse failure.
//
// This replaces the old pattern of callLLM() → manual extractJSONObject() → json.Unmarshal()
// that was duplicated across every dream phase with inconsistent error handling.
func callLLMJSON[T any](ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (T, error) {
	var zero T

	for attempt := range 2 {
		raw, err := callLocalAIJSON(ctx, client, model, system, user, maxTokens)
		if err != nil {
			return zero, err
		}

		// Empty response — the model returned no content. Don't retry since
		// a second call is unlikely to produce a different result.
		if strings.TrimSpace(raw) == "" {
			return zero, fmt.Errorf("callLLMJSON: empty response from model")
		}

		result, err := jsonutil.UnmarshalLLM[T](raw)
		if err == nil {
			return result, nil
		}

		// First attempt failed — retry once. The model may produce cleaner output
		// on a second call since local AI sampling is non-deterministic.
		if attempt == 0 {
			continue
		}

		return zero, fmt.Errorf("callLLMJSON: parse failed after retry: raw=%s", jsonutil.Truncate(raw, 300))
	}

	return zero, fmt.Errorf("callLLMJSON: unreachable")
}

// extractJSON delegates to jsonutil.ExtractObject for backward compatibility
// within this package. Other packages should use jsonutil.ExtractObject directly.
func extractJSON(s string) string {
	return jsonutil.ExtractObject(s)
}

// collectStream gathers all text deltas from an OpenAI-compatible streaming response.
func collectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), nil
			}
			return "", ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return strings.TrimSpace(sb.String()), nil
			}
			switch ev.Type {
			case "content_block_delta":
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
					sb.WriteString(delta.Delta.Text)
				}
			case "error":
				var errBody struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				}
				if json.Unmarshal(ev.Payload, &errBody) == nil && errBody.Message != "" {
					return "", fmt.Errorf("localai stream error: %s", errBody.Message)
				}
				return "", fmt.Errorf("localai stream error: %s", string(ev.Payload))
			}
		}
	}
}
