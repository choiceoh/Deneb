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
	// Verify all content is preserved.
	joined := strings.Join(chunks, "")
	if joined != text {
		t.Error("content not preserved after chunking")
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
