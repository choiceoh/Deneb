package config

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestBootstrapGatewayConfigMissingFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	t.Setenv("DENEB_GATEWAY_TOKEN", "")

	result, err := BootstrapGatewayConfig(BootstrapOptions{
		ConfigPath: cfgPath,
		Persist:    false,
	})
	testutil.NoError(t, err)
	// Should auto-generate a token.
	if result.GeneratedToken == "" {
		t.Error("expected auto-generated token for missing config")
	}
	if result.Auth.Mode != "token" {
		t.Errorf("got %q, want auth mode=token", result.Auth.Mode)
	}
	if result.Auth.Token == "" {
		t.Error("expected non-empty resolved token")
	}
}

func TestBootstrapGatewayConfigWithToken(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	t.Setenv("DENEB_GATEWAY_TOKEN", "")
	cfg := map[string]any{
		"gateway": map[string]any{
			"auth": map[string]any{
				"mode":  "token",
				"token": "my-secret-token",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(cfgPath, data, 0644)

	result, err := BootstrapGatewayConfig(BootstrapOptions{
		ConfigPath: cfgPath,
		Persist:    false,
	})
	testutil.NoError(t, err)
	if result.GeneratedToken != "" {
		t.Error("should not generate token when one is configured")
	}
	if result.Auth.Token != "my-secret-token" {
		t.Errorf("got %q, want configured token", result.Auth.Token)
	}
}

func TestBootstrapGatewayConfigTokenFromEnv(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	t.Setenv("DENEB_GATEWAY_TOKEN", "")
	os.WriteFile(cfgPath, []byte("{}"), 0644)
	t.Setenv("DENEB_GATEWAY_TOKEN", "env-token-123")

	result, err := BootstrapGatewayConfig(BootstrapOptions{
		ConfigPath: cfgPath,
		Persist:    false,
	})
	testutil.NoError(t, err)
	if result.Auth.Token != "env-token-123" {
		t.Errorf("got %q, want env token", result.Auth.Token)
	}
	if result.GeneratedToken != "" {
		t.Error("should not generate token when env token exists")
	}
}

func TestBootstrapGatewayConfigPasswordMode(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	t.Setenv("DENEB_GATEWAY_TOKEN", "")
	cfg := map[string]any{
		"gateway": map[string]any{
			"auth": map[string]any{
				"mode":     "password",
				"password": "test-password",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(cfgPath, data, 0644)

	result, err := BootstrapGatewayConfig(BootstrapOptions{
		ConfigPath: cfgPath,
		Persist:    false,
	})
	testutil.NoError(t, err)
	if result.Auth.Mode != "password" {
		t.Errorf("got %q, want auth mode=password", result.Auth.Mode)
	}
	if result.Auth.Password != "test-password" {
		t.Errorf("got %q, want password", result.Auth.Password)
	}
}

func TestBootstrapGatewayConfigPasswordModeNoPassword(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	t.Setenv("DENEB_GATEWAY_TOKEN", "")
	cfg := map[string]any{
		"gateway": map[string]any{
			"auth": map[string]any{
				"mode": "password",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(cfgPath, data, 0644)
	t.Setenv("DENEB_GATEWAY_PASSWORD", "")

	_, err := BootstrapGatewayConfig(BootstrapOptions{
		ConfigPath: cfgPath,
		Persist:    false,
	})
	if err == nil {
		t.Error("expected error for password mode without password")
	}
}

func TestBootstrapPersistToken(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	t.Setenv("DENEB_GATEWAY_TOKEN", "")
	// No config file — should create one.
	t.Setenv("DENEB_GATEWAY_TOKEN", "")

	result, err := BootstrapGatewayConfig(BootstrapOptions{
		ConfigPath: cfgPath,
		Persist:    true,
	})
	testutil.NoError(t, err)
	if !result.PersistedGeneratedToken {
		t.Error("expected token to be persisted")
	}

	// Verify the file was written.
	data := testutil.Must(os.ReadFile(cfgPath))
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatal(err)
	}
	gw, ok := written["gateway"].(map[string]any)
	if !ok {
		t.Fatal("expected gateway in written config")
	}
	auth, ok := gw["auth"].(map[string]any)
	if !ok {
		t.Fatal("expected gateway.auth in written config")
	}
	token, ok := auth["token"].(string)
	if !ok || token == "" {
		t.Error("expected non-empty token in written config")
	}
	if token != result.GeneratedToken {
		t.Error("written token should match generated token")
	}
}

func TestBootstrapAuthOverride(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	t.Setenv("DENEB_GATEWAY_TOKEN", "")
	os.WriteFile(cfgPath, []byte("{}"), 0644)
	t.Setenv("DENEB_GATEWAY_TOKEN", "")

	result, err := BootstrapGatewayConfig(BootstrapOptions{
		ConfigPath: cfgPath,
		Persist:    false,
		AuthOverride: &GatewayAuthConfig{
			Mode:  "token",
			Token: "override-token",
		},
	})
	testutil.NoError(t, err)
	if result.Auth.Token != "override-token" {
		t.Errorf("got %q, want override token", result.Auth.Token)
	}
}

func TestResolveMediaCleanupTTLMs(t *testing.T) {
	tests := []struct {
		hours    int
		expected int64
	}{
		{0, 1 * 60 * 60_000}, // Clamped to 1 hour.
		{1, 1 * 60 * 60_000},
		{24, 24 * 60 * 60_000},
		{200, 168 * 60 * 60_000}, // Clamped to 168 hours.
	}
	for _, tt := range tests {
		ms, err := ResolveMediaCleanupTTLMs(tt.hours)
		if err != nil {
			t.Errorf("hours=%d: unexpected error: %v", tt.hours, err)
		}
		if ms != tt.expected {
			t.Errorf("hours=%d: got %d, want %d", tt.hours, ms, tt.expected)
		}
	}
}

func TestPersistDefaultModel(t *testing.T) {
	logger := slog.Default()

	t.Run("existing config", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		t.Setenv("DENEB_GATEWAY_TOKEN", "")
		existing := map[string]any{
			"gateway": map[string]any{
				"auth": map[string]any{"token": "keep-me"},
			},
		}
		data, _ := json.Marshal(existing)
		os.WriteFile(cfgPath, data, 0644)

		if err := PersistDefaultModel(cfgPath, "zai/glm-5.1", logger); err != nil {
			t.Fatal(err)
		}

		raw := testutil.Must(os.ReadFile(cfgPath))
		var written map[string]any
		if err := json.Unmarshal(raw, &written); err != nil {
			t.Fatal(err)
		}

		// Model was persisted.
		agents, ok := written["agents"].(map[string]any)
		if !ok {
			t.Fatal("expected agents in written config")
		}
		if agents["defaultModel"] != "zai/glm-5.1" {
			t.Errorf("got %v, want defaultModel=zai/glm-5.1", agents["defaultModel"])
		}

		// Existing fields preserved.
		gw, ok := written["gateway"].(map[string]any)
		if !ok {
			t.Fatal("expected gateway preserved")
		}
		auth, ok := gw["auth"].(map[string]any)
		if !ok {
			t.Fatal("expected gateway.auth preserved")
		}
		if auth["token"] != "keep-me" {
			t.Errorf("got %v, want preserved token", auth["token"])
		}

		// Meta timestamp set.
		meta, ok := written["meta"].(map[string]any)
		if !ok {
			t.Fatal("expected meta in written config")
		}
		if meta["lastTouchedAt"] == nil || meta["lastTouchedAt"] == "" {
			t.Error("expected non-empty lastTouchedAt")
		}
	})

	t.Run("no existing file", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		t.Setenv("DENEB_GATEWAY_TOKEN", "")

		if err := PersistDefaultModel(cfgPath, "google/gemini-3.1-pro", logger); err != nil {
			t.Fatal(err)
		}

		raw := testutil.Must(os.ReadFile(cfgPath))
		var written map[string]any
		if err := json.Unmarshal(raw, &written); err != nil {
			t.Fatal(err)
		}
		agents, ok := written["agents"].(map[string]any)
		if !ok {
			t.Fatal("expected agents in config created from scratch")
		}
		if agents["defaultModel"] != "google/gemini-3.1-pro" {
			t.Errorf("got %v, want defaultModel=google/gemini-3.1-pro", agents["defaultModel"])
		}
	})
}

func TestPersistCustomProviderModel(t *testing.T) {
	logger := slog.Default()

	t.Run("creates provider in new config", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")

		result, err := PersistCustomProviderModel(cfgPath, " http://127.0.0.1:8000/v1/ ", " qwen3.6-35b-a3b ", logger)
		if err != nil {
			t.Fatal(err)
		}
		if result.ProviderID != "custom" {
			t.Fatalf("provider = %q, want custom", result.ProviderID)
		}
		if result.FullModelID != "custom/qwen3.6-35b-a3b" {
			t.Errorf("full model = %q, want custom/qwen3.6-35b-a3b", result.FullModelID)
		}
		if result.BaseURL != "http://127.0.0.1:8000/v1" {
			t.Errorf("base URL = %q, want normalized URL", result.BaseURL)
		}
		if !result.Added {
			t.Error("Added = false, want true")
		}

		raw := testutil.Must(os.ReadFile(cfgPath))
		var written map[string]any
		if err := json.Unmarshal(raw, &written); err != nil {
			t.Fatal(err)
		}
		providers := written["models"].(map[string]any)["providers"].(map[string]any)
		custom := providers["custom"].(map[string]any)
		if custom["baseUrl"] != "http://127.0.0.1:8000/v1" {
			t.Errorf("baseUrl = %v, want normalized URL", custom["baseUrl"])
		}
		if custom["api"] != "openai" {
			t.Errorf("api = %v, want openai", custom["api"])
		}
		models := custom["models"].([]any)
		if got := models[0].(map[string]any)["id"]; got != "qwen3.6-35b-a3b" {
			t.Errorf("model id = %v, want qwen3.6-35b-a3b", got)
		}
	})

	t.Run("reuses endpoint provider and keeps newest first", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		existing := map[string]any{
			"models": map[string]any{
				"providers": map[string]any{
					"local": map[string]any{
						"baseUrl": "http://127.0.0.1:8000/v1",
						"apiKey":  "keep-me",
						"models": []any{
							map[string]any{"id": "older-model"},
						},
					},
				},
			},
		}
		data, _ := json.Marshal(existing)
		os.WriteFile(cfgPath, data, 0644)

		result, err := PersistCustomProviderModel(cfgPath, "http://127.0.0.1:8000/v1", "new-model", logger)
		if err != nil {
			t.Fatal(err)
		}
		if result.ProviderID != "local" {
			t.Fatalf("provider = %q, want local", result.ProviderID)
		}
		if result.FullModelID != "local/new-model" {
			t.Errorf("full model = %q, want local/new-model", result.FullModelID)
		}

		raw := testutil.Must(os.ReadFile(cfgPath))
		var written map[string]any
		if err := json.Unmarshal(raw, &written); err != nil {
			t.Fatal(err)
		}
		local := written["models"].(map[string]any)["providers"].(map[string]any)["local"].(map[string]any)
		if local["apiKey"] != "keep-me" {
			t.Errorf("apiKey = %v, want preserved apiKey", local["apiKey"])
		}
		models := local["models"].([]any)
		if got := models[0].(map[string]any)["id"]; got != "new-model" {
			t.Errorf("first model = %v, want new-model", got)
		}
		if got := models[1].(map[string]any)["id"]; got != "older-model" {
			t.Errorf("second model = %v, want older-model", got)
		}
	})

	t.Run("deduplicates model", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		if _, err := PersistCustomProviderModel(cfgPath, "http://localhost:9000/v1", "same-model", logger); err != nil {
			t.Fatal(err)
		}
		result, err := PersistCustomProviderModel(cfgPath, "http://localhost:9000/v1/", "same-model", logger)
		if err != nil {
			t.Fatal(err)
		}
		if result.Added {
			t.Error("Added = true, want false for duplicate")
		}

		raw := testutil.Must(os.ReadFile(cfgPath))
		var written map[string]any
		if err := json.Unmarshal(raw, &written); err != nil {
			t.Fatal(err)
		}
		models := written["models"].(map[string]any)["providers"].(map[string]any)["custom"].(map[string]any)["models"].([]any)
		if len(models) != 1 {
			t.Fatalf("models len = %d, want 1", len(models))
		}
	})

	t.Run("rejects invalid endpoint", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")

		_, err := PersistCustomProviderModel(cfgPath, "127.0.0.1:8000/v1", "model", logger)
		if !errors.Is(err, ErrInvalidCustomModel) {
			t.Fatalf("error = %v, want ErrInvalidCustomModel", err)
		}
	})
}
