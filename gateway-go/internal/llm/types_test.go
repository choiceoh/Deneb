package llm

import (
	"encoding/json"
	"testing"
)

func TestSystemString(t *testing.T) {
	t.Run("non-empty string", func(t *testing.T) {
		raw := SystemString("hello world")
		if raw == nil {
			t.Fatal("expected non-nil")
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if s != "hello world" {
			t.Errorf("got %q, want %q", s, "hello world")
		}
	})

	t.Run("empty string returns nil", func(t *testing.T) {
		raw := SystemString("")
		if raw != nil {
			t.Errorf("expected nil, got %s", raw)
		}
	})
}

func TestSystemBlocks(t *testing.T) {
	t.Run("non-empty blocks", func(t *testing.T) {
		blocks := []ContentBlock{
			{Type: "text", Text: "hello"},
			{Type: "text", Text: "world"},
		}
		raw := SystemBlocks(blocks)
		if raw == nil {
			t.Fatal("expected non-nil")
		}
		var parsed []ContentBlock
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(parsed) != 2 {
			t.Fatalf("got %d blocks, want 2", len(parsed))
		}
		if parsed[0].Text != "hello" {
			t.Errorf("parsed[0].Text = %q, want %q", parsed[0].Text, "hello")
		}
	})

	t.Run("empty blocks returns nil", func(t *testing.T) {
		raw := SystemBlocks(nil)
		if raw != nil {
			t.Errorf("expected nil, got %s", raw)
		}
	})

	t.Run("empty slice returns nil", func(t *testing.T) {
		raw := SystemBlocks([]ContentBlock{})
		if raw != nil {
			t.Errorf("expected nil, got %s", raw)
		}
	})
}

func TestExtractSystemText(t *testing.T) {
	t.Run("plain string", func(t *testing.T) {
		raw := SystemString("system prompt")
		got := ExtractSystemText(raw)
		if got != "system prompt" {
			t.Errorf("got %q, want %q", got, "system prompt")
		}
	})

	t.Run("block array", func(t *testing.T) {
		raw := SystemBlocks([]ContentBlock{
			{Type: "text", Text: "part1"},
			{Type: "text", Text: "part2"},
		})
		got := ExtractSystemText(raw)
		if got != "part1part2" {
			t.Errorf("got %q, want %q", got, "part1part2")
		}
	})

	t.Run("block array skips non-text", func(t *testing.T) {
		raw := SystemBlocks([]ContentBlock{
			{Type: "text", Text: "text part"},
			{Type: "image"},
		})
		got := ExtractSystemText(raw)
		if got != "text part" {
			t.Errorf("got %q, want %q", got, "text part")
		}
	})

	t.Run("nil returns empty", func(t *testing.T) {
		got := ExtractSystemText(nil)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("empty returns empty", func(t *testing.T) {
		got := ExtractSystemText(json.RawMessage{})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage("user", "hello")
	if msg.Role != "user" {
		t.Errorf("role = %q, want %q", msg.Role, "user")
	}
	var text string
	if err := json.Unmarshal(msg.Content, &text); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if text != "hello" {
		t.Errorf("content = %q, want %q", text, "hello")
	}
}

func TestNewBlockMessage(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "abc"}},
	}
	msg := NewBlockMessage("user", blocks)
	if msg.Role != "user" {
		t.Errorf("role = %q, want %q", msg.Role, "user")
	}
	var parsed []ContentBlock
	if err := json.Unmarshal(msg.Content, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("got %d blocks, want 2", len(parsed))
	}
	if parsed[0].Type != "text" || parsed[0].Text != "hello" {
		t.Errorf("parsed[0] = %+v", parsed[0])
	}
	if parsed[1].Type != "image" || parsed[1].Source == nil {
		t.Errorf("parsed[1] = %+v", parsed[1])
	}
}
