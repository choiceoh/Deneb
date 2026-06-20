package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
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
	tool := ToolFiles(nil)

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

// TestToolFiles_SemanticSearch exercises the semantic search path: a wired
// search func returns ranked hits (with snippet), and an empty result falls back
// to a name search.
func TestToolFiles_SemanticSearch(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir())
	ctx := context.Background()

	// Fake semantic backend returns one ranked hit with a snippet.
	semantic := func(_ context.Context, _ string, _ int) ([]filestore.ScoredEntry, error) {
		return []filestore.ScoredEntry{{
			Entry:   filestore.Entry{Tag: "file", Name: "계약서.txt", PathDisplay: "/계약/계약서.txt", Size: 42},
			Score:   0.87,
			Snippet: "납기 지연 위약금 조항",
		}}, nil
	}
	tool := ToolFiles(semantic)

	// semantic=true → uses the vector backend, renders snippet + similarity.
	out, err := tool(ctx, []byte(`{"action":"search","query":"납기 위험","semantic":true}`))
	if err != nil {
		t.Fatalf("semantic search: %v", err)
	}
	if !strings.Contains(out, "시맨틱") || !strings.Contains(out, "계약서.txt") {
		t.Errorf("semantic search out = %q", out)
	}
	if !strings.Contains(out, "위약금") || !strings.Contains(out, "0.87") {
		t.Errorf("semantic search missing snippet/score: %q", out)
	}

	// semantic_search action is an alias for search semantic=true.
	out, err = tool(ctx, []byte(`{"action":"semantic_search","query":"납기 위험"}`))
	if err != nil {
		t.Fatalf("semantic_search alias: %v", err)
	}
	if !strings.Contains(out, "계약서.txt") {
		t.Errorf("semantic_search alias out = %q", out)
	}

	// Empty semantic result → falls back to name search (no hits here, but the
	// scope label shows the fallback happened, not a crash).
	emptySem := func(_ context.Context, _ string, _ int) ([]filestore.ScoredEntry, error) {
		return nil, nil
	}
	out, err = ToolFiles(emptySem)(ctx, []byte(`{"action":"search","query":"존재하지않는질의","semantic":true}`))
	if err != nil {
		t.Fatalf("semantic fallback: %v", err)
	}
	if !strings.Contains(out, "시맨틱 폴백") {
		t.Errorf("semantic fallback scope label missing: %q", out)
	}

	// nil semantic func with semantic=true → plain name search, no crash.
	out, err = ToolFiles(nil)(ctx, []byte(`{"action":"search","query":"x","semantic":true}`))
	if err != nil {
		t.Fatalf("nil semantic func: %v", err)
	}
	if !strings.Contains(out, "검색 결과 없음") {
		t.Errorf("nil semantic func out = %q", out)
	}
}
