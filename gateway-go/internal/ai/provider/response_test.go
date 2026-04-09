package provider

import (
	"encoding/json"
	"testing"
)

func TestFormatForChannel_TextOnly(t *testing.T) {
	resp := &ProviderResponse{
		Content: []ContentPart{
			{Type: "text", Text: "Hello"},
			{Type: "text", Text: "World"},
		},
	}
	result := FormatForChannel(resp)
	if result != "Hello\nWorld" {
		t.Errorf("got %q, want 'Hello\\nWorld'", result)
	}
}

func TestFormatForChannel_MixedParts(t *testing.T) {
	resp := &ProviderResponse{
		Content: []ContentPart{
			{Type: "thinking", Thinking: "let me think..."},
			{Type: "text", Text: "The answer is 42."},
			{Type: "tool_use", Name: "calculator"},
			{Type: "image", URL: "https://example.com/img.png"},
		},
	}
	result := FormatForChannel(resp)
	expected := "The answer is 42.\n[tool: calculator]\nhttps://example.com/img.png"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestFormatForChannel_ToolResultString(t *testing.T) {
	resp := &ProviderResponse{
		Content: []ContentPart{
			{Type: "tool_result", Content: json.RawMessage(`"file contents here"`)},
		},
	}
	result := FormatForChannel(resp)
	if result != "file contents here" {
		t.Errorf("got %q, want 'file contents here'", result)
	}
}

func TestFormatForChannel_ToolResultArray(t *testing.T) {
	resp := &ProviderResponse{
		Content: []ContentPart{
			{Type: "tool_result", Content: json.RawMessage(`[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]`)},
		},
	}
	result := FormatForChannel(resp)
	if result != "line1\nline2" {
		t.Errorf("got %q, want 'line1\\nline2'", result)
	}
}

func TestFormatForChannel_ImageNoURL(t *testing.T) {
	resp := &ProviderResponse{
		Content: []ContentPart{
			{Type: "image", Base64: "abc123"},
		},
	}
	result := FormatForChannel(resp)
	if result != "[image]" {
		t.Errorf("got %q, want '[image]'", result)
	}
}

func TestFormatForTranscript(t *testing.T) {
	resp := &ProviderResponse{
		Role:    "assistant",
		Content: []ContentPart{{Type: "text", Text: "hello"}},
		Model:   "gpt-4",
	}
	raw := FormatForTranscript(resp)
	if raw == nil {
		t.Fatal("expected non-nil raw message")
	}
	if !json.Valid(raw) {
		t.Error("expected valid JSON")
	}

	// Nil input.
	if FormatForTranscript(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestExtractText(t *testing.T) {
	resp := &ProviderResponse{
		Content: []ContentPart{
			{Type: "thinking", Thinking: "hmm"},
			{Type: "text", Text: "Answer:"},
			{Type: "tool_use", Name: "search"},
			{Type: "text", Text: "42"},
		},
	}
	result := ExtractText(resp)
	if result != "Answer:\n42" {
		t.Errorf("got %q, want 'Answer:\\n42'", result)
	}

	if ExtractText(nil) != "" {
		t.Error("expected empty for nil")
	}
}
