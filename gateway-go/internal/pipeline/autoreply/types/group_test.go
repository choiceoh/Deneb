package types

import "testing"

func TestNormalizeGroupActivation(t *testing.T) {
	tests := []struct {
		input  string
		want   GroupActivationMode
		wantOk bool
	}{
		{"mention", ActivationMention, true},
		{"always", ActivationAlways, true},
		{"MENTION", ActivationMention, true},
		{"Always", ActivationAlways, true},
		{"  mention  ", ActivationMention, true},
		{"", GroupActivationMode(""), false},
		{"never", GroupActivationMode(""), false},
		{"all", GroupActivationMode(""), false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeGroupActivation(tc.input)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
