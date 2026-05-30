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

func TestDeleteCustomProviderModel(t *testing.T) {
	logger := slog.Default()

	// providerModels reads back the model IDs persisted under a provider key.
	providerModels := func(t *testing.T, cfgPath, provider string) []string {
		t.Helper()
		var written map[string]any
		if err := json.Unmarshal(testutil.Must(os.ReadFile(cfgPath)), &written); err != nil {
			t.Fatal(err)
		}
		models, _ := written["models"].(map[string]any)
		providers, _ := models["providers"].(map[string]any)
		pc, ok := providers[provider].(map[string]any)
		if !ok {
			return nil
		}
		arr, _ := pc["models"].([]any)
		var ids []string
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				ids = append(ids, m["id"].(string))
			}
		}
		return ids
	}
	providerExists := func(t *testing.T, cfgPath, provider string) bool {
		t.Helper()
		var written map[string]any
		if err := json.Unmarshal(testutil.Must(os.ReadFile(cfgPath)), &written); err != nil {
			t.Fatal(err)
		}
		models, _ := written["models"].(map[string]any)
		providers, _ := models["providers"].(map[string]any)
		_, ok := providers[provider]
		return ok
	}

	t.Run("removes one model and keeps the rest", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		if _, err := PersistCustomProviderModel(cfgPath, "http://127.0.0.1:8000/v1", "keep-me", logger); err != nil {
			t.Fatal(err)
		}
		if _, err := PersistCustomProviderModel(cfgPath, "http://127.0.0.1:8000/v1", "typo-model", logger); err != nil {
			t.Fatal(err)
		}

		res, err := DeleteCustomProviderModel(cfgPath, "custom/typo-model", logger)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Removed {
			t.Error("Removed = false, want true")
		}
		if res.ProviderDropped {
			t.Error("ProviderDropped = true, want false (provider still has a model)")
		}
		got := providerModels(t, cfgPath, "custom")
		if len(got) != 1 || got[0] != "keep-me" {
			t.Errorf("remaining models = %v, want [keep-me]", got)
		}
	})

	t.Run("drops provider when last model removed", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		if _, err := PersistCustomProviderModel(cfgPath, "http://127.0.0.1:8000/v1", "only-model", logger); err != nil {
			t.Fatal(err)
		}

		res, err := DeleteCustomProviderModel(cfgPath, "custom/only-model", logger)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Removed || !res.ProviderDropped {
			t.Errorf("Removed=%v ProviderDropped=%v, want both true", res.Removed, res.ProviderDropped)
		}
		if providerExists(t, cfgPath, "custom") {
			t.Error("custom provider still present, want dropped")
		}
	})

	t.Run("clears role binding pointing at the deleted model", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		if _, err := PersistCustomProviderModel(cfgPath, "http://127.0.0.1:8000/v1", "bad-main", logger); err != nil {
			t.Fatal(err)
		}
		// Bind the freshly-added model to main + lightweight, like the picker does.
		if err := PersistRoleModel(cfgPath, "main", "custom/bad-main", logger); err != nil {
			t.Fatal(err)
		}
		if err := PersistRoleModel(cfgPath, "lightweight", "custom/bad-main", logger); err != nil {
			t.Fatal(err)
		}

		res, err := DeleteCustomProviderModel(cfgPath, "custom/bad-main", logger)
		if err != nil {
			t.Fatal(err)
		}
		wantRoles := map[string]bool{"main": true, "lightweight": true}
		if len(res.ClearedRoles) != 2 {
			t.Fatalf("ClearedRoles = %v, want main+lightweight", res.ClearedRoles)
		}
		for _, r := range res.ClearedRoles {
			if !wantRoles[r] {
				t.Errorf("unexpected cleared role %q", r)
			}
		}

		var written map[string]any
		if err := json.Unmarshal(testutil.Must(os.ReadFile(cfgPath)), &written); err != nil {
			t.Fatal(err)
		}
		agents, _ := written["agents"].(map[string]any)
		if _, present := agents["defaultModel"]; present {
			t.Error("agents.defaultModel still present, want cleared")
		}
		if _, present := agents["lightweightModel"]; present {
			t.Error("agents.lightweightModel still present, want cleared")
		}
	})

	t.Run("rejects non-custom provider", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		if _, err := PersistCustomProviderModel(cfgPath, "http://127.0.0.1:8000/v1", "m", logger); err != nil {
			t.Fatal(err)
		}

		_, err := DeleteCustomProviderModel(cfgPath, "zai/glm-5.1", logger)
		if !errors.Is(err, ErrInvalidCustomModel) {
			t.Fatalf("error = %v, want ErrInvalidCustomModel", err)
		}
	})

	t.Run("rejects malformed id", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")

		for _, id := range []string{"", "  ", "no-slash", "custom/", "/model"} {
			if _, err := DeleteCustomProviderModel(cfgPath, id, logger); !errors.Is(err, ErrInvalidCustomModel) {
				t.Errorf("id %q: error = %v, want ErrInvalidCustomModel", id, err)
			}
		}
	})

	t.Run("idempotent when model is absent", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")
		if _, err := PersistCustomProviderModel(cfgPath, "http://127.0.0.1:8000/v1", "present", logger); err != nil {
			t.Fatal(err)
		}

		res, err := DeleteCustomProviderModel(cfgPath, "custom/missing", logger)
		if err != nil {
			t.Fatal(err)
		}
		if res.Removed {
			t.Error("Removed = true, want false for absent model")
		}
		// The existing model must be untouched.
		if got := providerModels(t, cfgPath, "custom"); len(got) != 1 || got[0] != "present" {
			t.Errorf("models = %v, want [present]", got)
		}
	})

	t.Run("idempotent when config does not exist", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "deneb.json")

		res, err := DeleteCustomProviderModel(cfgPath, "custom/anything", logger)
		if err != nil {
			t.Fatalf("error = %v, want nil for missing config", err)
		}
		if res.Removed {
			t.Error("Removed = true, want false")
		}
	})
}
