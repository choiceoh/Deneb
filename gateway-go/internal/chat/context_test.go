package chat

import (
	"fmt"
	"log/slog"
	"testing"
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
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty string returns minimum", "", 1},
		{"short string returns minimum", "hi", 1},
		{"2 ASCII chars = 1 token", "ab", 1},
		{"4 ASCII chars = 2 tokens", "abcd", 2},
		{"8 ASCII chars = 4 tokens", "abcdefgh", 4},
		{"100 ASCII chars = 50 tokens", string(make([]byte, 100)), 50},
		{"1000 ASCII chars = 500 tokens", string(make([]byte, 1000)), 500},
		// Korean: 3 bytes/rune in UTF-8 — rune count is used, not byte count.
		// "비금도 해상태양광" = 9 runes → 9/2 = 4 tokens (was 27 bytes/4 = 6 with old formula)
		{"Korean 9 runes = 4 tokens", "비금도 해상태양광", 4},
		// 30-rune Korean sentence → 15 tokens (was 78 bytes/4 = 19 with old formula)
		{"Korean 30 runes = 15 tokens", "비금도 해상태양광 프로젝트 현황 보고서를 작성해 주세요", 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.input)
			if got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
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
	for i := 0; i < 5; i++ {
		msg := NewTextChatMessage("user", fmt.Sprintf("message %d", i), int64(i*1000))
		store.Append("test", msg)
	}

	t.Run("returns tail N messages", func(t *testing.T) {
		cfg := DefaultContextConfig()
		cfg.MaxMessages = 3
		result, err := assembleContext(store, "test", cfg, slog.Default())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
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
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Messages) != 5 {
			t.Errorf("got %d messages, want 5", len(result.Messages))
		}
	})

	t.Run("empty session returns empty result", func(t *testing.T) {
		cfg := DefaultContextConfig()
		result, err := assembleContext(store, "nonexistent", cfg, slog.Default())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Messages) != 0 {
			t.Errorf("got %d messages, want 0", len(result.Messages))
		}
	})

	t.Run("zero MaxMessages uses default", func(t *testing.T) {
		cfg := DefaultContextConfig()
		cfg.MaxMessages = 0
		result, err := assembleContext(store, "test", cfg, slog.Default())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
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
