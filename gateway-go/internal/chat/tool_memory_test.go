package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectMemoryFiles(t *testing.T) {
	t.Run("finds MEMORY.md", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Memory"), 0o644)

		files := collectMemoryFiles(dir)
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if !strings.HasSuffix(files[0], "MEMORY.md") {
			t.Errorf("expected MEMORY.md, got %q", files[0])
		}
	})

	t.Run("finds both cases", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("upper"), 0o644)
		os.WriteFile(filepath.Join(dir, "memory.md"), []byte("lower"), 0o644)

		files := collectMemoryFiles(dir)
		if len(files) != 2 {
			t.Fatalf("got %d files, want 2", len(files))
		}
	})

	t.Run("finds memory directory files", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
		os.WriteFile(filepath.Join(dir, "memory", "notes.md"), []byte("note"), 0o644)
		os.WriteFile(filepath.Join(dir, "memory", "todo.md"), []byte("todo"), 0o644)
		os.WriteFile(filepath.Join(dir, "memory", "data.txt"), []byte("txt"), 0o644) // not .md

		files := collectMemoryFiles(dir)
		if len(files) != 2 {
			t.Fatalf("got %d files, want 2 (.md only)", len(files))
		}
	})

	t.Run("empty workspace", func(t *testing.T) {
		dir := t.TempDir()
		files := collectMemoryFiles(dir)
		if len(files) != 0 {
			t.Fatalf("got %d files, want 0", len(files))
		}
	})
}

func TestToolMemorySearch(t *testing.T) {
	dir := t.TempDir()
	content := "# Project Notes\n\nThis is about golang testing.\nAnother line about rust.\nMore golang content here.\n"
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644)

	fn := toolMemorySearch(dir)

	t.Run("finds keyword match", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"query": "golang"})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "golang") {
			t.Errorf("expected golang match, got: %s", result)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"query": "python"})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "No matches") {
			t.Errorf("expected no-match message, got: %s", result)
		}
	})

	t.Run("empty query returns error", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"query": ""})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for empty query")
		}
	})

	t.Run("no memory files", func(t *testing.T) {
		emptyDir := t.TempDir()
		fn2 := toolMemorySearch(emptyDir)
		input, _ := json.Marshal(map[string]any{"query": "anything"})
		result, err := fn2(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "No memory files") {
			t.Errorf("expected no-memory-files message, got: %s", result)
		}
	})
}

func TestToolMemoryGet(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\n"
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644)

	fn := toolMemoryGet(dir)

	t.Run("full file", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"path": "MEMORY.md"})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "1\tline1") {
			t.Errorf("expected line-numbered output, got: %s", result)
		}
	})

	t.Run("line range", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"path":      "MEMORY.md",
			"startLine": 2,
			"endLine":   4,
		})
		result, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "2\tline2") {
			t.Errorf("expected line 2, got: %s", result)
		}
		if strings.Contains(result, "1\tline1") {
			t.Errorf("should not contain line 1, got: %s", result)
		}
		if strings.Contains(result, "5\tline5") {
			t.Errorf("should not contain line 5, got: %s", result)
		}
	})

	t.Run("empty path returns error", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"path": ""})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for empty path")
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"path": "nonexistent.md"})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}
