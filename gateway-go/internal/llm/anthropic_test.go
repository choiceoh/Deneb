package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamChat_Success(t *testing.T) {
	ssePayload := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-6","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != AnthropicAPIVersion {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, ssePayload)
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")
	events, err := c.StreamChat(context.Background(), ChatRequest{
		Model:     "claude-opus-4-6",
		Messages:  []Message{NewTextMessage("user", "Hello")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}

	var collected []StreamEvent
	for ev := range events {
		collected = append(collected, ev)
	}

	if len(collected) != 6 {
		t.Fatalf("expected 6 events, got %d", len(collected))
	}

	expectedTypes := []string{
		"message_start", "content_block_start", "content_block_delta",
		"content_block_stop", "message_delta", "message_stop",
	}
	for i, want := range expectedTypes {
		if collected[i].Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, collected[i].Type, want)
		}
	}
}

func TestStreamChat_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, "bad-key")
	_, err := c.StreamChat(context.Background(), ChatRequest{
		Model:     "claude-opus-4-6",
		Messages:  []Message{NewTextMessage("user", "Hello")},
		MaxTokens: 100,
	})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

func TestStreamChat_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Simulate slow stream.
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Block until client disconnects.
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := NewClient(server.URL, "test-key")
	events, err := c.StreamChat(ctx, ChatRequest{
		Model:     "claude-opus-4-6",
		Messages:  []Message{NewTextMessage("user", "Hello")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}

	// Should receive at least 1 event, then channel closes on timeout.
	count := 0
	for range events {
		count++
	}
	if count < 1 {
		t.Error("expected at least 1 event before cancellation")
	}
}
