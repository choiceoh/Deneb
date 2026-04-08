package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── ToolBatchRead ─────────────────────────────────────────────────────────

func TestToolBatchRead_basic(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("alpha\nbeta\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("gamma\ndelta\n"), 0o644)

	out := mustCallTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{
			{"file_path": filepath.Join(tmp, "a.txt")},
			{"file_path": filepath.Join(tmp, "b.txt")},
		},
	})

	if !strings.Contains(out, "alpha") {
		t.Error("missing content from a.txt")
	}
	if !strings.Contains(out, "gamma") {
		t.Error("missing content from b.txt")
	}
	if !strings.Contains(out, "2/2 files read successfully") {
		t.Errorf("unexpected summary: %s", out)
	}
}

func TestToolBatchRead_partialFailure(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "exists.txt"), []byte("hello\n"), 0o644)

	out := mustCallTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{
			{"file_path": filepath.Join(tmp, "exists.txt")},
			{"file_path": filepath.Join(tmp, "missing.txt")},
		},
	})

	if !strings.Contains(out, "hello") {
		t.Error("missing content from exists.txt")
	}
	if !strings.Contains(out, "Error reading") {
		t.Error("expected error for missing.txt")
	}
	if !strings.Contains(out, "1/2 files read successfully") {
		t.Errorf("unexpected summary: %s", out)
	}
}

func TestToolBatchRead_empty(t *testing.T) {
	tmp := t.TempDir()
	_, err := callTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{},
	})
	if err == nil {
		t.Error("expected error for empty files list")
	}
}

func TestToolBatchRead_withFunction(t *testing.T) {
	tmp := t.TempDir()
	goContent := `package main

func Hello() string {
	return "world"
}

func Goodbye() string {
	return "bye"
}
`
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(goContent), 0o644)

	out := mustCallTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{
			{"file_path": filepath.Join(tmp, "main.go"), "function": "Hello"},
		},
	})

	if !strings.Contains(out, "Hello") {
		t.Error("expected Hello function in output")
	}
	if !strings.Contains(out, "1/1 files read successfully") {
		t.Errorf("unexpected summary: %s", out)
	}
}

