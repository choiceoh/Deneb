package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCompressor_ShouldCompact(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)
	c := NewCompressor(CompactionConfig{
		FreshTailCount:         3,
		MaxUncompactedMessages: 5,
	}, nil)

	key := "test-session"
	w.EnsureSession(key, SessionHeader{ID: key, Version: 1})

	// Add 3 messages (below threshold).
	for i := 0; i < 3; i++ {
		msg, _ := json.Marshal(map[string]any{"role": "user", "content": "hello"})
		w.AppendMessage(key, msg)
	}

	should, _ := c.ShouldCompact(key, w)
	if should {
		t.Error("should not compact with 3 messages (threshold 5)")
	}

	// Add more to exceed threshold.
	for i := 0; i < 4; i++ {
		msg, _ := json.Marshal(map[string]any{"role": "assistant", "content": "reply"})
		w.AppendMessage(key, msg)
	}

	should, reason := c.ShouldCompact(key, w)
	if !should {
		t.Error("should compact with 7 messages (threshold 5)")
	}
	if reason == "" {
		t.Error("expected a reason string")
	}
}

func TestCompressor_Compact(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)
	c := NewCompressor(CompactionConfig{
		FreshTailCount:         2,
		MaxUncompactedMessages: 4,
	}, nil)

	key := "compact-test"
	w.EnsureSession(key, SessionHeader{ID: key, Version: 1})

	// Add 6 messages.
	for i := 0; i < 6; i++ {
		msg, _ := json.Marshal(map[string]any{
			"role":    "user",
			"content": "message " + string(rune('A'+i)),
		})
		w.AppendMessage(key, msg)
	}

	result, err := c.Compact(key, w)
	if err != nil {
		t.Fatalf("compact error: %v", err)
	}
	if !result.OK || !result.Compacted {
		t.Error("expected successful compaction")
	}
	if result.OriginalMessages != 6 {
		t.Errorf("expected 6 original, got %d", result.OriginalMessages)
	}
	if result.RetainedMessages != 2 {
		t.Errorf("expected 2 retained, got %d", result.RetainedMessages)
	}

	// Verify file structure: header + summary + 2 retained = 4 lines.
	path := filepath.Join(dir, key+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read compacted: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 4 {
		t.Errorf("expected 4 lines (header+summary+2 retained), got %d", lines)
	}
}

func TestCompressor_CompactBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)
	c := NewCompressor(CompactionConfig{FreshTailCount: 10}, nil)

	key := "small-session"
	w.EnsureSession(key, SessionHeader{ID: key, Version: 1})
	msg, _ := json.Marshal(map[string]any{"role": "user", "content": "hi"})
	w.AppendMessage(key, msg)

	result, err := c.Compact(key, w)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Compacted {
		t.Error("should not compact when below fresh tail count")
	}
}
