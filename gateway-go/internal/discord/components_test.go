package discord

import (
	"strings"
	"testing"
)

func TestCodeActionButtons(t *testing.T) {
	components := CodeActionButtons("discord:123456")
	if len(components) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(components))
	}
	row := components[0]
	if row.Type != ComponentActionRow {
		t.Errorf("expected ActionRow type %d, got %d", ComponentActionRow, row.Type)
	}
	if len(row.Components) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(row.Components))
	}

	// Verify button custom_ids.
	expectedPrefixes := []string{"test:", "commit:", "revert:"}
	for i, btn := range row.Components {
		if btn.Type != ComponentButton {
			t.Errorf("button %d: expected type %d, got %d", i, ComponentButton, btn.Type)
		}
		if !strings.HasPrefix(btn.CustomID, expectedPrefixes[i]) {
			t.Errorf("button %d: expected custom_id prefix %q, got %q", i, expectedPrefixes[i], btn.CustomID)
		}
		if !strings.Contains(btn.CustomID, "discord:123456") {
			t.Errorf("button %d: expected session key in custom_id, got %q", i, btn.CustomID)
		}
	}
}

func TestTestResultButtons(t *testing.T) {
	components := TestResultButtons("discord:789")
	if len(components) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(components))
	}
	if len(components[0].Components) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(components[0].Components))
	}
}

func TestParseButtonAction(t *testing.T) {
	tests := []struct {
		customID        string
		expectedAction  string
		expectedSession string
	}{
		{"test:discord:123456", "test", "discord:123456"},
		{"commit:discord:789", "commit", "discord:789"},
		{"revert:discord:abc", "revert", "discord:abc"},
		{"fix:discord:xyz", "fix", "discord:xyz"},
		{"nocolon", "nocolon", ""},
	}

	for _, tt := range tests {
		action, session := ParseButtonAction(tt.customID)
		if action != tt.expectedAction {
			t.Errorf("ParseButtonAction(%q): action = %q, want %q", tt.customID, action, tt.expectedAction)
		}
		if session != tt.expectedSession {
			t.Errorf("ParseButtonAction(%q): session = %q, want %q", tt.customID, session, tt.expectedSession)
		}
	}
}

func TestConfirmButtons(t *testing.T) {
	components := ConfirmButtons("discord:123", "push")
	if len(components) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(components))
	}
	row := components[0]
	if len(row.Components) != 2 {
		t.Fatalf("expected 2 buttons (confirm/cancel), got %d", len(row.Components))
	}
	if row.Components[0].Style != ButtonSuccess {
		t.Errorf("confirm button should be Success style")
	}
	if row.Components[1].Style != ButtonDanger {
		t.Errorf("cancel button should be Danger style")
	}
}
