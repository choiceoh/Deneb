package discord

import "testing"

func TestBot_isChannelOrThreadAllowed(t *testing.T) {
	cfg := &Config{
		AllowedChannels: []string{"12345678901234567"},
	}
	b := &Bot{
		config:        cfg,
		threadParents: make(map[string]string),
	}

	// Direct channel match.
	if !b.isChannelOrThreadAllowed("12345678901234567") {
		t.Error("expected allowed channel to pass")
	}

	// Unknown channel: not allowed.
	if b.isChannelOrThreadAllowed("99999999999999999") {
		t.Error("expected unknown channel to fail")
	}

	// Thread with parent in allowlist.
	b.threadParents["thread-111"] = "12345678901234567"
	if !b.isChannelOrThreadAllowed("thread-111") {
		t.Error("expected thread with allowed parent to pass")
	}

	// Thread with parent NOT in allowlist.
	b.threadParents["thread-222"] = "88888888888888888"
	if b.isChannelOrThreadAllowed("thread-222") {
		t.Error("expected thread with non-allowed parent to fail")
	}
}

func TestBot_isChannelOrThreadAllowed_NoAllowlist(t *testing.T) {
	cfg := &Config{} // No allowlist = allow all
	b := &Bot{
		config:        cfg,
		threadParents: make(map[string]string),
	}

	if !b.isChannelOrThreadAllowed("anything") {
		t.Error("expected all channels allowed when no allowlist")
	}
}
