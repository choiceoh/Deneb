package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStateDirDefault(t *testing.T) {
	// Unset overrides for this test.
	t.Setenv("DENEB_STATE_DIR", "")
	t.Setenv("CLAWDBOT_STATE_DIR", "")

	dir := ResolveStateDir()
	if dir == "" {
		t.Fatal("expected non-empty state dir")
	}
	// Should end with .deneb or a legacy dirname.
	base := filepath.Base(dir)
	valid := base == ".deneb" || base == ".clawdbot" || base == ".moldbot" || base == ".moltbot"
	if !valid {
		t.Errorf("unexpected state dir basename: %q", base)
	}
}

func TestResolveStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", tmp)
	t.Setenv("CLAWDBOT_STATE_DIR", "")

	dir := ResolveStateDir()
	if dir != tmp {
		t.Errorf("expected %q, got %q", tmp, dir)
	}
}

func TestResolveConfigPathOverride(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "custom.json")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_CONFIG_PATH", cfgPath)
	t.Setenv("CLAWDBOT_CONFIG_PATH", "")

	got := ResolveConfigPath()
	if got != cfgPath {
		t.Errorf("expected %q, got %q", cfgPath, got)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "nonexistent.json")

	snap, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Exists {
		t.Error("expected Exists=false for missing file")
	}
	if !snap.Valid {
		t.Error("expected Valid=true for missing file (defaults apply)")
	}
	// Defaults should be applied.
	if snap.Config.Gateway == nil {
		t.Fatal("expected gateway defaults")
	}
	if snap.Config.Gateway.Bind != "loopback" {
		t.Errorf("expected bind=loopback, got %q", snap.Config.Gateway.Bind)
	}
	if snap.Config.Gateway.Auth == nil || snap.Config.Gateway.Auth.Mode != "token" {
		t.Error("expected auth.mode=token default")
	}
	if snap.Config.Gateway.Port == nil || *snap.Config.Gateway.Port != DefaultGatewayPort {
		t.Errorf("expected port=%d", DefaultGatewayPort)
	}
}

func TestLoadConfigValid(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")

	port := 19999
	cfg := map[string]any{
		"gateway": map[string]any{
			"port": port,
			"bind": "lan",
			"auth": map[string]any{
				"mode":  "password",
				"password": "test-pw",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	snap, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Exists {
		t.Error("expected Exists=true")
	}
	if !snap.Valid {
		t.Errorf("expected Valid=true, issues: %v", snap.Issues)
	}
	if snap.Config.Gateway.Port == nil || *snap.Config.Gateway.Port != port {
		t.Errorf("expected port=%d", port)
	}
	if snap.Config.Gateway.Bind != "lan" {
		t.Errorf("expected bind=lan, got %q", snap.Config.Gateway.Bind)
	}
	if snap.Config.Gateway.Auth.Mode != "password" {
		t.Errorf("expected auth.mode=password, got %q", snap.Config.Gateway.Auth.Mode)
	}
	if snap.Config.Gateway.Auth.Password != "test-pw" {
		t.Errorf("expected password=test-pw")
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	if err := os.WriteFile(cfgPath, []byte("{invalid json}"), 0644); err != nil {
		t.Fatal(err)
	}

	snap, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Valid {
		t.Error("expected Valid=false for invalid JSON")
	}
	if len(snap.Issues) == 0 {
		t.Error("expected issues for invalid JSON")
	}
}

func TestLoadConfigInvalidBindMode(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "deneb.json")
	cfg := map[string]any{
		"gateway": map[string]any{
			"bind": "invalid-mode",
		},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	snap, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Valid {
		t.Error("expected Valid=false for invalid bind mode")
	}
}

func TestResolveGatewayPort(t *testing.T) {
	// Default.
	port := ResolveGatewayPort(nil)
	if port != DefaultGatewayPort {
		t.Errorf("expected %d, got %d", DefaultGatewayPort, port)
	}

	// Config override.
	p := 12345
	cfg := &DenebConfig{Gateway: &GatewayConfig{Port: &p}}
	port = ResolveGatewayPort(cfg)
	if port != 12345 {
		t.Errorf("expected 12345, got %d", port)
	}

	// Env override takes precedence.
	t.Setenv("DENEB_GATEWAY_PORT", "54321")
	t.Setenv("CLAWDBOT_GATEWAY_PORT", "")
	port = ResolveGatewayPort(cfg)
	if port != 54321 {
		t.Errorf("expected 54321, got %d", port)
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)

	if cfg.Gateway == nil {
		t.Fatal("gateway should not be nil")
	}
	if cfg.Gateway.Auth == nil || cfg.Gateway.Auth.Mode != "token" {
		t.Error("auth mode should default to token")
	}
	if cfg.Gateway.Bind != "loopback" {
		t.Errorf("bind should default to loopback, got %q", cfg.Gateway.Bind)
	}
	if cfg.Session == nil || cfg.Session.MainKey != "main" {
		t.Error("session.mainKey should default to main")
	}
	if cfg.Agents == nil || cfg.Agents.MaxConcurrent == nil || *cfg.Agents.MaxConcurrent != 8 {
		t.Error("agents.maxConcurrent should default to 8")
	}
	if cfg.Logging == nil || cfg.Logging.RedactSensitive != "tools" {
		t.Error("logging.redactSensitive should default to tools")
	}
}

func TestHashRaw(t *testing.T) {
	h1 := hashRaw(nil)
	h2 := hashRaw([]byte("hello"))
	h3 := hashRaw([]byte("hello"))

	if h1 == h2 {
		t.Error("nil and non-nil should have different hashes")
	}
	if h2 != h3 {
		t.Error("same input should produce same hash")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(h1))
	}
}
