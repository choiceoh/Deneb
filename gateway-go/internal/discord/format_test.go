package discord

import (
	"strings"
	"testing"
)

func TestChunkText_Short(t *testing.T) {
	chunks := ChunkText("hello world", 100)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "hello world" {
		t.Errorf("unexpected chunk: %q", chunks[0])
	}
}

func TestChunkText_SplitsAtNewline(t *testing.T) {
	text := strings.Repeat("line\n", 500)
	chunks := ChunkText(text, 100)
	for i, chunk := range chunks {
		if len(chunk) > 100 {
			t.Errorf("chunk %d exceeds limit: %d chars", i, len(chunk))
		}
	}
	joined := strings.Join(chunks, "")
	if joined != text {
		t.Error("content not preserved after chunking")
	}
}

func TestChunkText_PreservesCodeBlock(t *testing.T) {
	// Code block should not be split in the middle.
	before := "Some text here.\n\n"
	code := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"
	after := "\n\nMore text after."
	text := before + code + after

	chunks := ChunkText(text, 60)

	// Verify no chunk contains an unclosed code block.
	for i, chunk := range chunks {
		opens := strings.Count(chunk, "```")
		if opens%2 != 0 {
			t.Errorf("chunk %d has unclosed code block (``` count: %d): %q", i, opens, chunk)
		}
	}

	// Verify all content is preserved.
	joined := strings.Join(chunks, "")
	if joined != text {
		t.Error("content not preserved after chunking")
	}
}

func TestChunkText_LargeCodeBlock(t *testing.T) {
	// A code block that exceeds the chunk limit.
	code := "```go\n" + strings.Repeat("x := 1\n", 200) + "```\n"
	chunks := ChunkText(code, 500)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// All content should be preserved.
	joined := strings.Join(chunks, "")
	if joined != code {
		t.Error("content not preserved")
	}
}

func TestChunkText_NonPositiveLimit(t *testing.T) {
	text := "hello world"
	chunks := ChunkText(text, 0)
	if len(chunks) != 1 || chunks[0] != text {
		t.Fatalf("expected single untouched chunk, got %#v", chunks)
	}
}

func TestChunkText_LeadingFenceNearBoundaryHonorsDiscordHardLimit(t *testing.T) {
	// Closing fence is close to TextChunkLimit so the chunker may keep the fenced
	// block together. Even then, it must stay under Discord's hard 2000-char limit.
	code := "```go\n" + strings.Repeat("x := 1\n", 270) + "```"
	chunks := ChunkText(code, TextChunkLimit)
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	for i, chunk := range chunks {
		if len(chunk) > MaxMessageLength {
			t.Fatalf("chunk %d exceeds hard discord limit: %d", i, len(chunk))
		}
	}
}

func TestLastOpenCodeBlock(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantOpen bool
		wantLang string
	}{
		{"no code block", "hello world", false, ""},
		{"closed code block", "```go\ncode\n```", false, ""},
		{"open code block", "```python\ncode", true, "python"},
		{"two closed", "```go\na\n```\n```rust\nb\n```", false, ""},
		{"two, second open", "```go\na\n```\n```rust\nb", true, "rust"},
		{"no lang", "```\ncode", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, lang := lastOpenCodeBlock(tt.text)
			gotOpen := idx >= 0
			if gotOpen != tt.wantOpen {
				t.Errorf("open=%v, want %v (idx=%d)", gotOpen, tt.wantOpen, idx)
			}
			if gotOpen && lang != tt.wantLang {
				t.Errorf("lang=%q, want %q", lang, tt.wantLang)
			}
		})
	}
}

func TestWrapCodeBlock(t *testing.T) {
	result := WrapCodeBlock("fmt.Println(\"hello\")", "go")
	if !strings.HasPrefix(result, "```go\n") {
		t.Errorf("expected code block start, got: %q", result[:20])
	}
	if !strings.HasSuffix(result, "\n```") {
		t.Error("expected code block end")
	}
}

