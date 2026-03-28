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
)

// callSglang sends a streaming chat request to the local SGLang model and collects the full response.
func callSglang(ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (string, error) {
	events, err := client.StreamChatOpenAI(ctx, llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", user)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
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
	events, err := client.StreamChatOpenAI(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", user)},
		System:         llm.SystemString(system),
		MaxTokens:      maxTokens,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
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

		cleaned := extractJSON(raw)

		var result T
		if json.Unmarshal([]byte(cleaned), &result) == nil {
			return result, nil
		}

		// Try truncated JSON recovery: find the last complete object in an array,
		// close the array/object, and attempt to parse the partial result.
		if recovered := recoverTruncatedObject(cleaned); recovered != "" {
			if json.Unmarshal([]byte(recovered), &result) == nil {
				return result, nil
			}
		}

		// First attempt failed — retry once. The model may produce cleaner output
		// on a second call since SGLang sampling is non-deterministic.
		if attempt == 0 {
			continue
		}

		return zero, fmt.Errorf("callLLMJSON: parse failed after retry: raw=%s", truncate(raw, 300))
	}

	return zero, fmt.Errorf("callLLMJSON: unreachable")
}

// extractJSON removes thinking tags, code fences, and surrounding prose,
// returning the JSON object substring. Uses brace-depth tracking to find
// the complete outermost {...} rather than naive first/last index matching.
func extractJSON(s string) string {
	s = stripThinkingTags(s)
	s = strings.TrimSpace(s)

	// Strip markdown code fences.
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	s = strings.TrimSpace(s)

	// If it already starts with '{', it's likely clean JSON.
	if strings.HasPrefix(s, "{") {
		return s
	}

	// Find the outermost JSON object using brace-depth tracking.
	// This correctly handles prose like: 결과: {"a": {"b": 1}} 이상입니다.
	start := -1
	depth := 0
	inString := false
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if r == '}' {
			depth--
			if depth == 0 && start >= 0 {
				return s[start : i+1]
			}
		}
	}

	return s
}

// recoverTruncatedObject attempts to recover a parseable JSON object from
// truncated output (e.g. token limit hit mid-stream). It finds the start of
// an array value, locates the last complete object in it, closes the
// array and outer object. Returns empty string if recovery fails.
func recoverTruncatedObject(s string) string {
	arrStart := strings.Index(s, "[")
	if arrStart == -1 {
		return ""
	}

	// Find the prefix before the array (e.g. `{"results": `).
	prefix := strings.TrimSpace(s[:arrStart])

	sub := s[arrStart:]
	lastBrace := strings.LastIndex(sub, "}")
	if lastBrace == -1 {
		return ""
	}

	// Close the array.
	candidate := sub[:lastBrace+1] + "]"

	// If there was an outer object, close it too.
	if strings.HasPrefix(prefix, "{") {
		candidate = prefix + candidate + "}"
	}

	// Verify it's valid JSON before returning.
	if json.Valid([]byte(candidate)) {
		return candidate
	}

	// Fallback: try just the array portion.
	arrayOnly := sub[:lastBrace+1] + "]"
	if json.Valid([]byte(arrayOnly)) {
		return arrayOnly
	}

	return ""
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
			if ev.Type == "content_block_delta" {
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
					sb.WriteString(delta.Delta.Text)
				}
			}
		}
	}
}
