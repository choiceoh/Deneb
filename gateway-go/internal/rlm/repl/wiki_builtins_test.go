package repl

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newWikiTestEnv(t *testing.T) *Env {
	t.Helper()

	pages := map[string]string{
		"기술/dgx-spark.md": "---\nid: dgx-spark\ntitle: DGX Spark\nsummary: 128GB AI server\ncategory: 기술\ntags: [하드웨어, NVIDIA]\nrelated: [기술/go.md]\n---\n\n# DGX Spark\n\n128GB 통합 메모리.",
		"기술/go.md":        "---\nid: go-lang\ntitle: Go\nsummary: Deneb 주 개발 언어\ncategory: 기술\ntags: [언어]\n---\n\n# Go\n\nDeneb gateway is written in Go.",
	}

	wf := &WikiFuncs{
		Read: func(relPath string) (string, error) {
			if c, ok := pages[relPath]; ok {
				return c, nil
			}
			return "", &testErr{msg: "not found: " + relPath}
		},
		ReadBatch: func(relPaths []string) ([]string, error) {
			results := make([]string, len(relPaths))
			for i, p := range relPaths {
				if c, ok := pages[p]; ok {
					results[i] = c
				} else {
					results[i] = "ERROR: not found"
				}
			}
			return results, nil
		},
		List: func(category string) ([]string, error) {
			var result []string
			for path := range pages {
				if category == "" || strings.HasPrefix(path, category+"/") {
					result = append(result, path)
				}
			}
			return result, nil
		},
		Index: func(category string) (string, error) {
			return "id\tpath\ttitle\tsummary\ttags\ndgx-spark\t기술/dgx-spark.md\tDGX Spark\t128GB AI server\t하드웨어,NVIDIA\ngo-lang\t기술/go.md\tGo\tDeneb 주 개발 언어\t언어", nil
		},
		Search: func(ctx context.Context, query string, limit int) (string, error) {
			return "기술/dgx-spark.md\t0.95\t128GB 통합 메모리", nil
		},
	}

	return NewEnv(context.Background(), EnvConfig{
		Messages:   testMessages(),
		LLMQueryFn: noopLLMQuery,
		Wiki:       wf,
		Timeout:    5 * time.Second,
	})
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestWikiRead(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`page = wiki_read("기술/dgx-spark.md")
print(page[:20])`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "dgx-spark") {
		t.Errorf("expected dgx-spark content, got: %q", result.Stdout)
	}
}

func TestWikiReadBatch(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`pages = wiki_read_batch(["기술/dgx-spark.md", "기술/go.md"])
print(len(pages))`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "2") {
		t.Errorf("expected 2 pages, got: %q", result.Stdout)
	}
}

func TestWikiIndex(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`idx = wiki_index()
lines = idx.split("\n")
print(len(lines))`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
}

func TestWikiList(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`pages = wiki_list()
print(len(pages))`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "2") {
		t.Errorf("expected 2 pages, got: %q", result.Stdout)
	}
}

func TestWikiSearch(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`results = wiki_search("DGX")
print(results)`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "dgx-spark") {
		t.Errorf("expected dgx-spark in results, got: %q", result.Stdout)
	}
}

func TestWikiRead_NotFound(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`page = wiki_read("없는/파일.md")`)
	if result.Error == "" {
		t.Error("expected error for nonexistent page")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected 'not found' in error, got: %q", result.Error)
	}
}

func TestWikiWrite(t *testing.T) {
	var writtenPath, writtenContent string
	wf := &WikiFuncs{
		Read: func(relPath string) (string, error) {
			return "", &testErr{msg: "not found"}
		},
		Write: func(relPath, content string) error {
			writtenPath = relPath
			writtenContent = content
			return nil
		},
	}
	env := NewEnv(context.Background(), EnvConfig{
		Messages:   testMessages(),
		LLMQueryFn: noopLLMQuery,
		Wiki:       wf,
		Timeout:    5 * time.Second,
	})

	result := env.Execute(`wiki_write("결정/test", "# Test page")`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
	// Should auto-append .md
	if writtenPath != "결정/test.md" {
		t.Errorf("path = %q, want 결정/test.md", writtenPath)
	}
	if writtenContent != "# Test page" {
		t.Errorf("content = %q", writtenContent)
	}
}

func TestWikiListFiltered(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`pages = wiki_list("기술")
print(len(pages))
for p in pages:
    print(p)`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "2") {
		t.Errorf("expected 2 tech pages, got: %q", result.Stdout)
	}
}

func TestWikiSearchCustomLimit(t *testing.T) {
	env := newWikiTestEnv(t)
	result := env.Execute(`results = wiki_search("DGX", 5)
print(results)`)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
}
