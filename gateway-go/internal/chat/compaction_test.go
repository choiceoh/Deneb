package chat

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestDefaultCompactionConfig(t *testing.T) {
	cfg := DefaultCompactionConfig()
	if cfg.ContextThreshold != 0.85 {
		t.Errorf("ContextThreshold = %f, want %f", cfg.ContextThreshold, 0.85)
	}
	if cfg.FreshTailCount != defaultFreshTailCount {
		t.Errorf("FreshTailCount = %d, want %d", cfg.FreshTailCount, defaultFreshTailCount)
	}
}

func TestHandleSweepCommandLegacyFetchCandidates(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello world", Timestamp: 1000},
		{Role: "assistant", Content: "hi there", Timestamp: 2000},
	}

	cmd := json.RawMessage(`{"type":"fetchCandidates"}`)
	logger := slog.Default()

	result, err := handleSweepCommandLegacy(cmd, msgs, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["type"] != "candidates" {
		t.Errorf("type = %v, want %q", m["type"], "candidates")
	}

	items, ok := m["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map items, got %T", m["items"])
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	// Check first item fields.
	if items[0]["ordinal"] != 0 {
		t.Errorf("items[0].ordinal = %v, want 0", items[0]["ordinal"])
	}
	if items[0]["role"] != "user" {
		t.Errorf("items[0].role = %v, want %q", items[0]["role"], "user")
	}
	if items[0]["timestamp"] != int64(1000) {
		t.Errorf("items[0].timestamp = %v, want 1000", items[0]["timestamp"])
	}
}

func TestHandleSweepCommandLegacySummarize(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "short message"},
		{Role: "assistant", Content: strings.Repeat("a", 300)}, // long message, should truncate at 200
	}

	cmd := json.RawMessage(`{"type":"summarize","messageIds":[0,1]}`)
	logger := slog.Default()

	result, err := handleSweepCommandLegacy(cmd, msgs, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	if m["type"] != "summary" {
		t.Errorf("type = %v, want %q", m["type"], "summary")
	}

	text, _ := m["text"].(string)

	// Should contain [role] prefix.
	if !strings.Contains(text, "[user]") {
		t.Errorf("expected [user] prefix in summary, got: %s", text)
	}
	if !strings.Contains(text, "[assistant]") {
		t.Errorf("expected [assistant] prefix in summary, got: %s", text)
	}

	// Long message should be truncated.
	if !strings.Contains(text, "...") {
		t.Errorf("expected truncation marker in summary, got: %s", text)
	}
}

func TestHandleSweepCommandLegacySummarizeOutOfRange(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "only message"},
	}

	// Include out-of-range index.
	cmd := json.RawMessage(`{"type":"summarize","messageIds":[0,99]}`)
	logger := slog.Default()

	result, err := handleSweepCommandLegacy(cmd, msgs, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	text, _ := m["text"].(string)

	// Should only include the valid message.
	if !strings.Contains(text, "[user] only message") {
		t.Errorf("expected valid message in summary, got: %s", text)
	}
}

func TestHandleSweepCommandLegacyUnknownType(t *testing.T) {
	cmd := json.RawMessage(`{"type":"unknown"}`)
	logger := slog.Default()

	result, err := handleSweepCommandLegacy(cmd, nil, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	if m["type"] != "empty" {
		t.Errorf("type = %v, want %q", m["type"], "empty")
	}
}
