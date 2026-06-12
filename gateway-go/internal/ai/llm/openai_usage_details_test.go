package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestSplitPromptTokens verifies the Anthropic-semantics normalization of the
// OpenAI prompt-token count: input excludes cache reads so downstream sums
// (input + cacheRead) never double-count cache hits.
func TestSplitPromptTokens(t *testing.T) {
	cases := []struct {
		name      string
		usage     openAIUsage
		wantIn    int
		wantCache int
	}{
		{
			name:   "no details (cold or unsupported server)",
			usage:  openAIUsage{PromptTokens: 200},
			wantIn: 200, wantCache: 0,
		},
		{
			name: "vLLM/OpenAI prompt_tokens_details.cached_tokens",
			usage: openAIUsage{
				PromptTokens:        200,
				PromptTokensDetails: &promptTokensDetails{CachedTokens: 120},
			},
			wantIn: 80, wantCache: 120,
		},
		{
			name: "DeepSeek prompt_cache_hit_tokens spelling",
			usage: openAIUsage{
				PromptTokens:         200,
				PromptCacheHitTokens: 150,
			},
			wantIn: 50, wantCache: 150,
		},
		{
			name: "cached exceeding prompt is clamped",
			usage: openAIUsage{
				PromptTokens:        100,
				PromptTokensDetails: &promptTokensDetails{CachedTokens: 999},
			},
			wantIn: 0, wantCache: 100,
		},
		{
			name: "details present but zero (vLLM cold request)",
			usage: openAIUsage{
				PromptTokens:        100,
				PromptTokensDetails: &promptTokensDetails{CachedTokens: 0},
			},
			wantIn: 100, wantCache: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, cache := c.usage.splitPromptTokens()
			if in != c.wantIn || cache != c.wantCache {
				t.Errorf("splitPromptTokens() = (%d, %d), want (%d, %d)",
					in, cache, c.wantIn, c.wantCache)
			}
		})
	}
}

// TestStreamChat_CachedTokensOnUsageChunk verifies end-to-end that a vLLM-style
// usage chunk carrying prompt_tokens_details.cached_tokens surfaces as
// cache_read_input_tokens on the translated message_start — the field every
// downstream consumer (usage tracker, observe, modeltuner) reads. Before this,
// the OpenAI path reported 0 cache reads forever.
func TestStreamChat_CachedTokensOnUsageChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":    "chatcmpl-3",
			"model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{"role": "assistant", "content": "Hi"}},
			},
		}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":    "chatcmpl-3",
			"model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"},
			},
		}))
		// Usage-only chunk: 200 total prompt tokens, 120 served from the
		// prefix cache.
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":      "chatcmpl-3",
			"model":   "test-model",
			"choices": []map[string]any{},
			"usage": map[string]any{
				"prompt_tokens":         200,
				"completion_tokens":     30,
				"total_tokens":          230,
				"prompt_tokens_details": map[string]int{"cached_tokens": 120},
			},
		}))
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
	testutil.NoError(t, err)

	var inputTokens, cacheRead int
	for ev := range events {
		if ev.Type != "message_start" {
			continue
		}
		var ms MessageStart
		if json.Unmarshal(ev.Payload, &ms) == nil {
			// Last message_start wins (same as consumeStream).
			inputTokens = ms.Message.Usage.InputTokens
			cacheRead = ms.Message.Usage.CacheReadInputTokens
		}
	}

	if inputTokens != 80 {
		t.Errorf("InputTokens = %d, want 80 (200 prompt - 120 cached)", inputTokens)
	}
	if cacheRead != 120 {
		t.Errorf("CacheReadInputTokens = %d, want 120", cacheRead)
	}
}
