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
// some OpenAI-compatible providers (Z.AI, local AI, vLLM).
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

// TestStreamChat_MultipleToolCalls verifies that multiple streamed tool calls
// get correct block indices even when tool call fragments arrive interleaved.
func TestStreamChat_MultipleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Chunk 1: first tool call starts (index 0).
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-3", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index": 0, "id": "call_1", "type": "function",
						"function": map[string]string{"name": "read", "arguments": `{"pa`},
					}},
				},
			}},
		}))
		flusher.Flush()

		// Chunk 2: second tool call starts (index 1).
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-3", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index": 1, "id": "call_2", "type": "function",
						"function": map[string]string{"name": "grep", "arguments": `{"pat`},
					}},
				},
			}},
		}))
		flusher.Flush()

		// Chunk 3: more args for tool 0 (interleaved).
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-3", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index":    0,
						"function": map[string]string{"arguments": `th":"f.go"}`},
					}},
				},
			}},
		}))
		flusher.Flush()

		// Chunk 4: more args for tool 1.
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-3", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index":    1,
						"function": map[string]string{"arguments": `tern":"foo"}`},
					}},
				},
			}},
		}))
		flusher.Flush()

		// Chunk 5: finish.
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-3", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0, "delta": map[string]string{}, "finish_reason": "tool_calls",
			}},
		}))
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "read and search")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}

	// Track block starts and deltas by index.
	type blockInfo struct {
		startType string
		startName string
		deltas    []int // block indices from deltas
	}
	blocks := map[int]*blockInfo{}
	stopIndices := []int{}

	for ev := range events {
		switch ev.Type {
		case "content_block_start":
			var cbs ContentBlockStart
			if json.Unmarshal(ev.Payload, &cbs) == nil {
				blocks[cbs.Index] = &blockInfo{
					startType: cbs.ContentBlock.Type,
					startName: cbs.ContentBlock.Name,
				}
			}
		case "content_block_delta":
			var cbd ContentBlockDelta
			if json.Unmarshal(ev.Payload, &cbd) == nil {
				if b, ok := blocks[cbd.Index]; ok {
					b.deltas = append(b.deltas, cbd.Index)
				}
			}
		case "content_block_stop":
			var cbe ContentBlockStop
			if json.Unmarshal(ev.Payload, &cbe) == nil {
				stopIndices = append(stopIndices, cbe.Index)
			}
		}
	}

	// Tool 0 should be at block index 0, tool 1 at block index 1.
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if b := blocks[0]; b == nil || b.startName != "read" {
		t.Errorf("block 0: expected name=read, got %+v", b)
	}
	if b := blocks[1]; b == nil || b.startName != "grep" {
		t.Errorf("block 1: expected name=grep, got %+v", b)
	}

	// Verify deltas went to correct blocks.
	for idx, b := range blocks {
		for _, di := range b.deltas {
			if di != idx {
				t.Errorf("block %d received delta with index %d", idx, di)
			}
		}
	}

	// Both blocks should be stopped.
	if len(stopIndices) != 2 {
		t.Errorf("expected 2 stop events, got %d", len(stopIndices))
	}
}

// TestStreamChat_UnparseableContentSkipped verifies that messages with content
// that can't be parsed as string or blocks are skipped with a warning (not panic).
func TestStreamChat_UnparseableContentSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-4", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]string{"role": "assistant", "content": "ok"},
			}},
		}))
		flusher.Flush()

		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-4", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0, "delta": map[string]string{}, "finish_reason": "stop",
			}},
		}))
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	// Send a message with content that's neither string nor blocks (number).
	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model: "test-model",
		Messages: []Message{
			NewTextMessage("user", "hello"),
			{Role: "assistant", Content: json.RawMessage(`12345`)}, // unparseable
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}

	// Should not panic; stream should complete normally.
	var gotText bool
	for ev := range events {
		if ev.Type == "content_block_delta" {
			gotText = true
		}
	}
	if !gotText {
		t.Error("expected at least one content delta")
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
