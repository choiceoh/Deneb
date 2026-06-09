package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// streamChunks runs a fixed list of OpenAI SSE chunks through StreamChat and
// returns the assembled content blocks as the single-active-block consumer
// (executor.consumeStreamInto) would see them.
func streamChunks(t *testing.T, userMsg string, chunks []map[string]any) []ContentBlock {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(c))
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", userMsg)},
		MaxTokens: 100,
	})
	testutil.NoError(t, err)
	return assembleSingleBlock(events)
}

// TestStreamChat_ContentBeforeReasoning_AssembleViaSingleBlock guards the
// thinking-block index collision. Some OpenAI-compatible providers emit a
// content token before the reasoning stream. The translator used to open the
// thinking block at a hardcoded index 0 — colliding with the text block already
// at 0 — and never closed that text block, so the single-active-block consumer
// discarded the earlier text and misrouted later deltas. The fix gives the
// thinking block its own index and closes the open text block first.
func TestStreamChat_ContentBeforeReasoning_AssembleViaSingleBlock(t *testing.T) {
	chunks := []map[string]any{
		// Content arrives FIRST, before any reasoning.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"role": "assistant", "content": "Answer A. "}}}},
		// Reasoning arrives after content.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"reasoning": "thinking it through"}}}},
		// More content after reasoning.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"content": "Answer B."}}}},
		// Finish.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]string{}, "finish_reason": "stop"}}},
	}

	var text, thinking string
	for _, b := range streamChunks(t, "hi", chunks) {
		switch b.Type {
		case "text":
			text += b.Text
		case "thinking":
			thinking += b.Thinking
		}
	}

	// Before the fix the first text ("Answer A. ") was dropped when thinking
	// opened over the text block at the shared index 0.
	if text != "Answer A. Answer B." {
		t.Errorf("assembled text = %q, want %q (a text block was dropped/misrouted)",
			text, "Answer A. Answer B.")
	}
	if thinking != "thinking it through" {
		t.Errorf("assembled thinking = %q, want %q", thinking, "thinking it through")
	}
}

// TestStreamChat_RefusalSurfacedAsText guards the dropped-refusal bug. OpenAI
// streams a model refusal on delta.refusal with content null. delta.refusal was
// defined on the wire type but never read by the stream translator, so a refusal
// produced an empty reply (a silent no-reply). The fix surfaces refusal text the
// same way as content.
func TestStreamChat_RefusalSurfacedAsText(t *testing.T) {
	chunks := []map[string]any{
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"role": "assistant", "refusal": "I can't help "}}}},
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"refusal": "with that."}}}},
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]string{}, "finish_reason": "stop"}}},
	}

	var text string
	for _, b := range streamChunks(t, "do something disallowed", chunks) {
		if b.Type == "text" {
			text += b.Text
		}
	}

	if text != "I can't help with that." {
		t.Errorf("assembled text = %q, want the refusal surfaced as text (empty = silent no-reply)", text)
	}
}
