package pilot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestExtractDeltaText(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{
			name:    "standard payload",
			payload: []byte(`{"delta":{"text":"hello"}}`),
			want:    "hello",
		},
		{
			name:    "no text field",
			payload: []byte(`{"delta":{"type":"text_delta"}}`),
			want:    "",
		},
		{
			name:    "empty text",
			payload: []byte(`{"delta":{"text":""}}`),
			want:    "",
		},
		{
			name:    "escape sequence falls back to json unmarshal",
			payload: []byte(`{"delta":{"text":"line1\nline2"}}`),
			want:    "line1\nline2",
		},
		{
			name:    "malformed json",
			payload: []byte(`{not valid json`),
			want:    "",
		},
		{
			name:    "unicode text",
			payload: []byte(`{"delta":{"text":"안녕하세요"}}`),
			want:    "안녕하세요",
		},
		{
			name:    "text with spaces",
			payload: []byte(`{"delta":{"text":" world"}}`),
			want:    " world",
		},
		{
			name:    "empty payload",
			payload: []byte(``),
			want:    "",
		},
		{
			name:    "null payload",
			payload: nil,
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractDeltaText(tt.payload)
			if got != tt.want {
				t.Errorf("ExtractDeltaText(%s) = %q, want %q", tt.payload, got, tt.want)
			}
		})
	}
}

func TestTruncateHead(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxChars int
		want     string
		changed  bool // true if output should differ from input
	}{
		{
			name:     "short string under limit",
			input:    "hello",
			maxChars: 100,
			want:     "hello",
		},
		{
			name:     "exact limit",
			input:    "hello",
			maxChars: 5,
			want:     "hello",
		},
		{
			name:     "long string truncated",
			input:    "hello world, this is a long string",
			maxChars: 10,
			changed:  true,
		},
		{
			name:     "empty string",
			input:    "",
			maxChars: 10,
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateHead(tt.input, tt.maxChars)
			if tt.changed {
				// Truncated output should start with the first maxChars bytes
				// and contain the truncation message.
				prefix := tt.input[:tt.maxChars]
				if got[:tt.maxChars] != prefix {
					t.Errorf("TruncateHead prefix = %q, want %q", got[:tt.maxChars], prefix)
				}
				if len(got) <= tt.maxChars {
					t.Error("TruncateHead should append truncation message")
				}
				if !contains(got, "truncated") {
					t.Errorf("TruncateHead output should contain 'truncated', got %q", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("TruncateHead(%q, %d) = %q, want %q", tt.input, tt.maxChars, got, tt.want)
				}
			}
		})
	}
}


func TestCollectStream_ContentBlockDelta(t *testing.T) {
	ch := make(chan llm.StreamEvent, 3)
	ch <- llm.StreamEvent{
		Type:    "content_block_delta",
		Payload: json.RawMessage(`{"delta":{"text":"hello"}}`),
	}
	ch <- llm.StreamEvent{
		Type:    "content_block_delta",
		Payload: json.RawMessage(`{"delta":{"text":" world"}}`),
	}
	close(ch)

	got, err := CollectStream(context.Background(), ch)
	if err != nil {
		t.Fatalf("CollectStream returned error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("CollectStream = %q, want %q", got, "hello world")
	}
}


func TestCollectStream_ErrorEvent(t *testing.T) {
	ch := make(chan llm.StreamEvent, 2)
	ch <- llm.StreamEvent{
		Type:    "content_block_delta",
		Payload: json.RawMessage(`{"delta":{"text":"partial"}}`),
	}
	ch <- llm.StreamEvent{
		Type:    "error",
		Payload: json.RawMessage(`{"error":{"message":"rate limit exceeded"}}`),
	}
	close(ch)

	got, err := CollectStream(context.Background(), ch)
	if err == nil {
		t.Fatal("CollectStream should return error for error event")
	}
	if !contains(err.Error(), "rate limit exceeded") {
		t.Errorf("error = %q, want to contain 'rate limit exceeded'", err.Error())
	}
	if got != "partial" {
		t.Errorf("CollectStream partial text = %q, want %q", got, "partial")
	}
}

func TestCollectStream_ContextCancelledWithPartial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Buffered channel: one event, then no close — CollectStream will block
	// on the second receive until the context is cancelled.
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.StreamEvent{
		Type:    "content_block_delta",
		Payload: json.RawMessage(`{"delta":{"text":"partial content"}}`),
	}

	// Cancel after the first event has been consumed. We need to cancel
	// before CollectStream blocks on the empty channel.
	go func() {
		// Give CollectStream time to read the first event and block on second.
		// A small spin is fine here because the channel is buffered.
		cancel()
	}()

	got, err := CollectStream(ctx, ch)
	// When context is cancelled with partial content, CollectStream returns
	// the partial content with no error.
	if got != "partial content" {
		t.Errorf("CollectStream = %q, want %q", got, "partial content")
	}
	if err != nil {
		t.Errorf("CollectStream err = %v, want nil (partial content returned)", err)
	}
}


// contains is a test helper for substring check.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
