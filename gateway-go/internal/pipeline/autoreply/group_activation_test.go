package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

func TestNormalizeGroupActivation(t *testing.T) {
	tests := []struct {
		raw    string
		want   types.GroupActivationMode
		wantOk bool
	}{
		{"mention", types.ActivationMention, true},
		{"MENTION", types.ActivationMention, true},
		{"always", types.ActivationAlways, true},
		{"ALWAYS", types.ActivationAlways, true},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, ok := types.NormalizeGroupActivation(tt.raw)
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("types.NormalizeGroupActivation(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func TestParseActivationCommand(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantCmd  bool
		wantMode types.GroupActivationMode
	}{
		{"with mode", "/activation mention", true, types.ActivationMention},
		{"without mode", "/activation", true, ""},
		{"always mode", "/activation always", true, types.ActivationAlways},
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
