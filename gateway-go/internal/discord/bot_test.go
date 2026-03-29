package discord

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

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

func TestBot_handleDispatch_EnforcesGuildID(t *testing.T) {
	called := make(chan struct{}, 1)
	b := &Bot{
		config:        &Config{GuildID: "12345678901234567"},
		threadParents: make(map[string]string),
		handler: func(_ context.Context, _ *Message) {
			called <- struct{}{}
		},
	}

	msg := Message{
		ID:        "m1",
		ChannelID: "33333333333333333",
		GuildID:   "99999999999999999",
		Content:   "hello",
		Author:    &User{ID: "u1"},
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	b.handleDispatch(context.Background(), &GatewayPayload{T: "MESSAGE_CREATE", D: raw})
	select {
	case <-called:
		t.Error("expected handler not called for mismatched guild")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBot_handleDispatch_AllowsConfiguredGuild(t *testing.T) {
	called := make(chan struct{}, 1)
	b := &Bot{
		config:        &Config{GuildID: "12345678901234567"},
		threadParents: make(map[string]string),
		handler: func(_ context.Context, _ *Message) {
			called <- struct{}{}
		},
	}

	msg := Message{
		ID:        "m1",
		ChannelID: "33333333333333333",
		GuildID:   "12345678901234567",
		Content:   "hello",
		Author:    &User{ID: "u1"},
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	b.handleDispatch(context.Background(), &GatewayPayload{T: "MESSAGE_CREATE", D: raw})
	select {
	case <-called:
	case <-time.After(200 * time.Millisecond):
		t.Error("expected handler called for configured guild")
	}
}
