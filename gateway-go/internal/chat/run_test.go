package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestParseModelID(t *testing.T) {
	tests := []struct {
		input      string
		wantProv   string
		wantModel  string
	}{
		{"zai/glm-5.1", "zai", "glm-5.1"},
		{"anthropic/claude-3-opus", "anthropic", "claude-3-opus"},
		{"gpt-4", "", "gpt-4"},
		{"claude-3-sonnet", "", "claude-3-sonnet"},
		{"openai/gpt-4o-mini", "openai", "gpt-4o-mini"},
		{"", "", ""},
		{"a/b/c", "a", "b/c"}, // only first slash is split
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			prov, model := parseModelID(tt.input)
			if prov != tt.wantProv {
				t.Errorf("provider = %q, want %q", prov, tt.wantProv)
			}
			if model != tt.wantModel {
				t.Errorf("model = %q, want %q", model, tt.wantModel)
			}
		})
	}
}

func TestInferAPIType(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"anthropic", "anthropic"},
		{"openai", "openai"},
		{"zai", "openai"},
		{"deepseek", "openai"},
		{"sglang", "openai"},
		{"", "openai"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := inferAPIType(tt.provider)
			if got != tt.want {
				t.Errorf("inferAPIType(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestIsContextOverflow(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"context_length_exceeded", errors.New("context_length_exceeded"), true},
		{"context_too_long", errors.New("error: context_too_long for model"), true},
		{"prompt is too long", errors.New("prompt is too long"), true},
		{"maximum context length", errors.New("maximum context length exceeded"), true},
		{"unrelated error", errors.New("network timeout"), false},
		{"wrapped overflow", errors.New("api error: context_length_exceeded (400)"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isContextOverflow(tt.err)
			if got != tt.want {
				t.Errorf("isContextOverflow(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestDeliveryChannel(t *testing.T) {
	t.Run("nil returns empty", func(t *testing.T) {
		if got := deliveryChannel(nil); got != "" {
			t.Errorf("deliveryChannel(nil) = %q, want empty", got)
		}
	})

	t.Run("returns channel", func(t *testing.T) {
		d := &DeliveryContext{Channel: "telegram"}
		if got := deliveryChannel(d); got != "telegram" {
			t.Errorf("deliveryChannel = %q, want %q", got, "telegram")
		}
	})
}

func TestStopReasonFromCtx(t *testing.T) {
	t.Run("deadline exceeded returns timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()
		// Force the context to expire.
		<-ctx.Done()
		got := stopReasonFromCtx(ctx)
		if got != "timeout" {
			t.Errorf("stopReasonFromCtx = %q, want %q", got, "timeout")
		}
	})

	t.Run("canceled returns aborted", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got := stopReasonFromCtx(ctx)
		if got != "aborted" {
			t.Errorf("stopReasonFromCtx = %q, want %q", got, "aborted")
		}
	})
}

func TestResolveDefaultBaseURL(t *testing.T) {
	tests := []struct {
		provider string
		wantURL  string
	}{
		{"anthropic", llm.DefaultAnthropicBaseURL},
		{"zai", defaultZaiBaseURL},
		{"sglang", defaultSglangBaseURL},
		{"openai", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := resolveDefaultBaseURL(tt.provider)
			if got != tt.wantURL {
				t.Errorf("resolveDefaultBaseURL(%q) = %q, want %q", tt.provider, got, tt.wantURL)
			}
		})
	}
}

func TestBuildAttachmentBlocks(t *testing.T) {
	t.Run("text only", func(t *testing.T) {
		blocks := buildAttachmentBlocks("hello", nil)
		if len(blocks) != 1 {
			t.Fatalf("got %d blocks, want 1", len(blocks))
		}
		if blocks[0].Type != "text" || blocks[0].Text != "hello" {
			t.Errorf("block = %+v, want text block", blocks[0])
		}
	})

	t.Run("empty text no block", func(t *testing.T) {
		blocks := buildAttachmentBlocks("", []ChatAttachment{
			{Type: "image", Data: "base64data", MimeType: "image/png"},
		})
		if len(blocks) != 1 {
			t.Fatalf("got %d blocks, want 1", len(blocks))
		}
		if blocks[0].Type != "image" {
			t.Errorf("block type = %q, want %q", blocks[0].Type, "image")
		}
	})

	t.Run("text plus base64 image", func(t *testing.T) {
		blocks := buildAttachmentBlocks("describe this", []ChatAttachment{
			{Type: "image", Data: "base64data", MimeType: "image/jpeg"},
		})
		if len(blocks) != 2 {
			t.Fatalf("got %d blocks, want 2", len(blocks))
		}
		if blocks[0].Type != "text" {
			t.Errorf("blocks[0].Type = %q, want %q", blocks[0].Type, "text")
		}
		if blocks[1].Source == nil || blocks[1].Source.Type != "base64" {
			t.Errorf("blocks[1].Source = %+v, want base64 source", blocks[1].Source)
		}
	})

	t.Run("url image", func(t *testing.T) {
		blocks := buildAttachmentBlocks("", []ChatAttachment{
			{Type: "image", URL: "https://example.com/img.png", MimeType: "image/png"},
		})
		if len(blocks) != 1 {
			t.Fatalf("got %d blocks, want 1", len(blocks))
		}
		if blocks[0].Source == nil || blocks[0].Source.Type != "url" {
			t.Errorf("expected url source, got %+v", blocks[0].Source)
		}
	})

	t.Run("non-image attachments skipped", func(t *testing.T) {
		blocks := buildAttachmentBlocks("text", []ChatAttachment{
			{Type: "file", Data: "some-data"},
			{Type: "audio", URL: "https://example.com/audio.mp3"},
		})
		// Only the text block should be present.
		if len(blocks) != 1 {
			t.Fatalf("got %d blocks, want 1 (non-image skipped)", len(blocks))
		}
	})
}

func TestExtractTextFromMessage(t *testing.T) {
	t.Run("plain string content", func(t *testing.T) {
		msg := llm.NewTextMessage("user", "hello world")
		got := extractTextFromMessage(msg)
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	t.Run("block array content", func(t *testing.T) {
		msg := llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "text", Text: "block text"},
			{Type: "image"},
		})
		got := extractTextFromMessage(msg)
		if got != "block text" {
			t.Errorf("got %q, want %q", got, "block text")
		}
	})

	t.Run("empty content", func(t *testing.T) {
		msg := llm.Message{Role: "user", Content: json.RawMessage(`{}`)}
		got := extractTextFromMessage(msg)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestAppendAttachmentsToHistory(t *testing.T) {
	t.Run("replaces last user message", func(t *testing.T) {
		msgs := []llm.Message{
			llm.NewTextMessage("user", "first msg"),
			llm.NewTextMessage("assistant", "reply"),
			llm.NewTextMessage("user", "with image"),
		}
		attachments := []ChatAttachment{
			{Type: "image", Data: "base64data", MimeType: "image/png"},
		}
		result := appendAttachmentsToHistory(msgs, "with image", attachments)
		if len(result) != 3 {
			t.Fatalf("got %d messages, want 3", len(result))
		}
		// The last message should be a block message now.
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(result[2].Content, &blocks); err != nil {
			t.Fatalf("failed to unmarshal blocks: %v", err)
		}
		if len(blocks) < 2 {
			t.Fatalf("got %d blocks, want >=2", len(blocks))
		}
	})

	t.Run("no user message appends new", func(t *testing.T) {
		msgs := []llm.Message{
			llm.NewTextMessage("assistant", "hello"),
		}
		attachments := []ChatAttachment{
			{Type: "image", Data: "data", MimeType: "image/png"},
		}
		result := appendAttachmentsToHistory(msgs, "text", attachments)
		if len(result) != 2 {
			t.Fatalf("got %d messages, want 2", len(result))
		}
		if result[1].Role != "user" {
			t.Errorf("new message role = %q, want %q", result[1].Role, "user")
		}
	})
}
