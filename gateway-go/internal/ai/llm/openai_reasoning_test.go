package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestReasoningText_AcceptsBothFields guards the streaming reasoning field
// mismatch: deneb must read reasoning from both "reasoning_content"
// (DeepSeek/OpenRouter) and "reasoning" (vLLM/step3p7).
func TestReasoningText_AcceptsBothFields(t *testing.T) {
	cases := []struct {
		name string
		d    openAIDelta
		want string
	}{
		{"reasoning_content (deepseek/openrouter)", openAIDelta{ReasoningContent: "rc"}, "rc"},
		{"reasoning (vllm/step3p7)", openAIDelta{Reasoning: "r"}, "r"},
		{"prefer reasoning_content when both set", openAIDelta{ReasoningContent: "rc", Reasoning: "r"}, "rc"},
		{"neither", openAIDelta{}, ""},
	}
	for _, c := range cases {
		if got := c.d.reasoningText(); got != c.want {
			t.Errorf("%s: reasoningText() = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestStreamChat_VLLMReasoningEmitsThinking is the end-to-end guard: a vLLM-style
// stream that carries reasoning under "reasoning" (not "reasoning_content") must
// still surface as thinking_delta events. Before the fix deneb read only
// reasoning_content, so step3p7's reasoning was silently dropped (the user's
// "reasoning isn't showing / parsing" symptom). Complements #1810, which fixed
// the separate max_tokens=0 rejection.
func TestStreamChat_VLLMReasoningEmitsThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// vLLM/step3p7 streams reasoning under "reasoning" while content is null.
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "c1", "model": "step3p7",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "reasoning": "Let me think. "}}},
		}))
		flusher.Flush()
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "c1", "model": "step3p7",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"reasoning": "2+2=4."}}},
		}))
		flusher.Flush()
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "c1", "model": "step3p7",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "4"}, "finish_reason": "stop"}},
		}))
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "step3p7",
		Messages:  []Message{NewTextMessage("user", "2+2?")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var thinking, text string
	for ev := range events {
		if ev.Type != "content_block_delta" {
			continue
		}
		var cbd ContentBlockDelta
		if json.Unmarshal(ev.Payload, &cbd) != nil {
			continue
		}
		if cbd.Delta.Type == "thinking_delta" {
			thinking += cbd.Delta.Text
		} else {
			text += cbd.Delta.Text
		}
	}
	if thinking != "Let me think. 2+2=4." {
		t.Errorf("thinking = %q, want %q (vLLM 'reasoning' field was dropped)", thinking, "Let me think. 2+2=4.")
	}
	if text != "4" {
		t.Errorf("text = %q, want %q", text, "4")
	}
}
