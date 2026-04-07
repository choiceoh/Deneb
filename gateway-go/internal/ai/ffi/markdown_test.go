package ffi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestMarkdownToIR_Basic(t *testing.T) {
	ir, err := MarkdownToIR("**bold** and *italic*", "")
	testutil.NoError(t, err)
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
	testutil.NoError(t, err)
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
	testutil.NoError(t, err)
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
	testutil.NoError(t, err)
	if ir == nil {
		t.Fatal("expected non-nil IR")
	}
	t.Logf("IR with options: %s", string(ir))
}

func TestMarkdownDetectFences_Basic(t *testing.T) {
	text := "before\n```python\nprint('hi')\n```\nafter"
	fences, err := MarkdownDetectFences(text)
	testutil.NoError(t, err)
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
	testutil.NoError(t, err)
	if string(fences) != "[]" {
		t.Errorf("expected empty array, got %s", string(fences))
	}
}

func TestMarkdownToPlainText(t *testing.T) {
	text, err := MarkdownToPlainText("**bold** and [link](https://example.com)")
	testutil.NoError(t, err)
	if text == "" {
		t.Fatal("expected non-empty text")
	}
	t.Logf("Plain text: %q", text)
}

func TestMarkdownToIR_Headings(t *testing.T) {
	ir, err := MarkdownToIR("# Heading 1\n## Heading 2\n### Heading 3", "")
	testutil.NoError(t, err)
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(ir, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Headings should have '#' stripped
	if result.Text == "" {
		t.Error("expected non-empty text after heading strip")
	}
	if strings.Contains(result.Text, "# ") {
		t.Errorf("expected heading markers stripped, got %q", result.Text)
	}
}

func TestMarkdownToIR_Links(t *testing.T) {
	ir, err := MarkdownToIR("[Click here](https://example.com)", "")
	testutil.NoError(t, err)
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(ir, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(result.Text, "Click here") {
		t.Errorf("expected link text preserved, got %q", result.Text)
	}
	if strings.Contains(result.Text, "https://") {
		t.Errorf("expected URL stripped from text, got %q", result.Text)
	}
}

func TestMarkdownDetectFences_TildeFence(t *testing.T) {
	text := "before\n~~~python\nprint('hi')\n~~~\nafter"
	fences, err := MarkdownDetectFences(text)
	testutil.NoError(t, err)
	var spans []json.RawMessage
	if err := json.Unmarshal(fences, &spans); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(spans) != 1 {
		t.Errorf("expected 1 tilde fence span, got %d", len(spans))
	}
}

func TestMarkdownDetectFences_Unclosed(t *testing.T) {
	text := "start\n```python\nsome code\nno closing fence"
	fences, err := MarkdownDetectFences(text)
	testutil.NoError(t, err)
	var spans []struct {
		Start int `json:"start"`
		End   int `json:"end"`
	}
	if err := json.Unmarshal(fences, &spans); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 unclosed fence span, got %d", len(spans))
	}
	// Unclosed fence should extend to end of text
	if spans[0].End != len(text) {
		t.Errorf("expected unclosed fence end=%d, got %d", len(text), spans[0].End)
	}
}

func TestMarkdownDetectFences_MultipleFences(t *testing.T) {
	text := "```go\nfunc main(){}\n```\nsome text\n```rust\nfn main(){}\n```"
	fences, err := MarkdownDetectFences(text)
	testutil.NoError(t, err)
	var spans []json.RawMessage
	if err := json.Unmarshal(fences, &spans); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(spans) != 2 {
		t.Errorf("expected 2 fence spans, got %d", len(spans))
	}
}

func TestMarkdownDetectFences_IndentedFence(t *testing.T) {
	text := "   ```python\n   code\n   ```"
	fences, err := MarkdownDetectFences(text)
	testutil.NoError(t, err)
	var spans []json.RawMessage
	if err := json.Unmarshal(fences, &spans); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(spans) != 1 {
		t.Errorf("expected 1 indented fence span, got %d", len(spans))
	}
}

func TestMarkdownDetectFences_TooMuchIndent(t *testing.T) {
	// 4+ spaces of indent should NOT be treated as a fence
	text := "    ```python\n    code\n    ```"
	fences, err := MarkdownDetectFences(text)
	testutil.NoError(t, err)
	var spans []json.RawMessage
	if err := json.Unmarshal(fences, &spans); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("expected 0 fence spans for 4-space indent, got %d", len(spans))
	}
}

func TestMarkdownDetectFences_NoFences(t *testing.T) {
	text := "Just some normal text\nwith multiple lines\nbut no code fences"
	fences, err := MarkdownDetectFences(text)
	testutil.NoError(t, err)
	var spans []json.RawMessage
	if err := json.Unmarshal(fences, &spans); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("expected 0 fences, got %d", len(spans))
	}
}

func TestMarkdownToPlainText_Complex(t *testing.T) {
	input := "# Title\n\n**Bold** and *italic* with `code` and [link](https://example.com)\n\n## Section"
	text, err := MarkdownToPlainText(input)
	testutil.NoError(t, err)
	if strings.Contains(text, "**") || strings.Contains(text, "__") {
		t.Errorf("bold markers should be stripped, got %q", text)
	}
	if strings.Contains(text, "`") {
		t.Errorf("code backticks should be stripped, got %q", text)
	}
	if !strings.Contains(text, "Bold") || !strings.Contains(text, "italic") {
		t.Errorf("content should be preserved, got %q", text)
	}
}
