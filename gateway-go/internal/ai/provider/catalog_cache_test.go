package provider

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)


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
		t.Errorf("got %q, want provider 'anthropic'", m.Provider)
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


