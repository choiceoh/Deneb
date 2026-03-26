package chat

import "testing"

func TestIsSilentReply(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"NO_REPLY", true},
		{"  NO_REPLY  ", true},
		{"\nNO_REPLY\n", true},
		{"NO_REPLY\n", true},
		{"Hello NO_REPLY", false},        // Substantive content before token
		{"NO_REPLY Hello", false},        // Content after token
		{"Thanks for the update.", false}, // Normal reply
		{"", false},
		{"NO", false},
		{"no_reply", false}, // Case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsSilentReply(tt.input)
			if got != tt.want {
				t.Errorf("IsSilentReply(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripSilentToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello NO_REPLY", "Hello"},
		{"Some text\nNO_REPLY", "Some text"},
		{"NO_REPLY", ""},
		{"  NO_REPLY  ", ""},
		{"Hello world", "Hello world"},
		{"Report done **NO_REPLY", "Report done"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := StripSilentToken(tt.input)
			if got != tt.want {
				t.Errorf("StripSilentToken(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
