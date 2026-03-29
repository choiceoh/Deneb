package discord

import (
	"strings"
	"testing"
)

func TestConfig_IsEnabled(t *testing.T) {
	// Default: enabled.
	cfg := &Config{}
	if !cfg.IsEnabled() {
		t.Error("expected enabled by default")
	}

	// Explicit true.
	enabled := true
	cfg.Enabled = &enabled
	if !cfg.IsEnabled() {
		t.Error("expected enabled when explicitly true")
	}

	// Explicit false.
	disabled := false
	cfg.Enabled = &disabled
	if cfg.IsEnabled() {
		t.Error("expected disabled when explicitly false")
	}
}

func TestConfig_IsChannelAllowed(t *testing.T) {
	// No allowlist: allow all.
	cfg := &Config{}
	if !cfg.IsChannelAllowed("123") {
		t.Error("expected allowed when no allowlist")
	}

	// With allowlist.
	cfg.AllowedChannels = []string{"111", "222"}
	if !cfg.IsChannelAllowed("111") {
		t.Error("expected 111 allowed")
	}
	if cfg.IsChannelAllowed("333") {
		t.Error("expected 333 not allowed")
	}
}

func TestConfig_IsUserAllowed(t *testing.T) {
	// No allowlist: allow all.
	cfg := &Config{}
	if !cfg.IsUserAllowed("user1") {
		t.Error("expected allowed when no allowlist")
	}

	// With allowlist.
	cfg.AllowFrom = []string{"user1", "user2"}
	if !cfg.IsUserAllowed("user1") {
		t.Error("expected user1 allowed")
	}
	if cfg.IsUserAllowed("user3") {
		t.Error("expected user3 not allowed")
	}
}

func TestConfig_IsGuildAllowed(t *testing.T) {
	// No guild scope: allow all guilds/DMs.
	cfg := &Config{}
	if !cfg.IsGuildAllowed("") {
		t.Error("expected DMs allowed when no guildId configured")
	}
	if !cfg.IsGuildAllowed("12345678901234567") {
		t.Error("expected guild allowed when no guildId configured")
	}

	// Guild scope configured: only exact guild is allowed.
	cfg.GuildID = "12345678901234567"
	if !cfg.IsGuildAllowed("12345678901234567") {
		t.Error("expected configured guild allowed")
	}
	if cfg.IsGuildAllowed("99999999999999999") {
		t.Error("expected non-configured guild blocked")
	}
	if cfg.IsGuildAllowed("") {
		t.Error("expected DMs blocked when guildId is configured")
	}
}

func TestConfig_Validate(t *testing.T) {
	// Missing token.
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing token")
	}

	// Short token.
	cfg.BotToken = "short"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("expected 'too short' error, got %v", err)
	}

	// Valid token.
	cfg.BotToken = strings.Repeat("x", 70)
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}

	// Invalid guild ID.
	cfg.GuildID = "123"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "guildId") {
		t.Errorf("expected guildId error, got %v", err)
	}

	// Valid guild ID.
	cfg.GuildID = "12345678901234567"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}

	// Invalid channel ID in allowlist.
	cfg.AllowedChannels = []string{"bad"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "allowedChannels") {
		t.Errorf("expected allowedChannels error, got %v", err)
	}

	// Valid channel ID.
	cfg.AllowedChannels = []string{"12345678901234567"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestConfig_WorkspaceForChannel(t *testing.T) {
	cfg := &Config{}

	// No workspaces configured: empty.
	if ws := cfg.WorkspaceForChannel("123"); ws != "" {
		t.Errorf("expected empty, got %q", ws)
	}

	// DefaultWorkspace fallback.
	cfg.DefaultWorkspace = "/home/user/default"
	if ws := cfg.WorkspaceForChannel("123"); ws != "/home/user/default" {
		t.Errorf("expected default workspace, got %q", ws)
	}

	// Explicit channel mapping.
	cfg.Workspaces = map[string]string{
		"456": "/home/user/backend",
		"789": "/home/user/frontend",
	}
	if ws := cfg.WorkspaceForChannel("456"); ws != "/home/user/backend" {
		t.Errorf("expected backend workspace, got %q", ws)
	}
	if ws := cfg.WorkspaceForChannel("789"); ws != "/home/user/frontend" {
		t.Errorf("expected frontend workspace, got %q", ws)
	}
	// Unmapped channel falls back to default.
	if ws := cfg.WorkspaceForChannel("999"); ws != "/home/user/default" {
		t.Errorf("expected default workspace for unmapped channel, got %q", ws)
	}
}
