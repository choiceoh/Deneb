package document

import (
	"context"
	"testing"
)

func TestExtractFileTextReturnsRawOrEmpty(t *testing.T) {
	ctx := context.Background()
	if got := ExtractFileText(ctx, "note.txt", []byte("hello world")); got != "hello world" {
		t.Errorf("txt = %q", got)
	}
	if got := ExtractFileText(ctx, "readme.md", []byte("# title")); got != "# title" {
		t.Errorf("md = %q", got)
	}
	if got := ExtractFileText(ctx, "data.bin", []byte{0x00, 0x01}); got != "" {
		t.Errorf("unsupported format should be empty, got %q", got)
	}
}
