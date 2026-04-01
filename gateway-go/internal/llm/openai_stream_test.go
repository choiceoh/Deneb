package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStreamChat_UsageOnFinishChunk verifies that input tokens are
// captured when the provider bundles usage data on the finish_reason chunk
// (instead of a separate usage-only chunk). This is the pattern used by
// some OpenAI-compatible providers (Z.AI, sglang, vLLM).
func TestStreamChat_UsageOnFinishChunk(t *testing.T) {
	// Serve a minimal streaming response where usage is on the finish chunk.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Chunk 1: first content delta.
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":    "chatcmpl-1",
			"model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{"role": "assistant", "content": "Hello"}},
			},
		}))
		flusher.Flush()

		// Chunk 2: finish_reason with inline usage (no separate usage-only chunk).
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":    "chatcmpl-1",
			"model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"},
			},
			"usage": map[string]int{
				"prompt_tokens":     150,
				"completion_tokens": 25,
				"total_tokens":      175,
			},
		}))
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}

	var inputTokens, outputTokens int
	for ev := range events {
		switch ev.Type {
		case "message_start":
			var ms MessageStart
			if json.Unmarshal(ev.Payload, &ms) == nil {
				// Last message_start wins (same as consumeStream).
				inputTokens = ms.Message.Usage.InputTokens
			}
		case "message_delta":
			var md MessageDelta
			if json.Unmarshal(ev.Payload, &md) == nil {
				outputTokens = md.Usage.OutputTokens
			}
		}
	}

	if inputTokens != 150 {
		t.Errorf("InputTokens = %d, want 150", inputTokens)
	}
	if outputTokens != 25 {
		t.Errorf("OutputTokens = %d, want 25", outputTokens)
	}
}

// TestStreamChat_UsageOnSeparateChunk verifies the standard path where
// usage arrives in a separate chunk after the finish_reason chunk.
func TestStreamChat_UsageOnSeparateChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Chunk 1: content.
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":    "chatcmpl-2",
			"model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{"role": "assistant", "content": "World"}},
			},
		}))
		flusher.Flush()

		// Chunk 2: finish_reason (no usage).
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":    "chatcmpl-2",
			"model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"},
			},
		}))
		flusher.Flush()

		// Chunk 3: usage-only (no choices).
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":      "chatcmpl-2",
			"model":   "test-model",
			"choices": []map[string]any{},
			"usage": map[string]int{
				"prompt_tokens":     200,
				"completion_tokens": 30,
				"total_tokens":      230,
			},
		}))
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}

	var inputTokens, outputTokens int
	for ev := range events {
		switch ev.Type {
		case "message_start":
			var ms MessageStart
			if json.Unmarshal(ev.Payload, &ms) == nil {
				inputTokens = ms.Message.Usage.InputTokens
			}
		case "message_delta":
			var md MessageDelta
			if json.Unmarshal(ev.Payload, &md) == nil {
				outputTokens = md.Usage.OutputTokens
			}
		}
	}

	if inputTokens != 200 {
		t.Errorf("InputTokens = %d, want 200", inputTokens)
	}
	if outputTokens != 30 {
		t.Errorf("OutputTokens = %d, want 30", outputTokens)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
