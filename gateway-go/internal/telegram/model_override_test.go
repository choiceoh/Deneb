package telegram

import (
	"encoding/json"
	"testing"
)

func TestResolveChannelModelOverride_NoConfig(t *testing.T) {
	result := ResolveChannelModelOverride(ChannelModelOverrideParams{
		Channel: "telegram",
	})
	if result != nil {
		t.Error("expected nil when no config")
	}
}

func TestResolveChannelModelOverride_EmptyChannel(t *testing.T) {
	result := ResolveChannelModelOverride(ChannelModelOverrideParams{
		RawChannelsConfig: json.RawMessage(`{"modelByChannel":{"telegram":{"*":"claude-3"}}}`),
		Channel:           "",
	})
	if result != nil {
		t.Error("expected nil for empty channel")
	}
}

func TestResolveChannelModelOverride_WildcardMatch(t *testing.T) {
	config := json.RawMessage(`{
		"modelByChannel": {
			"telegram": {
				"*": "claude-sonnet-4-20250514"
			}
		}
	}`)
	result := ResolveChannelModelOverride(ChannelModelOverrideParams{
		RawChannelsConfig: config,
		Channel:           "telegram",
		GroupID:           "12345",
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want claude-sonnet-4-20250514", result.Model)
	}
	if result.MatchKey != "*" {
		t.Errorf("MatchKey = %q, want *", result.MatchKey)
	}
}

func TestResolveChannelModelOverride_ExactGroupMatch(t *testing.T) {
	config := json.RawMessage(`{
		"modelByChannel": {
			"telegram": {
				"chat-123": "gpt-4",
				"*": "claude-sonnet-4-20250514"
			}
		}
	}`)
	result := ResolveChannelModelOverride(ChannelModelOverrideParams{
		RawChannelsConfig: config,
		Channel:           "telegram",
		GroupID:           "chat-123",
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", result.Model)
	}
}

func TestResolveChannelModelOverride_CaseInsensitive(t *testing.T) {
	config := json.RawMessage(`{
		"modelByChannel": {
			"Telegram": {
				"*": "claude-3"
			}
		}
	}`)
	result := ResolveChannelModelOverride(ChannelModelOverrideParams{
		RawChannelsConfig: config,
		Channel:           "telegram",
		GroupID:           "123",
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Model != "claude-3" {
		t.Errorf("Model = %q, want claude-3", result.Model)
	}
}

func TestResolveChannelModelOverride_NoGroupID(t *testing.T) {
	config := json.RawMessage(`{
		"modelByChannel": {
			"telegram": {
				"*": "claude-3"
			}
		}
	}`)
	result := ResolveChannelModelOverride(ChannelModelOverrideParams{
		RawChannelsConfig: config,
		Channel:           "telegram",
	})
	// No group candidates, so wildcard won't match without candidates.
	if result != nil {
		t.Errorf("expected nil when no group ID and no candidates, got %+v", result)
	}
}

func TestBuildChannelKeyCandidates(t *testing.T) {
	keys := buildChannelKeyCandidates("12345", "#general", "Dev Team", "")
	if len(keys) == 0 {
		t.Error("expected at least one candidate key")
	}
	// Should include the groupID.
	found := false
	for _, k := range keys {
		if k == "12345" {
			found = true
		}
	}
	if !found {
		t.Error("expected groupID in candidates")
	}
}

func TestResolveParentGroupID(t *testing.T) {
	tests := []struct {
		groupID string
		want    string
	}{
		{"", ""},
		{"chat-123", ""},
		{"chat-123:thread:abc", "chat-123"},
		{"chat-123:topic:def", "chat-123"},
	}
	for _, tt := range tests {
		got := resolveParentGroupID(tt.groupID)
		if got != tt.want {
			t.Errorf("resolveParentGroupID(%q) = %q, want %q", tt.groupID, got, tt.want)
		}
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"General", "general"},
		{"Dev Team", "dev-team"},
		{"hello_world!", "helloworld"},
		{"café", "caf"},
	}
	for _, tt := range tests {
		got := normalizeSlug(tt.input)
		if got != tt.want {
			t.Errorf("normalizeSlug(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
