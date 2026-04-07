package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

func TestToolMultiEdit(t *testing.T) {
	dir := t.TempDir()
	fn := chattools.ToolMultiEdit(dir)

	t.Run("single file single edit", func(t *testing.T) {
		path := filepath.Join(dir, "multi1.txt")
		os.WriteFile(path, []byte("hello world"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"edits": []map[string]any{
				{"file_path": path, "old_string": "world", "new_string": "earth"},
			},
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "1 succeeded") {
			t.Errorf("expected 1 succeeded, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "hello earth" {
			t.Errorf("file content = %q, want %q", data, "hello earth")
		}
	})

	t.Run("multiple files", func(t *testing.T) {
		path1 := filepath.Join(dir, "multi2a.txt")
		path2 := filepath.Join(dir, "multi2b.txt")
		os.WriteFile(path1, []byte("import oldpkg"), 0o644)
		os.WriteFile(path2, []byte("from oldpkg import foo"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"edits": []map[string]any{
				{"file_path": path1, "old_string": "oldpkg", "new_string": "newpkg"},
				{"file_path": path2, "old_string": "oldpkg", "new_string": "newpkg"},
			},
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "2 succeeded") {
			t.Errorf("expected 2 succeeded, got: %s", result)
		}
		data1, _ := os.ReadFile(path1)
		data2, _ := os.ReadFile(path2)
		if string(data1) != "import newpkg" {
			t.Errorf("file1 content = %q, want %q", data1, "import newpkg")
		}
		if string(data2) != "from newpkg import foo" {
			t.Errorf("file2 content = %q, want %q", data2, "from newpkg import foo")
		}
	})

	t.Run("partial failure", func(t *testing.T) {
		path := filepath.Join(dir, "multi3.txt")
		os.WriteFile(path, []byte("abc def"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"edits": []map[string]any{
				{"file_path": path, "old_string": "abc", "new_string": "xyz"},
				{"file_path": path, "old_string": "NOTFOUND", "new_string": "oops"},
			},
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "1 succeeded") || !strings.Contains(result, "1 failed") {
			t.Errorf("expected 1 succeeded + 1 failed, got: %s", result)
		}
	})

	t.Run("replace_all across multiple occurrences in same file", func(t *testing.T) {
		path := filepath.Join(dir, "multi4.txt")
		os.WriteFile(path, []byte("aaa bbb aaa ccc aaa"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"edits": []map[string]any{
				{"file_path": path, "old_string": "aaa", "new_string": "zzz", "replace_all": true},
			},
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "3 replacements") {
			t.Errorf("expected 3 replacements, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "zzz bbb zzz ccc zzz" {
			t.Errorf("file content = %q, want %q", data, "zzz bbb zzz ccc zzz")
		}
	})

	t.Run("non-unique without replace_all fails", func(t *testing.T) {
		path := filepath.Join(dir, "multi5.txt")
		os.WriteFile(path, []byte("foo bar foo"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"edits": []map[string]any{
				{"file_path": path, "old_string": "foo", "new_string": "baz"},
			},
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "FAIL") || !strings.Contains(result, "not unique") {
			t.Errorf("expected uniqueness failure, got: %s", result)
		}
	})

	t.Run("empty edits returns error", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"edits": []map[string]any{}})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for empty edits")
		}
	})

	t.Run("multiple edits to same file apply sequentially", func(t *testing.T) {
		path := filepath.Join(dir, "multi6.txt")
		os.WriteFile(path, []byte("alpha beta gamma"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"edits": []map[string]any{
				{"file_path": path, "old_string": "alpha", "new_string": "ALPHA"},
				{"file_path": path, "old_string": "gamma", "new_string": "GAMMA"},
			},
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "2 succeeded") {
			t.Errorf("expected 2 succeeded, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "ALPHA beta GAMMA" {
			t.Errorf("file content = %q, want %q", data, "ALPHA beta GAMMA")
		}
	})
}

func TestToolTree(t *testing.T) {
	dir := t.TempDir()
	fn := chattools.ToolTree(dir)

	// Create test structure.
	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755)
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules", "react"), 0o755)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "util.go"), []byte("package pkg"), 0o644)
	os.WriteFile(filepath.Join(dir, "docs", "api.md"), []byte("# API"), 0o644)

	t.Run("basic tree output", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "src/") {
			t.Errorf("expected src/ directory, got: %s", result)
		}
		if !strings.Contains(result, "main.go") {
			t.Errorf("expected main.go file, got: %s", result)
		}
		if !strings.Contains(result, "directories") && !strings.Contains(result, "files") {
			t.Errorf("expected summary line, got: %s", result)
		}
	})

	t.Run("skips .git and node_modules", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(result, ".git/") {
			t.Errorf("should skip .git, got: %s", result)
		}
		if strings.Contains(result, "node_modules/") {
			t.Errorf("should skip node_modules, got: %s", result)
		}
	})

	t.Run("depth limit", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"depth": 1})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// At depth 1, we should see src/ and docs/ but not their children.
		if !strings.Contains(result, "src/") {
			t.Errorf("expected src/ at depth 1, got: %s", result)
		}
		if strings.Contains(result, "main.go") {
			t.Errorf("should not see main.go at depth 1, got: %s", result)
		}
	})

	t.Run("dirs_only", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"dirs_only": true})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(result, "README.md") {
			t.Errorf("should not show files in dirs_only mode, got: %s", result)
		}
		if !strings.Contains(result, "src/") {
			t.Errorf("should show directories, got: %s", result)
		}
	})

	t.Run("pattern filter", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"pattern": "*.go"})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "main.go") {
			t.Errorf("expected main.go with *.go filter, got: %s", result)
		}
		if strings.Contains(result, "README.md") {
			t.Errorf("should not show .md files with *.go filter, got: %s", result)
		}
	})
}

func TestToolDiff(t *testing.T) {
	// diffFiles mode doesn't require git — test it directly.
	dir := t.TempDir()

	t.Run("files mode identical", func(t *testing.T) {
		path1 := filepath.Join(dir, "a.txt")
		path2 := filepath.Join(dir, "b.txt")
		os.WriteFile(path1, []byte("same content"), 0o644)
		os.WriteFile(path2, []byte("same content"), 0o644)

		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{
			"mode": "files",
			"path": path1,
			"ref2": path2,
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "identical") {
			t.Errorf("expected identical message, got: %s", result)
		}
	})

	t.Run("files mode different", func(t *testing.T) {
		path1 := filepath.Join(dir, "c.txt")
		path2 := filepath.Join(dir, "d.txt")
		os.WriteFile(path1, []byte("line1\nline2\n"), 0o644)
		os.WriteFile(path2, []byte("line1\nline3\n"), 0o644)

		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{
			"mode": "files",
			"path": path1,
			"ref2": path2,
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "line2") || !strings.Contains(result, "line3") {
			t.Errorf("expected diff content, got: %s", result)
		}
	})

	t.Run("files mode missing path", func(t *testing.T) {
		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{
			"mode": "files",
			"path": "",
			"ref2": "",
		})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing paths")
		}
	})

	t.Run("unknown mode", func(t *testing.T) {
		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{"mode": "bogus"})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for unknown mode")
		}
	})
}