func TestDetectCodeLanguage(t *testing.T) {
	tests := []struct {
		file string
		lang string
	}{
		{"main.go", "go"},
		{"lib.rs", "rust"},
		{"app.py", "python"},
		{"index.ts", "typescript"},
		{"script.sh", "bash"},
		{"data.json", "json"},
		{"config.yaml", "yaml"},
		{"unknown.xyz", ""},
	}
	for _, tt := range tests {
		got := DetectCodeLanguage(tt.file)
		if got != tt.lang {
			t.Errorf("DetectCodeLanguage(%q) = %q, want %q", tt.file, got, tt.lang)
		}
	}
}

func TestTruncateForFile(t *testing.T) {
	short := strings.Repeat("a", TextChunkLimit-1)
	if TruncateForFile(short) {
		t.Error("expected false for short text")
	}

	long := strings.Repeat("a", TextChunkLimit+1)
	if !TruncateForFile(long) {
		t.Error("expected true for long text")
	}
}

func TestFormatReply_Short(t *testing.T) {
	result := FormatReply("hello world")
	if result.Text != "hello world" {
		t.Errorf("expected unchanged text, got %q", result.Text)
	}
	if result.FileContent != nil {
		t.Error("expected no file attachment for short text")
	}
}

func TestFormatReply_LargeCodeBlock(t *testing.T) {
	code := strings.Repeat("x := 1\n", 200)
	text := "Here's the code:\n```go\n" + code + "```\nDone."

	result := FormatReply(text)

	if result.FileContent == nil {
		t.Fatal("expected file attachment for large code block")
	}
	if result.FileName != "output.go" {
		t.Errorf("expected output.go, got %q", result.FileName)
	}
	if !strings.Contains(result.Text, "Here's the code:") {
		t.Error("expected surrounding text preserved")
	}
	if !strings.Contains(result.Text, "Done.") {
		t.Error("expected 'Done.' in text")
	}
	if string(result.FileContent) != code {
		t.Error("file content doesn't match code block")
	}
}

func TestFormatReply_NoLargeBlock(t *testing.T) {
	// Code block smaller than 200 chars should not be extracted as file.
	text := "Some text\n```go\nfmt.Println(\"hi\")\n```\nEnd."
	// Make text exceed TextChunkLimit to trigger FormatReply processing.
	text = text + strings.Repeat("\nMore text here.", 200)

	result := FormatReply(text)
	if result.FileContent != nil {
		t.Error("expected no file attachment for small code block")
	}
}

func TestExtractLargestCodeBlock(t *testing.T) {
	code := strings.Repeat("line\n", 50)
	text := "Before\n```python\n" + code + "```\nAfter"

	gotCode, gotLang, gotBefore, gotAfter := extractLargestCodeBlock(text)

	if gotCode != code {
		t.Errorf("code mismatch: got %d chars, want %d", len(gotCode), len(code))
	}
	if gotLang != "python" {
		t.Errorf("lang = %q, want python", gotLang)
	}
	if !strings.Contains(gotBefore, "Before") {
		t.Error("expected 'Before' in before text")
	}
	if !strings.Contains(gotAfter, "After") {
		t.Error("expected 'After' in after text")
	}
}

func TestExtractLargestCodeBlock_MultipleBlocks(t *testing.T) {
	small := "```go\nsmall\n```"
	large := "```rust\n" + strings.Repeat("big\n", 100) + "```"
	text := small + "\n" + large

	gotCode, gotLang, _, _ := extractLargestCodeBlock(text)

	if gotLang != "rust" {
		t.Errorf("expected largest block (rust), got %q", gotLang)
	}
	if len(gotCode) < 100 {
		t.Errorf("expected large code block, got %d chars", len(gotCode))
	}
}

func TestLangToFileExt(t *testing.T) {
	tests := []struct {
		lang string
		ext  string
	}{
		{"go", ".go"},
		{"rust", ".rs"},
		{"python", ".py"},
		{"javascript", ".js"},
		{"js", ".js"},
		{"typescript", ".ts"},
		{"ts", ".ts"},
		{"bash", ".sh"},
		{"diff", ".diff"},
		{"unknown", ".txt"},
		{"", ".txt"},
	}
	for _, tt := range tests {
		got := langToFileExt(tt.lang)
		if got != tt.ext {
			t.Errorf("langToFileExt(%q) = %q, want %q", tt.lang, got, tt.ext)
		}
	}
}
