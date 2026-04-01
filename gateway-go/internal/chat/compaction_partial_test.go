package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestSplitForPartialCompaction(t *testing.T) {
	msgs := make([]llm.Message, 10)
	for i := range msgs {
		msgs[i] = makeTextMessage("user", "msg")
	}

	t.Run("from direction", func(t *testing.T) {
		cfg := PartialCompactConfig{Direction: CompactFrom, PivotIndex: 6}
		preserved, toSummarize := SplitForPartialCompaction(msgs, cfg)
		if len(preserved) != 6 {
			t.Errorf("preserved = %d, want 6", len(preserved))
		}
		if len(toSummarize) != 4 {
			t.Errorf("toSummarize = %d, want 4", len(toSummarize))
		}
	})

	t.Run("up_to direction", func(t *testing.T) {
		cfg := PartialCompactConfig{Direction: CompactUpTo, PivotIndex: 3}
		preserved, toSummarize := SplitForPartialCompaction(msgs, cfg)
		if len(toSummarize) != 3 {
			t.Errorf("toSummarize = %d, want 3", len(toSummarize))
		}
		if len(preserved) != 7 {
			t.Errorf("preserved = %d, want 7", len(preserved))
		}
	})

	t.Run("invalid pivot", func(t *testing.T) {
		cfg := PartialCompactConfig{Direction: CompactFrom, PivotIndex: 0}
		preserved, toSummarize := SplitForPartialCompaction(msgs, cfg)
		if len(preserved) != 10 {
			t.Errorf("preserved = %d, want 10", len(preserved))
		}
		if len(toSummarize) != 0 {
			t.Errorf("toSummarize should be nil/empty")
		}
	})
}

func TestReassembleAfterPartialCompaction(t *testing.T) {
	preserved := []llm.Message{
		makeTextMessage("user", "hello"),
		makeTextMessage("assistant", "hi"),
	}

	t.Run("from direction", func(t *testing.T) {
		cfg := PartialCompactConfig{Direction: CompactFrom}
		result := ReassembleAfterPartialCompaction(preserved, "summary of later msgs", cfg)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(result))
		}
		// Last message should be the summary.
		lastContent := string(result[2].Content)
		if !strings.Contains(lastContent, "summary") {
			t.Error("last message should contain the summary")
		}
	})

	t.Run("up_to direction", func(t *testing.T) {
		cfg := PartialCompactConfig{Direction: CompactUpTo}
		result := ReassembleAfterPartialCompaction(preserved, "summary of earlier msgs", cfg)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(result))
		}
		// First message should be the summary.
		firstContent := string(result[0].Content)
		if !strings.Contains(firstContent, "summary") {
			t.Error("first message should contain the summary")
		}
	})

	t.Run("empty summary", func(t *testing.T) {
		cfg := PartialCompactConfig{Direction: CompactFrom}
		result := ReassembleAfterPartialCompaction(preserved, "", cfg)
		if len(result) != 2 {
			t.Errorf("expected 2 messages (preserved only), got %d", len(result))
		}
	})
}

func TestDefaultPartialCompactConfig(t *testing.T) {
	cfg := DefaultPartialCompactConfig(20)
	if cfg.Direction != CompactFrom {
		t.Errorf("direction = %q, want from", cfg.Direction)
	}
	if cfg.PivotIndex != 10 {
		t.Errorf("pivot = %d, want 10", cfg.PivotIndex)
	}

	// Small message count.
	cfg = DefaultPartialCompactConfig(6)
	if cfg.PivotIndex != 4 {
		t.Errorf("pivot = %d, want 4 for small count", cfg.PivotIndex)
	}
}
