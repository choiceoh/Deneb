package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestResolveStateDirDefault(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", "")

	dir := ResolveStateDir()
	if dir == "" {
		t.Fatal("expected non-empty state dir")
	}
	if filepath.Base(dir) != ".deneb" {
		t.Errorf("unexpected state dir basename: %q", filepath.Base(dir))
	}
}

func TestResolveStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", tmp)

	dir := ResolveStateDir()
	if dir != tmp {
		t.Errorf("got %q, want %q", dir, tmp)
	}
}

func TestResolveConfigPathOverride(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "custom.json")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_CONFIG_PATH", cfgPath)

	got := ResolveConfigPath()
	if got != cfgPath {
		t.Errorf("got %q, want %q", got, cfgPath)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "nonexistent.json")

	snap := testutil.Must(LoadConfig(cfgPath))
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
		t.Errorf("got %q, want bind=loopback", snap.Config.Gateway.Bind)
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
				"mode":     "password",
				"password": "test-pw",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	snap := testutil.Must(LoadConfig(cfgPath))
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
		t.Errorf("got %q, want bind=lan", snap.Config.Gateway.Bind)
	}
	if snap.Config.Gateway.Auth.Mode != "password" {
		t.Errorf("got %q, want auth.mode=password", snap.Config.Gateway.Auth.Mode)
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

	snap := testutil.Must(LoadConfig(cfgPath))
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

	snap := testutil.Must(LoadConfig(cfgPath))
	if snap.Valid {
		t.Error("expected Valid=false for invalid bind mode")
	}
}

func TestResolveGatewayPort(t *testing.T) {
	// Default.
	port := ResolveGatewayPort(nil)
	if port != DefaultGatewayPort {
		t.Errorf("got %d, want %d", port, DefaultGatewayPort)
	}

	// Config override.
	p := 12345
	cfg := &DenebConfig{Gateway: &GatewayConfig{Port: &p}}
	port = ResolveGatewayPort(cfg)
	if port != 12345 {
		t.Errorf("got %d, want 12345", port)
	}

	// Env override takes precedence.
	t.Setenv("DENEB_GATEWAY_PORT", "54321")
	port = ResolveGatewayPort(cfg)
	if port != 54321 {
		t.Errorf("got %d, want 54321", port)
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

func TestResolveAgentWorkspaceDirNilConfig(t *testing.T) {
	t.Setenv("DENEB_PROFILE", "")
	dir := ResolveAgentWorkspaceDir(nil)
	if dir == "" {
		t.Fatal("expected non-empty workspace dir")
	}
	if filepath.Base(dir) != "workspace" {
		t.Errorf("got %q, want basename 'workspace'", filepath.Base(dir))
	}
	// Should be under ~/.deneb/workspace.
	parent := filepath.Base(filepath.Dir(dir))
	if parent != ".deneb" {
		t.Errorf("got %q, want parent '.deneb'", parent)
	}
}

func TestResolveAgentWorkspaceDirFromDefaults(t *testing.T) {
	cfg := &DenebConfig{
		Agents: &AgentsConfig{
			Defaults: &AgentsDefaultsConfig{
				Workspace: "/srv/my-workspace",
			},
		},
	}
	dir := ResolveAgentWorkspaceDir(cfg)
	if dir != "/srv/my-workspace" {
		t.Errorf("got %q, want /srv/my-workspace", dir)
	}
}

func TestResolveAgentWorkspaceDirFromAgentList(t *testing.T) {
	isDefault := true
	cfg := &DenebConfig{
		Agents: &AgentsConfig{
			Defaults: &AgentsDefaultsConfig{
				Workspace: "/srv/default-workspace",
			},
			List: []AgentEntryConfig{
				{ID: "main", Default: &isDefault, Workspace: "/srv/main-workspace"},
			},
		},
	}
	dir := ResolveAgentWorkspaceDir(cfg)
	// Per-agent with default=true takes precedence over agents.defaults.workspace.
	if dir != "/srv/main-workspace" {
		t.Errorf("got %q, want /srv/main-workspace", dir)
	}
}

func TestResolveAgentWorkspaceDirProfile(t *testing.T) {
	t.Setenv("DENEB_PROFILE", "work")
	dir := ResolveAgentWorkspaceDir(nil)
	if filepath.Base(dir) != "workspace-work" {
		t.Errorf("got %q, want basename 'workspace-work'", filepath.Base(dir))
	}
}

func TestResolveAgentWorkspaceDirHomeTilde(t *testing.T) {
	cfg := &DenebConfig{
		Agents: &AgentsConfig{
			Defaults: &AgentsDefaultsConfig{
				Workspace: "~/my-workspace",
			},
		},
	}
	dir := ResolveAgentWorkspaceDir(cfg)
	if dir == "~/my-workspace" {
		t.Error("expected ~ to be expanded")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("got %q, want absolute path", dir)
	}
}

