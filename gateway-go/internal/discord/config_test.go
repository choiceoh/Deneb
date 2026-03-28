package discord

import "testing"

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
