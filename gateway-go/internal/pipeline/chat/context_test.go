package chat

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestDefaultContextConfig(t *testing.T) {
	cfg := DefaultContextConfig()
	if cfg.MemoryTokenBudget != defaultMemoryTokenBudget {
		t.Errorf("MemoryTokenBudget = %d, want %d", cfg.MemoryTokenBudget, defaultMemoryTokenBudget)
	}
	if cfg.FreshTailCount != 48 {
		t.Errorf("FreshTailCount = %d, want %d", cfg.FreshTailCount, 48)
	}
	if cfg.MaxMessages != 100 {
		t.Errorf("MaxMessages = %d, want %d", cfg.MaxMessages, 100)
	}
}

func TestEstimateTokens(t *testing.T) {
	// estimateTokens now delegates to tokenest.Estimate (script-aware).
	// Verify basic properties rather than exact values, since the
	// estimation engine uses per-script calibration constants.
	t.Run("empty returns zero", func(t *testing.T) {
		if got := estimateTokens(""); got != 0 {
			t.Errorf("estimateTokens(\"\") = %d, want 0", got)
		}
	})
	t.Run("single char returns at least 1", func(t *testing.T) {
		if got := estimateTokens("a"); got < 1 {
			t.Errorf("estimateTokens(\"a\") = %d, want >= 1", got)
		}
	})
	t.Run("longer text returns more tokens", func(t *testing.T) {
		short := estimateTokens("hello")
		long := estimateTokens("hello world this is a longer sentence")
		if long <= short {
			t.Errorf("longer text (%d) should produce more tokens than shorter (%d)", long, short)
		}
	})
	t.Run("Korean text produces reasonable estimate", func(t *testing.T) {
		got := estimateTokens("비금도 해상태양광 프로젝트 현황 보고서를 작성해 주세요")
		// 27 Hangul + 5 spaces → should be in reasonable range.
		if got < 10 || got > 40 {
			t.Errorf("Korean estimate = %d, want 10-40", got)
		}
	})
}

func TestTranscriptToMessages(t *testing.T) {
	t.Run("converts roles and content", func(t *testing.T) {
		transcript := []ChatMessage{
			NewTextChatMessage("user", "hello", 0),
			NewTextChatMessage("assistant", "hi there", 0),
		}
		msgs := transcriptToMessages(transcript)
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		if msgs[0].Role != "user" {
			t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, "user")
		}
		if msgs[1].Role != "assistant" {
			t.Errorf("msgs[1].Role = %q, want %q", msgs[1].Role, "assistant")
		}
	})

	t.Run("empty role defaults to user", func(t *testing.T) {
		transcript := []ChatMessage{
			NewTextChatMessage("", "no role", 0),
		}
		msgs := transcriptToMessages(transcript)
		if msgs[0].Role != "user" {
			t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, "user")
		}
	})

	t.Run("empty transcript", func(t *testing.T) {
		msgs := transcriptToMessages(nil)
		if len(msgs) != 0 {
			t.Errorf("got %d messages, want 0", len(msgs))
		}
	})
}

func TestAssembleContextFallback(t *testing.T) {
	store := newMemTranscriptStore()

	// Populate with messages.
	for i := range 5 {
		msg := NewTextChatMessage("user", fmt.Sprintf("message %d", i), int64(i*1000))
		store.Append("test", msg)
	}

	t.Run("returns tail N messages", func(t *testing.T) {
		cfg := DefaultContextConfig()
		cfg.MaxMessages = 3
		result, err := assembleContext(store, "test", cfg, slog.Default())
		testutil.NoError(t, err)
		if len(result.Messages) != 3 {
			t.Errorf("got %d messages, want 3", len(result.Messages))
		}
		if result.TotalMessages != 5 {
			t.Errorf("TotalMessages = %d, want 5", result.TotalMessages)
		}
	})

	t.Run("returns all when MaxMessages is larger", func(t *testing.T) {
		cfg := DefaultContextConfig()
		cfg.MaxMessages = 100
		result, err := assembleContext(store, "test", cfg, slog.Default())
		testutil.NoError(t, err)
		if len(result.Messages) != 5 {
			t.Errorf("got %d messages, want 5", len(result.Messages))
		}
	})

	t.Run("empty session returns empty result", func(t *testing.T) {
		cfg := DefaultContextConfig()
		result, err := assembleContext(store, "nonexistent", cfg, slog.Default())
		testutil.NoError(t, err)
		if len(result.Messages) != 0 {
			t.Errorf("got %d messages, want 0", len(result.Messages))
		}
	})

	t.Run("zero MaxMessages uses default", func(t *testing.T) {
		cfg := DefaultContextConfig()
		cfg.MaxMessages = 0
		result, err := assembleContext(store, "test", cfg, slog.Default())
		testutil.NoError(t, err)
		// defaultMaxMessages = 100, so all 5 messages should be returned.
		if len(result.Messages) != 5 {
			t.Errorf("got %d messages, want 5", len(result.Messages))
		}
	})
}

// memTranscriptStore is a minimal in-memory TranscriptStore for testing.
type memTranscriptStore struct {
	data map[string][]ChatMessage
}

func newMemTranscriptStore() *memTranscriptStore {
	return &memTranscriptStore{data: make(map[string][]ChatMessage)}
}

func (s *memTranscriptStore) Load(key string, limit int) ([]ChatMessage, int, error) {
	msgs := s.data[key]
	total := len(msgs)
	if limit > 0 && limit < total {
		msgs = msgs[total-limit:]
	}
	return msgs, total, nil
}

func (s *memTranscriptStore) Append(key string, msg ChatMessage) error {
	s.data[key] = append(s.data[key], msg)
	return nil
}

func (s *memTranscriptStore) Delete(key string) error {
	delete(s.data, key)
	return nil
}

func (s *memTranscriptStore) ListKeys() ([]string, error) {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *memTranscriptStore) Search(_ string, _ int) ([]SearchResult, error) {
	return nil, nil
}

func (s *memTranscriptStore) CloneRecent(_, _ string, _ int) error {
	return nil
}
