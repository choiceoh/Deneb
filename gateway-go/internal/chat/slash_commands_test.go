package chat

import "testing"

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		wantCmd string
		wantArg string
	}{
		{"/reset", false, "reset", ""},
		{"/kill", false, "kill", ""},
		{"/stop", false, "kill", ""},
		{"/cancel", false, "kill", ""},
		{"/status", false, "status", ""},
		{"/model claude-opus-4-6", false, "model", "claude-opus-4-6"},
		{"/model", false, "model", ""},
		{"/think", false, "think", ""},
		{"/unknown", true, "", ""},
		{"hello", true, "", ""},
		{"", true, "", ""},
		{" /reset ", false, "reset", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseSlashCommand(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseSlashCommand(%q) = %+v, want nil", tt.input, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseSlashCommand(%q) = nil, want command", tt.input)
			}
			if got.Command != tt.wantCmd {
				t.Errorf("ParseSlashCommand(%q).Command = %q, want %q", tt.input, got.Command, tt.wantCmd)
			}
			if got.Args != tt.wantArg {
				t.Errorf("ParseSlashCommand(%q).Args = %q, want %q", tt.input, got.Args, tt.wantArg)
			}
		})
	}
}
