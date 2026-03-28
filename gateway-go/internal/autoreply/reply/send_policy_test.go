package reply

import "testing"

func TestNormalizeSendPolicy(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   SendPolicy
		wantOk bool
	}{
		// "on" aliases
		{"on", "on", SendPolicyOn, true},
		{"true", "true", SendPolicyOn, true},
		{"yes", "yes", SendPolicyOn, true},
		{"1", "1", SendPolicyOn, true},
		{"enable", "enable", SendPolicyOn, true},
		{"enabled", "enabled", SendPolicyOn, true},
		{"on_uppercase", "ON", SendPolicyOn, true},
		{"true_mixed_case", "True", SendPolicyOn, true},
		{"on_with_spaces", "  on  ", SendPolicyOn, true},

		// "off" aliases
		{"off", "off", SendPolicyOff, true},
		{"false", "false", SendPolicyOff, true},
		{"no", "no", SendPolicyOff, true},
		{"0", "0", SendPolicyOff, true},
		{"disable", "disable", SendPolicyOff, true},
		{"disabled", "disabled", SendPolicyOff, true},
		{"off_uppercase", "OFF", SendPolicyOff, true},

		// "inherit" aliases
		{"inherit", "inherit", SendPolicyInherit, true},
		{"default", "default", SendPolicyInherit, true},
		{"empty_string", "", SendPolicyInherit, true},

		// invalid
		{"unknown", "maybe", "", false},
		{"random", "xyz", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeSendPolicy(tt.raw)
			if ok != tt.wantOk {
				t.Errorf("NormalizeSendPolicy(%q) ok = %v, want %v", tt.raw, ok, tt.wantOk)
			}
			if got != tt.want {
				t.Errorf("NormalizeSendPolicy(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsSendAllowed(t *testing.T) {
	tests := []struct {
		name         string
		policy       SendPolicy
		parentPolicy SendPolicy
		want         bool
	}{
		{"off_always_false", SendPolicyOff, SendPolicyOn, false},
		{"off_ignores_parent", SendPolicyOff, SendPolicyOff, false},
		{"on_always_true", SendPolicyOn, SendPolicyOff, true},
		{"on_ignores_parent", SendPolicyOn, SendPolicyOn, true},
		{"inherit_parent_on", SendPolicyInherit, SendPolicyOn, true},
		{"inherit_parent_off", SendPolicyInherit, SendPolicyOff, false},
		{"inherit_parent_inherit", SendPolicyInherit, SendPolicyInherit, true},
		{"empty_parent_on", "", SendPolicyOn, true},
		{"empty_parent_off", "", SendPolicyOff, false},
		{"unknown_defaults_true", SendPolicy("unknown"), SendPolicyOff, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSendAllowed(tt.policy, tt.parentPolicy)
			if got != tt.want {
				t.Errorf("IsSendAllowed(%q, %q) = %v, want %v", tt.policy, tt.parentPolicy, got, tt.want)
			}
		})
	}
}
