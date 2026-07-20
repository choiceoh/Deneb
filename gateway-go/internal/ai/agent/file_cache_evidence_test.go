package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// RecordReadEvidence creates a staleness baseline for reads the dedup cache
// skips, and keeps/drops the cached render depending on whether the bytes
// changed.
func TestRecordReadEvidence(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	t.Run("creates baseline without content", func(t *testing.T) {
		fc := NewFileCache(0)
		fc.RecordReadEvidence(path, data)
		entry := fc.Get(path)
		if entry == nil || entry.Content != "" || entry.ContentHash != ContentHashOf(data) {
			t.Fatalf("want content-less baseline with hash, got %+v", entry)
		}
		if err := fc.CheckStaleness(path); err != nil {
			t.Fatalf("fresh baseline must pass staleness: %v", err)
		}
	})

	t.Run("same bytes keep cached render, changed bytes drop it", func(t *testing.T) {
		fc := NewFileCache(0)
		info, _ := os.Stat(path)
		fc.Set(path, &FileCacheEntry{
			Path: path, MTime: info.ModTime(), Size: info.Size(),
			Content: "rendered", ContentHash: ContentHashOf(data),
		})
		fc.RecordReadEvidence(path, data)
		if entry := fc.Get(path); entry.Content != "rendered" {
			t.Fatalf("unchanged bytes must keep the cached render, got %+v", entry)
		}
		changed := []byte("one\nTWO\n")
		fc.RecordReadEvidence(path, changed)
		if entry := fc.Get(path); entry.Content != "" || entry.ContentHash != ContentHashOf(changed) {
			t.Fatalf("changed bytes must drop the render and adopt the new hash, got %+v", entry)
		}
	})
}

// UpdateAfterWrite must drop the pre-write rendered output — keeping it made
// the dedup cache replay pre-edit content on the next read.
func TestUpdateAfterWriteDropsStaleRender(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fc := NewFileCache(0)
	info, _ := os.Stat(path)
	data, _ := os.ReadFile(path)
	fc.Set(path, &FileCacheEntry{
		Path: path, MTime: info.ModTime(), Size: info.Size(),
		Content: "rendered-before", ContentHash: ContentHashOf(data),
	})
	if err := os.WriteFile(path, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fc.UpdateAfterWrite(path)
	entry := fc.Get(path)
	if entry == nil || entry.Content != "" {
		t.Fatalf("post-write entry must drop the stale render, got %+v", entry)
	}
	if err := fc.CheckStaleness(path); err != nil {
		t.Fatalf("post-write entry must be a fresh staleness baseline: %v", err)
	}
}
