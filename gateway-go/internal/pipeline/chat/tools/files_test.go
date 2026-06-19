package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractFileText(t *testing.T) {
	ctx := context.Background()
	if got := extractFileText(ctx, "note.txt", []byte("hello world")); got != "hello world" {
		t.Errorf("txt = %q", got)
	}
	if got := extractFileText(ctx, "readme.md", []byte("# title")); got != "# title" {
		t.Errorf("md = %q", got)
	}
	if got := extractFileText(ctx, "data.bin", []byte{0x00, 0x01}); got != "" {
		t.Errorf("unsupported format should be empty, got %q", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("짧은글", 10); got != "짧은글" {
		t.Errorf("short text changed: %q", got)
	}
	got := truncateRunes(strings.Repeat("가", 100), 10)
	if !strings.HasPrefix(got, strings.Repeat("가", 10)) {
		t.Errorf("prefix wrong: %q", got)
	}
	if !strings.Contains(got, "생략") {
		t.Errorf("missing ellipsis note: %q", got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatal("replacement char — rune boundary broken")
		}
	}
}

// TestToolFiles_RoundTrip exercises the local-store tool end to end:
// upload a local file, list it, then extract its text via analyze.
func TestToolFiles_RoundTrip(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir())
	ctx := context.Background()
	tool := ToolFiles()

	src := filepath.Join(t.TempDir(), "견적서.txt")
	if err := os.WriteFile(src, []byte("총액 1,000,000원"), 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	out, err := tool(ctx, []byte(`{"action":"upload","local_path":"`+src+`","dest_path":"/메일/견적서.txt"}`))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !strings.Contains(out, "저장 완료") {
		t.Errorf("upload out = %q", out)
	}

	out, err = tool(ctx, []byte(`{"action":"list","path":"/메일"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "견적서.txt") {
		t.Errorf("list out = %q", out)
	}

	out, err = tool(ctx, []byte(`{"action":"analyze","path":"/메일/견적서.txt"}`))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !strings.Contains(out, "1,000,000") {
		t.Errorf("analyze out = %q", out)
	}

	out, _ = tool(ctx, []byte(`{"action":"frobnicate"}`))
	if !strings.Contains(out, "알 수 없는") {
		t.Errorf("unknown action out = %q", out)
	}
}
