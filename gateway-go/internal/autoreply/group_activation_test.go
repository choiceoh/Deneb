package autoreply

import "testing"

func TestNormalizeGroupActivation(t *testing.T) {
	tests := []struct {
		raw    string
		want   GroupActivationMode
		wantOk bool
	}{
		{"mention", ActivationMention, true},
		{"MENTION", ActivationMention, true},
		{"always", ActivationAlways, true},
		{"ALWAYS", ActivationAlways, true},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, ok := NormalizeGroupActivation(tt.raw)
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("NormalizeGroupActivation(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func TestParseActivationCommand(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantCmd  bool
		wantMode GroupActivationMode
	}{
		{"with mode", "/activation mention", true, ActivationMention},
		{"without mode", "/activation", true, ""},
		{"always mode", "/activation always", true, ActivationAlways},
		{"not activation", "/status", false, ""},
		{"empty", "", false, ""},
		{"invalid mode", "/activation foo", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasCmd, mode := ParseActivationCommand(tt.raw, nil)
			if hasCmd != tt.wantCmd {
				t.Errorf("hasCommand = %v, want %v", hasCmd, tt.wantCmd)
			}
			if mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", mode, tt.wantMode)
			}
		})
	}
}
