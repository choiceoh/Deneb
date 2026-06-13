package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestHideModel verifies soft-hiding a cloud-catalog model: it lands in
// models.hiddenModels, clears any role that pointed at it, is visible to
// LoadHiddenModels, and is idempotent (re-hiding does not duplicate).
func TestHideModel(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "deneb.json")
	seed := `{"models":{"providers":{"openrouter":{"baseUrl":"https://openrouter.ai/api/v1","api":"openai"}}},` +
		`"agents":{"fallbackModel":"openrouter/anthropic/claude-opus-4.7"}}`
	if err := os.WriteFile(cfg, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	id := "openrouter/anthropic/claude-opus-4.7"

	res, err := HideModel(cfg, id, nil)
	if err != nil {
		t.Fatalf("HideModel: %v", err)
	}
	if !res.Hidden {
		t.Fatalf("expected Hidden=true")
	}
	if len(res.ClearedRoles) != 1 || res.ClearedRoles[0] != "fallback" {
		t.Fatalf("expected [fallback] cleared, got %v", res.ClearedRoles)
	}

	if set := LoadHiddenModels(cfg); !set[id] {
		t.Fatalf("LoadHiddenModels missing %s: %v", id, set)
	}

	var raw map[string]any
	data, _ := os.ReadFile(cfg)
	_ = json.Unmarshal(data, &raw)
	if agents, _ := raw["agents"].(map[string]any); agents["fallbackModel"] != nil {
		t.Fatalf("fallbackModel should have been cleared")
	}

	// Idempotent: hiding again must not duplicate the entry.
	if _, err := HideModel(cfg, id, nil); err != nil {
		t.Fatalf("HideModel (repeat): %v", err)
	}
	data, _ = os.ReadFile(cfg)
	_ = json.Unmarshal(data, &raw)
	models, _ := raw["models"].(map[string]any)
	if hidden, _ := models["hiddenModels"].([]any); len(hidden) != 1 {
		t.Fatalf("expected 1 hidden entry after re-hide, got %v", hidden)
	}
}

// TestHideModelRejectsBadID confirms ill-formed ids are rejected, never written.
func TestHideModelRejectsBadID(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "deneb.json")
	for _, bad := range []string{"", "   ", "noslash", "/leading", "trailing/"} {
		if _, err := HideModel(cfg, bad, nil); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Fatalf("rejected ids must not create the config file")
	}
}

// TestLoadHiddenModelsEmpty covers the nil-safe paths.
func TestLoadHiddenModelsEmpty(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "deneb.json")
	if set := LoadHiddenModels(cfg); set != nil {
		t.Fatalf("missing file should yield nil, got %v", set)
	}
	_ = os.WriteFile(cfg, []byte(`{"models":{}}`), 0o600)
	if set := LoadHiddenModels(cfg); len(set) != 0 {
		t.Fatalf("absent hiddenModels should yield empty, got %v", set)
	}
}
