package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestReadMemoryFile_CacheAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("v1"), 0o644)

	// First read: cache miss.
	content, err := readMemoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if content != "v1" {
		t.Fatalf("got %q, want %q", content, "v1")
	}

	// Second read: cache hit (same mtime).
	content, err = readMemoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if content != "v1" {
		t.Fatalf("got %q, want %q", content, "v1")
	}

	// Modify file — update mtime to force cache invalidation.
	// Use Chtimes to ensure mtime actually changes (some fast filesystems
	// may not update mtime if write happens within the same second).
	newTime := time.Now().Add(2 * time.Second)
	os.WriteFile(path, []byte("v2"), 0o644)
	os.Chtimes(path, newTime, newTime)

	content, err = readMemoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if content != "v2" {
		t.Fatalf("got %q after modification, want %q", content, "v2")
	}
}
