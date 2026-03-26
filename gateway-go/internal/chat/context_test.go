package chat

import (
	"encoding/json"
	"testing"
)

func TestDefaultContextConfig(t *testing.T) {
	cfg := DefaultContextConfig()
	if cfg.TokenBudget != 100_000 {
		t.Errorf("TokenBudget = %d, want %d", cfg.TokenBudget, 100_000)
	}
	if cfg.FreshTailCount != 32 {
		t.Errorf("FreshTailCount = %d, want %d", cfg.FreshTailCount, 32)
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
		{"4 chars = 1 token", "abcd", 1},
		{"8 chars = 2 tokens", "abcdefgh", 2},
		{"100 chars = 25 tokens", string(make([]byte, 100)), 25},
		{"1000 chars = 250 tokens", string(make([]byte, 1000)), 250},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.input)
			if got != tt.want {
				t.Errorf("estimateTokens(%d chars) = %d, want %d", len(tt.input), got, tt.want)
			}
		})
	}
}

func TestSelectMessagesByIDs(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "msg0"},
		{Role: "assistant", Content: "msg1"},
		{Role: "user", Content: "msg2"},
		{Role: "assistant", Content: "msg3"},
	}

	t.Run("empty IDs returns all", func(t *testing.T) {
		got := selectMessagesByIDs(msgs, nil)
		if len(got) != len(msgs) {
			t.Errorf("got %d messages, want %d", len(got), len(msgs))
		}
	})

	t.Run("empty slice returns all", func(t *testing.T) {
		got := selectMessagesByIDs(msgs, []string{})
		if len(got) != len(msgs) {
			t.Errorf("got %d messages, want %d", len(got), len(msgs))
		}
	})

	t.Run("valid IDs filter correctly", func(t *testing.T) {
		got := selectMessagesByIDs(msgs, []string{"msg_0", "msg_2"})
		if len(got) != 2 {
			t.Fatalf("got %d messages, want 2", len(got))
		}
		if got[0].Content != "msg0" {
			t.Errorf("got[0].Content = %q, want %q", got[0].Content, "msg0")
		}
		if got[1].Content != "msg2" {
			t.Errorf("got[1].Content = %q, want %q", got[1].Content, "msg2")
		}
	})

	t.Run("invalid IDs returns all messages", func(t *testing.T) {
		got := selectMessagesByIDs(msgs, []string{"invalid", "bad_id"})
		if len(got) != len(msgs) {
			t.Errorf("got %d messages, want %d (all)", len(got), len(msgs))
		}
	})

	t.Run("out of range IDs skipped", func(t *testing.T) {
		got := selectMessagesByIDs(msgs, []string{"msg_0", "msg_99"})
		if len(got) != 1 {
			t.Fatalf("got %d messages, want 1", len(got))
		}
		if got[0].Content != "msg0" {
			t.Errorf("got[0].Content = %q, want %q", got[0].Content, "msg0")
		}
	})
}

func TestTranscriptToMessages(t *testing.T) {
	t.Run("converts roles and content", func(t *testing.T) {
		transcript := []ChatMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
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
			{Role: "", Content: "no role"},
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

func TestHandleAssemblyCommand(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "hi"},
	}

	t.Run("fetchContextItems", func(t *testing.T) {
		cmd := `{"type":"fetchContextItems"}`
		result, err := handleAssemblyCommand(json.RawMessage(cmd), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}
		if m["type"] != "contextItems" {
			t.Errorf("type = %v, want %q", m["type"], "contextItems")
		}
		items, ok := m["items"].([]map[string]any)
		if !ok {
			t.Fatalf("expected []map[string]any items, got %T", m["items"])
		}
		if len(items) != 2 {
			t.Fatalf("got %d items, want 2", len(items))
		}
		// Verify first item structure.
		if items[0]["ordinal"] != 0 {
			t.Errorf("items[0].ordinal = %v, want 0", items[0]["ordinal"])
		}
		if items[0]["itemType"] != "message" {
			t.Errorf("items[0].itemType = %v, want %q", items[0]["itemType"], "message")
		}
	})

	t.Run("unknown type returns empty", func(t *testing.T) {
		cmd := `{"type":"unknown"}`
		result, err := handleAssemblyCommand(json.RawMessage(cmd), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := result.(map[string]any)
		if m["type"] != "empty" {
			t.Errorf("type = %v, want %q", m["type"], "empty")
		}
	})
}
