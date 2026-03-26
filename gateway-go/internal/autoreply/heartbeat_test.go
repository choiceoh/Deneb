package autoreply

import "testing"

func TestIsHeartbeatContentEffectivelyEmpty(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty string", "", true},
		{"whitespace only", "   \n  \n  ", true},
		{"headers only", "# Tasks\n## Section\n### Sub", true},
		{"empty list items", "- [ ]\n* [ ]\n+ ", true},
		{"with actionable content", "# Tasks\n- Buy groceries", false},
		{"hashtag not header", "#hashtag", false},
		{"header with content", "# Tasks\nDo something", false},
		{"mixed empty", "# Title\n\n- [ ]\n\n## Empty\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsHeartbeatContentEffectivelyEmpty(tt.content)
			if got != tt.want {
				t.Errorf("IsHeartbeatContentEffectivelyEmpty(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestResolveHeartbeatPrompt(t *testing.T) {
	if got := ResolveHeartbeatPrompt(""); got != HeartbeatPrompt {
		t.Errorf("empty input should return default prompt")
	}
	if got := ResolveHeartbeatPrompt("  "); got != HeartbeatPrompt {
		t.Errorf("whitespace input should return default prompt")
	}
	custom := "Check the weather"
	if got := ResolveHeartbeatPrompt(custom); got != custom {
		t.Errorf("custom prompt should be returned as-is")
	}
}

func TestStripHeartbeatToken(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		mode      StripHeartbeatMode
		maxAck    int
		wantSkip  bool
		wantText  string
		wantStrip bool
	}{
		{"empty", "", StripModeMessage, 0, true, "", false},
		{"only token", "HEARTBEAT_OK", StripModeMessage, 0, true, "", true},
		{"token with trailing text", "HEARTBEAT_OK All good!", StripModeMessage, 0, false, "All good!", true},
		{"no token present", "Everything is fine", StripModeMessage, 0, false, "Everything is fine", false},
		{"heartbeat mode short ack", "HEARTBEAT_OK short", StripModeHeartbeat, 300, true, "", true},
		{"heartbeat mode long ack", "HEARTBEAT_OK " + string(make([]byte, 400)), StripModeHeartbeat, 300, false, "", true},
		{"token at end", "Status update HEARTBEAT_OK", StripModeMessage, 0, false, "Status update", true},
		{"token with punctuation", "HEARTBEAT_OK.", StripModeHeartbeat, 300, true, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripHeartbeatToken(tt.raw, tt.mode, tt.maxAck)
			if got.ShouldSkip != tt.wantSkip {
				t.Errorf("ShouldSkip = %v, want %v", got.ShouldSkip, tt.wantSkip)
			}
			if tt.wantText != "" && got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			if got.DidStrip != tt.wantStrip {
				t.Errorf("DidStrip = %v, want %v", got.DidStrip, tt.wantStrip)
			}
		})
	}
}
