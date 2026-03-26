package autoreply

import "testing"

func TestIsSilentReplyText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		token string
		want  bool
	}{
		{"exact match", "NO_REPLY", "", true},
		{"with whitespace", "  NO_REPLY  ", "", true},
		{"with newlines", "\n NO_REPLY \n", "", true},
		{"empty", "", "", false},
		{"substantive with token", "Hello NO_REPLY", "", false},
		{"partial", "NO", "", false},
		{"custom token", "HEARTBEAT_OK", "HEARTBEAT_OK", true},
		{"wrong token", "NO_REPLY", "HEARTBEAT_OK", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSilentReplyText(tt.text, tt.token)
			if got != tt.want {
				t.Errorf("IsSilentReplyText(%q, %q) = %v, want %v", tt.text, tt.token, got, tt.want)
			}
		})
	}
}

func TestStripSilentToken(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		token string
		want  string
	}{
		{"trailing token", "Hello NO_REPLY", "", "Hello"},
		{"only token", "NO_REPLY", "", ""},
		{"no token", "Hello world", "", "Hello world"},
		{"token in middle", "Hello NO_REPLY world", "", "Hello NO_REPLY world"},
		{"with asterisks", "Hello **NO_REPLY", "", "Hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSilentToken(tt.text, tt.token)
			if got != tt.want {
				t.Errorf("StripSilentToken(%q, %q) = %q, want %q", tt.text, tt.token, got, tt.want)
			}
		})
	}
}

func TestIsSilentReplyPrefixText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		token string
		want  bool
	}{
		{"bare NO", "NO", "", true},
		{"NO_ prefix", "NO_", "", true},
		{"NO_RE prefix", "NO_RE", "", true},
		{"full token", "NO_REPLY", "", true},
		{"lowercase no", "no", "", false},
		{"mixed case", "No", "", false},
		{"too short", "N", "", false},
		{"empty", "", "", false},
		{"unrelated uppercase", "HELLO", "", false},
		{"heartbeat prefix", "HE", "HEARTBEAT_OK", false},
		{"heartbeat with underscore", "HEARTBEAT_", "HEARTBEAT_OK", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSilentReplyPrefixText(tt.text, tt.token)
			if got != tt.want {
				t.Errorf("IsSilentReplyPrefixText(%q, %q) = %v, want %v", tt.text, tt.token, got, tt.want)
			}
		})
	}
}
