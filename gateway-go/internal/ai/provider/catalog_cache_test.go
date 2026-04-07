package provider

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogCache_InitiallyStale(t *testing.T) {
	cc := NewCatalogCache("", "", nil)
	if !cc.IsStale() {
		t.Error("expected new cache to be stale")
	}
}

func TestCatalogCache_GetAfterUpdate(t *testing.T) {
	cc := NewCatalogCache("", "", nil)
	models := map[string]ModelMeta{
		"claude-opus-4-6": {ID: "claude-opus-4-6", Provider: "anthropic", ContextTokens: 200000},
	}
	cc.Update(models)

	m := cc.Get("claude-opus-4-6")
	if m == nil {
		t.Fatal("expected model to be cached")
	}
	if m.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", m.Provider)
	}
}

func TestCatalogCache_StaleAfterFileChange(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	os.WriteFile(configPath, []byte(`{}`), 0644)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cc := NewCatalogCache(configPath, "", logger)

	// Populate cache.
	cc.Update(map[string]ModelMeta{"m1": {ID: "m1"}})
	if cc.IsStale() {
		t.Error("expected cache to be fresh after update")
	}

	// Modify config file.
	os.WriteFile(configPath, []byte(`{"changed": true}`), 0644)
	if !cc.IsStale() {
		t.Error("expected cache to be stale after file change")
	}
}

func TestCatalogCache_ForceRefresh(t *testing.T) {
	cc := NewCatalogCache("", "", nil)
	cc.Update(map[string]ModelMeta{"m1": {ID: "m1"}})
	cc.ForceRefresh()
	if !cc.IsStale() {
		t.Error("expected cache to be stale after force refresh")
	}
}

func TestCatalogCache_Count(t *testing.T) {
	cc := NewCatalogCache("", "", nil)
	if cc.Count() != 0 {
		t.Error("expected 0 count for empty cache")
	}
	cc.Update(map[string]ModelMeta{"m1": {ID: "m1"}, "m2": {ID: "m2"}})
	if cc.Count() != 2 {
		t.Errorf("expected count 2, got %d", cc.Count())
	}
}
