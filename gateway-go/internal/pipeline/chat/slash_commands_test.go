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
		{"/help", false, "help", ""},
		{"/rollback list", false, "rollback", "list"},
		{"/update 확인", false, "update", "확인"},
		{"/restart", false, "restart", ""},
		{"/unknown", true, "", ""},
		{"hello", true, "", ""},
		{"", true, "", ""},
		{" /reset ", false, "reset", ""},
		{"/reset@MyBot", false, "reset", ""},
		{"/status@mybot", false, "status", ""},
		// Removed user commands must fall through to the LLM as plain text.
		{"/model claude-opus-4-6", true, "", ""},
		{"/think", true, "", ""},
		{"/pin 거래처 X", true, "", ""},
		{"/pins", true, "", ""},
		{"/unpin 1", true, "", ""},
		{"/mode", true, "", ""},
		{"/mail", true, "", ""},
		{"/insights", true, "", ""},
		{"/use-forum", true, "", ""},
		{"/models", true, "", ""},
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
