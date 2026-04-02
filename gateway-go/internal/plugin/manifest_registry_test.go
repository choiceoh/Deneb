package plugin

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestIsCompatiblePluginIDHint(t *testing.T) {
	tests := []struct {
		idHint     string
		manifestID string
		want       bool
	}{
		{"voice-call", "voice-call", true},
		{"voice-call", "voice-call-provider", true},
		{"voice-call-provider", "voice-call", true},
		{"voice-call", "voice-call-plugin", true},
		{"voice-call-plugin", "voice-call", true},
		{"voice-call", "voice-call-sandbox", true},
		{"voice-call", "other-plugin", false},
		{"foo", "bar", false},
	}
	for _, tt := range tests {
		got := IsCompatiblePluginIDHint(tt.idHint, tt.manifestID)
		if got != tt.want {
			t.Errorf("IsCompatiblePluginIDHint(%q, %q) = %v, want %v",
				tt.idHint, tt.manifestID, got, tt.want)
		}
	}
}

func TestNormalizeManifestLabel(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"  My Plugin  ", "My Plugin"},
		{"", ""},
		{"  ", ""},
		{"No Trim", "No Trim"},
	}
	for _, tt := range tests {
		got := NormalizeManifestLabel(tt.raw)
		if got != tt.want {
			t.Errorf("NormalizeManifestLabel(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestFullManifestRegistryLoad(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "test-plugin")
	os.MkdirAll(pluginDir, 0o755)
	pkg := `{
		"name": "test-plugin",
		"version": "2.0.0",
		"description": "A test plugin",
		"deneb": {
			"plugin": {
				"id": "test-plugin",
				"channels": ["telegram"],
				"providers": ["openai"]
			}
		}
	}`
	os.WriteFile(filepath.Join(pluginDir, "package.json"), []byte(pkg), 0o644)
	os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte("// test"), 0o644)

	logger := slog.Default()
	discoverer := NewPluginDiscoverer(logger)
	registry := NewFullManifestRegistry(discoverer, logger)

	result := registry.Load(LoadManifestRegistryParams{
		Roots:        PluginSourceRoots{Global: tmpDir},
		OwnershipUID: -1,
		NoCache:      true,
	})

	if len(result.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(result.Plugins))
	}

	rec := result.Plugins[0]
	if rec.ID != "test-plugin" {
		t.Errorf("expected ID 'test-plugin', got %q", rec.ID)
	}
	if rec.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", rec.Version)
	}
	if len(rec.Channels) != 1 || rec.Channels[0] != "telegram" {
		t.Errorf("expected channels [telegram], got %v", rec.Channels)
	}
	if len(rec.Providers) != 1 || rec.Providers[0] != "openai" {
		t.Errorf("expected providers [openai], got %v", rec.Providers)
	}

	// Test GetRecord.
	retrieved := registry.GetRecord("test-plugin")
	if retrieved == nil {
		t.Fatal("GetRecord returned nil")
	}
	if retrieved.ID != "test-plugin" {
		t.Errorf("GetRecord ID = %q, want 'test-plugin'", retrieved.ID)
	}

	// Test RecordCount.
	if registry.RecordCount() != 1 {
		t.Errorf("RecordCount = %d, want 1", registry.RecordCount())
	}
}

func TestFullManifestRegistryDeduplication(t *testing.T) {
	// Create two roots with the same plugin ID.
	workDir := t.TempDir()
	globalDir := t.TempDir()

	writePlugin := func(dir, id, version string) {
		pluginDir := filepath.Join(dir, id)
		os.MkdirAll(pluginDir, 0o755)
		pkg := `{
			"name": "` + id + `",
			"version": "` + version + `",
			"deneb": { "plugin": { "id": "` + id + `" } }
		}`
		os.WriteFile(filepath.Join(pluginDir, "package.json"), []byte(pkg), 0o644)
		os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte("// "+version), 0o644)
	}

	// Same ID in workspace (v1) and global (v2).
	writePlugin(workDir, "shared-plugin", "1.0.0")
	writePlugin(globalDir, "shared-plugin", "2.0.0")

	logger := slog.Default()
	discoverer := NewPluginDiscoverer(logger)
	registry := NewFullManifestRegistry(discoverer, logger)

	result := registry.Load(LoadManifestRegistryParams{
		Roots: PluginSourceRoots{
			Workspace: workDir,
			Global:    globalDir,
		},
		WorkspaceDir: workDir,
		OwnershipUID: -1,
		NoCache:      true,
	})

	// Should be deduplicated to 1 (workspace wins).
	if len(result.Plugins) != 1 {
		t.Fatalf("expected 1 plugin (deduped), got %d", len(result.Plugins))
	}
	if result.Plugins[0].Origin != OriginWorkspace {
		t.Errorf("expected workspace origin to win, got %q", result.Plugins[0].Origin)
	}
}

func TestFullManifestRegistryCaching(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "cached")
	os.MkdirAll(pluginDir, 0o755)
	os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte("// x"), 0o644)

	logger := slog.Default()
	discoverer := NewPluginDiscoverer(logger)
	registry := NewFullManifestRegistry(discoverer, logger)

	params := LoadManifestRegistryParams{
		Roots:        PluginSourceRoots{Global: tmpDir},
		OwnershipUID: -1,
	}

	r1 := registry.Load(params)
	r2 := registry.Load(params)
	if r1 != r2 {
		t.Error("expected same cached result on second call")
	}

	registry.ClearCache()
	r3 := registry.Load(params)
	if r3 == r1 {
		t.Error("expected different result after cache clear")
	}
}
