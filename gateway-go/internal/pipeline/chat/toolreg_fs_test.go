package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestResolvePath(t *testing.T) {
	// Use a temp dir as the workspace root so filepath.Abs works predictably.
	workspace := t.TempDir()

	tests := []struct {
		name       string
		path       string
		defaultDir string
		want       string
	}{
		{
			name:       "relative path joins with workspace",
			path:       "foo/bar.txt",
			defaultDir: workspace,
			want:       filepath.Join(workspace, "foo/bar.txt"),
		},
		{
			name:       "path traversal clamped to workspace",
			path:       "../../etc/passwd",
			defaultDir: workspace,
			want:       workspace,
		},
		{
			name:       "dot-dot in middle escapes workspace",
			path:       "foo/../../../../../../etc/passwd",
			defaultDir: workspace,
			want:       workspace,
		},
		{
			name:       "trailing slashes cleaned",
			path:       "foo/bar/",
			defaultDir: workspace,
			want:       filepath.Join(workspace, "foo/bar"),
		},
		{
			name:       "dot-slash cleaned",
			path:       "./foo",
			defaultDir: workspace,
			want:       filepath.Join(workspace, "foo"),
		},
		{
			name:       "empty path resolves to workspace",
			path:       "",
			defaultDir: workspace,
			want:       filepath.Join(workspace, "."),
		},
		{
			name:       "absolute path within workspace allowed",
			path:       filepath.Join(workspace, "subdir/file.txt"),
			defaultDir: workspace,
			want:       filepath.Join(workspace, "subdir/file.txt"),
		},
		{
			name:       "absolute path outside workspace clamped",
			path:       "/etc/passwd",
			defaultDir: workspace,
			want:       workspace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chattools.ResolvePath(tt.path, tt.defaultDir)
			// Normalize both for comparison.
			gotAbs, _ := filepath.Abs(got)
			wantAbs, _ := filepath.Abs(tt.want)
			if gotAbs != wantAbs {
				t.Errorf("ResolvePath(%q, %q) = %q, want %q", tt.path, tt.defaultDir, gotAbs, wantAbs)
			}
		})
	}
}

func TestToolRead(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	fn := chattools.ToolRead(dir)

	t.Run("basic read with line numbers", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"file_path": filepath.Join(dir, "test.txt")})
		result := testutil.Must(fn(context.Background(), input))
		if !strings.Contains(result, "1\tline1") {
			t.Errorf("expected line-numbered output, got: %s", result)
		}
		if !strings.Contains(result, "5\tline5") {
			t.Errorf("expected line 5 in output, got: %s", result)
		}
	})

	t.Run("read with offset and limit", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"file_path": filepath.Join(dir, "test.txt"),
			"offset":    3,
			"limit":     2,
		})
		result := testutil.Must(fn(context.Background(), input))
		// Offset 3 means start from line 3 (1-based), limit 2 means lines 3-4.
		if !strings.Contains(result, "3\tline3") {
			t.Errorf("expected line 3 in output, got: %s", result)
		}
		if !strings.Contains(result, "4\tline4") {
			t.Errorf("expected line 4 in output, got: %s", result)
		}
		if strings.Contains(result, "5\tline5") {
			t.Errorf("should not contain line 5, got: %s", result)
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"file_path": filepath.Join(dir, "nonexistent.txt")})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("empty file_path returns error", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"file_path": ""})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for empty file_path")
		}
	})
}

func TestToolWrite(t *testing.T) {
	dir := t.TempDir()
	fn := chattools.ToolWrite(dir)

	t.Run("write creates file", func(t *testing.T) {
		path := filepath.Join(dir, "out.txt")
		input, _ := json.Marshal(map[string]any{"file_path": path, "content": "hello"})
		result := testutil.Must(fn(context.Background(), input))
		if !strings.Contains(result, "Wrote") {
			t.Errorf("expected 'Wrote' in result, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "hello" {
			t.Errorf("file content = %q, want %q", data, "hello")
		}
	})

	t.Run("write creates parent directories", func(t *testing.T) {
		path := filepath.Join(dir, "sub", "deep", "file.txt")
		input, _ := json.Marshal(map[string]any{"file_path": path, "content": "nested"})
		_, err := fn(context.Background(), input)
		testutil.NoError(t, err)
		data, _ := os.ReadFile(path)
		if string(data) != "nested" {
			t.Errorf("file content = %q, want %q", data, "nested")
		}
	})

	t.Run("empty file_path returns error", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"file_path": "", "content": "x"})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for empty file_path")
		}
	})
}

func TestToolEdit(t *testing.T) {
	dir := t.TempDir()
	fn := chattools.ToolEdit(dir)

	t.Run("successful unique replacement", func(t *testing.T) {
		path := filepath.Join(dir, "edit.txt")
		os.WriteFile(path, []byte("hello world"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"file_path":  path,
			"old_string": "world",
			"new_string": "earth",
		})
		_, err := fn(context.Background(), input)
		testutil.NoError(t, err)
		data, _ := os.ReadFile(path)
		if string(data) != "hello earth" {
			t.Errorf("file content = %q, want %q", data, "hello earth")
		}
	})

	t.Run("old_string not found", func(t *testing.T) {
		path := filepath.Join(dir, "edit2.txt")
		os.WriteFile(path, []byte("hello world"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"file_path":  path,
			"old_string": "mars",
			"new_string": "earth",
		})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing old_string")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got: %v", err)
		}
	})

	t.Run("old_string not unique", func(t *testing.T) {
		path := filepath.Join(dir, "edit3.txt")
		os.WriteFile(path, []byte("aaa bbb aaa"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"file_path":  path,
			"old_string": "aaa",
			"new_string": "ccc",
		})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for non-unique old_string")
		}
		if !strings.Contains(err.Error(), "not unique") {
			t.Errorf("expected 'not unique' in error, got: %v", err)
		}
	})

	t.Run("empty old_string returns error", func(t *testing.T) {
		path := filepath.Join(dir, "edit4.txt")
		os.WriteFile(path, []byte("content"), 0o644)
		input, _ := json.Marshal(map[string]any{
			"file_path":  path,
			"old_string": "",
			"new_string": "x",
		})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for empty old_string")
		}
	})
}

