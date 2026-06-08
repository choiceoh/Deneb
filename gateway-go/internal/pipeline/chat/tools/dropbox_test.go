package tools

import (
	"context"
	"strings"
	"testing"
)

func TestExtractDropboxFileText(t *testing.T) {
	ctx := context.Background()
	if got := extractDropboxFileText(ctx, "note.txt", []byte("hello world")); got != "hello world" {
		t.Errorf("txt = %q", got)
	}
	if got := extractDropboxFileText(ctx, "readme.md", []byte("# title")); got != "# title" {
		t.Errorf("md = %q", got)
	}
	if got := extractDropboxFileText(ctx, "data.bin", []byte{0x00, 0x01}); got != "" {
		t.Errorf("unsupported format should be empty, got %q", got)
	}
}

func TestNormalizeDropboxPath(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"/":         "/",
		"folder/x":  "/folder/x",
		"/folder/x": "/folder/x",
		"  /a.pdf ": "/a.pdf",
		"a.pdf":     "/a.pdf",
	}
	for in, want := range cases {
		if got := normalizeDropboxPath(in); got != want {
			t.Errorf("normalizeDropboxPath(%q) = %q, want %q", in, got, want)
		}
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

func TestHasAnyExt(t *testing.T) {
	exts := []string{".md", ".pdf"}
	if !hasAnyExt("/x/Y.MD", exts) {
		t.Error("should match case-insensitively")
	}
	if hasAnyExt("/x/y.txt", exts) {
		t.Error("should not match .txt")
	}
}

func TestToolDropbox_NoAuth(t *testing.T) {
	// Isolate HOME so DefaultClient finds no credentials and the tool returns
	// the friendly setup guidance instead of attempting a network call.
	t.Setenv("HOME", t.TempDir())
	out, err := ToolDropbox()(context.Background(), []byte(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "deneb-dropbox-auth") {
		t.Errorf("should guide the user to the auth CLI, got: %q", out)
	}
}
