package chat

import "testing"

func TestParseSlashCommand_Restart(t *testing.T) {
	tests := []struct {
		input   string
		wantArg string
	}{
		{"/restart", ""},
		{"/restart 확인", "확인"},
		{"/restart confirm", "confirm"},
		{"/재시작", ""},
		{"/재시작 확인", "확인"},
		{"/restart@DenebBot", ""},
		{"/restart@DenebBot 확인", "확인"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSlashCommand(tt.input)
			if got == nil {
				t.Fatalf("parseSlashCommand(%q) = nil, want command", tt.input)
			}
			if got.Command != "restart" {
				t.Errorf("parseSlashCommand(%q).Command = %q, want %q", tt.input, got.Command, "restart")
			}
			if !got.Handled {
				t.Errorf("parseSlashCommand(%q).Handled = false, want true", tt.input)
			}
			if got.Args != tt.wantArg {
				t.Errorf("parseSlashCommand(%q).Args = %q, want %q", tt.input, got.Args, tt.wantArg)
			}
		})
	}
}
