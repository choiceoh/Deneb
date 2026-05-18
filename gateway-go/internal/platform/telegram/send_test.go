package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
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
	testutil.NoError(t, err)
	numChunks := len(results)
	if numChunks < 3 {
		t.Fatalf("got %d, want at least 3 results", numChunks)
	}

	// Only the first chunk should have tried HTML (then fallen back).
	// All subsequent chunks should go straight to plain text.
	if htmlCalls.Load() != 1 {
		t.Errorf("got %d, want 1 HTML attempt (first chunk only)", htmlCalls.Load())
	}
	// Total calls: 1 HTML fail + N plain text successes = N+1.
	expectedCalls := int32(numChunks + 1)
	if callCount.Load() != expectedCalls {
		t.Errorf("got %d, want %d total API calls", callCount.Load(), expectedCalls)
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
		t.Fatal("got nil, want error for forbidden")
	}
	// Should contain the original error, not a fallback attempt.
	if !strings.Contains(err.Error(), "chunks failed") || !strings.Contains(err.Error(), "[0]") {
		t.Errorf("expected chunks-failed error with index 0, got: %v", err)
	}
}

// TestSendHTMLChunks_KeyboardOnLastChunk verifies that when a reply is split
// across multiple sendMessage calls, the inline keyboard attaches to the
// final chunk only. This is what the draft-edit-then-append flow relies on
// to keep buttons at the end of the visible reply.
func TestSendHTMLChunks_KeyboardOnLastChunk(t *testing.T) {
	type captured struct {
		text      string
		hasMarkup bool
		hasHTMLPM bool
	}
	var calls []captured

	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		txt, _ := req["text"].(string)
		_, hasMarkup := req["reply_markup"]
		pm, _ := req["parse_mode"].(string)
		calls = append(calls, captured{text: txt, hasMarkup: hasMarkup, hasHTMLPM: pm == "HTML"})

		resp := APIResponse{
			OK:     true,
			Result: json.RawMessage(`{"message_id":1,"chat":{"id":123,"type":"private"}}`),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	chunks := []string{"first", "middle", "last"}
	kb := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Go", CallbackData: "go"}}},
	}

	_, err := SendHTMLChunks(context.Background(), c, 123, chunks, kb)
	if err != nil {
		t.Fatalf("SendHTMLChunks returned error: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d sendMessage calls, want 3", len(calls))
	}
	for i, call := range calls {
		wantMarkup := i == len(calls)-1
		if call.hasMarkup != wantMarkup {
			t.Errorf("chunk %d: hasMarkup=%v, want %v", i, call.hasMarkup, wantMarkup)
		}
		if !call.hasHTMLPM {
			t.Errorf("chunk %d: expected parse_mode=HTML, got plain", i)
		}
	}
	if calls[0].text != "first" || calls[2].text != "last" {
		t.Errorf("chunk order/text wrong: %+v", calls)
	}
}

// TestSendHTMLChunks_HTMLFallbackPropagates verifies that when the first
// chunk hits an HTML parse error, the retry path fires AND subsequent chunks
// also drop parse_mode — matching SendText's contract.
func TestSendHTMLChunks_HTMLFallbackPropagates(t *testing.T) {
	var htmlAttempts, plainAttempts atomic.Int32

	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if pm, _ := req["parse_mode"].(string); pm == "HTML" {
			htmlAttempts.Add(1)
			resp := APIResponse{
				OK:          false,
				ErrorCode:   400,
				Description: "Bad Request: can't parse entities",
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(resp)
			return
		}
		plainAttempts.Add(1)
		resp := APIResponse{
			OK:     true,
			Result: json.RawMessage(`{"message_id":1,"chat":{"id":123,"type":"private"}}`),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	chunks := []string{"<bad>a</bad>", "b", "c"}
	_, err := SendHTMLChunks(context.Background(), c, 123, chunks, nil)
	if err != nil {
		t.Fatalf("SendHTMLChunks returned error after fallback: %v", err)
	}
	// First chunk: one HTML attempt + one plain retry. Remaining chunks: plain only.
	if htmlAttempts.Load() != 1 {
		t.Errorf("htmlAttempts=%d, want 1 (only first chunk retries)", htmlAttempts.Load())
	}
	if plainAttempts.Load() != 3 {
		t.Errorf("plainAttempts=%d, want 3 (fallback + 2 remaining)", plainAttempts.Load())
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
