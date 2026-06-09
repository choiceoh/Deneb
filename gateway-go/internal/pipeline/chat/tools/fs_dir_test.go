package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestToolEdit_DirectoryTargetGivesClearError and the write variant verify that
// targeting a directory (a common LLM slip, or the result of ResolvePath
// clamping an out-of-workspace path to the workspace root) yields a clear
// "is a directory" message instead of a confusing low-level read/rename error.
func TestToolEdit_DirectoryTargetGivesClearError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatalf("prep: %v", err)
	}
	fn := ToolEdit(dir)
	_, err := callTool(t, fn, map[string]any{
		"file_path":  "adir",
		"old_string": "x",
		"new_string": "y",
	})
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected a clear 'is a directory' error, got: %v", err)
	}
}

func TestToolWrite_DirectoryTargetGivesClearError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatalf("prep: %v", err)
	}
	fn := ToolWrite(dir)
	_, err := callTool(t, fn, map[string]any{
		"file_path": "adir",
		"content":   "x",
	})
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected a clear 'is a directory' error, got: %v", err)
	}
}
