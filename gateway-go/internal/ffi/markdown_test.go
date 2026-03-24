package ffi

import (
	"encoding/json"
	"testing"
)

func TestMarkdownToIR_Basic(t *testing.T) {
	ir, err := MarkdownToIR("**bold** and *italic*", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		Text          string `json:"text"`
		HasCodeBlocks bool   `json:"has_code_blocks"`
	}
	if err := json.Unmarshal(ir, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty text")
	}
	if result.HasCodeBlocks {
		t.Error("expected no code blocks")
	}
	t.Logf("IR text: %q", result.Text)
}

func TestMarkdownToIR_Empty(t *testing.T) {
	ir, err := MarkdownToIR("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(ir, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
}

func TestMarkdownToIR_CodeBlock(t *testing.T) {
	ir, err := MarkdownToIR("```go\nfmt.Println(\"hi\")\n```", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		HasCodeBlocks bool `json:"has_code_blocks"`
	}
	if err := json.Unmarshal(ir, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !result.HasCodeBlocks {
		t.Error("expected has_code_blocks to be true")
	}
}

func TestMarkdownToIR_WithOptions(t *testing.T) {
	options := `{"enableSpoilers":true,"headingStyle":"bold"}`
	ir, err := MarkdownToIR("# Heading\n||spoiler||", options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ir == nil {
		t.Fatal("expected non-nil IR")
	}
	t.Logf("IR with options: %s", string(ir))
}

func TestMarkdownDetectFences_Basic(t *testing.T) {
	text := "before\n```python\nprint('hi')\n```\nafter"
	fences, err := MarkdownDetectFences(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var spans []json.RawMessage
	if err := json.Unmarshal(fences, &spans); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(spans) != 1 {
		t.Errorf("expected 1 fence span, got %d", len(spans))
	}
	t.Logf("Fences: %s", string(fences))
}

func TestMarkdownDetectFences_Empty(t *testing.T) {
	fences, err := MarkdownDetectFences("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(fences) != "[]" {
		t.Errorf("expected empty array, got %s", string(fences))
	}
}

func TestMarkdownToPlainText(t *testing.T) {
	text, err := MarkdownToPlainText("**bold** and [link](https://example.com)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text == "" {
		t.Fatal("expected non-empty text")
	}
	t.Logf("Plain text: %q", text)
}
