package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileCache_GetMiss(t *testing.T) {
	fc := NewFileCache(10)
	if entry := fc.Get("/nonexistent"); entry != nil {
		t.Error("expected nil for cache miss")
	}
}

func TestFileCache_SetAndGet(t *testing.T) {
	fc := NewFileCache(10)
	entry := &FileCacheEntry{
		Path:      "/tmp/test.go",
		MTime:     time.Now(),
		Size:      100,
		Content:   "hello",
		ReadAt:    time.Now(),
		ReadCount: 1,
	}
	fc.Set("/tmp/test.go", entry)

	got := fc.Get("/tmp/test.go")
	if got == nil {
		t.Fatal("expected cache hit")
	}
	if got.Content != "hello" {
		t.Errorf("content = %q, want %q", got.Content, "hello")
	}
}

func TestFileCache_LRUEviction(t *testing.T) {
	fc := NewFileCache(3)

	for i := 0; i < 3; i++ {
		path := filepath.Join("/tmp", string(rune('a'+i))+".go")
		fc.Set(path, &FileCacheEntry{Path: path, Content: path})
	}

	// Cache is full (a, b, c). Adding d should evict a.
	fc.Set("/tmp/d.go", &FileCacheEntry{Path: "/tmp/d.go", Content: "/tmp/d.go"})

	if fc.Get("/tmp/a.go") != nil {
		t.Error("expected /tmp/a.go to be evicted")
	}
	if fc.Get("/tmp/b.go") == nil {
		t.Error("expected /tmp/b.go to still be cached")
	}
	if fc.Get("/tmp/d.go") == nil {
		t.Error("expected /tmp/d.go to be cached")
	}
}

func TestFileCache_LRUAccessRefresh(t *testing.T) {
	fc := NewFileCache(3)

	fc.Set("/tmp/a.go", &FileCacheEntry{Path: "/tmp/a.go"})
	fc.Set("/tmp/b.go", &FileCacheEntry{Path: "/tmp/b.go"})
	fc.Set("/tmp/c.go", &FileCacheEntry{Path: "/tmp/c.go"})

	// Access a → moves to back. Eviction order becomes: b, c, a.
	fc.Get("/tmp/a.go")

	// Adding d should evict b (oldest in LRU order).
	fc.Set("/tmp/d.go", &FileCacheEntry{Path: "/tmp/d.go"})

	if fc.Get("/tmp/b.go") != nil {
		t.Error("expected /tmp/b.go to be evicted (LRU)")
	}
	if fc.Get("/tmp/a.go") == nil {
		t.Error("expected /tmp/a.go to survive (recently accessed)")
	}
}

func TestFileCache_Invalidate(t *testing.T) {
	fc := NewFileCache(10)
	fc.Set("/tmp/x.go", &FileCacheEntry{Path: "/tmp/x.go"})
	fc.Set("/tmp/y.go", &FileCacheEntry{Path: "/tmp/y.go"})

	fc.Invalidate("/tmp/x.go")

	if fc.Get("/tmp/x.go") != nil {
		t.Error("expected /tmp/x.go to be invalidated")
	}
	if fc.Get("/tmp/y.go") == nil {
		t.Error("expected /tmp/y.go to survive")
	}
}

func TestFileCache_InvalidateAll(t *testing.T) {
	fc := NewFileCache(10)
	fc.Set("/tmp/a.go", &FileCacheEntry{Path: "/tmp/a.go"})
	fc.Set("/tmp/b.go", &FileCacheEntry{Path: "/tmp/b.go"})

	fc.InvalidateAll()

	if fc.Get("/tmp/a.go") != nil || fc.Get("/tmp/b.go") != nil {
		t.Error("expected all entries to be invalidated")
	}
}

func TestFileChanged_Unchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	info, _ := os.Stat(path)
	entry := &FileCacheEntry{
		Path:  path,
		MTime: info.ModTime(),
		Size:  info.Size(),
	}

	if FileChanged(path, entry) {
		t.Error("expected file to be unchanged")
	}
}

func TestFileChanged_MtimeChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	entry := &FileCacheEntry{
		Path:  path,
		MTime: time.Now().Add(-time.Hour), // old mtime
		Size:  5,
	}

	if !FileChanged(path, entry) {
		t.Error("expected file to be detected as changed (mtime)")
	}
}

func TestFileChanged_SizeChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	info, _ := os.Stat(path)
	entry := &FileCacheEntry{
		Path:  path,
		MTime: info.ModTime(),
		Size:  5, // wrong size
	}

	if !FileChanged(path, entry) {
		t.Error("expected file to be detected as changed (size)")
	}
}

func TestFileChanged_FileDeleted(t *testing.T) {
	entry := &FileCacheEntry{
		Path:  "/nonexistent/file.txt",
		MTime: time.Now(),
		Size:  10,
	}

	if !FileChanged("/nonexistent/file.txt", entry) {
		t.Error("expected deleted file to be detected as changed")
	}
}

func TestFormatCachedRead(t *testing.T) {
	entry := &FileCacheEntry{
		MTime:     time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
		ReadCount: 3,
		Content:   "[File: executor.go | 42 lines]\n1\tpackage main\n",
	}

	result := FormatCachedRead("executor.go", entry)

	// FormatCachedRead now returns the cached content directly
	// so the agent always has file content even after context compression.
	if result != entry.Content {
		t.Errorf("expected cached content, got: %s", result)
	}
}

func TestFileCache_UpdateExisting(t *testing.T) {
	fc := NewFileCache(10)
	fc.Set("/tmp/a.go", &FileCacheEntry{Content: "v1"})
	fc.Set("/tmp/a.go", &FileCacheEntry{Content: "v2"})

	got := fc.Get("/tmp/a.go")
	if got == nil || got.Content != "v2" {
		t.Error("expected updated content")
	}
}
