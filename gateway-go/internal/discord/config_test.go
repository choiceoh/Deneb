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
