// sglang.go — Shared SGLang helpers for the memory package.
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
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// sglangNoThinking disables reasoning mode for all local sglang calls.
// Qwen3.5 reasoning adds latency and "Thinking Process:" preambles that
// leak into output; none of our sglang tasks need it.
var sglangNoThinking = map[string]any{
	"chat_template_kwargs": map[string]any{"enable_thinking": false},
}

// callSglang sends a streaming chat request to the local SGLang model and collects the full response.
func callSglang(ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (string, error) {
	events, err := client.StreamChat(ctx, llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", user)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
		ExtraBody: sglangNoThinking,
	})
	if err != nil {
		return "", err
	}
	if events == nil {
		return "", fmt.Errorf("sglang: nil event channel")
	}
	return collectStream(ctx, events)
}

// callSglangJSON is like callSglang but requests JSON-formatted output
// via response_format. Use for endpoints that must return valid JSON.
func callSglangJSON(ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (string, error) {
	events, err := client.StreamChat(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", user)},
		System:         llm.SystemString(system),
		MaxTokens:      maxTokens,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
		ExtraBody:      sglangNoThinking,
	})
	if err != nil {
		return "", err
	}
	if events == nil {
		return "", fmt.Errorf("sglang: nil event channel")
	}
	return collectStream(ctx, events)
}

// callLLMJSON is the single entry point for all dreaming-phase LLM calls.
// It calls the local SGLang model with JSON mode, extracts the JSON object from
// potentially noisy output (thinking tags, prose, code fences, truncation), and
// unmarshals into T. Retries once on parse failure.
//
// This replaces the old pattern of callLLM() → manual extractJSONObject() → json.Unmarshal()
// that was duplicated across every dream phase with inconsistent error handling.
func callLLMJSON[T any](ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (T, error) {
	var zero T

	for attempt := range 2 {
		raw, err := callSglangJSON(ctx, client, model, system, user, maxTokens)
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
		// on a second call since SGLang sampling is non-deterministic.
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
					return "", fmt.Errorf("sglang stream error: %s", errBody.Message)
				}
				return "", fmt.Errorf("sglang stream error: %s", string(ev.Payload))
			}
		}
	}
}
