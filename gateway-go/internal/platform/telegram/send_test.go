package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
)

func TestSendTextHTMLFallbackAllChunks(t *testing.T) {
	// When HTML parse fails on any chunk, all remaining chunks should be sent as plain text.
	var callCount atomic.Int32
	// Track which calls had parse_mode set.
	var htmlCalls atomic.Int32

	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		if pm, ok := req["parse_mode"]; ok && pm == "HTML" {
			htmlCalls.Add(1)
			// Fail with parse error for all HTML requests.
			resp := APIResponse{
				OK:          false,
				ErrorCode:   400,
				Description: "Bad Request: can't parse entities",
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Plain text succeeds.
		resp := APIResponse{
			OK:     true,
			Result: json.RawMessage(`{"message_id":1,"chat":{"id":123,"type":"private"},"text":"ok"}`),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	// Create text that will produce multiple chunks.
	chunk := strings.Repeat("a", TextChunkLimit)
	text := chunk + "\n" + chunk + "\n" + chunk

	results, err := SendText(context.Background(), c, 123, text, SendOptions{
		ParseMode: "HTML",
	})
	if err != nil {
		t.Fatalf("SendText failed: %v", err)
	}
	numChunks := len(results)
	if numChunks < 3 {
		t.Fatalf("expected at least 3 results, got %d", numChunks)
	}

	// Only the first chunk should have tried HTML (then fallen back).
	// All subsequent chunks should go straight to plain text.
	if htmlCalls.Load() != 1 {
		t.Errorf("expected 1 HTML attempt (first chunk only), got %d", htmlCalls.Load())
	}
	// Total calls: 1 HTML fail + N plain text successes = N+1.
	expectedCalls := int32(numChunks + 1)
	if callCount.Load() != expectedCalls {
		t.Errorf("expected %d total API calls, got %d", expectedCalls, callCount.Load())
	}
}

func TestSendTextHTMLFallbackOnlyOnParseError(t *testing.T) {
	// Non-parse API errors should not trigger fallback.
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{
			OK:          false,
			ErrorCode:   403,
			Description: "Forbidden: bot was blocked by the user",
		}
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	_, err := SendText(context.Background(), c, 123, "hello", SendOptions{
		ParseMode: "HTML",
	})
	if err == nil {
		t.Fatal("expected error for forbidden, got nil")
	}
	// Should contain the original error, not a fallback attempt.
	if !strings.Contains(err.Error(), "chunk 0") {
		t.Errorf("expected chunk 0 error, got: %v", err)
	}
}

func TestSendTextNoFallbackWithoutHTML(t *testing.T) {
	// Plain text mode should not attempt fallback even on errors.
	var callCount atomic.Int32
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		resp := APIResponse{
			OK:          false,
			ErrorCode:   400,
			Description: "Bad Request: can't parse entities",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	_, err := SendText(context.Background(), c, 123, "hello", SendOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount.Load() != 1 {
		t.Errorf("expected exactly 1 call (no fallback), got %d", callCount.Load())
	}
}

func TestIsHTMLParseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "parse error",
			err:  &httpretry.APIError{StatusCode: 400, Message: "Bad Request: can't parse entities"},
			want: true,
		},
		{
			name: "non-parse 400",
			err:  &httpretry.APIError{StatusCode: 400, Message: "Bad Request: chat not found"},
			want: false,
		},
		{
			name: "rate limit",
			err:  &httpretry.APIError{StatusCode: 429, Message: "Too Many Requests"},
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHTMLParseError(tt.err); got != tt.want {
				t.Errorf("isHTMLParseError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUploadPooledBuffer(t *testing.T) {
	// Verify Upload works correctly with pooled buffers.
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "multipart/form-data") {
			t.Errorf("expected multipart content type, got %s", ct)
		}
		resp := APIResponse{
			OK:     true,
			Result: json.RawMessage(`{"message_id":1,"chat":{"id":123,"type":"private"}}`),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	data := strings.NewReader("file content here")
	result, err := c.Upload(context.Background(), "sendDocument", "document", "test.txt", data, map[string]string{
		"chat_id": "123",
	})
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
